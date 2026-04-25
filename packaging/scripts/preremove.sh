#!/bin/sh
set -e

# =============================================================================
# CyberPower UPS Monitoring Tool - Pre-Removal Script
#
# Stops and disables the service on package removal.
# Preserves the ups-monitor user and /opt/ups-monitor directory
# so that reinstalling the package is clean.
# =============================================================================

echo "Stopping CyberPower UPS monitoring service..."

if command -v systemctl >/dev/null 2>&1; then
    systemctl stop cyberpower-ups-monitor.service >/dev/null 2>&1 || true
    systemctl disable cyberpower-ups-monitor.service >/dev/null 2>&1 || true
    echo "Service stopped and disabled."
else
    echo "WARNING: systemctl not found. Unable to stop service."
fi

exit 0

