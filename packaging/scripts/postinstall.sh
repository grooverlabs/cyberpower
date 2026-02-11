#!/bin/sh
set -e

# Reload udev rules
udevadm control --reload-rules && udevadm trigger

# Reload systemd and enable service
systemctl daemon-reload
systemctl enable cyberpower-ups-monitor.service
systemctl restart cyberpower-ups-monitor.service
