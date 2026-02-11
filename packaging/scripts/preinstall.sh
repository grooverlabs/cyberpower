#!/bin/sh
set -e

# Create service user if it doesn't exist
if ! id "cyberpower-ups" >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /bin/false --user-group cyberpower-ups
fi

# Ensure user is in plugdev group
usermod -aG plugdev cyberpower-ups
