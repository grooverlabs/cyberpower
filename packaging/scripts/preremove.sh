#!/bin/sh
set -e

# Stop and disable service
systemctl stop cyberpower-ups-monitor.service || true
systemctl disable cyberpower-ups-monitor.service || true
