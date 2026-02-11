package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"cyberpower/ups"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-fuego/fuego"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func init() {
	// Make the JSON response pretty
	fuego.SendJSON = func(w http.ResponseWriter, r *http.Request, ans any) error {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(ans)
	}
}

func main() {
	// Initialize Collector
	upsCollector = NewUPSCollector()

	// Create a custom registry to exclude default Go/Process metrics
	reg := prometheus.NewRegistry()
	reg.MustRegister(upsCollector)

	s := fuego.NewServer(
		fuego.WithAddr(":9999"),
		fuego.WithEngineOptions(
			fuego.WithOpenAPIConfig(fuego.OpenAPIConfig{
				PrettyFormatJSON: true,
				DisableLocalSave: true,
			}),
		),
	)

	// Set Open API info
	s.OpenAPI.Description().Info.Title = "CyberPower UPS Monitor"
	s.OpenAPI.Description().Info.Description = "Control and Monitor CyberPower UPS devices via USB HID"
	s.OpenAPI.Description().Servers = openapi3.Servers{
		{URL: "http://localhost:9999"},
	}

	// Register metrics endpoint using the custom registry
	fuego.GetStd(s, "/metrics", func(w http.ResponseWriter, r *http.Request) {
		promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	})

	// Device routes
	fuego.Get(s, "/ups", listDevices)
	fuego.Get(s, "/ups/{serial}", getDeviceStatus)
	fuego.Post(s, "/ups/{serial}/battery-test", runBatteryTest)
	fuego.Post(s, "/ups/{serial}/beeper", setBeeper)

	// TODO we need to get the hostname for someplace.
	fmt.Println("Server starting on http://0.0.0.0:9999")
	fmt.Println("Swagger UI available at http://0.0.0.0:9999/swagger")
	fmt.Println("Metrics available at http://0.0.0.0:9999/metrics")

	if err := s.Run(); err != nil {
		log.Fatal(err)
	}
}

// UPSCollector implements the prometheus.Collector interface
type UPSCollector struct {
	mu sync.Mutex

	// Descriptors
	inputVoltage   *prometheus.Desc
	outputVoltage  *prometheus.Desc
	batteryCharge  *prometheus.Desc
	batteryRuntime *prometheus.Desc
	loadWatts      *prometheus.Desc
	loadPercent    *prometheus.Desc
	status         *prometheus.Desc // 0=Normal, 1=Battery, 2=LowBattery
}

func NewUPSCollector() *UPSCollector {
	return &UPSCollector{
		inputVoltage: prometheus.NewDesc(
			"ups_input_voltage_volts",
			"Input (Utility) Voltage",
			[]string{"serial", "model"}, nil,
		),
		outputVoltage: prometheus.NewDesc(
			"ups_output_voltage_volts",
			"Output Voltage",
			[]string{"serial", "model"}, nil,
		),
		batteryCharge: prometheus.NewDesc(
			"ups_battery_charge_percent",
			"Battery Charge Percentage",
			[]string{"serial", "model"}, nil,
		),
		batteryRuntime: prometheus.NewDesc(
			"ups_battery_runtime_minutes",
			"Estimated Runtime Remaining",
			[]string{"serial", "model"}, nil,
		),
		loadWatts: prometheus.NewDesc(
			"ups_load_watts",
			"Load in Watts",
			[]string{"serial", "model"}, nil,
		),
		loadPercent: prometheus.NewDesc(
			"ups_load_percent",
			"Load Percentage",
			[]string{"serial", "model"}, nil,
		),
		status: prometheus.NewDesc(
			"ups_status_code",
			"UPS Status: 0=Normal, 1=On Battery, 2=Low Battery",
			[]string{"serial", "model"}, nil,
		),
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
	c.mu.Lock()
	defer c.mu.Unlock()

	devices, err := ups.List()
	if err != nil {
		log.Printf("Error scanning devices during collection: %v", err)
		return
	}

	for _, d := range devices {
		func(u *ups.UPS) {
			defer u.Close()

			props, err := u.GetProperties()
			if err != nil {
				log.Printf("Error reading properties: %v", err)
				return
			}

			status, err := u.GetStatus()
			if err != nil {
				log.Printf("Error reading status for %s: %v", props.SerialNumber, err)
				return
			}

			statusCode := 0.0
			if status.State == "On Battery" {
				statusCode = 1.0
			} else if status.State == "Low Battery" {
				statusCode = 2.0
			}

			labels := []string{props.SerialNumber, props.ModelName}

			ch <- prometheus.MustNewConstMetric(c.inputVoltage, prometheus.GaugeValue, float64(status.UtilityVoltage), labels...)
			ch <- prometheus.MustNewConstMetric(c.outputVoltage, prometheus.GaugeValue, float64(status.OutputVoltage), labels...)
			ch <- prometheus.MustNewConstMetric(c.batteryCharge, prometheus.GaugeValue, float64(status.BatteryCapacity), labels...)
			ch <- prometheus.MustNewConstMetric(c.batteryRuntime, prometheus.GaugeValue, float64(status.RemainingRuntime), labels...)
			ch <- prometheus.MustNewConstMetric(c.loadWatts, prometheus.GaugeValue, float64(status.Load), labels...)
			ch <- prometheus.MustNewConstMetric(c.loadPercent, prometheus.GaugeValue, float64(status.LoadPercentage), labels...)
			ch <- prometheus.MustNewConstMetric(c.status, prometheus.GaugeValue, statusCode, labels...)
		}(d)
	}
}

var upsCollector *UPSCollector

func listDevices(c fuego.ContextNoBody) ([]ups.Properties, error) {
	upsCollector.mu.Lock()
	defer upsCollector.mu.Unlock()

	devices, err := ups.List()
	if err != nil {
		return nil, fuego.BadRequestError{Err: err, Detail: "Failed to scan for devices"}
	}

	var results []ups.Properties
	for _, d := range devices {
		func(u *ups.UPS) {
			defer u.Close()
			props, err := u.GetProperties()
			if err == nil && props != nil {
				results = append(results, *props)
			}
		}(d)
	}
	return results, nil
}

type FullStatus struct {
	Properties ups.Properties `json:"properties"`
	Status     ups.Status     `json:"status"`
	BeeperCode int            `json:"beeper_code"`
}

func getDeviceStatus(c fuego.ContextNoBody) (FullStatus, error) {
	serial := c.PathParam("serial")

	upsCollector.mu.Lock()
	defer upsCollector.mu.Unlock()

	device, err := ups.Load(serial)
	if err != nil {
		return FullStatus{}, fuego.NotFoundError{Err: err, Detail: "UPS not found"}
	}
	defer device.Close()

	props, _ := device.GetProperties()
	status, _ := device.GetStatus()
	beeper, _ := device.GetBeeperStatus()

	return FullStatus{
		Properties: *props,
		Status:     *status,
		BeeperCode: beeper,
	}, nil
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

	upsCollector.mu.Lock()
	defer upsCollector.mu.Unlock()

	device, err := ups.Load(serial)
	if err != nil {
		return MessageResponse{}, fuego.NotFoundError{Err: err, Detail: "UPS not found"}
	}
	defer device.Close()

	switch body.Action {
	case "quick":
		err = device.StartQuickTest()
	case "deep":
		err = device.StartDeepTest()
	case "stop":
		err = device.StopTest()
	}

	if err != nil {
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

	upsCollector.mu.Lock()
	defer upsCollector.mu.Unlock()

	device, err := ups.Load(serial)
	if err != nil {
		return MessageResponse{}, fuego.NotFoundError{Err: err, Detail: "UPS not found"}
	}
	defer device.Close()

	if err := device.SetBeeper(body.Enable); err != nil {
		return MessageResponse{}, fuego.BadRequestError{Err: err, Detail: "Failed to set beeper"}
	}

	status := "disabled"
	if body.Enable {
		status = "enabled"
	}
	return MessageResponse{Message: fmt.Sprintf("Beeper %s for %s", status, serial)}, nil
}
