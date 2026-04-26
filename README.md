# CyberPower UPS Monitoring Tool (Go)

A lightweight Go tool for monitoring and controlling CyberPower UPS
devices (verified on `CP1500PFCLCDa`) over USB HID. Ships two binaries:

* **`ups-cli`** — one-shot CLI for status, beeper, and battery-test commands.
* **`ups-monitor`** — long-running service exposing a web dashboard,
  JSON REST API, Prometheus metrics, and optional SMS alerts.

It does not require `nut-server`; it talks directly to the device's HID
interface via udev rules.

## Features

* **Real-time monitoring** of input/output voltage, battery %, runtime,
  load (W & %), and overall state.
* **Web dashboard** (`http://host:9999/`) — auto-refreshing list of all
  attached UPS with an at-a-glance Power column, plus per-device detail
  pages with battery-test and beeper controls (HTMX + Tailwind, no JS
  build step required).
* **REST API** under `/api/` with OpenAPI docs at `/swagger`.
* **Prometheus metrics** at `/metrics`, ready to scrape.
* **SMS alerts** on `Utility ↔ Battery` transitions via Triton (optional;
  see [Alerting](#alerting)).
* **Multi-device** — every attached UPS is enumerated and polled.
* **Safe USB access** — a single shared poller serialises HID reads and
  writes per-serial so the dashboard, API, and Prometheus can never race
  on the bus.
* **Beeper control** and **battery testing** (Quick / Deep / Stop).

## Architecture

```
                   ┌────────────────────────────┐
USB HID  ─────────►│  gateways.UPSGateway       │
(per device)       │   • single 15 s poller     │
                   │   • per-serial mutex map   │
                   │   • in-memory cache        │
                   └─────────┬──────────────────┘
                             │   snapshots
        ┌────────────────────┼─────────────────────┐
        ▼                    ▼                     ▼
   web dashboard       JSON /api/ups         /metrics scrape
   (templ + HTMX)      (fuego + Swagger)     (Prometheus)
                             │
                             ▼  on transition
                       gateways.Notifier ── POST → Triton (SMS)
```

All read paths consume cache snapshots; nothing else touches the USB bus
directly. Writes (battery test, beeper) take the per-serial mutex so they
never overlap with the poller or each other.

## Installation

### Prerequisites (from-source builds only)

The `make build` pipeline runs `tailwindcss` and `templ generate` before
compiling. Install them once:

```bash
# Templ (Go template engine)
go install github.com/a-h/templ/cmd/templ@latest

# Tailwind CSS standalone CLI (no Node required)
# https://github.com/tailwindlabs/tailwindcss/releases
curl -sLo ~/.local/bin/tailwindcss \
  https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64
chmod +x ~/.local/bin/tailwindcss
```

The generated `app.css` is committed, so plain `go build` works without
the Tailwind CLI; only `make build` re-runs it.

### From source

```bash
git clone <repository-url>
cd cyberpower
make build                  # native architecture
make build ARCH=arm64       # Raspberry Pi / 64-bit ARM
make build ARCH=amd64       # Intel/AMD 64-bit
```

Binaries land in `dist/`:

```
dist/ups-cli-<arch>
dist/ups-monitor-<arch>
```

### As a Debian package

The repo ships an `nfpm.yaml` that produces a `.deb` containing the
binaries, the systemd unit, the udev rule, and a dedicated
`ups-monitor` service user.

```bash
make package ARCH=arm64     # or amd64
sudo dpkg -i dist/cyberpower-ups_*.deb
```

To uninstall:

```bash
sudo apt remove cyberpower-ups
# or
sudo dpkg -r cyberpower-ups
```

## Setup (Manual, for `ups-cli` only)

*Skip this section if you installed via the Debian package — it does
all of this for you.*

To access the USB device without `sudo`:

1. **Create the udev rule:**

   ```bash
   echo 'KERNEL=="hidraw*", ATTRS{idVendor}=="0764", ATTRS{idProduct}=="0601", GROUP="plugdev", MODE="0660"' \
     | sudo tee /etc/udev/rules.d/99-cyberpower.rules
   ```

2. **Reload rules:**

   ```bash
   sudo udevadm control --reload-rules && sudo udevadm trigger
   ```

3. **Add your user to `plugdev`:**

   ```bash
   sudo usermod -aG plugdev $USER
   ```

   *Log out and back in for this to take effect.*

## Usage

### `ups-cli` — one-shot CLI

```bash
./ups-cli                        # show status of all attached UPS
./ups-cli -list                  # list serial numbers and exit
./ups-cli -target CXXJV2019877   # restrict to one device
./ups-cli -beeper enable         # or disable
./ups-cli -test quick            # 10-second self-test
./ups-cli -test deep             # runs until battery is low
./ups-cli -test stop             # abort a running test
```

**Example status output:**

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

### `ups-monitor` — service

Run the binary directly (or via the systemd unit installed by the deb):

```bash
./ups-monitor
# Server starting on http://0.0.0.0:9999
# Swagger UI available at http://0.0.0.0:9999/swagger
# Metrics available at http://0.0.0.0:9999/metrics
```

Routes:

| Path                              | Description                                           |
|-----------------------------------|-------------------------------------------------------|
| `/`                               | HTML dashboard (auto-refreshes every 15 s)            |
| `/device/{serial}`                | Per-device detail page with controls                  |
| `/partials/devices`               | HTMX partial: device list                             |
| `/partials/device/{serial}`       | HTMX partial: one device's body                       |
| `/api/ups`                        | JSON list of all cached snapshots                     |
| `/api/ups/{serial}`               | JSON snapshot for one device                          |
| `/api/ups/{serial}/battery-test`  | POST — trigger or stop a self-test                    |
| `/api/ups/{serial}/beeper`        | POST — enable/disable beeper                          |
| `/swagger`                        | OpenAPI documentation                                 |
| `/metrics`                        | Prometheus exposition format                          |
| `/static/css/app.css`             | Embedded stylesheet                                   |

The service shuts down cleanly on `SIGINT` or `SIGTERM`: in-flight HTTP
requests are drained for up to 10 s before exit.

## Monitoring with Prometheus & Grafana

`/metrics` exposes standard Prometheus metrics. Scrape config:

```yaml
scrape_configs:
  - job_name: 'cyberpower-ups'
    static_configs:
      - targets: ['<your-ups-host>:9999']
```

A sample is in `monitoring/prometheus/prometheus.yml`.

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
of the service runs normally. Behaviour:

* Fires on `Utility ↔ Battery` transitions only (low-battery alerts go
  through Prometheus below).
* 30-second per-serial cooldown suppresses flapping power.
* No retries — the service logs and continues if Triton is unreachable.
* The first poll for each device is treated as a baseline (no alert)
  so a service restart doesn't double-send.

For the systemd unit, drop the variables in
`/etc/ups-monitor/config.env` (the path the unit's `EnvironmentFile=`
already points at).

#### Prometheus alerts

You can configure Prometheus to alert when utility power is lost. A
sample rule file is in `monitoring/prometheus/alerts.yml`:

```yaml
rule_files:
  - "alerts.yml"
```

The provided alerts cover:

* **UPSOnBattery** — `ups_status_code > 0` (utility power lost).
* **UPSLowBattery** — `ups_status_code == 2` (critical battery level).

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
