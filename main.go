package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"cyberpower/gateways"
	"cyberpower/ups"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-fuego/fuego"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func init() {
	// Pretty-print JSON responses for human readers.
	fuego.SendJSON = func(w http.ResponseWriter, r *http.Request, ans any) error {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(ans)
	}
}

// gateway is initialised in main and shared by every handler/collector.
var gateway *gateways.UPSGateway

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gateway = gateways.New(0) // 0 → DefaultPollInterval (15s)
	gateway.SetNotifier(gateways.NewNotifier())
	gateway.Start(ctx)

	reg := prometheus.NewRegistry()
	reg.MustRegister(NewUPSCollector(gateway))

	s := fuego.NewServer(
		fuego.WithAddr(":9999"),
		fuego.WithEngineOptions(
			fuego.WithOpenAPIConfig(fuego.OpenAPIConfig{
				PrettyFormatJSON: true,
				DisableLocalSave: true,
			}),
		),
	)

	s.OpenAPI.Description().Info.Title = "CyberPower UPS Monitor"
	s.OpenAPI.Description().Info.Description = "Control and monitor CyberPower UPS devices via USB HID"
	s.OpenAPI.Description().Servers = openapi3.Servers{
		{URL: "http://localhost:9999"},
	}

	// Metrics endpoint stays at the root because Prometheus scrape configs
	// across the fleet already point at /metrics.
	fuego.GetStd(s, "/metrics", func(w http.ResponseWriter, r *http.Request) {
		promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	})

	// JSON API now lives under /api/. The web UI (added in Phase 2) will
	// claim the root namespace.
	fuego.Get(s, "/api/ups", listDevices)
	fuego.Get(s, "/api/ups/{serial}", getDeviceStatus)
	fuego.Post(s, "/api/ups/{serial}/battery-test", runBatteryTest)
	fuego.Post(s, "/api/ups/{serial}/beeper", setBeeper)

	// Web dashboard, partials and form-POST endpoints.
	registerWebRoutes(s)

	fmt.Println("Server starting on http://0.0.0.0:9999")
	fmt.Println("Swagger UI available at http://0.0.0.0:9999/swagger")
	fmt.Println("Metrics available at http://0.0.0.0:9999/metrics")

	// Run the HTTP server in a goroutine so we can wait on ctx for a
	// signal and shut everything down cleanly. Without this, fuego's
	// blocking Run() prevents Ctrl-C from triggering shutdown.
	serverErr := make(chan error, 1)
	go func() {
		if err := s.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			log.Fatalf("server error: %v", err)
		}
	case <-ctx.Done():
		log.Println("shutdown signal received")
	}

	// Stop accepting new requests; give in-flight requests up to 10s.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}

	// ctx is already cancelled (or about to be on server-error exit);
	// stop() ensures the poller goroutine sees Done and exits, then we
	// block until it has fully drained its current device read.
	stop()
	gateway.Wait()
	log.Println("shutdown complete")
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

func listDevices(c fuego.ContextNoBody) ([]ups.Properties, error) {
	return gateway.List(), nil
}

func getDeviceStatus(c fuego.ContextNoBody) (gateways.CachedStatus, error) {
	serial := c.PathParam("serial")
	snap, err := gateway.Get(serial)
	if err != nil {
		return gateways.CachedStatus{}, fuego.NotFoundError{Err: err, Detail: "UPS not found"}
	}
	return snap, nil
}

type BatteryTestRequest struct {
	Action string `json:"action" validate:"required,oneof=quick deep stop"`
}

type MessageResponse struct {
	Message string `json:"message"`
}

func runBatteryTest(c fuego.ContextWithBody[BatteryTestRequest]) (MessageResponse, error) {
	serial := c.PathParam("serial")
	body, err := c.Body()
	if err != nil {
		return MessageResponse{}, err
	}

	if err := gateway.RunBatteryTest(serial, gateways.BatteryTestAction(body.Action)); err != nil {
		return MessageResponse{}, fuego.BadRequestError{Err: err, Detail: "Failed to execute test command"}
	}
	return MessageResponse{Message: fmt.Sprintf("Battery test command %q sent to %s", body.Action, serial)}, nil
}

type BeeperRequest struct {
	Enable bool `json:"enable"`
}

func setBeeper(c fuego.ContextWithBody[BeeperRequest]) (MessageResponse, error) {
	serial := c.PathParam("serial")
	body, err := c.Body()
	if err != nil {
		return MessageResponse{}, err
	}

	if err := gateway.SetBeeper(serial, body.Enable); err != nil {
		return MessageResponse{}, fuego.BadRequestError{Err: err, Detail: "Failed to set beeper"}
	}
	state := "disabled"
	if body.Enable {
		state = "enabled"
	}
	return MessageResponse{Message: fmt.Sprintf("Beeper %s for %s", state, serial)}, nil
}

// ---------------------------------------------------------------------------
// Prometheus collector — reads exclusively from the gateway cache.
// ---------------------------------------------------------------------------

type UPSCollector struct {
	gw *gateways.UPSGateway

	inputVoltage   *prometheus.Desc
	outputVoltage  *prometheus.Desc
	batteryCharge  *prometheus.Desc
	batteryRuntime *prometheus.Desc
	loadWatts      *prometheus.Desc
	loadPercent    *prometheus.Desc
	status         *prometheus.Desc
}

func NewUPSCollector(gw *gateways.UPSGateway) *UPSCollector {
	labels := []string{"serial", "model"}
	return &UPSCollector{
		gw:             gw,
		inputVoltage:   prometheus.NewDesc("ups_input_voltage_volts", "Input (Utility) Voltage", labels, nil),
		outputVoltage:  prometheus.NewDesc("ups_output_voltage_volts", "Output Voltage", labels, nil),
		batteryCharge:  prometheus.NewDesc("ups_battery_charge_percent", "Battery Charge Percentage", labels, nil),
		batteryRuntime: prometheus.NewDesc("ups_battery_runtime_minutes", "Estimated Runtime Remaining", labels, nil),
		loadWatts:      prometheus.NewDesc("ups_load_watts", "Load in Watts", labels, nil),
		loadPercent:    prometheus.NewDesc("ups_load_percent", "Load Percentage", labels, nil),
		status:         prometheus.NewDesc("ups_status_code", "UPS Status: 0=Normal, 1=On Battery, 2=Low Battery", labels, nil),
	}
}

func (c *UPSCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.inputVoltage
	ch <- c.outputVoltage
	ch <- c.batteryCharge
	ch <- c.batteryRuntime
	ch <- c.loadWatts
	ch <- c.loadPercent
	ch <- c.status
}

func (c *UPSCollector) Collect(ch chan<- prometheus.Metric) {
	for _, snap := range c.gw.Snapshots() {
		// Skip devices whose latest poll returned an error so we don't
		// emit zero-valued metrics that look like real readings.
		if snap.Err != "" {
			continue
		}

		statusCode := 0.0
		switch snap.Status.State {
		case "On Battery":
			statusCode = 1.0
		case "Low Battery":
			statusCode = 2.0
		}

		labels := []string{snap.Properties.SerialNumber, snap.Properties.ModelName}
		gauge := prometheus.GaugeValue

		ch <- prometheus.MustNewConstMetric(c.inputVoltage, gauge, float64(snap.Status.UtilityVoltage), labels...)
		ch <- prometheus.MustNewConstMetric(c.outputVoltage, gauge, float64(snap.Status.OutputVoltage), labels...)
		ch <- prometheus.MustNewConstMetric(c.batteryCharge, gauge, float64(snap.Status.BatteryCapacity), labels...)
		ch <- prometheus.MustNewConstMetric(c.batteryRuntime, gauge, float64(snap.Status.RemainingRuntime), labels...)
		ch <- prometheus.MustNewConstMetric(c.loadWatts, gauge, float64(snap.Status.Load), labels...)
		ch <- prometheus.MustNewConstMetric(c.loadPercent, gauge, float64(snap.Status.LoadPercentage), labels...)
		ch <- prometheus.MustNewConstMetric(c.status, gauge, statusCode, labels...)
	}
}
