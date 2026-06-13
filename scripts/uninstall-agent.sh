#!/usr/bin/env sh
set -eu

# Uninstall C-Plane agent and remove all related files.
# Usage: sudo ./uninstall-agent.sh [--keep-apps]

KEEP_APPS="0"
SERVICE_NAME="cplane-agent"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/c-plane"
STATE_DIR="/var/lib/c-plane-agent"
LOG_DIR="/var/log/c-plane-agent"
APPS_DIR="/opt/c-plane"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --keep-apps) KEEP_APPS="1"; shift ;;
    -h|--help)
      echo "Usage: uninstall-agent.sh [--keep-apps]"
      echo "  --keep-apps   Keep deployed application files in $APPS_DIR"
      exit 0
      ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then
  echo "This uninstaller must run as root. Re-run with sudo." >&2
  exit 1
fi

echo "Stopping and disabling $SERVICE_NAME service..."
if command -v systemctl >/dev/null 2>&1; then
  systemctl stop "$SERVICE_NAME" 2>/dev/null || true
  systemctl disable "$SERVICE_NAME" 2>/dev/null || true
  rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
  systemctl daemon-reload
fi

echo "Removing binary..."
rm -f "$INSTALL_DIR/cplane-agent"

echo "Removing configuration and state..."
rm -rf "$CONFIG_DIR"
rm -rf "$STATE_DIR"
rm -rf "$LOG_DIR"

if [ "$KEEP_APPS" = "0" ]; then
  echo "Removing application files..."
  rm -rf "$APPS_DIR"
else
  echo "Keeping application files in $APPS_DIR"
fi

echo "Removing cplane system user..."
if id cplane >/dev/null 2>&1; then
  userdel cplane 2>/dev/null || true
fi

echo "C-Plane agent uninstalled successfully."
