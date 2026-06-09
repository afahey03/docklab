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
- Local workspaces can still be upgraded via **Upgrade to cloud**; cloud-created envs cannot re-provision
- Dashboard create form: **Local workspace** / **Cloud workspace (EC2)** toggle with inline cloud settings
- **Upgrade to cloud** modal for local workspaces (replaces Settings tab)

---

## Sprint 8 🔲 Cloud Lifecycle Automation

### Goal
Stop paying for idle cloud resources.

### Scope
- Extend idle detection to provisioned EC2 instances (not just local containers)
- Configurable policies: idle → stop EC2, idle longer → terminate EC2
- Separate policies for local container vs cloud resource lifecycle
- Dashboard indicators when cloud resources are running but workspace is idle
- Reconciliation improvements for orphaned EC2 instances (Terraform state vs DB drift)

### Definition of Done
- Idle provisioned environments no longer leave EC2 running indefinitely.
- Users can configure or see the idle cloud policy applied to their environments.

---

## Sprint 9 🔲 Production Hardening and Deployment

### Goal
Move from a local dev demo to something deployable and operable in production.

### Scope
- Deployment pipeline (CD): staged deploy to a hosted environment (e.g. ECS, Fly.io, or EC2 + Docker Compose)
- Rate limiting on auth and provisioning endpoints
- Structured metrics and health dashboards (Prometheus/CloudWatch or equivalent)
- Alerting on failed operations, reconciliation events, and worker errors
- Secrets management (no plaintext AWS keys in env files for production)
- Resource quotas per user (max environments, max concurrent operations)
- Expanded test coverage for environment, terraform, and operation flows
- Optional: JWT refresh tokens

### Definition of Done
- Platform runs in a non-local hosted environment with monitoring and automated deploys.
- Basic abuse and cost-runaway protections are in place.

---

## Sprint 10 🔲 Cost Tracking Hardening (Stretch)

### Goal
Turn cost estimates into durable, auditable usage data.

### Scope
- `environment_usage` table: runtime minutes, estimated cost, started/ended timestamps
- Usage history UI with charts and export
- AWS Pricing API integration for accurate regional rates
- Cost alerts or budget thresholds

### Definition of Done
- Users can review historical usage and cost per environment, not just live estimates.

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
| 8 | 🔲 Next | Cloud lifecycle automation |
| 9 | 🔲 Planned | Production hardening + deployment |
| 10 | 🔲 Stretch | Durable cost tracking |
