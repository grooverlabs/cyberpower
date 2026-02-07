package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"cyberpower/ups"
)

func main() {
	beeperPtr := flag.String("beeper", "", "Set beeper status: 'enable' or 'disable'")
	testPtr := flag.String("test", "", "Run battery test: 'quick', 'deep', or 'stop'")
	dumpPtr := flag.Bool("dump", false, "Dump all feature reports")
	flag.Parse()

	// To use the ups package, we first need to get a list of connected devices.
	devices, err := ups.List()
	if err != nil {
		log.Fatalf("Error listing UPS devices: %v", err)
	}

	if len(devices) == 0 {
		fmt.Println("No CyberPower UPS devices found.")
		return
	}

	fmt.Printf("Found %d UPS device(s).\n\n", len(devices))

	// Now you can iterate over the devices and interact with them.
	for i, device := range devices {
		func(i int, device *ups.UPS) {
			// It's important to close the device when you're done with it.
			defer device.Close()

			fmt.Printf("--- UPS #%d ---\n", i+1)

			// Handle flags if present
			if *dumpPtr {
				fmt.Println("Dumping Feature Reports (0x01 - 0x40):")
				for id := 0x01; id <= 0x40; id++ {
					buf, err := device.GetFeatureReport(byte(id))
					if err == nil && len(buf) > 0 {
						// Filter out empty/null reports to reduce noise if needed, 
						// but seeing them is useful.
						fmt.Printf("  Report 0x%02x: % x | %q\n", id, buf, buf)
					}
				}
				fmt.Println()
			}

			if *beeperPtr != "" {
				enable := strings.ToLower(*beeperPtr) == "enable"
				fmt.Printf("Setting beeper to %v...\n", enable)
				if err := device.SetBeeper(enable); err != nil {
					log.Printf("Error setting beeper: %v\n", err)
				} else {
					fmt.Println("Beeper set successfully.")
				}
			}

			if *testPtr != "" {
				var err error
				switch strings.ToLower(*testPtr) {
				case "quick":
					fmt.Println("Starting quick test...")
					err = device.StartQuickTest()
				case "deep":
					fmt.Println("Starting deep test...")
					err = device.StartDeepTest()
				case "stop":
					fmt.Println("Stopping test...")
					err = device.StopTest()
				default:
					fmt.Printf("Unknown test command: %s\n", *testPtr)
				}
				if err != nil {
					log.Printf("Error running test command: %v\n", err)
				} else {
					fmt.Println("Test command sent successfully.")
				}
			}

			// Get and print the device properties.
			properties, err := device.GetProperties()
			if err != nil {
				log.Printf("Could not get properties for device #%d: %v", i+1, err)
			} else {
										fmt.Println("Properties:")
										fmt.Printf("  Model: %s\n", properties.ModelName)
										fmt.Printf("  Firmware: %s\n", properties.FirmwareNumber)
										fmt.Printf("  Rating: %s, %s\n", properties.RatingVoltage, properties.RatingPower)
									}				
		// Get and print the device status.
		status, err := device.GetStatus()
			if err != nil {
				log.Printf("Could not get status for device #%d: %v", i+1, err)
			} else {
				fmt.Println("Status:")
				fmt.Printf("  State: %s\n", status.State)
				fmt.Printf("  Power Source: %s\n", status.PowerSupplyBy)
				fmt.Printf("  Utility Voltage: %d V\n", status.UtilityVoltage)
							fmt.Printf("  Output Voltage: %d V\n", status.OutputVoltage)
							fmt.Printf("  Battery: %d%%\n", status.BatteryCapacity)
							fmt.Printf("  Load: %dW (%d%%)\n", status.Load, status.LoadPercentage)
													fmt.Printf("  Runtime: %d minutes\n", status.RemainingRuntime)
													fmt.Printf("  Test Result: %s\n", status.TestResult)
												}								// Check beeper status
							
								if beeperVal, err := device.GetBeeperStatus(); err == nil {
							
									statusStr := "Unknown"
							
									switch beeperVal {
							
									case 1:
							
										statusStr = "Disabled"
							
									case 2:
							
										statusStr = "Enabled"
							
									case 3:
							
										statusStr = "Muted"
							
									}
							
									fmt.Printf("  Beeper: %s (Code: %d)\n", statusStr, beeperVal)
							
								} else {
							
									fmt.Printf("  Beeper: Error (%v)\n", err)
							
								}
							
						
							
								fmt.Println()
							
							}(i, device)
	}
}