// Package gateways provides cached access to UPS devices.
//
// USB HID devices cannot tolerate concurrent reads. The UPS gateway runs a
// single poller goroutine that reads every detected device on a fixed
// interval and stores the result in an in-memory cache. All read paths
// (HTTP API, web UI, Prometheus collector) consume cache data — never the
// USB bus directly.
//
// Write operations (battery test, beeper) bypass the cache and access the
// device directly, serialised per-serial via a sync.Mutex map so they
// never overlap with the poller or with one another.
package gateways

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"cyberpower/ups"
)

// DefaultPollInterval matches the typical Prometheus scrape interval to
// keep total USB activity low while still giving the dashboard reasonably
// fresh data.
const DefaultPollInterval = 15 * time.Second

// CachedStatus is the snapshot of a single device produced by the poller.
type CachedStatus struct {
	Properties  ups.Properties `json:"properties"`
	Status      ups.Status     `json:"status"`
	BeeperCode  int            `json:"beeper_code"`
	LastUpdated time.Time      `json:"last_updated"`
	Err         string         `json:"error,omitempty"`
}

// Stale reports whether the snapshot is older than the supplied threshold.
func (c CachedStatus) Stale(maxAge time.Duration) bool {
	return time.Since(c.LastUpdated) > maxAge
}

// ErrNotFound is returned when a serial has never been seen by the poller.
var ErrNotFound = errors.New("ups not found")

// UPSGateway owns the device cache and serialises write operations.
type UPSGateway struct {
	interval time.Duration
	notifier *Notifier

	mu    sync.RWMutex
	cache map[string]CachedStatus

	// One mutex per serial. Acquired by the poller for each device read and
	// by every write op so reads and writes never overlap on the same
	// device. A separate sync.Mutex protects the map itself.
	deviceMuMu sync.Mutex
	deviceMu   map[string]*sync.Mutex

	// prevPowerBy stores the last observed PowerSupplyBy per serial so we
	// can detect Utility ↔ Battery transitions and forward them to the
	// notifier. Only mutated from the poller goroutine.
	prevPowerBy map[string]string

	stopOnce sync.Once
	stopped  chan struct{}
}

// New constructs a gateway with the supplied poll interval. Pass 0 to
// accept DefaultPollInterval.
func New(interval time.Duration) *UPSGateway {
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	return &UPSGateway{
		interval:    interval,
		cache:       make(map[string]CachedStatus),
		deviceMu:    make(map[string]*sync.Mutex),
		prevPowerBy: make(map[string]string),
		stopped:     make(chan struct{}),
	}
}

// SetNotifier attaches a notifier so the poller can fire SMS alerts on
// Utility ↔ Battery transitions. Pass nil (or never call) to disable.
func (g *UPSGateway) SetNotifier(n *Notifier) {
	g.notifier = n
}

// Notifier returns the attached notifier (or nil if SMS is disabled).
// Used by the web UI's "Test SMS" handler.
func (g *UPSGateway) Notifier() *Notifier {
	return g.notifier
}

// Start runs the first poll synchronously so the cache is non-empty before
// callers begin serving traffic, then launches the background poller. The
// poller exits when ctx is cancelled.
func (g *UPSGateway) Start(ctx context.Context) {
	g.pollOnce()
	go g.run(ctx)
}

func (g *UPSGateway) run(ctx context.Context) {
	defer close(g.stopped)
	t := time.NewTicker(g.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.pollOnce()
		}
	}
}

// Wait blocks until the poller goroutine has exited. Useful for tests and
// graceful shutdown.
func (g *UPSGateway) Wait() {
	<-g.stopped
}

// pollOnce enumerates all attached devices and refreshes their cache
// entries. Errors are stored on the snapshot so callers (UI especially)
// can surface them rather than seeing a silent gap.
func (g *UPSGateway) pollOnce() {
	devices, err := ups.List()
	if err != nil {
		log.Printf("gateway: list devices: %v", err)
		return
	}

	seen := make(map[string]struct{}, len(devices))

	for _, d := range devices {
		serial := g.refreshDevice(d)
		if serial != "" {
			seen[serial] = struct{}{}
		}
	}

	// Drop cache entries for devices that are no longer present so the UI
	// doesn't show ghosts. We deliberately keep them while present even on
	// transient errors.
	g.mu.Lock()
	for serial := range g.cache {
		if _, ok := seen[serial]; !ok {
			delete(g.cache, serial)
			delete(g.prevPowerBy, serial)
		}
	}
	g.mu.Unlock()
}

// refreshDevice reads a single device end-to-end and writes the result to
// the cache. It always closes the handle, even on error. Returns the
// serial if one could be read, otherwise "".
func (g *UPSGateway) refreshDevice(u *ups.UPS) string {
	defer u.Close()

	props, err := u.GetProperties()
	if err != nil || props == nil {
		log.Printf("gateway: read properties: %v", err)
		return ""
	}

	mu := g.lockFor(props.SerialNumber)
	mu.Lock()
	defer mu.Unlock()

	snap := CachedStatus{
		Properties:  *props,
		LastUpdated: time.Now(),
	}

	status, err := u.GetStatus()
	if err != nil {
		snap.Err = fmt.Sprintf("status: %v", err)
	} else if status != nil {
		snap.Status = *status
	}

	beeper, err := u.GetBeeperStatus()
	if err != nil {
		// Beeper read failures shouldn't poison the whole snapshot.
		log.Printf("gateway: beeper status %s: %v", props.SerialNumber, err)
	} else {
		snap.BeeperCode = beeper
	}

	g.mu.Lock()
	g.cache[props.SerialNumber] = snap
	g.mu.Unlock()

	g.maybeNotify(snap)

	return props.SerialNumber
}

// maybeNotify fires an SMS alert when a device's PowerSupplyBy changes
// between Utility and Battery. The very first observation of a serial is
// treated as a baseline (no alert) so a service restart while a UPS is
// already on battery doesn't blast a duplicate notification. Only called
// from the poller goroutine, so prevPowerBy needs no extra locking.
func (g *UPSGateway) maybeNotify(snap CachedStatus) {
	if snap.Err != "" {
		// Skip on a partial read — we don't know the real state.
		return
	}
	serial := snap.Properties.SerialNumber
	newState := snap.Status.PowerSupplyBy
	prev, seen := g.prevPowerBy[serial]
	g.prevPowerBy[serial] = newState
	if !seen {
		return
	}
	if !IsAlertable(prev, newState) {
		return
	}
	if g.notifier == nil {
		log.Printf("notifier: %s transition %s → %s (alerts disabled)", serial, prev, newState)
		return
	}
	g.notifier.Notify(context.Background(), PowerEvent{
		Serial:     serial,
		Model:      snap.Properties.ModelName,
		OldState:   prev,
		NewState:   newState,
		BatteryPct: snap.Status.BatteryCapacity,
		RuntimeMin: snap.Status.RemainingRuntime,
	})
}

// lockFor returns the per-serial mutex, creating one on first use.
func (g *UPSGateway) lockFor(serial string) *sync.Mutex {
	g.deviceMuMu.Lock()
	defer g.deviceMuMu.Unlock()
	if m, ok := g.deviceMu[serial]; ok {
		return m
	}
	m := &sync.Mutex{}
	g.deviceMu[serial] = m
	return m
}

// List returns a snapshot of every cached device's properties.
func (g *UPSGateway) List() []ups.Properties {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]ups.Properties, 0, len(g.cache))
	for _, c := range g.cache {
		out = append(out, c.Properties)
	}
	return out
}

// Snapshots returns the full cache, sorted by serial for stable rendering.
func (g *UPSGateway) Snapshots() []CachedStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]CachedStatus, 0, len(g.cache))
	for _, c := range g.cache {
		out = append(out, c)
	}
	return out
}

// Get returns the cached snapshot for one serial.
func (g *UPSGateway) Get(serial string) (CachedStatus, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c, ok := g.cache[serial]
	if !ok {
		return CachedStatus{}, ErrNotFound
	}
	return c, nil
}

// BatteryTestAction is one of the test commands accepted by RunBatteryTest.
type BatteryTestAction string

const (
	BatteryTestQuick BatteryTestAction = "quick"
	BatteryTestDeep  BatteryTestAction = "deep"
	BatteryTestStop  BatteryTestAction = "stop"
)

// RunBatteryTest dispatches a battery test command directly to the device,
// holding the per-serial lock so it cannot race with the poller.
func (g *UPSGateway) RunBatteryTest(serial string, action BatteryTestAction) error {
	mu := g.lockFor(serial)
	mu.Lock()
	defer mu.Unlock()

	device, err := ups.Load(serial)
	if err != nil {
		return fmt.Errorf("load %s: %w", serial, err)
	}
	defer device.Close()

	switch action {
	case BatteryTestQuick:
		return device.StartQuickTest()
	case BatteryTestDeep:
		return device.StartDeepTest()
	case BatteryTestStop:
		return device.StopTest()
	default:
		return fmt.Errorf("unknown battery test action %q", action)
	}
}

// SetBeeper enables or disables the audible alarm on the named device.
func (g *UPSGateway) SetBeeper(serial string, enable bool) error {
	mu := g.lockFor(serial)
	mu.Lock()
	defer mu.Unlock()

	device, err := ups.Load(serial)
	if err != nil {
		return fmt.Errorf("load %s: %w", serial, err)
	}
	defer device.Close()

	return device.SetBeeper(enable)
}
