# C-Plane PRD: Lightweight CI/CD Control Plane

## 1. Product Summary

C-Plane is a lightweight CI/CD and deployment control plane for small to medium self-managed servers. It focuses on repository setup, host registration, branch/tag based deployment rules, secure deploy signaling, host-side execution, release tracking, rollback, logs, and auditability.

The system is intentionally smaller than a full CI/CD suite. It does not try to replace GitHub Actions, GitLab CI, Jenkins, or Kubernetes. Instead, it provides a simple deployment layer that can receive signals from Git providers or CI jobs, decide what should be deployed, notify the correct host agent, and supervise deployment execution.

Core design principle:

```text
MQTT = lightweight signal
HTTPS API = job detail, auth, logs, audit, artifact metadata
DB = source of truth
Go agent = reliable executor
Host scripts/recipes = application-specific deploy logic
```

## 2. Goals

- Provide a high-performance, low-resource deployment system.
- Support repository registration and branch/tag deployment rules.
- Support host registration with installable host agents.
- Allow agents to receive deploy/revert/restart signals without exposing inbound host ports.
- Support multiple app stacks such as PHP, Ruby, Node.js, Go, Java, and static apps.
- Keep host deployment logic flexible through YAML recipes and optional shell scripts.
- Provide atomic release switching and fast rollback.
- Provide audit logs for deployment, rollback, manual approval, and host activity.
- Make the MVP useful on a single low-cost VPS.

## 3. Non-Goals

- Full CI runner replacement.
- Kubernetes orchestration.
- Complex build matrix execution.
- Full secrets manager replacement.
- Distributed artifact registry in the MVP.
- Multi-cloud provisioning in the MVP.
- Running arbitrary remote commands from the dashboard.

## 4. Target Users

### Solo Developer / Small Team

Needs a simple way to deploy apps from Git branches or tags to personal VPS instances.

### Internal Tooling Team

Needs predictable deployments, logs, rollback, and audit records without operating a heavy CI/CD platform.

### Agency / Multi-App Operator

Manages many small apps across multiple hosts and wants branch-based deployment rules with low operational overhead.

## 5. Success Criteria

- A new host can be registered and connected in under 5 minutes.
- A repository can be connected and mapped to staging/production rules in under 10 minutes.
- A deploy job can be triggered by Git webhook, manual dashboard action, or external CI call.
- Agent host does not need an inbound public port.
- Rollback can switch to a retained release in under 10 seconds for symlink-based deployments.
- Agent idle memory target: less than 30 MB.
- Control plane MVP can run on a 1 vCPU / 512 MB RAM VPS for small workloads.
- Every deploy/revert action is auditable.

## 6. Recommended Stack

### Control Plane

```text
Language      : Go
HTTP API      : Go net/http or Chi
Database MVP  : SQLite
Database Scale: PostgreSQL
MQTT Broker   : Mosquitto
Dashboard     : SvelteKit or Go templ + HTMX
Reverse Proxy : Caddy or Nginx
Process       : systemd
```

### Host Agent

```text
Language      : Go
Install       : systemd service
Signal        : MQTT over TLS
Job Detail    : HTTPS API
Execution     : local recipe/script runner
Logs          : HTTPS upload, optional MQTT status event
State         : /var/lib/c-plane-agent
Config        : /etc/c-plane/agent.env or agent.toml
```

### Deployment Runtime on Host

```text
App root      : configured per app, for example /var/apps/<app>, /var/www/<app>, /srv/apps/<app>
Release model : <app_root>/releases/<release_id>
Current app   : <app_root>/current -> releases/<release_id>
Shared config : <app_root>/shared
Service       : systemd, pm2, supervisor, php-fpm, custom script
```

`/var/apps/<app>` is only the recommended default for new apps. C-Plane must support existing project locations such as `/var/www/api-al-waqtu`, `/var/www/kaligede`, `/home/deploy/apps/<app>`, or `/srv/apps/<app>`. The agent must derive deployment paths from the app `root_path` and must not hardcode `/var/apps`.

## 7. High-Level Architecture

```text
GitHub/GitLab/CI
      |
      v
Control Plane API + Dashboard
      |
      +--> Database
      |
      +--> MQTT Broker
              |
              v
        Go Agent on Host
              |
              v
       Deploy Recipe / Script
              |
              v
        App Runtime / systemd
```

## 8. Deployment Signal Design

The agent does not wait for a signal from the operating system. It maintains an outbound connection to the central MQTT broker.

### Why MQTT

MQTT is lightweight, uses publish/subscribe semantics, and fits the host-agent model well. Each host subscribes only to its own topic. The control plane publishes a small signal when a new job is available.

### Important Rule

MQTT messages must not contain arbitrary shell commands.

MQTT is used only as a notification path. The real job data is stored in the control plane database and fetched through authenticated HTTPS.

Example MQTT payload:

```json
{
  "type": "job_available",
  "job_id": "dep_123"
}
```

Agent then calls:

```text
GET /api/agent/jobs/dep_123
```

## 9. MQTT Topology

### Broker Location

The MQTT broker runs on the control plane server.

```text
Control server
  - Go API
  - Dashboard
  - DB
  - MQTT broker

Target host
  - Go agent
  - app deploy scripts
```

### Public Ports

```text
443  : Control plane API/dashboard HTTPS
8883 : MQTT over TLS
```

Port `1883` must not be used over public internet unless behind a private network/VPN.

### Topic Structure

```text
cplane/hosts/<host_id>/jobs
cplane/hosts/<host_id>/status
cplane/hosts/<host_id>/logs
cplane/env/<environment>/jobs
```

MVP should use host-specific topics first:

```text
cplane/hosts/srv_001/jobs
```

### MQTT ACL Example

For host `srv_001`:

```text
subscribe: cplane/hosts/srv_001/jobs
publish  : cplane/hosts/srv_001/status
publish  : cplane/hosts/srv_001/logs
```

The agent must not be allowed to subscribe to another host topic.

## 10. Core Workflows

### 10.1 Register Host

1. Admin opens dashboard.
2. Admin creates a new host.
3. Control plane generates `host_id`, agent token, MQTT credentials, and install command.
4. Admin runs installer on target server.
5. Installer creates system user and config.
6. Installer registers systemd service.
7. Agent starts and sends heartbeat.
8. Dashboard marks host as online.

Example install command:

```bash
curl -fsSLo install-agent.sh https://deploy.example.com/install-agent.sh
sudo bash install-agent.sh \
  --api-url https://deploy.example.com \
  --host-id srv_001 \
  --token <one_time_install_token>
```

### 10.2 Register Repository

1. Admin adds repository URL.
2. Admin configures Git provider webhook or deploy key.
3. Admin maps branch/tag patterns to environments.
4. Admin selects target host and app.
5. Admin defines deploy recipe or custom script path.

Example rules:

```text
main      -> production
staging   -> staging
tag v*    -> production release
```

### 10.2.1 Setup Server App

The host agent is installed once at server level and can manage multiple apps on that host. An app setup flow binds one repository to one host and one dynamic project root.

1. Admin installs the global agent on the target host.
2. Admin adds or selects a repository.
3. Admin opens the dashboard setup form.
4. Admin selects host and repository.
5. Admin enters app name, environment, root path, domain, runtime, deploy ref, recipe path, release retention, and Nginx management preference.
6. Control plane creates the app record.
7. Control plane creates a `setup_app` job with structured metadata.
8. Agent fetches the job over HTTPS.
9. Agent prepares the app root, release directories, shared directory, current symlink, and recipe placeholder.
10. If Nginx management is enabled, agent writes the site config, enables it, validates Nginx, and reloads Nginx.
11. Agent uploads logs and marks the job success or failed.

The setup job must support dynamic roots such as:

```text
/var/www/api-al-waqtu
/var/www/kaligede
/var/apps/api-prod
/srv/apps/backend
```

The agent should run with only the privileges needed for the selected host operations. Managing `/var/www` and `/etc/nginx` usually requires a root service or a narrowly scoped privileged helper.

### 10.3 Deploy from Git Webhook

1. Git provider sends webhook to control plane.
2. Control plane validates signature.
3. Control plane matches branch/tag rule.
4. Control plane creates deployment job in DB.
5. Control plane publishes MQTT signal to target host topic.
6. Agent receives `job_available`.
7. Agent fetches job detail over HTTPS.
8. Agent validates app/action allowlist.
9. Agent executes deployment.
10. Agent streams logs/status back.
11. Control plane marks job success or failure.

### 10.4 Manual Deploy

1. Admin opens app page.
2. Admin selects ref, commit, tag, or artifact.
3. Admin clicks deploy.
4. Production deploy may require approval.
5. Control plane creates deployment job.
6. Agent executes job.

### 10.5 Rollback

1. Admin opens the app release history page.
2. Admin selects one retained successful release to roll back to.
3. Dashboard shows release metadata such as ref, commit SHA, deploy time, deploy actor, healthcheck result, and current/previous state.
4. Admin confirms rollback.
5. Control plane creates rollback job.
6. MQTT notifies agent.
7. Agent switches symlink to the selected release.
8. Agent restarts/reloads service.
9. Agent runs healthcheck.
10. Control plane records rollback audit event.

Rollback should not require GitHub, GitLab, or the original repository when the selected release directory is still retained on the host. The agent should perform the rollback from local release directories by switching the `current` symlink.

If the selected release has already been removed by retention cleanup, the dashboard must mark it unavailable for rollback. In that case, the user must deploy again from source or artifact.

### 10.6 Release History Retention

1. Every successful deploy creates a release record and a release directory on the target host.
2. C-Plane keeps a configurable number of successful releases per app/environment.
3. Default retention should be 5 successful releases.
4. Minimum retention should be 3 successful releases.
5. Failed releases may be retained temporarily for debugging, then removed by cleanup policy.
6. Dashboard rollback is only available for releases that still have a retained host-side release directory.

Recommended defaults:

```text
successful_releases_keep = 5
successful_releases_min  = 3
failed_releases_ttl      = 72h
logs_ttl                 = configurable
```

The retention policy should run after successful deployments and should never remove the active release. It should also avoid deleting the immediately previous successful release when possible, so fast rollback remains available.

## 11. Agent Installation Layout

The control plane and agent live in the same repository but are built as separate binaries:

```text
cmd/cplane        : control plane API/dashboard process
cmd/cplane-agent  : host-side agent process
```

The target app server should install only `cplane-agent`. The agent is installed at server level, not inside an individual app project directory.

```text
/usr/local/bin/cplane-agent
/etc/c-plane/agent.toml
/opt/c-plane/apps/
/var/lib/c-plane-agent/
/var/log/c-plane-agent/
```

Dashboard host registration should generate a copy-paste install command:

```bash
curl -fsSLo install-agent.sh https://deploy.example.com/install-agent.sh
sudo bash install-agent.sh \
  --api-url https://deploy.example.com \
  --host-id srv_001 \
  --token <one_time_install_token>
```

The installer should:

1. Detect OS and CPU architecture.
2. Download or install the correct `cplane-agent` binary.
3. Create the `cplane` system user when needed.
4. Create config, state, log, and app recipe directories.
5. Write `/etc/c-plane/agent.toml`.
6. Write `/etc/c-plane/agent.token` with the one-time install token.
7. Run `cplane-agent register` to exchange the install token for a runtime agent token.
8. Register and start `cplane-agent.service`.

The install token must be short-lived and one-time use. After successful registration, the agent stores only the runtime agent token.

Example `agent.toml`:

```toml
host_id = "srv_001"
api_url = "https://deploy.example.com"
mqtt_url = "mqtts://deploy.example.com:8883"
state_dir = "/var/lib/c-plane-agent"
log_dir = "/var/log/c-plane-agent"

[auth]
token_file = "/etc/c-plane/agent.token"

[apps.api_prod]
app_id = "api_prod"
root = "/var/apps/api-prod"
service = "api-prod.service"
recipe = "/opt/c-plane/apps/api-prod/deploy.yaml"
allowed_actions = ["deploy", "rollback", "restart"]
```

Example systemd service:

```ini
[Unit]
Description=C-Plane Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=cplane
Group=cplane
ExecStart=/usr/local/bin/cplane-agent --config /etc/c-plane/agent.toml
Restart=always
RestartSec=5
WorkingDirectory=/var/lib/c-plane-agent

[Install]
WantedBy=multi-user.target
```

## 12. Deployment Recipe

Recipe files define how a specific app should be deployed. The agent should support both built-in stack adapters and custom scripts.

Example:

```yaml
app: api-prod
strategy: symlink-release

source:
  type: git
  repo: git@github.com:org/api.git
  ref: main

runtime:
  stack: node
  service: api-prod.service

paths:
  root: /var/apps/api-prod
  releases: /var/apps/api-prod/releases
  shared: /var/apps/api-prod/shared
  current: /var/apps/api-prod/current

steps:
  prepare:
    - npm ci
    - npm run build
  activate:
    - systemctl reload api-prod.service

healthcheck:
  type: http
  url: http://127.0.0.1:3000/health
  timeout_seconds: 15

rollback:
  strategy: previous_release
```

Custom script recipe:

```yaml
app: api-prod
strategy: custom-script
script:
  deploy: /opt/c-plane/apps/api-prod/deploy.sh
  rollback: /opt/c-plane/apps/api-prod/rollback.sh
  restart: /opt/c-plane/apps/api-prod/restart.sh
healthcheck:
  url: http://127.0.0.1:3000/health
```

## 13. Stack Adapters

The MVP should not over-automate stack detection. It should provide helpful defaults but always allow custom scripts.

### PHP

Detection:

```text
composer.json
```

Default commands:

```text
composer install --no-dev --prefer-dist --optimize-autoloader
php artisan migrate --force optional
php artisan config:cache optional
```

### Node.js

Detection:

```text
package.json
```

Default commands:

```text
npm ci
npm run build optional
systemctl reload <service> or pm2 reload
```

### Ruby

Detection:

```text
Gemfile
```

Default commands:

```text
bundle install --deployment
bundle exec rake assets:precompile optional
systemctl reload <service>
```

### Go

Detection:

```text
go.mod
```

Default commands:

```text
go build -o app ./cmd/app optional
systemctl restart <service>
```

### Java

Detection:

```text
pom.xml or build.gradle
```

Default commands:

```text
mvn package or gradle build
systemctl restart <service>
```

### Static Site

Detection:

```text
index.html, dist/, build/, public/
```

Default commands:

```text
copy build output to release directory
reload web server optional
```

## 14. Release Strategy

C-Plane should use atomic release directories by default.

C-Plane should use atomic release directories by default, derived from each app's configured `root_path`.

Example for the recommended default:

```text
/var/apps/myapp/releases/20260612-120001
/var/apps/myapp/releases/20260612-121500
/var/apps/myapp/current -> /var/apps/myapp/releases/20260612-121500
/var/apps/myapp/shared/.env
```

Example for an existing `/var/www` app:

```text
/var/www/api-al-waqtu/releases/20260612-120001
/var/www/api-al-waqtu/releases/20260612-121500
/var/www/api-al-waqtu/current -> /var/www/api-al-waqtu/releases/20260612-121500
/var/www/api-al-waqtu/shared/.env
```

For legacy apps that cannot migrate to `current` symlink layout immediately, use `custom-script` strategy. In that mode, C-Plane stores `root_path` for visibility and policy checks, while local scripts own the exact deploy and rollback behavior.

Deploy flow:

1. Create new release directory.
2. Fetch source or artifact.
3. Link shared files.
4. Run build/install steps.
5. Run pre-activation checks.
6. Switch `current` symlink.
7. Restart/reload service.
8. Run healthcheck.
9. Mark release successful.

Rollback flow:

1. Select a retained successful release from release history.
2. Verify the selected release still exists on the host.
3. Switch `<app_root>/current` symlink to the selected release directory.
4. Restart/reload service.
5. Run healthcheck.
6. Mark rollback successful.

Release retention:

- Default to keeping the last 5 successful releases per app/environment.
- Allow app-level override, with a recommended minimum of 3 successful releases.
- Keep failed release directories temporarily for debugging, for example 24-72 hours.
- Never remove the currently active release.
- Prefer keeping the immediately previous successful release even when cleanup runs.
- Mark database release records as unavailable for rollback if their host-side release directory has been removed.

Dashboard release history should allow rollback to any retained successful release, not only the immediately previous release.

## 15. API Surface

### Public / Integration APIs

```text
POST /api/webhooks/github
POST /api/webhooks/gitlab
POST /api/deployments
GET  /api/deployments/:id
POST /api/deployments/:id/approve
POST /api/deployments/:id/cancel
GET  /api/apps/:id/releases
POST /api/releases/:id/rollback
```

### Agent APIs

```text
POST /api/agent/heartbeat
GET  /api/agent/jobs/:id
POST /api/agent/jobs/:id/start
POST /api/agent/jobs/:id/logs
POST /api/agent/jobs/:id/complete
POST /api/agent/jobs/:id/fail
```

### Admin APIs

```text
GET    /api/repos
POST   /api/repos
GET    /api/hosts
POST   /api/hosts
GET    /api/apps
POST   /api/apps
GET    /api/environments
POST   /api/environments
GET    /api/audit-events
```

## 16. Data Model

### repos

```text
id
name
provider
url
default_branch
webhook_secret_hash
created_at
updated_at
```

### hosts

```text
id
name
status
last_seen_at
agent_version
mqtt_username
agent_token_hash
created_at
updated_at
```

### apps

```text
id
name
repo_id
host_id
environment_id
root_path
recipe_path
successful_releases_keep
failed_releases_ttl_hours
created_at
updated_at
```

`root_path` is required and must be fully dynamic per app. It may point to `/var/apps/<app>`, `/var/www/<app>`, `/srv/apps/<app>`, `/home/deploy/apps/<app>`, or another operator-defined path. The agent must use this value, or the root configured in the local recipe, as the base path for release, shared, and current directories.

### deploy_rules

```text
id
repo_id
app_id
match_type branch|tag
pattern
environment_id
auto_deploy boolean
requires_approval boolean
created_at
updated_at
```

### deployment_jobs

```text
id
app_id
host_id
repo_id
action deploy|rollback|restart
status queued|signaled|running|success|failed|canceled
ref
commit_sha
artifact_url
artifact_checksum
requested_by
approved_by
started_at
finished_at
created_at
updated_at
```

### releases

```text
id
app_id
deployment_job_id
release_key
commit_sha
artifact_checksum
path
status preparing|active|previous|failed
available_for_rollback boolean
retained_until
activated_at
created_at
```

### audit_events

```text
id
actor_type user|agent|system
actor_id
action
resource_type
resource_id
metadata_json
ip_address
created_at
```

## 17. Security Requirements

- All external traffic must use TLS.
- MQTT must use `mqtts://` on port `8883` for public access.
- No anonymous MQTT auth.
- Each host must have isolated MQTT credentials.
- Each host may only subscribe/publish to allowed topics.
- Agent token must be hashed server-side.
- Install token should be one-time or short-lived.
- Job payload must be structured and must not be arbitrary command text.
- Agent must enforce local app/action allowlist.
- Production deploys can require manual approval.
- Secrets must not be printed in logs.
- Artifact checksum must be verified when artifact deployment is used.
- Webhook signatures must be verified.
- Audit log must record deploy, rollback, approval, host registration, token rotation, and failed auth.

## 18. Observability

### Agent Heartbeat

Agent sends periodic heartbeat:

```json
{
  "host_id": "srv_001",
  "agent_version": "0.1.0",
  "status": "online",
  "running_jobs": 0
}
```

### Logs

Agent should upload logs in batches over HTTPS.

MVP:

```text
POST /api/agent/jobs/:id/logs
```

Future:

```text
Live dashboard streaming via SSE/WebSocket
```

### Metrics

MVP metrics:

- deployment duration
- deployment success/failure count
- rollback count
- host online/offline state
- last heartbeat age
- queue depth

## 19. Failure Handling

### MQTT Disconnected

- Agent reconnects with exponential backoff.
- Agent sends heartbeat after reconnect.
- Agent may call HTTPS API to check pending jobs after reconnect.

### Job Signal Lost

MQTT is not the source of truth. Jobs remain in DB. Agent should periodically reconcile:

```text
GET /api/agent/jobs/pending
```

### Deploy Fails Before Activation

- Mark job failed.
- Keep current symlink unchanged.
- Keep failed release directory for debugging or cleanup policy.

### Deploy Fails After Activation

- Run healthcheck.
- If healthcheck fails and rollback policy is enabled, switch to previous release.
- Mark original deployment failed and rollback attempted.

### Agent Restarts During Job

- Agent records local job lock/state.
- On startup, agent reports interrupted job.
- Control plane decides whether to retry, mark failed, or require manual intervention.

## 20. MVP Scope

### Must Have

- Go control plane API.
- SQLite database.
- Host registration.
- Go agent as systemd service.
- Mosquitto MQTT broker integration.
- MQTT job signal.
- HTTPS job detail fetch.
- Manual deploy job creation.
- GitHub webhook support.
- Branch/tag deployment rules.
- App recipe with custom script support.
- Symlink release strategy.
- Rollback to retained release from dashboard.
- Basic logs and status.
- Audit log.

### Should Have

- SvelteKit or HTMX dashboard.
- Production deploy approval.
- Healthcheck support.
- Agent installer script.
- Token rotation.
- Configurable retention policy for old releases/logs.
- Dashboard release history with rollback to any retained successful release.

### Could Have

- GitLab webhook support.
- Artifact upload/download support.
- Built-in stack adapters.
- Live log streaming.
- Slack/Discord notification.
- Multi-user RBAC.

### Not MVP

- Kubernetes deploy.
- Docker Swarm management.
- Full CI build runners.
- Complex workflow DAG.
- Multi-region broker clustering.

## 21. Suggested Build Phases

### Phase 1: Local Prototype

- Go API with SQLite.
- Create job manually through API.
- Go agent connects to MQTT.
- Agent receives job signal.
- Agent fetches job detail.
- Agent runs local echo/custom script.
- Agent reports success/failure.

### Phase 2: Real Host Deploy

- Install agent via systemd.
- Add recipe file support.
- Add release directory strategy.
- Add service restart.
- Add healthcheck.
- Add rollback.

### Phase 3: Git Integration

- GitHub webhook.
- Branch/tag rule matching.
- Deploy from source or artifact.
- Log collection.
- Dashboard release history.
- Configurable release retention for dashboard rollback.

### Phase 4: Security and Operations

- MQTT TLS and ACL.
- Token hashing and rotation.
- Approval flow.
- Audit log dashboard.
- Agent version reporting.
- Release/log retention.

### Phase 5: Product Polish

- Multi-user RBAC.
- Live logs.
- Stack adapters.
- Notification integrations.
- Agent self-update.
- Postgres support.

## 22. Open Questions

- Should the first dashboard use SvelteKit or Go templ + HTMX?
- Should the MVP include source deploy only, artifact deploy only, or both?
- Should production deploy require approval by default?
- Should agent support self-update in v1 or later?
- Should Mosquitto be embedded in deployment docs only, or managed by the control plane installer?
- Should recipe commands be fully declarative or shell-first for v1?

## 23. Recommended MVP Decision

Recommended first implementation:

```text
Control Plane : Go + SQLite
Dashboard     : HTMX first, SvelteKit later if needed
MQTT Broker   : Mosquitto
Agent         : Go
Signal        : MQTT over TLS
Job Details   : HTTPS
Deploy Logic  : custom script first, YAML recipe next
Release Model : symlink releases
Runtime       : systemd
Proxy         : Caddy
```

This keeps the project small while preserving the architecture needed for a more serious deployment product later.
