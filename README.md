# DockLab

Cloud-based remote development environment platform. Users authenticate, manage Docker workspaces locally, open a browser terminal, and optionally provision AWS EC2 infrastructure through Terraform.

<img width="1903" height="909" alt="image" src="https://github.com/user-attachments/assets/86ff5978-103c-4bb7-bff8-c441eac62b8e" />
<img width="1918" height="630" alt="image" src="https://github.com/user-attachments/assets/58856c24-a734-4b7f-ba4e-118c2f9ef2c0" />
<img width="1918" height="376" alt="image" src="https://github.com/user-attachments/assets/51a19ba1-e7d9-460e-acc7-b3c7c74c0297" />

## Current status

**MVP complete through Sprint 6 (June 2026).** The platform supports auth, local Docker workspaces, browser terminals, Terraform EC2 provisioning, idle auto-stop for local containers, and estimated cloud usage/cost visibility.

**Not yet viable as a full remote-dev product.** EC2 instances are provisioned and tracked, but workspaces and terminals still run on the local Docker host — not on the remote machine. See [Known limitations](#known-limitations) and the [project plan](plan/docklab_project_plan.md) for what remains.

## Architecture

```text
React Frontend (Vite + TypeScript + Tailwind)
    ↓
Go API Server (Gin)
    ↓
Terraform Runner Service (CLI, bundled in Docker image)
    ↓
AWS EC2 Instance (provisioned; remote workspace attach not yet wired)
    ↓
Docker Workspace Container (local host today)
    ↓
Browser Terminal via WebSockets (PTY + xterm.js)
```

## Repository layout

```text
cmd/server/                # Go API entrypoint
internal/
  handlers/                # HTTP and WebSocket handlers
  services/                # Business logic (auth, env, terminal, terraform, lifecycle, reconciliation)
  repositories/            # PostgreSQL data access
  models/                  # Domain models
  middleware/              # JWT auth middleware
  config/                  # Env-based configuration
  database/                # PostgreSQL pool and schema bootstrap
pkg/logger/                # Shared structured logger (log/slog)
frontend/                  # React + TypeScript + Tailwind dashboard
plan/                      # Project plan and sprint tracking
.github/workflows/         # CI (Go tests, frontend lint/build, Docker build)
```

## Backend features

- `/health` endpoint with database connectivity check
- PostgreSQL connection pool (`pgxpool`) with schema bootstrap on startup
- Environment variable configuration (see [Environment configuration](#environment-configuration))
- JWT auth: `/api/v1/auth/register`, `/api/v1/auth/login`, protected `/api/v1/auth/me`
- Password hashing with bcrypt
- Environment lifecycle APIs:
  - `POST /api/v1/environments` — create
  - `GET /api/v1/environments` — list
  - `GET /api/v1/environments/:id` — get one
  - `POST /api/v1/environments/:id/start` — start container
  - `POST /api/v1/environments/:id/stop` — stop container
  - `DELETE /api/v1/environments/:id` — delete environment (async; tears down cloud resources)
- Terraform provisioning: `POST /api/v1/environments/:id/provision`
- Cloud-only termination: `POST /api/v1/environments/:id/destroy-cloud`
- Async operation status: `GET /api/v1/operations/:id`
- Postgres-persisted operation tracking (survives backend restarts)
- Typed provisioning validation errors (`code` + `error`)
- Local Docker workspace lifecycle via `docker` CLI
- PTY-backed browser terminal (`GET /api/v1/environments/:id/terminal/ws`)
- Structured JSON logging (`log/slog`)
- Cloud drift/orphan reconciliation (runs on startup and every 5 minutes)
- Auto-sleep lifecycle worker: stops idle **local** running environments after `IDLE_STOP_AFTER_MINUTES` (default 60); terminal sessions refresh `last_activity_at` every 60 s
- Persisted cloud usage metadata (`cloud_instance_type`, `cloud_provisioned_at`) for dashboard cost estimation

## Frontend features

- Login, register, and JWT token persistence
- Protected `/dashboard` route
- Dashboard views: **Environments**, **Usage & Cost**, **Settings** (cloud provisioning defaults)
- Environment create/start/stop/delete/provision/terminate controls
- Async operation polling with progress feedback
- In-app confirmation modals for destructive actions
- xterm.js terminal panel with resize, reconnect, and copy/paste shortcuts

## Known limitations

These are intentional MVP boundaries documented in the sprint plan:

| Area | Current behavior | Needed for viability |
|------|------------------|----------------------|
| Remote workspaces | Terminal attaches to **local** Docker containers only | SSH into EC2 and run/manage remote Docker workspaces |
| Cloud auto-sleep | Idle policy stops local containers; **EC2 keeps running** | Stop or terminate idle EC2 instances |
| Cost tracking | Client-side estimates from hardcoded t3 on-demand rates | Persisted usage history; optional AWS Pricing API |
| Auth sessions | Single JWT with fixed TTL; no refresh tokens | Refresh tokens or shorter-lived access tokens |
| Production ops | Local Docker Compose dev setup | Deployment pipeline, monitoring, rate limiting, secrets management |
| CI/CD | GitHub Actions runs tests, lint, and Docker build on push/PR | Automated deployment to a hosted environment |

## Local development

### Prerequisites

- Go 1.25+
- Node.js 20+
- Docker + Docker Compose
- Terraform CLI (optional locally; bundled in the backend Docker image)

### 1) Start backend + PostgreSQL

```bash
docker compose up --build
```

This mounts the Docker socket into the backend container so environment lifecycle actions can manage local containers. The backend image includes the Terraform CLI.

Backend runs at `http://localhost:8080`.

### 2) Run frontend

```bash
cd frontend
npm install
npm run dev
```

Frontend runs at `http://localhost:5173`.

### Environment configuration

Copy and adjust:

```bash
cp .env.example .env
```

Used variables:

| Variable | Purpose |
|----------|---------|
| `APP_ENV` | Application environment label |
| `PORT` | HTTP listen port (default `8080`) |
| `DATABASE_URL` | PostgreSQL connection string |
| `JWT_SECRET` | Signing key for JWT tokens |
| `JWT_TTL_MINUTES` | Token lifetime in minutes |
| `AWS_ACCESS_KEY_ID` | Required for Terraform provisioning |
| `AWS_SECRET_ACCESS_KEY` | Required for Terraform provisioning |
| `AWS_SESSION_TOKEN` | Optional, for temporary credentials |
| `AWS_DEFAULT_REGION` | Optional, default `us-east-1` |
| `DOKLAB_TERRAFORM_STATE_BUCKET` | S3 bucket for remote Terraform state |
| `DOKLAB_TERRAFORM_STATE_REGION` | Region for the state bucket |
| `DOKLAB_TERRAFORM_STATE_TABLE` | DynamoDB table for state locking (`LockID` string partition key) |
| `DOKLAB_TERRAFORM_STATE_KEY_PREFIX` | Optional, default `docklab/environments` |
| `IDLE_STOP_AFTER_MINUTES` | Optional, default `60` — local container idle timeout |

## How to use

1. Start the backend and PostgreSQL with Docker Compose.
2. Start the frontend dev server from the `frontend/` folder.
3. Open `http://localhost:5173` in your browser.
4. Create an account on `/register` and sign in on `/login`.
5. Use the dashboard to create and manage local Docker environments.
6. Open **Terminal** on a running environment to run shell commands from the browser.
7. Set cloud defaults under **Settings**, then use **Provision** to trigger Terraform-based EC2 provisioning.
8. Use **Terminate EC2** to destroy cloud resources while keeping the environment record.
9. Use **Delete** to remove both the environment and any provisioned cloud resources.
10. Long-running cloud actions run asynchronously; the dashboard polls operation status until completion.
11. Operation status is persisted in PostgreSQL, so polling survives backend restarts.
12. Running environments with no terminal activity for longer than `IDLE_STOP_AFTER_MINUTES` are automatically stopped locally.
13. The **Usage & Cost** view estimates EC2 runtime spend for provisioned environments.

Terminal tips:

- Copy selected text with `Ctrl+Shift+C`.
- Paste with `Ctrl+Shift+V`.
- Use **Reconnect** in the terminal panel if the WebSocket drops.

## Validation commands

Backend:

```bash
go test ./cmd/... ./internal/... ./pkg/...
```

Frontend:

```bash
cd frontend
npm run lint
npm run build
```

CI runs the same checks on every push and pull request via `.github/workflows/ci.yml`.

## Terraform state backend setup

Before provisioning, create or point DockLab at:

- An S3 bucket for Terraform state storage
- A DynamoDB table for state locking with `LockID` as the partition key

Then set the `DOKLAB_TERRAFORM_STATE_*` variables in your `.env`.

## End-to-end test checklist

1. Start PostgreSQL and the backend with `docker compose up --build`.
2. Start the frontend with `cd frontend && npm run dev`.
3. Log in and create a local environment.
4. Open the terminal and confirm shell access in the local container.
5. Provision with a valid AWS region, AMI, instance type, and credentials.
6. Confirm the operation advances from `queued` → `running` → `succeeded`.
7. Verify the environment card shows `provisioned`, instance ID, and public IP.
8. Verify **Usage & Cost** shows the provisioned environment and a non-zero rate estimate for supported instance types.
9. Use **Terminate EC2** and confirm cloud resources are removed while the environment remains.
10. Use **Delete** and confirm both the environment row and EC2 resources are removed.

## Roadmap

See [plan/sprints.md](plan/sprints.md) for sprint-level tracking and [plan/docklab_project_plan.md](plan/docklab_project_plan.md) for the full phase plan.

**Next priorities for a viable product:**

1. **Sprint 7 — Remote orchestration:** SSH into provisioned EC2, deploy workspace containers remotely, attach the browser terminal to remote shells.
2. **Sprint 8 — Cloud lifecycle automation:** Auto-stop or terminate idle EC2 instances; close the cost leak from orphaned cloud resources.
3. **Sprint 9 — Production hardening & deployment:** Rate limiting, monitoring, secrets management, and a deployment pipeline beyond CI.
