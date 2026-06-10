# DockLab — Remote Dev Environment Manager

Phase-by-phase development plan.

Last updated: June 2026

---

## Project overview

### Goal

Build a cloud-based remote development environment platform where users can provision isolated development machines or containers from a browser.

### Current status (June 2026)

**All planned phases (1–10) are complete, including the former "avoid initially" stretch scope.** DockLab supports local and remote Docker workspaces with browser terminals, Terraform EC2 provisioning with SSH bootstrap, idle lifecycle automation, durable usage/cost tracking with budgets, production hardening (refresh tokens, OAuth, rate limits, quotas, metrics, alerts, secrets, CD), and the advanced feature set: browser IDE, workspace snapshots, environment sharing with collaborative terminals, GitHub repo auto-clone, a template marketplace, and an optional Kubernetes runtime backend.

**Core remote development works** when AWS credentials, Terraform state backend, and an EC2 SSH private key are configured. Remaining work is polish (see "Not implemented yet").

For sprint-level tracking, see [sprints.md](./sprints.md).

### Implemented

| Area | What exists |
|------|-------------|
| **Foundation** | Go (Gin) backend, React (Vite) frontend, PostgreSQL, Docker Compose, structured logging |
| **Authentication** | Register, login, JWT middleware, bcrypt, protected routes, CORS |
| **Local environments** | Create/list/start/stop/delete local Docker workspaces (`creation_mode = local`) |
| **Cloud environments** | Create cloud workspaces at create time (`creation_mode = cloud`); EC2 + remote bootstrap in one async flow |
| **Local → cloud upgrade** | Optional `POST .../provision` on local workspaces to attach EC2 |
| **Remote orchestration** | SSH + remote Docker lifecycle, post-provision bootstrap, runtime routing, remote health API |
| **Browser terminal** | WebSocket + PTY + xterm.js — local docker exec or remote SSH docker exec |
| **Terraform provisioning** | EC2 with SG + Docker user-data, S3 state + DynamoDB locking, async operations |
| **Operations** | Postgres-persisted operation queue; polling API; survives restarts |
| **Reconciliation** | Stale operation and provisioning-state repair (startup + every 5 min) |
| **Auto-sleep** | Idle workspace containers stopped after `IDLE_STOP_AFTER_MINUTES`; idle EC2 stopped/terminated per cloud lifecycle policy |
| **Cost tracking** | Persisted `environment_usage` sessions, AWS Pricing API rates (static fallback), per-environment billing rollups, monthly budgets + alerts |
| **Auth hardening** | Rotating JWT refresh tokens, logout/revocation, GitHub OAuth login |
| **Abuse protection** | Per-IP rate limiting (auth/provision/API budgets), per-user environment and operation quotas |
| **Observability** | Prometheus `/metrics`, webhook alerting (operations, lifecycle, reconciliation, budgets) |
| **Secrets** | Optional AWS Secrets Manager bootstrap at startup; production needs no `.env` (IAM-role credential chain, base64 SSH key from the secret, only `POSTGRES_PASSWORD` exported for the co-located DB) |
| **Browser IDE** | code-server sidecar per workspace sharing the `/workspace` volume (local + EC2 via port 8443) |
| **Snapshots** | `docker commit` snapshot/restore/delete; named workspace volumes survive restores |
| **Collaboration** | Environment sharing by email; multi-client shared terminal sessions |
| **GitHub integration** | OAuth login + `repo_url` auto-clone into `/workspace` at create |
| **Templates** | Curated template catalog API + dashboard picker |
| **Kubernetes** | Optional `DOKLAB_RUNTIME=kubernetes` backend (Deployments via kubectl; scale 0/1 stop/start) |
| **CI/CD** | GitHub Actions CI + CD (GHCR images, optional SSH deploy, `docker-compose.prod.yml`) |

### Not implemented yet

| Priority | Gap | Impact |
|----------|-----|--------|
| **P3** | Per-user lifecycle policy | Global env thresholds only |
| **P3** | Distributed rate limiting (Redis) | In-memory limiter is per-instance |
| **P3** | K8s snapshots/IDE + PVC persistence | Advanced features are Docker-only |
| **P3** | TLS/reverse proxy for IDE in production | IDE served over plain HTTP with password auth |
| **P4** | Managed runtime (ECS/Fly.io) | CD targets a Docker Compose host |

### Current focus

**Polish and operations.** All planned scope is delivered; remaining work is the polish list above plus real-world load testing.

---

## Recommended tech stack

### In use today

| Layer | Choice |
|-------|--------|
| Frontend | React 19, TypeScript, Tailwind CSS 4, React Router, Vite, xterm.js |
| Backend | Go 1.25, Gin, Gorilla WebSocket, creack/pty, pgx/v5, golang.org/x/crypto/ssh, prometheus/client_golang, aws-sdk-go-v2 (EC2, Pricing, Secrets Manager) |
| Database | PostgreSQL 16 |
| Infrastructure | Terraform 1.9, AWS EC2, Docker CLI (local + remote over SSH), optional kubectl (Kubernetes backend), code-server (browser IDE) |
| DevOps | Docker Compose (dev + prod), GitHub Actions (CI + CD → GHCR + SSH deploy), nginx (frontend image) |

### Planned / optional

| Layer | Choice | When |
|-------|--------|------|
| Caching / queues | Redis | If multi-replica rate limiting or async scale requires it |
| Dashboards | Grafana on top of `/metrics` | When operating a hosted instance |
| Managed runtime | ECS / Fly.io | If the Compose host outgrows its capacity |

### Deliberately not used

- TanStack Query — dashboard uses direct fetch + local state
- Zustand/Redux — not needed at current scale

---

## System architecture

### Current architecture

```text
React Frontend
       ↓ HTTP / WebSocket (token pair w/ auto-refresh; GitHub OAuth)
Go API Server (middleware: JWT, rate limiting, Prometheus metrics)
       ├── Docker CLI  →  Local workspace containers + code-server IDE sidecars (runtime_target = local)
       ├── kubectl     →  Kubernetes Deployments (DOKLAB_RUNTIME = kubernetes)
       └── Terraform CLI  →  AWS EC2 (Docker user-data, SSH + IDE security group)
              ↓ SSH + remote Docker CLI
         Workspace container + IDE sidecar on EC2 (runtime_target = remote)
PostgreSQL (users, environments, operations, refresh_tokens, environment_usage,
            user_settings, environment_shares, environment_snapshots)
Background workers: reconciliation, lifecycle (idle workspace/EC2 stop + terminate), budget watcher
Observability: /metrics (Prometheus) + webhook alerts; secrets via AWS Secrets Manager (optional)
```

---

## Repository structure (actual)

```text
cmd/server/           # API entrypoint
internal/
  handlers/           # HTTP + WebSocket handlers (auth, env, usage, snapshots, shares, IDE)
  services/           # Auth, OAuth, environment, terminal, terraform, lifecycle, reconciliation,
                      # usage, pricing, snapshots, shares, IDE, K8s runtime, metrics, alerts
  repositories/       # PostgreSQL access
  models/             # Domain types
  middleware/         # JWT, rate limiting, metrics
  config/             # Env config + AWS Secrets Manager loader
  database/           # Pool + schema bootstrap
pkg/logger/
frontend/             # React dashboard (+ production Dockerfile/nginx.conf)
plan/                 # This plan + sprints
.github/workflows/    # CI + CD
docker-compose.yml         # Dev stack
docker-compose.prod.yml    # Production stack (GHCR images)
Dockerfile            # Backend + Terraform CLI + kubectl
```

---

## Phase status overview

| Phase | Name | Status |
|-------|------|--------|
| 1 | Project foundation | ✅ Complete |
| 2 | Authentication | ✅ Complete (refresh tokens + GitHub OAuth) |
| 3 | Local Docker environments | ✅ Complete |
| 4 | Browser terminal | ✅ Complete (local, SSH remote, K8s exec; shared multi-client) |
| 5 | Terraform integration | ✅ Complete |
| 6 | Remote container orchestration | ✅ Complete |
| 7 | Auto-sleep & lifecycle automation | ✅ Complete |
| 8 | Cost tracking dashboard | ✅ Complete (persisted usage, Pricing API, budgets) |
| 9 | Production hardening | ✅ Complete (CD, rate limits, quotas, metrics, alerts, secrets) |
| 10 | Advanced features | ✅ Complete (IDE, snapshots, sharing, GitHub, templates, K8s) |

---

# PHASE 1 — Project Foundation ✅

## Goal
Set up architecture, local development environment, and basic frontend/backend communication.

## Delivered
- Monorepo layout (`cmd/`, `internal/`, `frontend/`)
- React + TypeScript + Tailwind frontend scaffold
- Go backend with Gin, structured logging, config, PostgreSQL
- Docker Compose for backend + PostgreSQL
- Health endpoint

## Success criteria — Met
- Frontend and backend communicate successfully
- Local development works with Docker Compose + Vite dev server

---

# PHASE 2 — Authentication & User Management ✅

## Goal
Implement secure user authentication and session management.

## Delivered
- User registration and login
- JWT authentication and middleware
- Password hashing with bcrypt
- Protected routes and session persistence in frontend
- `users` table: id, email, password_hash, created_at, updated_at

## Delivered later (Sprint 9)
- Rotating single-use JWT refresh tokens with logout/revocation (`refresh_tokens` table, SHA-256 hashed)
- GitHub OAuth login with signed-JWT state and user upsert (`auth_provider`)

## Success criteria — Met
- Users can securely authenticate; API routes are protected; sessions renew without re-login

---

# PHASE 3 — Local Docker Environment Manager ✅

## Goal
Create isolated local Docker workspaces before touching AWS.

## Delivered
- Docker CLI integration (`ContainerRuntime` interface)
- Environment CRUD and lifecycle APIs
- Per-user workspace isolation via `user_email`
- `environments` table with container_id, status, image, name

## Success criteria — Met
- Users create and manage isolated local containers; state persists in PostgreSQL

---

# PHASE 4 — Browser Terminal System ✅

## Goal
Implement real-time browser terminal access.

## Delivered
- WebSocket gateway with JWT auth
- PTY integration via creack/pty
- xterm.js frontend with FitAddon, reconnect, copy/paste
- Activity tracking (`last_activity_at`) on terminal connect and periodic refresh

## Limitation
Terminal attaches to **local** Docker containers only. Remote attach is Phase 6.

## Success criteria — Met (local scope)
- Fully interactive browser terminal works reliably for local workspaces

---

# PHASE 5 — Terraform Integration ✅

## Goal
Provision real AWS infrastructure automatically.

## Delivered
- Terraform CLI runner with generated workspaces per environment
- EC2 provisioning with security groups
- Remote state (S3 + DynamoDB locking)
- Async provision/destroy/delete operations with PostgreSQL persistence
- Cloud metadata on environments; typed validation errors
- Reconciliation for stale operations and stuck provisioning states
- Backend Docker image bundles Terraform 1.9

## Deferred from original scope
- Elastic IP (optional — not required for MVP)
- IAM role attachment beyond what Terraform module creates

## Success criteria — Met
- Users provision EC2 instances from the UI; metadata is stored and visible

---

# PHASE 6 — Remote Container Orchestration ✅

## Goal
Run Docker workspaces on provisioned EC2 machines and attach the browser terminal remotely.

## Delivered
- SSH client with `DOKLAB_SSH_PRIVATE_KEY_PATH`, configurable user/port/timeouts
- Terraform: security group (SSH), Docker install user-data, public IP association
- `SSHDockerRuntime` and `RuntimeResolver` for local vs remote routing
- Post-provision bootstrap with SSH/Docker wait (2s poll), EC2 user-data image pre-pull, phased progress in dashboard, remote container creation, runtime migration
- Remote terminal via SSH PTY + `docker exec`
- `GET /api/v1/environments/:id/remote-health`
- `runtime_target` and `cloud_key_name` on environments; destroy-cloud reverts local-upgraded envs to local Docker (cloud-created workspaces use delete only)

## Success criteria — Met
- Remote workspaces are operational; terminal sessions run on EC2 when provisioned
- Local flow unchanged when cloud is not provisioned

---

# PHASE 7 — Auto-Sleep & Lifecycle Automation ✅

## Goal
Prevent unnecessary cloud and local resource costs.

## Delivered
- `last_activity_at` tracking via terminal sessions
- Lifecycle worker stops idle workspace containers (local or remote) after configurable threshold
- Cloud lifecycle worker stops idle provisioned EC2, then terminates stopped EC2 after a longer threshold
- `cloud_stopped` status with **Start** to wake EC2 and restart remote workspace
- `GET /api/v1/lifecycle-policy`; dashboard idle policy summary and billing warnings
- Reconciliation repairs stale provisioning states and clears DB rows when EC2 instances no longer exist in AWS

## Remaining (optional)
- Per-user or per-environment lifecycle policy overrides
- CPU/network activity signals beyond terminal activity

## Success criteria — Met
- Idle environments and cloud resources are cleaned automatically
- No orphaned EC2 instances left running indefinitely without dashboard visibility

---

# PHASE 8 — Cost Tracking Dashboard ✅

## Goal
Provide visibility into infrastructure costs.

## Delivered
- `cloud_instance_type` and `cloud_provisioned_at` persisted on environments
- `environment_usage` table: per-session hourly rate, started/ended timestamps, runtime minutes, estimated cost
- Sessions open/close automatically as EC2 becomes billable (provision, start, stop, terminate, delete, reconciliation)
- AWS Pricing API integration with caching and a static on-demand fallback table
- Dashboard **Usage & Cost** view: month-to-date and all-time totals, per-environment cost bars, usage session history
- Monthly budgets (`user_settings`) with a budget watcher worker and webhook alerts on overruns

## Success criteria — Met
- Users understand historical usage and cost, not just live estimates

---

# PHASE 9 — Production Hardening ✅

## Goal
Make the platform production-quality and deployable.

## Delivered
- Structured JSON logging
- Health checks with DB connectivity
- GitHub Actions CI (fmt, tests, lint, build, Docker image build)
- GitHub Actions CD: GHCR image build/push (backend + frontend) and optional SSH deploy to a Docker Compose host (`docker-compose.prod.yml`)
- Per-IP token bucket rate limiting (auth / provisioning / general API budgets)
- Prometheus `/metrics`: HTTP, operations, environments, terminal sessions, lifecycle, alerts
- Webhook alerting for failed operations, lifecycle actions/failures, reconciliation repairs, and budget overruns
- AWS Secrets Manager bootstrap for production secrets
- Per-user resource quotas (max environments, max concurrent operations)
- JWT refresh tokens + GitHub OAuth (listed under Phase 2)
- Graceful shutdown with background worker cancellation
- Input validation on provisioning requests; repo URL validation hardened against shell injection
- Expanded unit test coverage (refresh tokens, rate limiter, pricing, templates, repo validation)

## Remaining (polish)
- Distributed rate limiting for multi-replica deployments
- Container isolation hardening and network restrictions
- Retry systems for transient Terraform/SSH failures
- Load testing under concurrent usage

## Success criteria — Met
- Platform is deployable to a hosted environment with monitoring, alerting, and abuse protections

---

# PHASE 10 — Advanced Features ✅

All former stretch goals are delivered:

| Feature | Status | Notes |
|---------|--------|-------|
| Browser VS Code | ✅ | code-server sidecar sharing the workspace volume; localhost port locally, port 8443 on EC2 |
| Kubernetes | ✅ | Optional `DOKLAB_RUNTIME=kubernetes` backend; Deployments via kubectl, scale 0/1 for stop/start |
| Workspace persistence | ✅ | `docker commit` snapshots with restore; named `/workspace` volumes survive restores |
| Collaborative sessions | ✅ | Environment sharing by email; multi-client shared terminal PTYs |
| GitHub integration | ✅ | OAuth login + `repo_url` auto-clone at create time |
| Template marketplace | ✅ | Curated catalog API + dashboard picker (Node.js, Go, Python, Rust, Java, base) |

Known boundaries: snapshots and the IDE sidecar require a Docker runtime (not available on the Kubernetes backend); IDE traffic is HTTP + password (put a TLS proxy in front for production).

---

## Scope summary

### Built ✅
- Authentication (passwords, refresh tokens, GitHub OAuth)
- Docker workspace creation (local + remote + optional Kubernetes)
- Browser terminal (local, remote over SSH, K8s exec; shared multi-client sessions)
- Browser IDE (code-server)
- Terraform EC2 provisioning with Docker bootstrap
- Auto-sleep (workspace containers + idle EC2 stop/terminate)
- Durable cost tracking (usage sessions, Pricing API, budgets, alerts)
- Workspace snapshots and environment sharing
- Templates and repo auto-clone
- Production hardening (rate limits, quotas, metrics, alerts, secrets)
- CI + CD (GHCR images, SSH deploy, prod compose)

### Polish backlog
1. Per-user lifecycle policies
2. Distributed rate limiting (Redis) for multi-replica deployments
3. K8s feature parity (PVC-backed persistence, IDE)
4. TLS reverse proxy for IDE access
5. Load testing and container isolation hardening

---

## Biggest engineering challenges

| # | Challenge | Status |
|---|-----------|--------|
| 1 | PTY + WebSocket streaming | ✅ Local, remote SSH, and K8s exec paths; multi-client broadcast |
| 2 | Infrastructure state management | ✅ Reconciliation + remote Terraform state + usage session hooks |
| 3 | Security isolation | ✅ Quotas + rate limits; container/network hardening remains polish |
| 4 | Cleanup logic | ✅ Workspace idle stop, EC2 idle stop/terminate, volume cleanup on delete |
| 5 | Concurrent session handling | 🟡 Works at current scale; needs load testing |
| 6 | Remote SSH + Docker reliability | ✅ Implemented; retry systems for transient failures remain polish |

---

## Related docs

- [README.md](../README.md) — setup, API reference, and limitations
- [sprints.md](./sprints.md) — sprint-level delivery tracking
