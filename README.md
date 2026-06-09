# DockLab

Cloud-based remote development environment platform. Users authenticate, manage Docker workspaces locally, open a browser terminal, and optionally provision AWS EC2 infrastructure through Terraform.

<img width="1903" height="909" alt="image" src="https://github.com/user-attachments/assets/86ff5978-103c-4bb7-bff8-c441eac62b8e" />
<img width="1918" height="630" alt="image" src="https://github.com/user-attachments/assets/58856c24-a734-4b7f-ba4e-118c2f9ef2c0" />
<img width="1918" height="376" alt="image" src="https://github.com/user-attachments/assets/51a19ba1-e7d9-460e-acc7-b3c7c74c0297" />

## Current status

**MVP complete through Sprint 7 (June 2026).** The platform supports auth, local and remote Docker workspaces, browser terminals (local or over SSH), Terraform EC2 provisioning with post-provision bootstrap, idle auto-stop, estimated cloud usage/cost visibility, and remote health checks.

**Core remote-dev flow works when AWS and SSH are configured.** At create time, choose a **local Docker workspace** or a **cloud workspace (EC2)**. Cloud environments provision EC2 and bootstrap the remote container in one async flow — no throwaway local container first. Local environments can optionally be upgraded to EC2 later. See [Known limitations](#known-limitations) for remaining gaps.

## Architecture

```text
React Frontend (Vite + TypeScript + Tailwind)
    ↓
Go API Server (Gin)
    ├── Docker CLI  →  Local workspace containers (when target = local)
    └── Terraform CLI  →  AWS EC2 (when target = cloud or upgrade from local)
              ↓ SSH + remote Docker CLI
         Workspace container on EC2 (runtime_target = remote)
    ↓
Browser Terminal via WebSockets (local PTY or SSH PTY + xterm.js)
```

## Repository layout

```text
cmd/server/                # Go API entrypoint
internal/
  handlers/                # HTTP and WebSocket handlers
  services/                # Business logic (auth, env, terminal, terraform, SSH remote runtime, lifecycle, reconciliation)
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
  - `POST /api/v1/environments` — create (`target`: `local` or `cloud`; cloud requires `provision` payload; returns `201` for local, `202` with operation for cloud)
  - `GET /api/v1/environments` — list
  - `GET /api/v1/environments/:id` — get one
  - `POST /api/v1/environments/:id/start` — start container
  - `POST /api/v1/environments/:id/stop` — stop container
  - `DELETE /api/v1/environments/:id` — delete environment (async; tears down cloud resources)
- Remote health: `GET /api/v1/environments/:id/remote-health` (SSH, Docker daemon, and workspace container readiness)
- Retry remote bootstrap: `POST /api/v1/environments/:id/retry-bootstrap` (adopts existing remote container when present)
- Terraform provisioning: `POST /api/v1/environments/:id/provision` — upgrade a **local** workspace to EC2 (blocked for cloud-created environments and when EC2 already exists)
- Detach cloud resources: `POST /api/v1/environments/:id/destroy-cloud` (local-upgraded envs only; reverts to local Docker; blocked for cloud-created workspaces)
- Async operation status: `GET /api/v1/operations/:id`
- Postgres-persisted operation tracking (survives backend restarts)
- Typed provisioning validation errors (`code` + `error`)
- Local Docker workspace lifecycle via `docker` CLI
- Remote Docker workspace lifecycle over SSH when `runtime_target = remote`
- Post-provision bootstrap: wait for SSH/Docker (2s poll interval), pre-pull workspace image in EC2 user-data, ensure remote container by name (`docklab-{environment_id}`), switch runtime target; dashboard shows live bootstrap phase in `cloud_error`
- PTY-backed browser terminal (`GET /api/v1/environments/:id/terminal/ws`) — local or remote via SSH
- Structured JSON logging (`log/slog`)
- Cloud drift/orphan reconciliation (runs on startup and every 5 minutes)
- Auto-sleep lifecycle worker: stops idle running environments (local or remote container) after `IDLE_STOP_AFTER_MINUTES` (default 60); terminal sessions refresh `last_activity_at` every 60 s
- Persisted cloud usage metadata (`creation_mode`, `cloud_instance_type`, `cloud_provisioned_at`, `cloud_key_name`, `runtime_target`) for dashboard visibility

## Frontend features

- Login, register, and JWT token persistence
- Protected `/dashboard` route
- Dashboard views: **Environments**, **Usage & Cost**
- Create environment with **Local workspace** or **Cloud workspace (EC2)** toggle and inline cloud settings
- **Upgrade to cloud** modal on local workspaces (region, instance type, AMI, key pair)
- Environment create/start/stop/delete/upgrade-to-cloud/terminate controls with context-aware button availability
- Separate workspace and cloud status badges on environment cards
- Runtime target and remote health indicators (including workspace container readiness); 5s dashboard refresh while cloud provisioning is in progress
- **Complete remote setup** / **Retry remote setup** when bootstrap is incomplete or failed
- Async operation polling with progress feedback
- In-app confirmation modals for destructive actions
- xterm.js terminal panel with resize, reconnect, and copy/paste shortcuts

## Known limitations

These are intentional MVP boundaries documented in the sprint plan:

| Area | Current behavior | Remaining work |
|------|------------------|----------------|
| Cloud auto-sleep | Idle policy stops workspace containers; **EC2 keeps running** | Stop or terminate idle EC2 instances (Sprint 8) |
| SSH key setup | Backend reads private key from `DOKLAB_SSH_PRIVATE_KEY_PATH`; must match EC2 key pair | Secrets management for production |
| Remote bootstrap | Fixed `ec2-user` default; Amazon Linux–oriented user-data | Ubuntu AMI auto-detection, configurable bootstrap scripts |
| Cost tracking | Client-side estimates from hardcoded t3 on-demand rates | Persisted usage history; optional AWS Pricing API |
| Auth sessions | Single JWT with fixed TTL; no refresh tokens | Refresh tokens or shorter-lived access tokens |
| Production ops | Local Docker Compose dev setup | Deployment pipeline, monitoring, rate limiting |
| CI/CD | GitHub Actions runs tests, lint, and Docker build on push/PR | Automated deployment to a hosted environment |

## Local development

### Prerequisites

- Go 1.25+
- Node.js 20+
- Docker + Docker Compose
- Terraform CLI (optional locally; bundled in the backend Docker image)
- AWS EC2 key pair: private key file readable by the backend for SSH after provisioning

### SSH key for remote workspaces

Provisioning requires two related but different values:

| Setting | Example | Meaning |
|---------|---------|---------|
| **Key pair name** (create or upgrade modal) | `docklab-key` | Name of the key pair **registered in AWS EC2** in your target region |
| **Private key path** (`DOKLAB_SSH_PRIVATE_KEY_PATH`) | `./docklab-key.pem` | Local file path to the matching **private** key |

Do not put `.pem` in the key pair name — that is only the local filename.

If the key pair does not exist in AWS yet, create or import it in the same region you provision to (for example `us-east-1`):

```bash
aws ec2 import-key-pair --key-name docklab-key --public-key-material fileb://docklab-key.pub --region us-east-1
```

The backend connects over SSH using the private key at `DOKLAB_SSH_PRIVATE_KEY_PATH` (must match that key pair).

**Docker Compose defaults:** the backend container reads the key from `/run/secrets/docklab-ssh-key.pem`, mounted from `DOKLAB_SSH_PRIVATE_KEY_HOST_PATH` on your machine (default `./docklab-key.pem` in the project root). Set in `.env`:

```bash
DOKLAB_SSH_PRIVATE_KEY_HOST_PATH=./docklab-key.pem
DOKLAB_SSH_PRIVATE_KEY_PATH=/run/secrets/docklab-ssh-key.pem
```

When running the backend in Docker Compose, mount the key into the container and point the env var at the in-container path, for example:

```yaml
# docker-compose.yml (backend service)
environment:
  DOKLAB_SSH_PRIVATE_KEY_PATH: /run/secrets/ec2-key.pem
volumes:
  - ./my-key.pem:/run/secrets/ec2-key.pem:ro
```

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
| `IDLE_STOP_AFTER_MINUTES` | Optional, default `60` — workspace container idle timeout |
| `DOKLAB_SSH_PRIVATE_KEY_PATH` | **Required for remote workspaces** — path to EC2 SSH private key |
| `DOKLAB_SSH_USER` | Optional, default `ec2-user` |
| `DOKLAB_SSH_PORT` | Optional, default `22` |
| `DOKLAB_SSH_CONNECT_TIMEOUT_SECONDS` | Optional, default `15` |
| `DOKLAB_REMOTE_BOOTSTRAP_TIMEOUT_SECONDS` | Optional, default `300` — max wait for SSH/Docker after provision |

## How to use

1. Start the backend and PostgreSQL with Docker Compose.
2. Start the frontend dev server from the `frontend/` folder.
3. Open `http://localhost:5173` in your browser.
4. Create an account on `/register` and sign in on `/login`.
5. Use the dashboard to create a **local workspace** or **cloud workspace (EC2)**.
6. Open **Terminal** on a running environment to run shell commands from the browser.
7. For local workspaces, click **Upgrade to cloud** and fill in the provision modal to attach EC2.
8. Cloud workspaces provision EC2 asynchronously at create time; poll until the operation succeeds.
9. After cloud provisioning succeeds, the environment uses `runtime_target = remote` and the terminal connects over SSH.
10. Use **Check remote health** to verify SSH, Docker, and workspace container readiness.
11. Use **Terminate EC2** on local-upgraded workspaces to destroy EC2 and revert to local Docker.
12. Use **Delete** to remove an environment; for cloud workspaces this also terminates EC2.
13. Long-running cloud actions run asynchronously; the dashboard polls operation status until completion.
14. Operation status is persisted in PostgreSQL, so polling survives backend restarts.
15. Running environments with no terminal activity for longer than `IDLE_STOP_AFTER_MINUTES` are automatically stopped (local or remote container).
16. The **Usage & Cost** view estimates EC2 runtime spend for provisioned environments.

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
3. Log in and create a **local workspace**; open the terminal and confirm shell access.
4. Create a **cloud workspace (EC2)** with valid region, AMI, instance type, and key pair name.
5. Configure `DOKLAB_SSH_PRIVATE_KEY_PATH` and use the matching key pair name in the create or upgrade modal.
6. Confirm the cloud create operation advances from `queued` → `running` → `succeeded`.
7. Verify the cloud environment card shows `creation_mode = cloud`, `runtime_target = remote`, `provisioned`, instance ID, and public IP.
8. Use **Check remote health** — SSH, Docker, and workspace should report ok.
9. Open the terminal and run commands on the remote workspace container.
10. Create a local workspace and use **Upgrade to cloud** via the provision modal; confirm it switches to remote runtime.
11. Verify **Usage & Cost** shows provisioned environments and non-zero rate estimates.
12. Use **Terminate EC2** on an upgraded local env to revert to local Docker; cloud workspaces use **Delete** only.
13. Use **Delete** and confirm both the environment row and EC2 resources are removed.

## Roadmap

See [plan/sprints.md](plan/sprints.md) for sprint-level tracking and [plan/docklab_project_plan.md](plan/docklab_project_plan.md) for the full phase plan.

**Next priorities:**

1. **Sprint 8 — Cloud lifecycle automation:** Auto-stop or terminate idle EC2 instances.
2. **Sprint 9 — Production hardening & deployment:** Rate limiting, monitoring, secrets management, and CD beyond CI.
3. **Sprint 10 — Cost tracking hardening:** Persisted usage history and accurate pricing.
