# DockLab Sprint Plan

Last updated: June 2026

This file tracks sprint-level delivery. For phase-level architecture and long-term scope, see [docklab_project_plan.md](./docklab_project_plan.md).

---

## Sprint 1 ✅ Authentication Foundation

### Goal
Deliver a functional, secure login flow from React frontend to Go backend.

### Delivered
- Backend user persistence in PostgreSQL (`users` table bootstrap on startup)
- `POST /api/v1/auth/register`, `POST /api/v1/auth/login`, `GET /api/v1/auth/me`
- Password hashing and verification with bcrypt
- JWT-based session token issuance and middleware protection
- Frontend login/register forms, token persistence, protected dashboard route, sign-out
- Backend CORS support for local frontend development

### Definition of Done — Met
- A user can register, log in, and access the dashboard while authenticated.
- `/api/v1/auth/me` returns the current authenticated user email.
- Unauthenticated users are redirected to `/login` when requesting `/dashboard`.

---

## Sprint 2 ✅ Local Environment Lifecycle

### Goal
Create and manage per-user local Docker workspaces.

### Delivered
- `environments` table and repository/service layers
- Environment APIs: create, list, get, start, stop, delete
- Docker CLI integration for container lifecycle
- Dashboard environment list and lifecycle actions

### Definition of Done — Met
- Authenticated users can create and manage isolated local containers.
- Environment state persists in PostgreSQL.

---

## Sprint 3 ✅ Browser Terminal

### Goal
Provide real-time interactive terminal access to running workspaces.

### Delivered
- Backend WebSocket terminal gateway (`/api/v1/environments/:id/terminal/ws`)
- PTY session management and resize handling
- xterm.js terminal panel with live shell streaming
- Connection lifecycle handling (reconnect/disconnect cleanup)
- Copy/paste shortcuts and reconnect UX

### Definition of Done — Met
- User can open a browser terminal and run shell commands in a workspace container.
- Terminal supports resize events and PTY behavior for interactive shells.

---

## Sprint 4 ✅ Terraform Provisioning (MVP Slice)

### Goal
Provision AWS EC2 instances from the platform.

### Delivered
- Terraform CLI runner service with generated execution workspaces
- Terraform bundled in the backend Docker image
- Durable remote Terraform state backend with DynamoDB locking
- Provision, destroy-cloud, and delete-with-teardown flows
- Persisted cloud metadata: status, region, instance type, instance ID, public IP, errors
- Async operation queue with PostgreSQL persistence and frontend polling
- Backend provisioning validation with typed API error codes
- In-app confirmation modals for destructive actions
- Cloud drift/orphan reconciliation service (startup + every 5 min):
  - Operations stuck in `queued`/`running` > 30 min → marked `failed`
  - Environments stuck in `provisioning`/`deprovisioning` > 30 min with no active operation → marked `provision_failed`

### Definition of Done — Met
- User-triggered provisioning creates EC2 resources and stores metadata in DockLab.
- Provisioning status, region, instance ID, and public IP are visible on environment cards.

---

## Sprint 5 ✅ Auto-Sleep and Local Cleanup

### Goal
Prevent idle local resource waste.

### Delivered
- `last_activity_at` column on environments, updated on terminal connect and every 60 s during active sessions
- Lifecycle worker: scans for running environments idle longer than `IDLE_STOP_AFTER_MINUTES` (default 60) and stops their **local** Docker container
- EC2 instances are intentionally left running (cloud lifecycle is Sprint 8)

### Definition of Done — Met
- Idle local environments are automatically stopped according to policy.
- Stale cloud provisioning states are auto-corrected without manual intervention.

---

## Sprint 6 ✅ Cost Visibility and CI

### Goal
Give users visibility into cloud runtime and estimated EC2 spend; establish automated quality gates.

### Delivered
- Persist `cloud_instance_type` and `cloud_provisioned_at` after successful EC2 provisioning
- Dashboard **Usage & Cost** view: active cloud count, accrued spend estimate, projected monthly run rate
- Per-environment runtime and hourly/accrued spend for common t3 instance types (hardcoded on-demand rates)
- Dashboard **Settings** view for cloud provisioning defaults (region, instance type, AMI, key pair)
- GitHub Actions CI on push/PR: Go fmt + tests, frontend lint + build, Docker image build

### Definition of Done — Met
- Users can see estimated cloud spend for provisioned environments.
- CI catches regressions before merge.

### Explicitly out of scope (deferred)
- Persisted usage history table
- Cost charts and billing export
- AWS Pricing API integration
- Production deployment (CD)

---

## Sprint 7 ✅ Remote Container Orchestration

### Goal
Make provisioned EC2 instances usable as actual remote development workspaces.

### Delivered
- SSH client (`golang.org/x/crypto/ssh`) with configurable user, port, and private key path
- Terraform EC2 template: security group (SSH ingress), Docker install user-data, public IP
- `SSHDockerRuntime`: remote container create/start/stop/delete over SSH
- `RuntimeResolver`: routes lifecycle and terminal to local or remote based on `runtime_target`
- Post-provision bootstrap: wait for SSH + Docker (2s poll), pre-pull workspace image in EC2 user-data, phased progress messages, create remote workspace, remove local container, set `runtime_target = remote`
- Remote browser terminal: SSH PTY session running `docker exec` on EC2
- `GET /api/v1/environments/:id/remote-health` — SSH reachability and Docker daemon checks
- Environment model: `runtime_target`, `cloud_key_name` columns
- Destroy-cloud flow reverts workspace to local Docker before tearing down EC2
- Dashboard: runtime target display, required key pair for provision, **Check remote health** button
- Idle auto-stop works for remote containers (EC2 instance still left running — Sprint 8)

### Definition of Done — Met
- After provisioning EC2, a user can open a browser terminal connected to the remote workspace container.
- Local-only flow continues to work when cloud is not provisioned.

### Post-Sprint 7 enhancement ✅ Create-time local vs cloud
- `POST /api/v1/environments` accepts `target` (`local` | `cloud`) and optional `provision` payload
- Cloud create skips local Docker; provisions EC2 asynchronously (`202` + operation)
- `creation_mode` column distinguishes cloud-created vs local-created environments
- Local workspaces can still be upgraded via **Upgrade to cloud**; cloud-created envs cannot re-provision or use **Terminate EC2** (delete only)
- Dashboard create form: **Local workspace** / **Cloud workspace (EC2)** toggle with inline cloud settings
- **Upgrade to cloud** modal for local workspaces (replaces Settings tab)

---

## Sprint 8 ✅ Cloud Lifecycle Automation

### Goal
Stop paying for idle cloud resources.

### Delivered
- `CloudLifecycleService` background worker: idle workspace stop (existing) + idle EC2 stop + idle EC2 terminate
- AWS EC2 API integration for stop/start (`AWSEC2InstanceClient`)
- New `cloud_stopped` status; **Start** wakes stopped EC2 and restarts remote workspace
- Env-configurable thresholds: `IDLE_STOP_AFTER_MINUTES`, `IDLE_CLOUD_STOP_AFTER_MINUTES`, `IDLE_CLOUD_TERMINATE_AFTER_MINUTES`, `DOKLAB_CLOUD_IDLE_POLICY_ENABLED`
- `GET /api/v1/lifecycle-policy` for dashboard policy visibility
- Dashboard idle policy summary, billing warning when workspace stopped but EC2 running, `cloud_stopped` indicator
- Reconciliation clears environments whose EC2 instances no longer exist in AWS

### Definition of Done — Met
- Idle provisioned environments no longer leave EC2 running indefinitely.
- Users can see the idle cloud policy applied to their environments (env-configured thresholds).

---

## Sprint 9 ✅ Production Hardening and Deployment

### Goal
Move from a local dev demo to something deployable and operable in production.

### Delivered
- CD pipeline (`.github/workflows/cd.yml`): backend + frontend images built and pushed to GHCR on `main`, optional SSH deploy that pulls and restarts `docker-compose.prod.yml` on the host
- `docker-compose.prod.yml` (GHCR images, nginx-served frontend, Postgres with password) and `frontend/Dockerfile` + `nginx.conf`
- Per-IP token bucket rate limiting with separate budgets: auth (`20/min`), provisioning (`10/min`), general API (`240/min`) — configurable, returns `429` + `Retry-After`
- Prometheus `/metrics`: HTTP request counts/latency, operation outcomes, environments created, terminal clients, lifecycle actions, alerts
- Webhook alerting (`DOKLAB_ALERT_WEBHOOK_URL`): failed operations, cloud lifecycle actions/failures, reconciliation repairs, budget overruns
- AWS Secrets Manager bootstrap (`DOKLAB_SECRETS_MANAGER_SECRET_ID`): hydrates env vars from a JSON secret before config load
- `.env`-free production: EC2/Pricing clients use the default AWS credential chain (IAM instance profiles work with no static keys), the EC2 SSH key can be delivered base64-encoded via `DOKLAB_SSH_PRIVATE_KEY_B64` (materialized to a `0600` file at boot), and `docker-compose.prod.yml` only requires `POSTGRES_PASSWORD` exported on the host; the backend refuses to start in production with the default JWT secret
- Per-user quotas: max environments and max concurrent operations with typed `429` API errors
- JWT refresh tokens: rotating single-use tokens (SHA-256 hashed at rest), `POST /auth/refresh`, `POST /auth/logout`, frontend auto-refresh on `401`
- GitHub OAuth login: `GET /auth/github/login` + callback, signed-JWT state, user upsert (`auth_provider`), tokens delivered via URL fragment
- Expanded test coverage: refresh rotation/revocation, rate limiter behavior, pricing fallback/caching, repo URL validation, template catalog

### Definition of Done — Met
- Platform is deployable to a hosted Docker Compose environment with automated image delivery, metrics, and alerting.
- Abuse (rate limits, quotas) and cost-runaway (budgets, idle lifecycle) protections are in place.

---

## Sprint 10 ✅ Cost Tracking Hardening

### Goal
Turn cost estimates into durable, auditable usage data.

### Delivered
- `environment_usage` table: per-session hourly rate, started/ended timestamps, runtime minutes, estimated cost; sessions open/close automatically across provision, start, stop, terminate, delete, and reconciliation
- AWS Pricing API integration with in-memory caching and a static on-demand rate fallback (`GET /api/v1/pricing`)
- Usage history UI: session table, month-to-date and all-time totals, per-environment cost bars for the current month
- Budgets: `user_settings` table, `GET/PUT /api/v1/billing/budget`, budget watcher worker raising once-per-month webhook alerts, over-budget indicator in the dashboard

### Definition of Done — Met
- Users can review historical usage and cost per environment, not just live estimates.

---

## Sprint 11 ✅ Advanced Features (former "avoid initially" scope)

### Goal
Deliver the Phase 10 stretch features: IDE, snapshots, collaboration, GitHub repos, templates, Kubernetes.

### Delivered
- **Browser IDE**: code-server sidecar per workspace (`POST /environments/:id/ide/start|stop`, status endpoint); shares the workspace volume; random password per start; localhost port mapping locally, port `8443` on EC2 (Terraform SG updated); dashboard Manage panel shows URL + password
- **Workspace snapshots**: `docker commit`-based snapshot/restore/delete APIs and UI; workspaces now mount a named volume (`docklab-ws-<name>`) at `/workspace` preserved across restores; unsupported on the Kubernetes backend
- **Collaboration**: `environment_shares` table, share/unshare APIs, `shared_environments` in the list response, "Shared with you" dashboard section, and multi-client shared terminal sessions (one PTY broadcast to all connected clients)
- **GitHub integration**: OAuth login (Sprint 9) plus `repo_url` auto-clone into `/workspace` at create time (https-only, shell-injection-safe validation)
- **Template marketplace**: curated template catalog (`GET /api/v1/templates`) with Node.js, Go, Python, Rust, Java, and base images; dashboard template picker
- **Kubernetes runtime**: `DOKLAB_RUNTIME=kubernetes` schedules local workspaces as `kubectl` Deployments (stop/start = scale 0/1, terminal via `kubectl exec`); kubectl bundled in the backend image

### Definition of Done — Met
- Every Phase 10 feature is usable end to end from the dashboard with documented limitations (IDE/snapshots are Docker-only).

---

## Summary

| Sprint | Status | Theme |
|--------|--------|-------|
| 1 | ✅ Done | Authentication |
| 2 | ✅ Done | Local Docker lifecycle |
| 3 | ✅ Done | Browser terminal |
| 4 | ✅ Done | Terraform EC2 provisioning |
| 5 | ✅ Done | Local auto-sleep |
| 6 | ✅ Done | Cost visibility + CI |
| 7 | ✅ Done | Remote orchestration |
| 8 | ✅ Done | Cloud lifecycle automation |
| 9 | ✅ Done | Production hardening + deployment |
| 10 | ✅ Done | Durable cost tracking |
| 11 | ✅ Done | Advanced features (IDE, snapshots, sharing, templates, K8s) |
