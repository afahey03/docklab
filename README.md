# DockLab

Cloud-based remote development environment platform. Users authenticate, manage Docker workspaces locally, open a browser terminal, and optionally provision AWS EC2 infrastructure through Terraform.

<img width="1903" height="909" alt="image" src="https://github.com/user-attachments/assets/86ff5978-103c-4bb7-bff8-c441eac62b8e" />
<img width="1918" height="630" alt="image" src="https://github.com/user-attachments/assets/58856c24-a734-4b7f-ba4e-118c2f9ef2c0" />
<img width="1918" height="376" alt="image" src="https://github.com/user-attachments/assets/51a19ba1-e7d9-460e-acc7-b3c7c74c0297" />

## Current status

**MVP complete through Sprint 7 (June 2026).** The platform supports auth, local and remote Docker workspaces, browser terminals (local or over SSH), Terraform EC2 provisioning with post-provision bootstrap, idle auto-stop, estimated cloud usage/cost visibility, and remote health checks.

**Core remote-dev flow works when AWS and SSH are configured.** Environments start locally; after provisioning EC2 with a matching key pair, DockLab bootstraps Docker on the instance, migrates the workspace remotely, and routes the browser terminal over SSH. See [Known limitations](#known-limitations) for remaining gaps.

## Architecture

```text
React Frontend (Vite + TypeScript + Tailwind)
    ↓
Go API Server (Gin)
    ├── Docker CLI  →  Local workspace containers (default)
    └── Terraform CLI  →  AWS EC2 (Docker via user-data, SSH access)
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
  - `POST /api/v1/environments` — create
  - `GET /api/v1/environments` — list
  - `GET /api/v1/environments/:id` — get one
  - `POST /api/v1/environments/:id/start` — start container
  - `POST /api/v1/environments/:id/stop` — stop container
  - `DELETE /api/v1/environments/:id` — delete environment (async; tears down cloud resources)
- Remote health: `GET /api/v1/environments/:id/remote-health` (SSH, Docker daemon, and workspace container readiness)
- Retry remote bootstrap: `POST /api/v1/environments/:id/retry-bootstrap` (adopts existing remote container when present)
- Terraform provisioning: `POST /api/v1/environments/:id/provision` (requires EC2 key pair name; blocked when EC2 already exists)
- Cloud-only termination: `POST /api/v1/environments/:id/destroy-cloud` (reverts workspace to local Docker)
- Async operation status: `GET /api/v1/operations/:id`
- Postgres-persisted operation tracking (survives backend restarts)
- Typed provisioning validation errors (`code` + `error`)
- Local Docker workspace lifecycle via `docker` CLI
- Remote Docker workspace lifecycle over SSH when `runtime_target = remote`
- Post-provision bootstrap: wait for SSH/Docker, ensure remote container by name (`docklab-{environment_id}`), switch runtime target
- PTY-backed browser terminal (`GET /api/v1/environments/:id/terminal/ws`) — local or remote via SSH
- Structured JSON logging (`log/slog`)
- Cloud drift/orphan reconciliation (runs on startup and every 5 minutes)
- Auto-sleep lifecycle worker: stops idle running environments (local or remote container) after `IDLE_STOP_AFTER_MINUTES` (default 60); terminal sessions refresh `last_activity_at` every 60 s
- Persisted cloud usage metadata (`cloud_instance_type`, `cloud_provisioned_at`, `cloud_key_name`, `runtime_target`) for dashboard visibility

## Frontend features

- Login, register, and JWT token persistence
- Protected `/dashboard` route
- Dashboard views: **Environments**, **Usage & Cost**, **Settings** (cloud provisioning defaults)
- Environment create/start/stop/delete/provision/terminate controls with context-aware button availability
- Separate workspace and cloud status badges on environment cards
- Runtime target and remote health indicators (including workspace container readiness)
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
| **Key pair name** (dashboard Settings) | `docklab-key` | Name of the key pair **registered in AWS EC2** in your target region |
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
5. Use the dashboard to create and manage environments (local Docker by default).
6. Open **Terminal** on a running environment to run shell commands from the browser.
7. Under **Settings**, set cloud defaults including a **required EC2 key pair name**, then use **Provision**.
8. After provisioning succeeds, the environment switches to `runtime_target = remote` and the terminal connects over SSH to the EC2 workspace.
9. Use **Check remote health** to verify SSH and Docker on provisioned environments.
10. Use **Terminate EC2** to destroy cloud resources and revert the workspace to local Docker.
11. Use **Delete** to remove both the environment and any provisioned cloud resources.
12. Long-running cloud actions run asynchronously; the dashboard polls operation status until completion.
13. Operation status is persisted in PostgreSQL, so polling survives backend restarts.
14. Running environments with no terminal activity for longer than `IDLE_STOP_AFTER_MINUTES` are automatically stopped (local or remote container).
15. The **Usage & Cost** view estimates EC2 runtime spend for provisioned environments.

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
5. Configure `DOKLAB_SSH_PRIVATE_KEY_PATH` and set the matching key pair name under **Settings**.
6. Provision with a valid AWS region, AMI, instance type, credentials, and key pair name.
7. Confirm the operation advances from `queued` → `running` → `succeeded`.
8. Verify the environment card shows `runtime_target = remote`, `provisioned`, instance ID, and public IP.
9. Use **Check remote health** — SSH and Docker should report ok.
10. Open the terminal and run commands on the remote workspace container.
11. Verify **Usage & Cost** shows the provisioned environment and a non-zero rate estimate.
12. Use **Terminate EC2** and confirm cloud resources are removed and runtime reverts to local.
13. Use **Delete** and confirm both the environment row and EC2 resources are removed.

## Roadmap

See [plan/sprints.md](plan/sprints.md) for sprint-level tracking and [plan/docklab_project_plan.md](plan/docklab_project_plan.md) for the full phase plan.

**Next priorities:**

1. **Sprint 8 — Cloud lifecycle automation:** Auto-stop or terminate idle EC2 instances.
2. **Sprint 9 — Production hardening & deployment:** Rate limiting, monitoring, secrets management, and CD beyond CI.
3. **Sprint 10 — Cost tracking hardening:** Persisted usage history and accurate pricing.
