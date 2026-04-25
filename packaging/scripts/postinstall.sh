#!/bin/sh
set -e

# =============================================================================
# CyberPower UPS Monitoring Tool - Post-Installation Script
# 
# This script:
# 1. Creates the ups-monitor system user if it doesn't exist
# 2. Sets up the working directory with proper permissions
# 3. Configures USB device access via udev
# 4. Enables and starts the systemd service
# =============================================================================

# --- Create service user if needed -------------------------------------------
if ! id -u ups-monitor >/dev/null 2>&1; then
    echo "Creating ups-monitor system user..."
    useradd --system --no-create-home --shell /usr/sbin/nologin \
        --home-dir /opt/ups-monitor ups-monitor
fi

# --- Set up working directory ------------------------------------------------
echo "Setting up working directory..."
mkdir -p /opt/ups-monitor
chown ups-monitor:ups-monitor /opt/ups-monitor
chmod 0750 /opt/ups-monitor

# --- Add user to plugdev group for USB device access -------------------------
echo "Configuring USB device access..."
usermod -aG plugdev ups-monitor || true

# --- Set up configuration directory ------------------------------------------
mkdir -p /etc/ups-monitor

# On first install, copy example config if real config doesn't exist
# On upgrades, preserve existing config (don't overwrite)
if [ ! -f /etc/ups-monitor/config.env ] && [ -f /etc/ups-monitor/config.env.example ]; then
    echo "Creating default configuration from example..."
    cp /etc/ups-monitor/config.env.example /etc/ups-monitor/config.env
fi

# Ensure config file has correct permissions (readable by ups-monitor)
if [ -f /etc/ups-monitor/config.env ]; then
    chown root:ups-monitor /etc/ups-monitor/config.env
    chmod 0640 /etc/ups-monitor/config.env
fi

chown root:ups-monitor /etc/ups-monitor
chmod 0750 /etc/ups-monitor

# --- Udev rules (with error recovery) ----------------------------------------
if command -v udevadm >/dev/null 2>&1; then
    echo "Reloading udev rules..."
    udevadm control --reload-rules && udevadm trigger || true
else
    echo "WARNING: udevadm not found. USB device rules may not be immediately active."
    echo "         Install udev or manually reload udev rules with:"
    echo "         sudo udevadm control --reload-rules"
fi

# --- Systemd setup (with error recovery) ------------------------------------
if command -v systemctl >/dev/null 2>&1; then
    echo "Configuring systemd service..."
    systemctl daemon-reload || true
    systemctl enable cyberpower-ups-monitor.service >/dev/null 2>&1 || true
    
    # Try to start the service, but don't fail if it can't
    # (e.g., no USB device found, permission issues)
    echo "Starting service..."
    systemctl restart cyberpower-ups-monitor.service || true
else
    echo "WARNING: systemctl not found. Service not started."
    echo "         Start manually with: /usr/bin/ups-monitor"
fi

# --- Print post-install instructions -----------------------------------------
cat <<'INSTRUCTIONS'

================================================================================
  CyberPower UPS Monitoring Tool Installed Successfully
================================================================================

NEXT STEPS:

  1. Verify USB device configuration:
     
     sudo ls -la /dev/hidraw* | head -5
     (Look for devices with group 'plugdev' and rw permissions)
  
  2. Check user configuration:
     
     id ups-monitor
     (Should show membership in groups: ups-monitor, plugdev)

  3. View service status:
     
     sudo systemctl status cyberpower-ups-monitor
     journalctl -u cyberpower-ups-monitor -f
  
  4. Test the CLI tool (requires USB device to be present):
     
     /usr/bin/ups-cli

CONFIGURATION:

  The service can be configured via environment variables. If needed:
  
     sudo vi /etc/ups-monitor/config.env
     sudo systemctl restart cyberpower-ups-monitor

TROUBLESHOOTING:

  Service won't start? Check:
     • USB device connected: lsusb | grep -i cyber
     • Device permissions: ls -la /dev/hidraw*
     • User access: sudo systemctl status cyberpower-ups-monitor
     • Full logs: journalctl -u cyberpower-ups-monitor -n 50

  Device not found? Try:
     • Reload udev rules: sudo udevadm control --reload-rules
     • Reconnect USB device
     • Verify lsusb shows: "0764:0601" for CP1500PFCLCDa

  Still having issues?
     • Check GitHub: https://github.com/grooverlabs/cyberpower
     • View logs: journalctl -u cyberpower-ups-monitor -b

================================================================================

INSTRUCTIONS

exit 0
