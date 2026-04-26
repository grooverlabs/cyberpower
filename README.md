# CyberPower UPS Monitoring Tool (Go)

A lightweight, standalone Go tool for monitoring and controlling CyberPower UPS devices (specifically verified on the `CP1500PFCLCDa` model) via USB HID.

This tool provides real-time status (Voltage, Load, Battery), Beeper control, and Battery Test management without requiring the heavy `nut-server` daemon, although it pairs well with standard udev rules.

## Features

*   **Real-time Monitoring:** Reads Input/Output Voltage, Battery %, Runtime, Load (Watts & %), and Status.
*   **Beeper Control:** Enable or Disable the audible alarm (verified correct Report IDs).
*   **Battery Testing:** Trigger Quick or Deep self-tests and Stop them safely.
*   **Smart Status:** Detects "On Battery" state instantly via voltage readings even if firmware status bits are laggy.

## Installation

### From Source
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

### As a Debian Package
The project includes an `nfpm` configuration to build a `.deb` package that automatically handles udev rules, user creation, and systemd service setup.

1.  **Build the package:**
    ```bash
    make deb
    ```

2.  **Install the package:**
    ```bash
    sudo dpkg -i cyberpower-ups_*.deb
    ```

3.  **Remove the package:**
    ```bash
    sudo apt remove cyberpower-ups
    # or
    sudo dpkg -r cyberpower-ups
    ```

## Setup (Manual)
*Note: This is only required if you are not using the Debian package.*

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

## Monitoring with Prometheus & Grafana

The `ups-monitor` service exports standard Prometheus metrics on port `9999` at the `/metrics` endpoint.

### Prometheus Configuration
To scrape metrics, add the following to your `prometheus.yml`:
```yaml
scrape_configs:
  - job_name: 'cyberpower-ups'
    static_configs:
      - targets: ['<your-ups-host>:9999']
```
A sample configuration is provided in `monitoring/prometheus/prometheus.yml`.

### Alerting

#### Built-in SMS alerts (via Triton)

The monitor can send SMS notifications directly when a UPS transitions
between utility power and battery, using a Triton (`go/src/triton`) API
key. Configure with three environment variables:

| Variable                  | Description                                            |
|---------------------------|--------------------------------------------------------|
| `CYBERPOWER_TRITON_URL`   | Base URL of the Triton instance (e.g. `https://triton.example.com`) |
| `CYBERPOWER_TRITON_TOKEN` | Bearer token issued by Triton (`tri_…`)                |
| `CYBERPOWER_SMS_TO`       | Comma-separated `+E.164` recipients (e.g. `+15551234567,+15555550199`) |

If any of these is unset the notifier is disabled silently and the rest
of the service runs normally. Behavior:

* Fires on `Utility ↔ Battery` transitions only (low-battery alerts go
  through Prometheus below).
* 30-second per-serial cooldown suppresses flapping power.
* No retries — the service logs and continues if Triton is unreachable.
* The first poll for each device is treated as a baseline (no alert)
  so a service restart doesn't double-send.

For the systemd unit, drop the variables in `/etc/cyberpower/env` and
reference it via `EnvironmentFile=`.

#### Prometheus alerts
You can configure Prometheus to alert you when utility power is lost. A sample alert rule file is provided in `monitoring/prometheus/alerts.yml`.

To use it, add the following to your `prometheus.yml`:
```yaml
rule_files:
  - "alerts.yml"
```

The provided alerts cover:
*   **UPSOnBattery**: Triggers when `ups_status_code > 0` (Utility power lost).
*   **UPSLowBattery**: Triggers when `ups_status_code == 2` (Critical battery level).

### Grafana Dashboard
A pre-configured Grafana dashboard is available in `monitoring/grafana/dashboard.json`.
1.  Open Grafana.
2.  Go to **Dashboards** -> **Import**.
3.  Upload the `dashboard.json` file or paste its contents.
4.  Select your Prometheus datasource.

The dashboard includes:
*   **Battery Charge Gauge** (with color thresholds).
*   **Load Gauge** (Percentage).
*   **Voltage Levels Graph** (Input vs. Output).
*   **Load History** (Watts).
*   **Status Stat** (Normal, On Battery, Low Battery).

## Technical Details (CP1500PFCLCDa)

This tool uses specific HID Report IDs verified against this model to ensure safety and accuracy:
*   **Beeper Control:** Report `0x0c` (Values: 1=Disable, 2=Enable)
*   **Test Control:** Report `0x14` (Values: 1=Quick, 2=Deep, 3=Abort)
*   **Load Power:** Report `0x19` (Active Power Watts)
*   **Load %:** Report `0x13`

## Power Control & Shutdown

This library intentionally **does not** implement features to turn off the UPS outlets (Load Shedding) or initiate a device shutdown (`Shutdown()`, `ShutdownAndStayOff()`, etc.).

While the HID Report IDs for these features on the CP1500PFCLCDa are identified in other projects (typically Report `0x15` for `DelayBeforeShutdown`), we have decided not to include them for the following reasons:

1.  **Risk of Accidental Power Loss:** Triggering these commands physically cuts power to all devices connected to the "Battery + Surge" outlets. A bug, network error, or accidental API call could result in immediate data loss or hardware damage for connected systems.
2.  **Safety First:** Unlike beeper control or battery tests, which are non-destructive, power-cycling a UPS remotely is a high-stakes operation. This is best handled by established, hardened tools like Network UPS Tools (NUT) if such functionality is required.
3.  **Hardware Dependency:** Power-off byte sequences are highly specific to firmware revisions. Using the wrong values can lead to "zombie" states where the UPS remains off even after utility power returns.
