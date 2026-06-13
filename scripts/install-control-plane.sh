#!/usr/bin/env sh
set -eu

SOURCE_DIR="$(pwd)"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/c-plane"
STATE_DIR="/var/lib/c-plane"
LOG_DIR="/var/log/c-plane"
SERVICE_NAME="cplane"
ADDR="127.0.0.1:8080"
DB_PATH=""
BUILD_DIR=""
SKIP_BUILD="0"

usage() {
  cat <<'EOF'
Usage:
  install-control-plane.sh [options]

Options:
  --source-dir PATH     Source checkout path. Default: current directory.
  --install-dir PATH    Default: /usr/local/bin
  --config-dir PATH     Default: /etc/c-plane
  --state-dir PATH      Default: /var/lib/c-plane
  --log-dir PATH        Default: /var/log/c-plane
  --addr ADDR           Bind address. Default: 127.0.0.1:8080
  --db-path PATH        SQLite database path. Default: <state-dir>/cplane.db
  --build-dir PATH      Build cache/work directory. Default: <source-dir>/.cache/install
  --skip-build          Install existing ./cplane binary from source dir.
  -h, --help            Show help.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --source-dir) SOURCE_DIR="${2:-}"; shift 2 ;;
    --install-dir) INSTALL_DIR="${2:-}"; shift 2 ;;
    --config-dir) CONFIG_DIR="${2:-}"; shift 2 ;;
    --state-dir) STATE_DIR="${2:-}"; shift 2 ;;
    --log-dir) LOG_DIR="${2:-}"; shift 2 ;;
    --addr) ADDR="${2:-}"; shift 2 ;;
    --db-path) DB_PATH="${2:-}"; shift 2 ;;
    --build-dir) BUILD_DIR="${2:-}"; shift 2 ;;
    --skip-build) SKIP_BUILD="1"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then
  echo "This installer must run as root. Re-run with sudo." >&2
  exit 1
fi

if [ -z "$DB_PATH" ]; then
  DB_PATH="$STATE_DIR/cplane.db"
fi

if [ -z "$BUILD_DIR" ]; then
  BUILD_DIR="$SOURCE_DIR/.cache/install"
fi

if [ ! -f "$SOURCE_DIR/go.mod" ]; then
  echo "go.mod not found in source dir: $SOURCE_DIR" >&2
  exit 1
fi

if ! id cplane >/dev/null 2>&1; then
  if command -v useradd >/dev/null 2>&1; then
    useradd --system --home "$STATE_DIR" --shell /usr/sbin/nologin cplane
  elif command -v adduser >/dev/null 2>&1; then
    adduser --system --home "$STATE_DIR" --no-create-home --disabled-login cplane
  else
    echo "Cannot create cplane user: useradd/adduser not found" >&2
    exit 1
  fi
fi

mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$STATE_DIR" "$LOG_DIR" "$BUILD_DIR"

if [ "$SKIP_BUILD" = "1" ]; then
  if [ ! -x "$SOURCE_DIR/cplane" ]; then
    echo "--skip-build requires executable $SOURCE_DIR/cplane" >&2
    exit 1
  fi
  cp "$SOURCE_DIR/cplane" "$INSTALL_DIR/cplane"
else
  if ! command -v go >/dev/null 2>&1; then
    echo "Go is required to build C-Plane from source." >&2
    exit 1
  fi
  (
    cd "$SOURCE_DIR"
    GOCACHE="$BUILD_DIR/go-build" go build -o "$BUILD_DIR/cplane" ./cmd/cplane
  )
  cp "$BUILD_DIR/cplane" "$INSTALL_DIR/cplane"
fi

chmod 0755 "$INSTALL_DIR/cplane"

cat > "$CONFIG_DIR/cplane.env" <<EOF
CPLANE_ADDR=$ADDR
CPLANE_DB_PATH=$DB_PATH
EOF

chmod 0640 "$CONFIG_DIR/cplane.env"
chown -R cplane:cplane "$CONFIG_DIR" "$STATE_DIR" "$LOG_DIR" 2>/dev/null || true

if command -v systemctl >/dev/null 2>&1; then
  cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=C-Plane Control Plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=cplane
Group=cplane
EnvironmentFile=$CONFIG_DIR/cplane.env
ExecStart=$INSTALL_DIR/cplane
Restart=always
RestartSec=5
WorkingDirectory=$STATE_DIR

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable "$SERVICE_NAME"
  systemctl restart "$SERVICE_NAME"
  systemctl --no-pager --full status "$SERVICE_NAME" || true
else
  echo "systemctl not found. Start manually:"
  echo "CPLANE_ADDR=$ADDR CPLANE_DB_PATH=$DB_PATH $INSTALL_DIR/cplane"
fi

echo "C-Plane control plane installed"
echo "Config: $CONFIG_DIR/cplane.env"
echo "Database: $DB_PATH"
