package ups

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"rafaelmartins.com/p/usbhid"
)

const (
	// cyberPowerVendorID is the USB Vendor ID for CyberPower Systems.
	cyberPowerVendorID uint16 = 0x0764
	// cp1500ProductID is the USB Product ID for the CP1500PFCLCD model.
	cp1500ProductID uint16 = 0x0601
)

// Properties represents the static information about the UPS.
type Properties struct {
	ModelName      string
	FirmwareNumber string
	RatingVoltage  string
	RatingPower    string
	NominalPowerVA int
	VendorID       string
	ProductID      string
	BatteryType    string
}

// Status represents the current state of the UPS.
type Status struct {
	State            string
	PowerSupplyBy    string
	UtilityVoltage   int
	OutputVoltage    int
	BatteryCapacity  int
	RemainingRuntime int
	Load             int
	LoadPercentage   int // This might be derived from Load and RatingPower
	InputFrequency   float64
	Temperature      float64
	LineInteraction  string
	TestResult       string
	LastPowerEvent   string
}

// UPS provides methods to interact with a single CyberPower UPS device.
type UPS struct {
	device           *usbhid.Device
	ratingPowerWatts int
}

// List will find and return all connected CyberPower UPS devices that the
// program has permission to read.
func List() ([]*UPS, error) {
	filter := func(d *usbhid.Device) bool {
		// Only accept the specific model (CP1500PFCLCDa) that we have verified.
		// Other models may use different HID Report IDs, so using them with
		// this hardcoded protocol implementation could be dangerous.
		return d.VendorId() == cyberPowerVendorID && d.ProductId() == cp1500ProductID
	}

	devices, err := usbhid.Enumerate(filter)
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate HID devices: %w", err)
	}

	var upsDevices []*UPS
	for _, dev := range devices {
		if err := dev.Open(true); err != nil {
			fmt.Printf("Warning: failed to open device %s: %v\n", dev, err)
			continue
		}
		upsDevices = append(upsDevices, &UPS{device: dev})
	}

	return upsDevices, nil
}

// Close releases the handle to the UPS device.
func (u *UPS) Close() {
	if u.device != nil && u.device.IsOpen() {
		u.device.Close()
	}
}

// GetFeatureReport reads a raw feature report from the device.
func (u *UPS) GetFeatureReport(reportID byte) ([]byte, error) {
	if u.device == nil || !u.device.IsOpen() {
		return nil, fmt.Errorf("device is not open")
	}
	return u.device.GetFeatureReport(reportID)
}

func decodeString(buf []byte) string {
	if len(buf) == 0 {
		return ""
	}
	// Try UTF-16LE first (common in HID)
	if len(buf) >= 2 && buf[1] == 0 {
		u16 := make([]uint16, len(buf)/2)
		for i := range u16 {
			u16[i] = binary.LittleEndian.Uint16(buf[i*2:])
		}
		return strings.Trim(string(utf16.Decode(u16)), "\x00 ")
	}
	// Fallback to ASCII
	return strings.Trim(string(buf), "\x00 ")
}

// GetProperties returns the static properties of the UPS.
func (u *UPS) GetProperties() (*Properties, error) {
	if u.device == nil || !u.device.IsOpen() {
		return nil, fmt.Errorf("device is not open")
	}

	var fw, ratingPower string
	var ratingVoltage, ratingPowerWatts, ratingPowerVA, nominalPowerVA int

	// Get Rating Voltage from feature report 0x0e
	buf, err := u.device.GetFeatureReport(0x0e)
	if err == nil && len(buf) > 0 {
		ratingVoltage = int(buf[0])
	}

	// Get Rating Power (Watts and VA) from feature report 0x18
	buf, err = u.device.GetFeatureReport(0x18)
	if err == nil && len(buf) >= 4 {
		// Data: e803dc05 -> 0x03e8 (1000W), 0x05dc (1500VA) - assuming little-endian
		ratingPowerWatts = int(binary.LittleEndian.Uint16(buf[0:2]))
		ratingPowerVA = int(binary.LittleEndian.Uint16(buf[2:4]))
		ratingPower = fmt.Sprintf("%d W (%d VA)", ratingPowerWatts, ratingPowerVA)
		nominalPowerVA = ratingPowerVA
		u.ratingPowerWatts = ratingPowerWatts
	}

	// Try to get Firmware Number from feature report 0x17 or 0x11
	buf, err = u.device.GetFeatureReport(0x17)
	if err == nil && len(buf) > 0 {
		fw = decodeString(buf)
	}
	if fw == "" {
		buf, err = u.device.GetFeatureReport(0x11)
		if err == nil && len(buf) > 0 {
			fw = decodeString(buf)
		}
	}
	if fw == "" {
		fw = "N/A"
	}

	return &Properties{
		ModelName:      u.device.Product(),
		VendorID:       fmt.Sprintf("0x%04x", u.device.VendorId()),
		ProductID:      fmt.Sprintf("0x%04x", u.device.ProductId()),
		FirmwareNumber: fw,
		RatingVoltage:  fmt.Sprintf("%d V", ratingVoltage),
		RatingPower:    ratingPower,
		NominalPowerVA: nominalPowerVA,
	}, nil
}

// GetStatus returns the current status of the UPS.
func (u *UPS) GetStatus() (*Status, error) {
	if u.device == nil || !u.device.IsOpen() {
		return nil, fmt.Errorf("device is not open")
	}

	var id byte
	var buf []byte
	var err error

	// Retry loop for reading the correct report
	timeout := time.After(5 * time.Second)
	for {
		c := make(chan struct{}, 1)
		go func() {
			id, buf, err = u.device.GetInputReport()
			c <- struct{}{}
		}()

		select {
		case <-c:
			if err != nil {
				return nil, fmt.Errorf("failed to get input report: %w", err)
			}
			if id == 0x08 {
				goto ParseReport
			}
			continue
		case <-timeout:
			return nil, fmt.Errorf("timed out waiting for input report 0x08")
		}
	}

ParseReport:
	if len(buf) < 5 {
		return nil, fmt.Errorf("input report 0x08 was too short (expected 5 bytes, got %d)", len(buf))
	}

	runtimeSeconds := binary.BigEndian.Uint16(buf[2:4])

	status := &Status{
		BatteryCapacity: int(buf[0]),
		RemainingRuntime: int(runtimeSeconds / 60), // Convert seconds to minutes
	}

	// Get Active Power (Watts) from feature report 0x19
	wattBuf, err := u.device.GetFeatureReport(0x19)
	if err == nil && len(wattBuf) >= 2 {
		status.Load = int(binary.LittleEndian.Uint16(wattBuf[0:2]))
	}

	// Parse status flags from buf[4]
	// 0x01: Utility Power Present
	// 0x02: Discharging (On Battery)
	// 0x04: Battery Low
	// 0x40: Charging
	statusFlag := int(buf[4])
	if statusFlag&0x02 != 0 {
		status.State = "On Battery"
		status.PowerSupplyBy = "Battery"
		if statusFlag&0x04 != 0 {
			status.State = "Low Battery"
		}
	} else {
		status.State = "Normal"
		status.PowerSupplyBy = "Utility Power"
		if statusFlag&0x40 != 0 {
			status.PowerSupplyBy = "Utility Power (Charging)"
		}
	}
	
	// Get Utility Voltage from feature report 0x0f
	utilVoltageBuf, err := u.device.GetFeatureReport(0x0f)
	if err == nil && len(utilVoltageBuf) > 0 {
		status.UtilityVoltage = int(utilVoltageBuf[0])
	}

	// Get Output Voltage from feature report 0x12
	outVoltageBuf, err := u.device.GetFeatureReport(0x12)
	if err == nil && len(outVoltageBuf) > 0 {
		status.OutputVoltage = int(outVoltageBuf[0])
	}

	// Get Load Percentage from feature report 0x13
	loadBuf, err := u.device.GetFeatureReport(0x13)
	if err == nil && len(loadBuf) > 0 {
		status.LoadPercentage = int(loadBuf[0])
	}

	// Override Status based on Voltages if logic suggests On Battery
	// (Fixes laggy status bits on some firmware revisions)
	if status.UtilityVoltage < 50 && status.OutputVoltage > 100 {
		status.State = "On Battery"
		status.PowerSupplyBy = "Battery"
	}

	// Get Test Result from feature report 0x14
	testBuf, err := u.device.GetFeatureReport(0x14)
	if err == nil && len(testBuf) > 0 {
		switch testBuf[0] {
		case 1:
			status.TestResult = "Done and passed"
		case 2:
			status.TestResult = "Done and warning"
		case 3:
			status.TestResult = "Done and error"
		case 4:
			status.TestResult = "Aborted"
		case 5:
			status.TestResult = "In progress"
		case 6:
			status.TestResult = "No test initiated"
		case 7:
			status.TestResult = "Test scheduled"
		default:
			status.TestResult = fmt.Sprintf("Unknown (%d)", testBuf[0])
		}
	}

	return status, nil
}

// SetBeeper controls the UPS audible alarm.
func (u *UPS) SetBeeper(enabled bool) error {
	if u.device == nil || !u.device.IsOpen() {
		return fmt.Errorf("device is not open")
	}
	// Correct Report ID for Beeper is 0x0c (from NUT debug)
	// 1 = disabled, 2 = enabled, 3 = muted
	val := byte(1) // Disable
	if enabled {
		val = byte(2) // Enable
	}
	return u.device.SetFeatureReport(0x0c, []byte{val})
}

// GetBeeperStatus returns the status of the UPS audible alarm.
// 1: Disabled, 2: Enabled, 3: Muted, Other: Unknown
func (u *UPS) GetBeeperStatus() (int, error) {
	if u.device == nil || !u.device.IsOpen() {
		return 0, fmt.Errorf("device is not open")
	}
	// Correct Report ID for Beeper is 0x0c
	buf, err := u.device.GetFeatureReport(0x0c)
	if err != nil {
		return 0, err
	}
	if len(buf) == 0 {
		return 0, fmt.Errorf("empty report")
	}
	return int(buf[0]), nil
}

func (u *UPS) SetLowBatteryThreshold(percent int) error {
	return fmt.Errorf("not implemented")
}

func (u *UPS) SetShutdownDelay(seconds int) error {
	return fmt.Errorf("not implemented")
}

func (u *UPS) SetStartupDelay(seconds int) error {
	return fmt.Errorf("not implemented")
}

// StartQuickTest triggers a quick self-test of the UPS battery.
func (u *UPS) StartQuickTest() error {
	if u.device == nil || !u.device.IsOpen() {
		return fmt.Errorf("device is not open")
	}
	// Correct Report ID for Test is 0x14 (from NUT debug)
	// Value 1 = Quick Test
	return u.device.SetFeatureReport(0x14, []byte{1})
}

// StartDeepTest triggers a deep self-test of the UPS battery.
func (u *UPS) StartDeepTest() error {
	if u.device == nil || !u.device.IsOpen() {
		return fmt.Errorf("device is not open")
	}
	// Value 2 = Deep Test
	return u.device.SetFeatureReport(0x14, []byte{2})
}

// StopTest stops any ongoing UPS self-test.
func (u *UPS) StopTest() error {
	if u.device == nil || !u.device.IsOpen() {
		return fmt.Errorf("device is not open")
	}
	// Value 3 = Abort Test
	return u.device.SetFeatureReport(0x14, []byte{3})
}

func (u *UPS) Shutdown() error {
	return fmt.Errorf("not implemented")
}

func (u *UPS) ShutdownAndStayOff() error {
	return fmt.Errorf("not implemented")
}

func (u *UPS) StopShutdown() error {
	return fmt.Errorf("not implemented")
}
