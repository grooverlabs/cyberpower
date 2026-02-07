# CyberPower UPS Monitoring Tool (Go)

A lightweight, standalone Go tool for monitoring and controlling CyberPower UPS devices (specifically verified on the `CP1500PFCLCDa` model) via USB HID.

This tool provides real-time status (Voltage, Load, Battery), Beeper control, and Battery Test management without requiring the heavy `nut-server` daemon, although it pairs well with standard udev rules.

## Features

*   **Real-time Monitoring:** Reads Input/Output Voltage, Battery %, Runtime, Load (Watts & %), and Status.
*   **Beeper Control:** Enable or Disable the audible alarm (verified correct Report IDs).
*   **Battery Testing:** Trigger Quick or Deep self-tests and Stop them safely.
*   **Smart Status:** Detects "On Battery" state instantly via voltage readings even if firmware status bits are laggy.

## Installation

1.  **Clone the repository:**
    ```bash
    git clone <repository-url>
    cd cyberpower
    ```

2.  **Build the project:**
    ```bash
    make
    ```
    This will create the `ups-cli` tool and the `ups-monitor` service.

## Setup (Linux Permissions)

To access the USB device without `sudo`, add a udev rule:

1.  **Create the rule file:**
    ```bash
    echo 'KERNEL=="hidraw*", ATTRS{idVendor}=="0764", ATTRS{idProduct}=="0601", GROUP="plugdev", MODE="0660"' | sudo tee /etc/udev/rules.d/99-cyberpower.rules
    ```

2.  **Reload rules:**
    ```bash
    sudo udevadm control --reload-rules && sudo udevadm trigger
    ```

3.  **Add user to group:**
    ```bash
    sudo usermod -aG plugdev $USER
    ```
    *(Log out and back in for this to take effect)*

## Usage

### Monitor Status (Default)
Simply run the tool to see the status of all connected devices.
```bash
./ups-cli
```

**Example Output:**
```text
Found 1 UPS device(s).

--- UPS #1 ---
Properties:
  Model: CP1500PFCLCDa
  Firmware: N/A
  Rating: 120 V, 1000 W (1500 VA)
Status:
  State: Normal
  Power Source: Utility Power
  Utility Voltage: 122 V
  Output Voltage: 122 V
  Battery: 100%
  Load: 136W (13%)
  Runtime: 81 minutes
  Test Result: No test initiated
  Beeper: Enabled (Code: 2)
```

### Control Commands

**Disable Beeper:**
```bash
./ups-cli -beeper disable
```

**Enable Beeper:**
```bash
./ups-cli -beeper enable
```

**Run Battery Test:**
```bash
./ups-cli -test quick   # 10-second test
./ups-cli -test deep    # Runs until battery is low
./ups-cli -test stop    # Abort running test
```

## Technical Details (CP1500PFCLCDa)

This tool uses specific HID Report IDs verified against this model to ensure safety and accuracy:
*   **Beeper Control:** Report `0x0c` (Values: 1=Disable, 2=Enable)
*   **Test Control:** Report `0x14` (Values: 1=Quick, 2=Deep, 3=Abort)
*   **Load Power:** Report `0x19` (Active Power Watts)
*   **Load %:** Report `0x13`
