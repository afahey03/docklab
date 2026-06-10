# DockLab

Cloud-based remote development environment platform. Users authenticate, manage Docker workspaces locally, open a browser terminal, and optionally provision AWS EC2 infrastructure through Terraform.

<img width="1903" height="909" alt="image" src="https://github.com/user-attachments/assets/86ff5978-103c-4bb7-bff8-c441eac62b8e" />
<img width="1918" height="630" alt="image" src="https://github.com/user-attachments/assets/58856c24-a734-4b7f-ba4e-118c2f9ef2c0" />
<img width="1918" height="376" alt="image" src="https://github.com/user-attachments/assets/51a19ba1-e7d9-460e-acc7-b3c7c74c0297" />

## Current status

On top of the Sprint 1–8 MVP (auth, local/remote Docker workspaces, browser terminals, Terraform EC2 provisioning with bootstrap, idle lifecycle automation, reconciliation), the platform now ships:

- **Production hardening:** JWT refresh tokens with rotation, GitHub OAuth login, per-IP rate limiting, per-user resource quotas, Prometheus `/metrics`, webhook alerting, AWS Secrets Manager bootstrap (production runs with no `.env` file: IAM-role credentials and a base64 SSH key from the secret), CD workflow (GHCR images + SSH deploy) with `docker-compose.prod.yml`
- **Durable cost tracking:** persisted `environment_usage` sessions, AWS Pricing API rates (with static fallback), billing summary with per-environment rollups, monthly budgets with alerts
- **Advanced features:** browser IDE (code-server sidecar), workspace snapshots (`docker commit` + restore), environment sharing with collaborative shared terminals, a template marketplace, Git repo auto-clone at create time, and an optional Kubernetes runtime backend (`DOKLAB_RUNTIME=kubernetes`)

**Core remote-dev flow works when AWS and SSH are configured.** At create time, choose a **local Docker workspace** or a **cloud workspace (EC2)**. Cloud environments provision EC2 and bootstrap the remote container in one async flow — no throwaway local container first. Local environments can optionally be upgraded to EC2 later. See [Known limitations](#known-limitations) for remaining gaps.

## Architecture

```text
React Frontend (Vite + TypeScript + Tailwind)
    ↓
Go API Server (Gin)
    ├── middleware: JWT auth, rate limiting, Prometheus metrics
    ├── Docker CLI  →  Local workspace containers (when target = local)
    ├── kubectl     →  Kubernetes Deployments (when DOKLAB_RUNTIME = kubernetes)
    └── Terraform CLI  →  AWS EC2 (when target = cloud or upgrade from local)
              ↓ SSH + remote Docker CLI
         Workspace container on EC2 (runtime_target = remote)
    ↓
Browser Terminal via WebSockets (local PTY or SSH PTY + xterm.js; multi-client shared sessions)
Browser IDE via code-server sidecar containers (shares the workspace volume)
Background workers: reconciliation, idle lifecycle, budget watcher
Observability: /metrics (Prometheus) + webhook alerts
```

## Repository layout

```text
cmd/server/                # Go API entrypoint
internal/
  handlers/                # HTTP and WebSocket handlers (auth, environments, usage, snapshots, shares, IDE)
  services/                # Business logic (auth, OAuth, env, terminal, terraform, SSH/K8s runtimes, lifecycle, reconciliation, usage, pricing, snapshots, shares, IDE, metrics, alerts)
  repositories/            # PostgreSQL data access
  models/                  # Domain models
  middleware/              # JWT auth, rate limiting, Prometheus metrics
  config/                  # Env-based configuration + AWS Secrets Manager loader
  database/                # PostgreSQL pool and schema bootstrap
pkg/logger/                # Shared structured logger (log/slog)
frontend/                  # React + TypeScript + Tailwind dashboard (+ production Dockerfile/nginx)
plan/                      # Project plan and sprint tracking
.github/workflows/         # CI (tests, lint, Docker build) + CD (GHCR push, SSH deploy)
docker-compose.yml         # Local dev stack
docker-compose.prod.yml    # Production stack using GHCR images
```

## Backend features

- `/health` endpoint with database connectivity check; `/metrics` Prometheus endpoint (when `DOKLAB_METRICS_ENABLED=true`)
- PostgreSQL connection pool (`pgxpool`) with schema bootstrap on startup
- Environment variable configuration (see [Environment configuration](#environment-configuration)); optional AWS Secrets Manager bootstrap (`DOKLAB_SECRETS_MANAGER_SECRET_ID`)
- Auth:
  - `POST /api/v1/auth/register`, `POST /api/v1/auth/login` — return an access token **and** a rotating refresh token
  - `POST /api/v1/auth/refresh` — exchange a refresh token for a new pair (old token revoked)
  - `POST /api/v1/auth/logout` — revoke a refresh token
  - `GET /api/v1/auth/github/login` + `GET /api/v1/auth/github/callback` — GitHub OAuth sign-in (tokens delivered via URL fragment)
  - Protected `GET /api/v1/auth/me`
- Password hashing with bcrypt
- Per-IP token bucket rate limiting (separate budgets for auth, provisioning, and general API; `429` + `Retry-After`)
- Per-user quotas: max environments (`DOKLAB_MAX_ENVIRONMENTS_PER_USER`) and max concurrent operations (`DOKLAB_MAX_CONCURRENT_OPERATIONS_PER_USER`)
- Webhook alerts (`DOKLAB_ALERT_WEBHOOK_URL`) for failed operations, lifecycle/reconciliation events, and budget overruns
- Environment lifecycle APIs:
  - `POST /api/v1/environments` — create (`target`: `local` or `cloud`; optional `repo_url` auto-clones into `/workspace`; optional `template_id`; cloud requires `provision` payload; returns `201` for local, `202` with operation for cloud)
  - `GET /api/v1/environments` — list (includes `shared_environments` shared with you)
  - `GET /api/v1/environments/:id` — get one
  - `POST /api/v1/environments/:id/start` — start container
  - `POST /api/v1/environments/:id/stop` — stop container
  - `DELETE /api/v1/environments/:id` — delete environment (async; tears down cloud resources)
- Template marketplace: `GET /api/v1/templates` — curated prebuilt workspace images (Node.js, Go, Python, etc.)
- Workspace snapshots (`docker commit` based):
  - `POST /api/v1/environments/:id/snapshots` — snapshot a running workspace
  - `GET /api/v1/environments/:id/snapshots` — list snapshots
  - `POST /api/v1/environments/:id/snapshots/:snapshotId/restore` — recreate the workspace from a snapshot image
  - `DELETE /api/v1/environments/:id/snapshots/:snapshotId`
- Environment sharing (collaboration):
  - `POST /api/v1/environments/:id/shares` — share with another user by email
  - `GET /api/v1/environments/:id/shares`, `DELETE /api/v1/environments/:id/shares/:email`
  - Shared users get terminal access; terminal sessions are multi-client (all connected clients see the same PTY)
- Browser IDE (code-server sidecar sharing the workspace volume):
  - `POST /api/v1/environments/:id/ide/start` — returns URL + generated password
  - `GET /api/v1/environments/:id/ide` — status; `POST /api/v1/environments/:id/ide/stop`
- Usage & billing:
  - `GET /api/v1/usage` — persisted EC2 usage sessions with totals
  - `GET /api/v1/billing/summary` — month-to-date rollup per environment + budget state
  - `GET /api/v1/billing/budget`, `PUT /api/v1/billing/budget` — monthly budget and alert preference
  - `GET /api/v1/pricing?instance_type=&region=` — hourly rate via AWS Pricing API (static fallback)
- Optional Kubernetes runtime backend (`DOKLAB_RUNTIME=kubernetes`): local workspaces become `kubectl` Deployments (scale 0/1 for stop/start); snapshots and IDE are Docker-only
- Remote health: `GET /api/v1/environments/:id/remote-health` (SSH, Docker daemon, and workspace container readiness)
- Retry remote bootstrap: `POST /api/v1/environments/:id/retry-bootstrap` (adopts existing remote container when present)
- Terraform provisioning: `POST /api/v1/environments/:id/provision` — upgrade a **local** workspace to EC2 (blocked for cloud-created environments and when EC2 already exists)
- Detach cloud resources: `POST /api/v1/environments/:id/destroy-cloud` (local-upgraded envs only; reverts to local Docker; blocked for cloud-created workspaces)
- Async operation status: `GET /api/v1/operations/:id`
- Lifecycle policy: `GET /api/v1/lifecycle-policy` (workspace/EC2 idle thresholds)
- Postgres-persisted operation tracking (survives backend restarts)
- Typed provisioning validation errors (`code` + `error`)
- Local Docker workspace lifecycle via `docker` CLI
- Remote Docker workspace lifecycle over SSH when `runtime_target = remote`
- Post-provision bootstrap: wait for SSH/Docker (2s poll interval), pre-pull workspace image in EC2 user-data, ensure remote container by name (`docklab-{environment_id}`), switch runtime target; `cloud_status` becomes `provisioned` once EC2 exists while workspace bootstrap progress appears in `cloud_error` and the workspace badge (`bootstrapping`)
- Cloud delete/terminate tears down the remote workspace container before Terraform destroys EC2; unreachable SSH during cleanup is treated as best-effort so delete does not fail after the instance is already gone
- PTY-backed browser terminal (`GET /api/v1/environments/:id/terminal/ws`) — local, remote via SSH, or Kubernetes `kubectl exec`; multiple clients can join the same session (owner + shared users)
- Workspace files live in a named volume (`docklab-ws-<name>`) mounted at `/workspace`, shared with the IDE sidecar; snapshots tar the volume into the committed image and restore it on recreate
- Persisted usage sessions open/close automatically as EC2 becomes billable (provision, start, stop, terminate, delete, reconciliation)
- Structured JSON logging (`log/slog`)
- Cloud drift/orphan reconciliation (runs on startup and every 5 minutes; clears DB rows when EC2 instances no longer exist in AWS)
- Auto-sleep lifecycle: stops idle workspace containers after `IDLE_STOP_AFTER_MINUTES` (default 60); stops idle EC2 after `IDLE_CLOUD_STOP_AFTER_MINUTES` (default 2× workspace threshold); terminates stopped EC2 after `IDLE_CLOUD_TERMINATE_AFTER_MINUTES` (default 1440); terminal sessions refresh `last_activity_at` every 60 s
- Persisted cloud usage metadata (`creation_mode`, `cloud_instance_type`, `cloud_provisioned_at`, `cloud_key_name`, `runtime_target`) for dashboard visibility

## Frontend features

- Login, register, **Continue with GitHub** (OAuth), token-pair persistence with transparent access-token refresh on `401`
- Logout revokes the refresh token server-side
- Protected `/dashboard` route
- Dashboard views: **Environments**, **Usage & Cost**
- Template marketplace picker on the create form (one click sets the image)
- Optional **Git repository to clone** field — the repo is cloned into `/workspace` on create
- Create environment with **Local workspace** or **Cloud workspace (EC2)** toggle and inline cloud settings
- Per-environment **Manage** panel: snapshots (create/restore/delete), sharing (grant/revoke by email), and browser IDE (start/stop, URL + password display)
- **Shared with you** section listing environments other users shared, with collaborative terminal access
- Server-driven **Usage & Cost** view: month-to-date and all-time tracked spend, per-environment cost bars, usage session history table, and a monthly budget editor with alert toggle
- **Upgrade to cloud** modal on local workspaces (region, instance type, AMI, key pair)
- Environment create/start/stop/delete/upgrade-to-cloud/terminate controls with context-aware button availability
- Separate workspace and cloud status badges on environment cards (`cloud: provisioned` once EC2 exists; `workspace: bootstrapping` while the remote container is attaching)
- Runtime target and remote health indicators (including workspace container readiness); 5s dashboard refresh while cloud provisioning is in progress
- Idle cloud policy summary and billing warnings when EC2 is running but the workspace is stopped; `cloud_stopped` indicator when EC2 was auto-stopped
- **Complete remote setup** / **Retry remote setup** when bootstrap is incomplete or failed
- Async operation polling with progress feedback
- In-app confirmation modals for destructive actions
- xterm.js terminal panel with resize, reconnect, and copy/paste shortcuts

## Known limitations

| Area | Current behavior | Remaining work |
|------|------------------|----------------|
| Per-user idle policy | Global env-based thresholds only | Per-user or per-environment lifecycle settings |
| Remote bootstrap | Fixed `ec2-user` default; Amazon Linux–oriented user-data | Ubuntu AMI auto-detection, configurable bootstrap scripts |
| Kubernetes runtime | Local workspaces as Deployments via `kubectl`; stop/start = scale 0/1 | Snapshots and the IDE sidecar are Docker-only; no PVC-backed persistence yet |
| Rate limiting | In-memory per-instance token buckets | Shared store (e.g. Redis) for multi-replica deployments |
| Cost tracking | Tracked sessions priced at on-demand rates (Pricing API or static fallback) | Not actual AWS invoice data; no spot/reserved pricing |
| Browser IDE | code-server over HTTP (`:8443` on EC2, random localhost port locally), password auth | TLS termination / reverse proxy for production IDE access |
| Deploy target | CD builds GHCR images and optionally deploys over SSH to a Docker Compose host | Managed runtime (ECS/Fly.io) if horizontal scale is needed |

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
| `AWS_ACCESS_KEY_ID` | Optional — the backend uses the default AWS credential chain, so an EC2 instance profile / ECS task role works with no static keys |
| `AWS_SECRET_ACCESS_KEY` | Optional (see above) |
| `AWS_SESSION_TOKEN` | Optional, for temporary credentials |
| `AWS_DEFAULT_REGION` | Optional, default `us-east-1` |
| `DOKLAB_TERRAFORM_STATE_BUCKET` | S3 bucket for remote Terraform state |
| `DOKLAB_TERRAFORM_STATE_REGION` | Region for the state bucket |
| `DOKLAB_TERRAFORM_STATE_TABLE` | DynamoDB table for state locking (`LockID` string partition key) |
| `DOKLAB_TERRAFORM_STATE_KEY_PREFIX` | Optional, default `docklab/environments` |
| `IDLE_STOP_AFTER_MINUTES` | Optional, default `60` — workspace container idle timeout |
| `IDLE_CLOUD_STOP_AFTER_MINUTES` | Optional, default `2 × IDLE_STOP_AFTER_MINUTES` — stop provisioned EC2 after inactivity |
| `IDLE_CLOUD_TERMINATE_AFTER_MINUTES` | Optional, default `1440` — terminate stopped EC2 after inactivity (`0` disables auto-terminate) |
| `DOKLAB_CLOUD_IDLE_POLICY_ENABLED` | Optional, default `true` — enable automatic EC2 stop/terminate |
| `DOKLAB_SSH_PRIVATE_KEY_PATH` | **Required for remote workspaces** (unless `DOKLAB_SSH_PRIVATE_KEY_B64` is set) — path to EC2 SSH private key |
| `DOKLAB_SSH_PRIVATE_KEY_B64` | Optional — base64-encoded private key (e.g. delivered via Secrets Manager); written to disk at boot and takes precedence over the path |
| `DOKLAB_SSH_USER` | Optional, default `ec2-user` |
| `DOKLAB_SSH_PORT` | Optional, default `22` |
| `DOKLAB_SSH_CONNECT_TIMEOUT_SECONDS` | Optional, default `15` |
| `DOKLAB_REMOTE_BOOTSTRAP_TIMEOUT_SECONDS` | Optional, default `300` — max wait for SSH/Docker after provision |
| `REFRESH_TOKEN_TTL_DAYS` | Optional, default `30` — refresh token lifetime |
| `DOKLAB_GITHUB_CLIENT_ID` / `DOKLAB_GITHUB_CLIENT_SECRET` | GitHub OAuth app credentials (blank disables GitHub login) |
| `DOKLAB_GITHUB_REDIRECT_URL` | OAuth callback, e.g. `http://localhost:8080/api/v1/auth/github/callback` |
| `DOKLAB_FRONTEND_BASE_URL` | Optional, default `http://localhost:5173` — OAuth redirect target and CORS origin |
| `DOKLAB_RATE_LIMIT_ENABLED` | Optional, default `true` |
| `DOKLAB_AUTH_RATE_LIMIT_PER_MINUTE` / `DOKLAB_PROVISION_RATE_LIMIT_PER_MINUTE` / `DOKLAB_API_RATE_LIMIT_PER_MINUTE` | Optional, defaults `20` / `10` / `240` |
| `DOKLAB_MAX_ENVIRONMENTS_PER_USER` | Optional, default `10` (`0` disables) |
| `DOKLAB_MAX_CONCURRENT_OPERATIONS_PER_USER` | Optional, default `3` (`0` disables) |
| `DOKLAB_METRICS_ENABLED` | Optional, default `true` — exposes `/metrics` |
| `DOKLAB_ALERT_WEBHOOK_URL` | Optional — POSTs JSON alerts to this URL |
| `DOKLAB_SECRETS_MANAGER_SECRET_ID` / `DOKLAB_SECRETS_MANAGER_REGION` | Optional — hydrate env vars from an AWS Secrets Manager JSON secret at boot |
| `DOKLAB_PRICING_API_ENABLED` | Optional, default `true` — AWS Pricing API for hourly rates |
| `DOKLAB_RUNTIME` | Optional, default `docker` — set `kubernetes` to schedule local workspaces via `kubectl` |
| `DOKLAB_K8S_NAMESPACE` / `DOKLAB_K8S_CONTEXT` | Optional, defaults `docklab` / current context |
| `DOKLAB_IDE_ENABLED` | Optional, default `true` — browser IDE sidecars |
| `DOKLAB_IDE_IMAGE` | Optional, default `codercom/code-server:latest` |
| `DOKLAB_IDE_REMOTE_PORT` | Optional, default `8443` — IDE port on EC2 (matches the Terraform SG rule) |

## How to use

1. Start the backend and PostgreSQL with Docker Compose.
2. Start the frontend dev server from the `frontend/` folder.
3. Open `http://localhost:5173` in your browser.
4. Create an account on `/register`, sign in on `/login`, or use **Continue with GitHub** (when OAuth is configured).
5. Use the dashboard to create a **local workspace** or **cloud workspace (EC2)** — pick a template and/or paste an `https://` Git repo URL to auto-clone into `/workspace`.
6. Open **Terminal** on a running environment to run shell commands from the browser.
7. Click **Manage** on an environment for snapshots, sharing, and the browser IDE.
8. **Start IDE** launches a code-server sidecar; open the URL and use the shown password for full VS Code in the browser against the same `/workspace` files.
9. **Snapshot** a running workspace, then **Restore** later to recreate the container from that image.
10. **Share** an environment by email; the recipient sees it under **Shared with you** and can join a collaborative terminal (all clients share one PTY).
11. For local workspaces, click **Upgrade to cloud** and fill in the provision modal to attach EC2.
12. Cloud workspaces provision EC2 asynchronously at create time; poll until the operation succeeds.
13. After cloud provisioning succeeds, the environment uses `runtime_target = remote` and the terminal connects over SSH.
14. Use **Check remote health** to verify SSH, Docker, and workspace container readiness.
15. Use **Terminate EC2** on local-upgraded workspaces to destroy EC2 and revert to local Docker.
16. Use **Delete** to remove an environment; for cloud workspaces this also terminates EC2.
17. Long-running cloud actions run asynchronously; the dashboard polls operation status until completion. Operation status survives backend restarts.
18. Running environments with no terminal activity for longer than `IDLE_STOP_AFTER_MINUTES` are automatically stopped (local or remote container).
19. Idle provisioned EC2 instances are automatically stopped, then terminated per `IDLE_CLOUD_STOP_AFTER_MINUTES` and `IDLE_CLOUD_TERMINATE_AFTER_MINUTES`; use **Start** to wake a `cloud_stopped` instance.
20. The dashboard shows the active idle policy and warnings when EC2 is still billing while the workspace is stopped.
21. The **Usage & Cost** view shows tracked usage sessions, month-to-date spend per environment, and lets you set a monthly budget; exceeding it raises a webhook alert (when configured).
22. Access tokens auto-refresh in the background; **Sign out** revokes your refresh token.

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

## Production deployment (CD)

`.github/workflows/cd.yml` runs on pushes to `main`:

1. Builds and pushes `docklab-backend` and `docklab-frontend` images to GHCR (tagged `latest` + commit SHA).
2. Optionally deploys over SSH (set repo variable `DOKLAB_DEPLOY_ENABLED=true` and secrets `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_SSH_KEY`, `DEPLOY_PATH`): pulls the new images and restarts `docker-compose.prod.yml` on the host.

### Running production without a `.env` file

The deploy host only needs `docker-compose.prod.yml` — no `.env` file:

- **`POSTGRES_PASSWORD`** is the one variable Compose itself needs (the co-located Postgres container can't read Secrets Manager). Export it in the deploy shell or supply the GitHub secret `DEPLOY_POSTGRES_PASSWORD`, which the CD workflow exports during deploy. Using an external/managed database instead? Export `DATABASE_URL` and skip the bundled Postgres.
- **Everything else comes from AWS Secrets Manager**: set `DOKLAB_SECRETS_MANAGER_SECRET_ID` (exported on the host or via the repo variable of the same name, which the CD workflow forwards) and put `JWT_SECRET`, GitHub OAuth credentials, alert webhook URL, and `DOKLAB_SSH_PRIVATE_KEY_B64` (the EC2 private key, base64-encoded) in the secret's JSON. The backend hydrates them at boot and writes the SSH key to a `0600` file inside the container — no `.pem` on the host.
- **AWS credentials** use the default credential chain: run the host with an EC2 instance profile (or ECS task role) granting EC2/Pricing/Secrets Manager access and leave `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` unset. Static keys still work and take precedence when present.
- The backend refuses to start with `APP_ENV=production` if `JWT_SECRET` was not provided by either source.

A `.env` file next to the compose file still works for all of these values if you prefer it. The frontend image bakes in `VITE_API_BASE_URL` from the repo variable `DOKLAB_API_BASE_URL`.

## Terraform state backend setup

Before provisioning, create or point DockLab at:

- An S3 bucket for Terraform state storage
- A DynamoDB table for state locking with `LockID` as the partition key

Then set the `DOKLAB_TERRAFORM_STATE_*` variables in your `.env`.

## End-to-end test checklist

### Core flow (Sprints 1–8)

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
11. Use **Terminate EC2** on an upgraded local env to revert to local Docker; cloud workspaces use **Delete** only.
12. Use **Delete** and confirm both the environment row and EC2 resources are removed.
13. Leave a provisioned environment idle past the workspace threshold, confirm the workspace stops, then EC2 moves to `cloud_stopped`.
14. Call `GET /api/v1/lifecycle-policy` and confirm the dashboard idle policy summary matches your env configuration.

### Auth & hardening (Sprint 9)

15. Log in and confirm the response stores both tokens; wait past `JWT_TTL_MINUTES` (or set it to `1`) and confirm an API call transparently refreshes instead of logging you out.
16. **Sign out**, then try `POST /api/v1/auth/refresh` with the old refresh token — it must be rejected.
17. With a GitHub OAuth app configured, use **Continue with GitHub** and confirm you land on the dashboard signed in.
18. Hammer `POST /api/v1/auth/login` more than `DOKLAB_AUTH_RATE_LIMIT_PER_MINUTE` times in a minute — expect `429` with `Retry-After`.
19. Set `DOKLAB_MAX_ENVIRONMENTS_PER_USER=1`, create one environment, then confirm a second create returns the quota error.
20. Open `http://localhost:8080/metrics` and confirm Prometheus metrics (HTTP requests, operations, terminal clients) are exposed.
21. Point `DOKLAB_ALERT_WEBHOOK_URL` at a request bin and confirm a failed provision posts an alert.

### Templates, repos, snapshots, sharing, IDE (Sprint 10+)

22. Create an environment from a **template** and confirm the image matches the template.
23. Create an environment with a Git `repo_url` and confirm the repo is cloned under `/workspace` in the terminal.
24. Open **Manage**: write a file in `/workspace`, create a **snapshot**, delete the file, **Restore** the snapshot, and confirm the file is back.
25. **Share** the environment with a second account; from that account confirm it appears under **Shared with you** and open the terminal from both accounts — keystrokes/output appear in both.
26. **Start IDE**, open the URL, enter the password, and edit a file under `/workspace`; confirm the change is visible from the terminal.
27. On a provisioned cloud workspace, start the IDE and confirm it is reachable at `http://<public-ip>:8443`.

### Usage & billing (Sprint 10)

28. Provision a cloud workspace and confirm **Usage & Cost** shows an open usage session with a non-zero hourly rate.
29. Stop/terminate the instance and confirm the session closes with runtime minutes and estimated cost.
30. Set a small monthly budget, exceed it, and confirm the budget card flags over-budget (and a webhook alert fires when configured).

### Kubernetes runtime (optional)

31. With a local cluster (kind/minikube) and `DOKLAB_RUNTIME=kubernetes`, create a local environment and confirm a Deployment appears in `DOKLAB_K8S_NAMESPACE`; stop/start scales it 0/1; the terminal uses `kubectl exec`.

## Roadmap

See [plan/sprints.md](plan/sprints.md) for sprint-level tracking and [plan/docklab_project_plan.md](plan/docklab_project_plan.md) for the full phase plan.

All planned sprints (1–10) and the former stretch goals (browser IDE, Kubernetes runtime, snapshots, collaboration, GitHub integration, template marketplace) are delivered. Remaining polish items are tracked in [Known limitations](#known-limitations).
