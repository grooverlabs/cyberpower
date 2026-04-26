package gateways

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Notifier sends SMS alerts via Triton when a UPS transitions between
// utility power and battery. Construction reads three env variables;
// if any are unset, NewNotifier returns nil and the gateway treats
// notifications as disabled.
//
//	CYBERPOWER_TRITON_URL    e.g. https://triton.example.com
//	CYBERPOWER_TRITON_TOKEN  Bearer token (tri_…)
//	CYBERPOWER_SMS_TO        comma-separated +E.164 numbers
type Notifier struct {
	baseURL    string
	token      string
	recipients []string

	httpClient *http.Client

	// Per-serial cooldown timestamps. If now < lastSent[serial]+cooldown
	// the event is dropped, suppressing flapping power.
	cooldown time.Duration
	mu       sync.Mutex
	lastSent map[string]time.Time
}

// PowerEvent describes a transition between two PowerSupplyBy values
// for one UPS. Only Utility ↔ Battery transitions are forwarded.
type PowerEvent struct {
	Serial    string
	Model     string
	OldState  string
	NewState  string
	BatteryPct int
	RuntimeMin int
}

// NewNotifier builds a Notifier from environment variables. Returns nil
// (and logs why) when any required variable is missing.
func NewNotifier() *Notifier {
	url := strings.TrimRight(os.Getenv("CYBERPOWER_TRITON_URL"), "/")
	token := os.Getenv("CYBERPOWER_TRITON_TOKEN")
	to := strings.TrimSpace(os.Getenv("CYBERPOWER_SMS_TO"))
	if url == "" || token == "" || to == "" {
		log.Printf("notifier: SMS alerts disabled (set CYBERPOWER_TRITON_URL, CYBERPOWER_TRITON_TOKEN, CYBERPOWER_SMS_TO to enable)")
		return nil
	}
	var recipients []string
	for _, n := range strings.Split(to, ",") {
		if t := strings.TrimSpace(n); t != "" {
			recipients = append(recipients, t)
		}
	}
	if len(recipients) == 0 {
		log.Printf("notifier: SMS alerts disabled (CYBERPOWER_SMS_TO had no usable phone numbers)")
		return nil
	}
	log.Printf("notifier: SMS alerts enabled to %d recipient(s) via %s", len(recipients), url)
	return &Notifier{
		baseURL:    url,
		token:      token,
		recipients: recipients,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		cooldown:   30 * time.Second,
		lastSent:   make(map[string]time.Time),
	}
}

// Notify dispatches an SMS for one transition. Safe to call from the
// poller goroutine; HTTP work is done synchronously but bounded by the
// 10 s client timeout. Errors are logged; callers ignore the return.
func (n *Notifier) Notify(ctx context.Context, ev PowerEvent) {
	if n == nil {
		return
	}
	if !n.allow(ev.Serial) {
		log.Printf("notifier: %s suppressed (cooldown)", ev.Serial)
		return
	}

	body := messageFor(ev)
	idem := fmt.Sprintf("cyberpower-%s-%s-%d", ev.Serial, stateTag(ev.NewState), time.Now().Unix())

	req := buildRequest(idem, body, n.recipients, ev)
	payload, err := json.Marshal(req)
	if err != nil {
		log.Printf("notifier: marshal: %v", err)
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+"/notifications", bytes.NewReader(payload))
	if err != nil {
		log.Printf("notifier: build request: %v", err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+n.token)

	resp, err := n.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("notifier: post: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("notifier: triton returned %d for %s", resp.StatusCode, ev.Serial)
		return
	}
	log.Printf("notifier: alert sent for %s (%s → %s)", ev.Serial, ev.OldState, ev.NewState)
}

// allow returns true and records the timestamp if the cooldown for serial
// has elapsed; false if the most recent send was too recent.
func (n *Notifier) allow(serial string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	if last, ok := n.lastSent[serial]; ok && now.Sub(last) < n.cooldown {
		return false
	}
	n.lastSent[serial] = now
	return true
}

// messageFor renders the SMS body. Keep it short — many carriers split
// at 160 chars and we want a single segment.
func messageFor(ev PowerEvent) string {
	switch {
	case isBattery(ev.NewState):
		return fmt.Sprintf("[CyberPower] %s (%s) on battery — %d%%, ~%d min runtime",
			ev.Model, ev.Serial, ev.BatteryPct, ev.RuntimeMin)
	case isUtility(ev.NewState):
		return fmt.Sprintf("[CyberPower] %s (%s) power restored", ev.Model, ev.Serial)
	default:
		return fmt.Sprintf("[CyberPower] %s (%s) state: %s", ev.Model, ev.Serial, ev.NewState)
	}
}

// stateTag is the short token embedded in the idempotency key.
func stateTag(state string) string {
	if isBattery(state) {
		return "battery"
	}
	if isUtility(state) {
		return "utility"
	}
	return "other"
}

// IsAlertable reports whether a transition between the two PowerSupplyBy
// values is one we'd actually SMS about. Drives the gateway's logic so
// the notifier itself stays dumb.
func IsAlertable(oldState, newState string) bool {
	if oldState == "" || newState == "" || oldState == newState {
		return false
	}
	return (isUtility(oldState) && isBattery(newState)) ||
		(isBattery(oldState) && isUtility(newState))
}

func isBattery(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "Battery") }
func isUtility(s string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "utility")
}

// buildRequest is broken out so tests can assert the exact wire shape
// without re-implementing JSON construction.
func buildRequest(idempotencyKey, body string, to []string, ev PowerEvent) map[string]any {
	recipients := make([]map[string]any, 0, len(to))
	for _, num := range to {
		recipients = append(recipients, map[string]any{
			"channel": "sms",
			"to":      num,
			"payload": map[string]any{"body": body},
		})
	}
	return map[string]any{
		"kind":            "sms",
		"idempotency_key": idempotencyKey,
		"recipients":      recipients,
		"metadata": map[string]any{
			"serial":     ev.Serial,
			"model":      ev.Model,
			"old_state":  ev.OldState,
			"new_state":  ev.NewState,
		},
	}
}
