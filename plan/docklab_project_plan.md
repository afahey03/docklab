# DockLab — Remote Dev Environment Manager

Phase-by-phase development plan.

Last updated: June 2026

---

## Project overview

### Goal

Build a cloud-based remote development environment platform where users can provision isolated development machines or containers from a browser.

### Current status (June 2026)

**Sprints 1–6 are complete.** DockLab is a working MVP for local Docker workspaces with browser terminals, Terraform EC2 provisioning, idle local auto-stop, estimated cloud cost visibility, and CI quality gates.

**The product is not yet fully viable.** EC2 is provisioned and tracked, but workspaces and terminals still run on the local Docker host. The highest-priority remaining work is remote orchestration (Phase 6 / Sprint 7), followed by cloud lifecycle automation and production hardening.

For sprint-level tracking, see [sprints.md](./sprints.md).

### Implemented

| Area | What exists |
|------|-------------|
| **Foundation** | Go (Gin) backend, React (Vite) frontend, PostgreSQL, Docker Compose, structured logging |
| **Authentication** | Register, login, JWT middleware, bcrypt, protected routes, CORS |
| **Local environments** | Create/list/start/stop/delete Docker workspaces per user |
| **Browser terminal** | WebSocket + PTY + xterm.js with resize, reconnect, copy/paste |
| **Terraform provisioning** | EC2 provision/destroy, remote S3 state + DynamoDB locking, async operations, validation errors |
| **Operations** | Postgres-persisted operation queue; polling API; survives restarts |
| **Reconciliation** | Stale operation and provisioning-state repair (startup + every 5 min) |
| **Auto-sleep (local)** | Idle local containers stopped after `IDLE_STOP_AFTER_MINUTES` |
| **Cost visibility** | Dashboard usage/cost estimates from `cloud_instance_type` + `cloud_provisioned_at` |
| **CI** | GitHub Actions: Go fmt/tests, frontend lint/build, Docker build |

### Not implemented yet (required for viability)

| Priority | Gap | Impact |
|----------|-----|--------|
| **P0** | Remote orchestration over SSH | EC2 exists but is not used as a workspace host |
| **P1** | Cloud auto-stop/terminate for idle EC2 | Provisioned instances can run indefinitely |
| **P2** | Production deployment (CD) | No hosted environment; dev-only Docker Compose |
| **P2** | Monitoring, alerting, rate limiting | Not operable under real load or abuse |
| **P3** | Persisted usage history and accurate pricing | Cost view is live estimate only |
| **P3** | JWT refresh tokens | Sessions expire without renewal |
| **Future** | IDE in browser, K8s, templates, collaboration | Stretch goals |

### Current focus

**Sprint 7 — Remote container orchestration.** Wire provisioned EC2 into the workspace and terminal flow so DockLab delivers actual remote development, not just local Docker with optional cloud metadata.

---

## Recommended tech stack

### In use today

| Layer | Choice |
|-------|--------|
| Frontend | React 19, TypeScript, Tailwind CSS 4, React Router, Vite, xterm.js |
| Backend | Go 1.25, Gin, Gorilla WebSocket, creack/pty, pgx/v5 |
| Database | PostgreSQL 16 |
| Infrastructure | Terraform 1.9, AWS EC2, Docker CLI |
| DevOps | Docker Compose, GitHub Actions (CI) |

### Planned / optional

| Layer | Choice | When |
|-------|--------|------|
| Remote access | SSH (golang.org/x/crypto/ssh or similar) | Sprint 7 |
| Caching / queues | Redis | Optional, if async scale requires it |
| Monitoring | Prometheus + Grafana or CloudWatch | Sprint 9 |
| Deployment | GitHub Actions CD → hosted runtime | Sprint 9 |
| Reverse proxy | Nginx or ALB | Sprint 9 |

### Deliberately not used (yet)

- TanStack Query — dashboard uses direct fetch + local state
- Zustand/Redux — not needed at current scale
- Kubernetes — Docker is sufficient for MVP

---

## System architecture

### Today

```text
React Frontend
       ↓ HTTP / WebSocket
Go API Server
       ├── Docker CLI  →  Local workspace containers  →  Browser terminal
       └── Terraform CLI  →  AWS EC2 (metadata only; not yet wired to terminal)
PostgreSQL (users, environments, operations)
Background workers: reconciliation, lifecycle (local idle stop)
```

### Target (after Phase 6)

```text
React Frontend
       ↓
Go API Server
       ↓
Terraform Runner  →  AWS EC2
       ↓ SSH
Remote Docker  →  Workspace container on EC2
       ↓
Browser Terminal via WebSockets
```

---

## Repository structure (actual)

```text
cmd/server/           # API entrypoint
internal/
  handlers/           # HTTP + WebSocket handlers
  services/           # Auth, environment, terminal, terraform, lifecycle, reconciliation
  repositories/       # PostgreSQL access
  models/             # Domain types
  middleware/         # JWT
  config/             # Env config
  database/           # Pool + schema bootstrap
pkg/logger/
frontend/             # React dashboard
plan/                 # This plan + sprints
.github/workflows/    # CI
docker-compose.yml
Dockerfile            # Backend + Terraform CLI
```

---

## Phase status overview

| Phase | Name | Status |
|-------|------|--------|
| 1 | Project foundation | ✅ Complete |
| 2 | Authentication | ✅ Complete (refresh tokens deferred) |
| 3 | Local Docker environments | ✅ Complete |
| 4 | Browser terminal | ✅ Complete (local only) |
| 5 | Terraform integration | ✅ Complete (MVP slice) |
| 6 | Remote container orchestration | 🔲 **Not started — next** |
| 7 | Auto-sleep & lifecycle automation | 🟡 Partial (local only) |
| 8 | Cost tracking dashboard | 🟡 Partial (estimates only) |
| 9 | Production hardening | 🟡 Partial (CI only; no CD/monitoring) |
| 10 | Advanced features | 🔲 Future |

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

## Deferred
- Refresh tokens
- OAuth / GitHub login

## Success criteria — Met
- Users can securely authenticate; API routes are protected

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

# PHASE 6 — Remote Container Orchestration 🔲 NEXT

## Goal
Run Docker workspaces on provisioned EC2 machines and attach the browser terminal remotely.

## Why this is critical
Without Phase 6, DockLab provisions cloud infrastructure that the product never uses. This is the single largest gap between the current MVP and a viable remote dev platform.

## Features to build
- SSH client and credential/key management for provisioned instances
- EC2 bootstrap: ensure Docker is installed and reachable
- Remote Docker commands (create/start/stop/delete) over SSH
- Terminal routing: when `cloud_status = provisioned`, attach PTY to remote container
- Health monitoring: EC2 reachability, remote Docker daemon status
- Fallback: local Docker flow when cloud is not provisioned

## Example flow

```text
Provision EC2 (Phase 5 — done)
      ↓
SSH into instance
      ↓
Ensure Docker is running
      ↓
Launch workspace container on EC2
      ↓
Attach browser terminal to remote shell
```

## Backend tasks
- SSH client implementation
- Remote `ContainerRuntime` implementation (parallel to local Docker CLI)
- Environment service: select local vs remote runtime based on cloud state
- Startup/user-data scripts for EC2 bootstrap

## Success criteria
- Remote workspaces are fully operational; terminal sessions run on EC2

---

# PHASE 7 — Auto-Sleep & Lifecycle Automation 🟡 PARTIAL

## Goal
Prevent unnecessary cloud and local resource costs.

## Delivered (local only)
- `last_activity_at` tracking via terminal sessions
- Lifecycle worker stops idle **local** running containers after configurable threshold
- Reconciliation repairs stale provisioning states and orphaned operations

## Remaining
- Idle detection for provisioned EC2 (stop or terminate)
- Configurable tiered policies (e.g. idle 15 min → stop container; idle 1 hr → stop EC2; idle 24 hr → terminate)
- Scheduled cleanup for environments marked deleted but with lingering cloud resources
- CPU/network activity signals (optional enhancement beyond terminal activity)

## Success criteria (full)
- Idle environments and cloud resources are cleaned automatically
- No orphaned EC2 instances left running indefinitely

---

# PHASE 8 — Cost Tracking Dashboard 🟡 PARTIAL

## Goal
Provide visibility into infrastructure costs.

## Delivered
- `cloud_instance_type` and `cloud_provisioned_at` persisted on environments
- Dashboard **Usage & Cost** view with accrued spend and monthly run-rate estimates
- Hardcoded on-demand rates for common t3 instance types

## Remaining

```sql
environment_usage  -- not yet created
- environment_id
- runtime_minutes
- estimated_cost
- started_at
- ended_at
```

- Usage history UI with charts
- AWS Pricing API for regional accuracy
- Cost alerts or budget thresholds

## Success criteria (full)
- Users understand historical usage and cost, not just live estimates

---

# PHASE 9 — Production Hardening 🟡 PARTIAL

## Goal
Make the platform production-quality and deployable.

## Delivered
- Structured JSON logging
- Health checks with DB connectivity
- GitHub Actions CI (fmt, tests, lint, build, Docker image build)
- Graceful shutdown with background worker cancellation
- Input validation on provisioning requests
- In-app confirmation for destructive actions

## Remaining
- Deployment pipeline (CD)
- Rate limiting
- Metrics and alerting (Prometheus/Grafana or CloudWatch)
- Secrets management (AWS Secrets Manager, SSM, or similar)
- Resource quotas per user
- Container isolation hardening and network restrictions
- Expanded integration test coverage
- Retry systems for transient Terraform/SSH failures

## Success criteria
- Platform is stable and operable under concurrent usage in a hosted environment

---

# PHASE 10 — Advanced Features (Future)

Optional enhancements after the core product is viable:

| Feature | Description |
|---------|-------------|
| Browser VS Code | code-server integration |
| Kubernetes | Replace Docker orchestration with K8s scheduling |
| Workspace persistence | Snapshot and restore environments |
| Collaborative sessions | Shared terminals, multi-user workspaces |
| GitHub integration | OAuth login, auto-clone repositories |
| Template marketplace | Prebuilt Node.js, Go, Python environments |

---

## Recommended MVP scope

### Built ✅
- Authentication
- Docker workspace creation (local)
- Browser terminal (local)
- Terraform EC2 provisioning
- Auto-sleep (local containers)
- Cost estimates (live)
- CI quality gates

### Build next (viability)
1. Remote orchestration (Phase 6)
2. Cloud lifecycle automation (Phase 7 completion)
3. Production deployment and hardening (Phase 9 completion)

### Avoid initially
- Kubernetes
- Full billing systems
- Multi-user collaboration
- Full IDE in browser

---

## Biggest engineering challenges

| # | Challenge | Status |
|---|-----------|--------|
| 1 | PTY + WebSocket streaming | ✅ Solved for local; remote path TBD |
| 2 | Infrastructure state management | ✅ Reconciliation + remote Terraform state |
| 3 | Security isolation | 🟡 Basic; needs quotas and hardening |
| 4 | Cleanup logic | 🟡 DB/Terraform reconciliation done; EC2 idle cleanup missing |
| 5 | Concurrent session handling | 🟡 Works at MVP scale; needs load testing |
| 6 | Remote SSH + Docker reliability | 🔲 Not started |

---

## Resume description

> Built a cloud-based remote development platform using React, Go, Terraform, Docker, and AWS EC2. Implemented browser-based terminal streaming over WebSockets, automated infrastructure provisioning, containerized developer environments, lifecycle automation, async operation tracking, and real-time cost estimation. CI pipeline with GitHub Actions.

---

## Related docs

- [README.md](../README.md) — setup, API reference, and limitations
- [sprints.md](./sprints.md) — sprint-level delivery tracking
