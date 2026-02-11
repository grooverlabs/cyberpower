#!/bin/sh
set -e

systemctl daemon-reload
udevadm control --reload-rules
