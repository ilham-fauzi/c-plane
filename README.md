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
- Heartbeat audit retention keeps only the last 5 heartbeat events per host.
- MVP dashboard for hosts, repositories, apps, setup jobs, deploy jobs, and audit events.
- MVP `cplane-agent` binary with register, heartbeat, polling, setup-app execution, and placeholder deploy completion.
- Agent installer script for server-level installation.
- Server app setup job that prepares dynamic app roots, initial release/current/shared folders, recipe placeholders, and optional Nginx sites.
- Release retention fields and rollback job creation.

Not implemented yet:

- Real recipe execution.
- MQTT signaling.
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

## Install the Control Plane

Clone-and-install one-liner:

```bash
git clone https://github.com/ilham-fauzi/c-plane.git && \
cd c-plane && \
sudo bash scripts/install-control-plane.sh
```

With a custom bind address:

```bash
git clone https://github.com/ilham-fauzi/c-plane.git && \
cd c-plane && \
sudo bash scripts/install-control-plane.sh --addr 127.0.0.1:8080
```

The installer builds `cmd/cplane`, installs `/usr/local/bin/cplane`, writes `/etc/c-plane/cplane.env`, creates `/var/lib/c-plane`, and starts `cplane.service`.

Default service layout:

```text
/usr/local/bin/cplane
/etc/c-plane/cplane.env
/var/lib/c-plane/cplane.db
/var/log/c-plane
/etc/systemd/system/cplane.service
```

Expose the service through Caddy or Nginx:

```text
https://deploy.example.com -> http://127.0.0.1:8080
```

## Build

```bash
GOCACHE="$PWD/.cache/go-build" go build ./cmd/cplane ./cmd/cplane-agent
```

## Test

```bash
GOCACHE="$PWD/.cache/go-build" go test ./...
```

## Release

Release tags must publish binary assets because the agent installer downloads `cplane-agent-<os>-<arch>` from the latest GitHub Release.

```bash
make release VERSION=v0.1.0
make publish VERSION=v0.1.0
```

Pushing a `v*` tag also runs the release workflow and uploads linux amd64/arm64 assets automatically.

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
  --token install_xxx \
  --run-as-root
```

For local testing with a locally built binary:

```bash
sudo bash scripts/install-agent.sh \
  --api-url http://127.0.0.1:18080 \
  --host-id srv_xxx \
  --token install_xxx \
  --binary-path ./cplane-agent \
  --run-as-root
```

The installer writes the one-time install token first, runs `cplane-agent register`, exchanges it for a runtime agent token, then starts `cplane-agent.service`.

Use `--run-as-root` when the agent must create app folders under protected paths such as `/var/www` and manage `/etc/nginx/sites-available`. Omit it only when the target app roots and recipes are writable by the `cplane` system user and Nginx is managed outside C-Plane.

## Server App Setup Flow

The dashboard can queue a server setup job after a host and repository exist:

1. Create a host and install the generated global agent command on the target server.
2. Add the repository URL.
3. Submit `Setup Server App` with target host, repository, app name, dynamic project root path, domain, runtime, ref, recipe path, and Nginx setting.
4. The control plane creates the app record and queues a `setup_app` deployment job.
5. The global agent picks up the job, prepares `<app_root>/releases/initial`, `<app_root>/current`, `<app_root>/shared`, writes the recipe placeholder, and optionally writes/enables the Nginx site.

Example dynamic roots:

```text
/var/www/api-al-waqtu
/var/www/kaligede
/srv/apps/backend
```

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
