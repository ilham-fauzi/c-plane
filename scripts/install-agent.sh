#!/usr/bin/env sh
set -eu

GITHUB_REPO="ilham-fauzi/c-plane"
VERSION="latest"
API_URL=""
HOST_ID=""
TOKEN=""
BINARY_URL=""
BINARY_PATH=""
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/c-plane"
STATE_DIR="/var/lib/c-plane-agent"
LOG_DIR="/var/log/c-plane-agent"
APPS_DIR="/opt/c-plane/apps"
SERVICE_NAME="cplane-agent"
POLL_INTERVAL_SECONDS="15"
REGISTER="1"
RUN_AS_ROOT="0"

usage() {
  cat <<'EOF'
Usage:
  install-agent.sh --api-url URL --host-id HOST_ID --token INSTALL_TOKEN [options]

Options:
  --binary-url URL              Download cplane-agent from URL.
  --binary-path PATH            Install an existing local cplane-agent binary.
  --install-dir PATH            Default: /usr/local/bin
  --config-dir PATH             Default: /etc/c-plane
  --state-dir PATH              Default: /var/lib/c-plane-agent
  --log-dir PATH                Default: /var/log/c-plane-agent
  --apps-dir PATH               Default: /opt/c-plane/apps
  --poll-interval-seconds N     Default: 15
  --run-as-root                 Run the agent service as root for app and Nginx setup jobs.
  --skip-register               Install files but do not exchange install token.
  -h, --help                    Show help.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --api-url) API_URL="${2:-}"; shift 2 ;;
    --host-id) HOST_ID="${2:-}"; shift 2 ;;
    --token) TOKEN="${2:-}"; shift 2 ;;
    --version) VERSION="${2:-}"; shift 2 ;;
    --binary-url) BINARY_URL="${2:-}"; shift 2 ;;
    --binary-path) BINARY_PATH="${2:-}"; shift 2 ;;
    --install-dir) INSTALL_DIR="${2:-}"; shift 2 ;;
    --config-dir) CONFIG_DIR="${2:-}"; shift 2 ;;
    --state-dir) STATE_DIR="${2:-}"; shift 2 ;;
    --log-dir) LOG_DIR="${2:-}"; shift 2 ;;
    --apps-dir) APPS_DIR="${2:-}"; shift 2 ;;
    --poll-interval-seconds) POLL_INTERVAL_SECONDS="${2:-}"; shift 2 ;;
    --run-as-root) RUN_AS_ROOT="1"; shift ;;
    --skip-register) REGISTER="0"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [ -z "$API_URL" ] || [ -z "$HOST_ID" ] || [ -z "$TOKEN" ]; then
  echo "--api-url, --host-id, and --token are required" >&2
  usage
  exit 2
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "This installer must run as root. Re-run with sudo." >&2
  exit 1
fi

detect_os() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux) echo "linux" ;;
    darwin) echo "darwin" ;;
    *) echo "Unsupported OS: $os" >&2; exit 1 ;;
  esac
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
  esac
}

download() {
  url="$1"
  dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -q "$url" -O "$dest"
    return
  fi
  echo "curl or wget is required to download cplane-agent" >&2
  exit 1
}

OS="$(detect_os)"
ARCH="$(detect_arch)"
AGENT_BIN="$INSTALL_DIR/cplane-agent"

if [ -z "$BINARY_URL" ]; then
  if [ "$VERSION" = "latest" ]; then
    BINARY_URL="https://github.com/${GITHUB_REPO}/releases/latest/download/cplane-agent-${OS}-${ARCH}"
  else
    BINARY_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/cplane-agent-${OS}-${ARCH}"
  fi
fi

mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$STATE_DIR" "$LOG_DIR" "$APPS_DIR"

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

if [ -n "$BINARY_PATH" ]; then
  cp "$BINARY_PATH" "$AGENT_BIN"
else
  tmp="$(mktemp)"
  download "$BINARY_URL" "$tmp"
  mv "$tmp" "$AGENT_BIN"
fi
chmod 0755 "$AGENT_BIN"

cat > "$CONFIG_DIR/agent.toml" <<EOF
host_id = "$HOST_ID"
api_url = "$API_URL"
mqtt_url = "mqtts://deploy.example.com:8883"
state_dir = "$STATE_DIR"
log_dir = "$LOG_DIR"
poll_interval_seconds = "$POLL_INTERVAL_SECONDS"

[auth]
token_file = "$CONFIG_DIR/agent.token"
EOF

printf '%s\n' "$TOKEN" > "$CONFIG_DIR/agent.token"
chmod 0600 "$CONFIG_DIR/agent.token"
chown -R cplane:cplane "$CONFIG_DIR" "$STATE_DIR" "$LOG_DIR" "$APPS_DIR" 2>/dev/null || true

if command -v systemctl >/dev/null 2>&1; then
  SERVICE_USER_LINES="User=cplane
Group=cplane"
  if [ "$RUN_AS_ROOT" = "1" ]; then
    SERVICE_USER_LINES=""
  fi
  cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=C-Plane Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
$SERVICE_USER_LINES
ExecStart=$AGENT_BIN run --config $CONFIG_DIR/agent.toml
Restart=always
RestartSec=5
WorkingDirectory=$STATE_DIR

[Install]
WantedBy=multi-user.target
EOF
fi

if [ "$REGISTER" = "1" ]; then
  "$AGENT_BIN" register --config "$CONFIG_DIR/agent.toml"
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
  systemctl enable "$SERVICE_NAME"
  systemctl restart "$SERVICE_NAME"
  systemctl --no-pager --full status "$SERVICE_NAME" || true
else
  echo "systemctl not found. Start manually:"
  echo "$AGENT_BIN run --config $CONFIG_DIR/agent.toml"
fi

echo "C-Plane agent installed for host $HOST_ID"
