# C-Plane

C-Plane is a lightweight CI/CD control plane for small to medium self-managed servers. It provides host registration, deployment job tracking, host-side agents, release history, rollback, logs, and auditability without trying to replace full CI systems such as GitHub Actions, GitLab CI, Jenkins, or Kubernetes.

## Architecture

```text
GitHub/GitLab/CI
      |
      v
C-Plane API + Dashboard
      |
      +--> SQLite/PostgreSQL
      |
      +--> MQTT broker
              |
              v
        cplane-agent on target host
              |
              v
       Local recipe/script runner
              |
              v
        App runtime / systemd
```

The control plane and agent live in the same repository but are built as separate binaries:

```text
cmd/cplane        # Control plane API
cmd/cplane-agent  # Host-side agent
```

## Current Status

This repository is in early MVP development.

Implemented:

- Go control plane API.
- SQLite store.
- Host, repository, app, deployment job, release, log, and audit models.
- Agent registration endpoint.
- Agent heartbeat and polling endpoints.
- MVP `cplane-agent` binary with register, heartbeat, polling, and placeholder job completion.
- Agent installer script for server-level installation.
- Release retention fields and rollback job creation.

Not implemented yet:

- Real recipe execution.
- MQTT signaling.
- Dashboard UI.
- GitHub webhook validation.
- Agent token enforcement middleware.
- Binary release/download endpoint.

## Requirements

- Go 1.26+
- SQLite through the pure Go `modernc.org/sqlite` driver
- Linux target hosts for systemd agent installation

## Run the Control Plane

```bash
GOCACHE="$PWD/.cache/go-build" \
CPLANE_ADDR=:18080 \
CPLANE_DB_PATH=.cache/cplane-dev.db \
go run ./cmd/cplane
```

Health check:

```bash
curl -s http://127.0.0.1:18080/healthz
```

## Build

```bash
GOCACHE="$PWD/.cache/go-build" go build ./cmd/cplane ./cmd/cplane-agent
```

## Test

```bash
GOCACHE="$PWD/.cache/go-build" go test ./...
```

## Agent Installation

The agent is installed at server level, not inside an individual app project directory.

Default layout:

```text
/usr/local/bin/cplane-agent
/etc/c-plane/agent.toml
/etc/c-plane/agent.token
/var/lib/c-plane-agent
/var/log/c-plane-agent
/opt/c-plane/apps
```

Dashboard-generated install command should look like:

```bash
curl -fsSLo install-agent.sh https://deploy.example.com/install-agent.sh
sudo bash install-agent.sh \
  --api-url https://deploy.example.com \
  --host-id srv_xxx \
  --token install_xxx
```

For local testing with a locally built binary:

```bash
sudo bash scripts/install-agent.sh \
  --api-url http://127.0.0.1:18080 \
  --host-id srv_xxx \
  --token install_xxx \
  --binary-path ./cplane-agent
```

The installer writes the one-time install token first, runs `cplane-agent register`, exchanges it for a runtime agent token, then starts `cplane-agent.service`.

## Dynamic App Roots

C-Plane must not assume apps always live under `/var/apps`.

Each app has its own `root_path`, for example:

```text
/var/apps/api-prod
/var/www/api-al-waqtu
/var/www/kaligede
/srv/apps/backend
/home/deploy/apps/admin
```

For symlink releases, paths are derived from the configured app root:

```text
<app_root>/releases/<release_id>
<app_root>/current -> releases/<release_id>
<app_root>/shared
```

Legacy apps that cannot migrate to a symlink layout should use `custom-script` strategy.

## API Surface

Current MVP endpoints:

```text
GET  /healthz
GET  /api/hosts
POST /api/hosts
GET  /api/repos
POST /api/repos
GET  /api/apps
POST /api/apps
GET  /api/apps/{id}/releases
GET  /api/deployments
POST /api/deployments
GET  /api/deployments/{id}
POST /api/deployments/{id}/approve
POST /api/deployments/{id}/cancel
POST /api/releases/{id}/rollback
POST /api/agent/register
POST /api/agent/heartbeat
GET  /api/agent/jobs/pending
GET  /api/agent/jobs/{id}
POST /api/agent/jobs/{id}/start
POST /api/agent/jobs/{id}/logs
POST /api/agent/jobs/{id}/complete
POST /api/agent/jobs/{id}/fail
GET  /api/audit-events
```

## Documentation

Product requirements live in:

```text
docs/C_PLANE_PRD.md
```
