# DockLab

Cloud-based remote development environment platform starter.

## Architecture

```text
React Frontend
    ↓
Go API Server
    ↓
Terraform Runner Service
    ↓
AWS EC2 Instance
    ↓
Docker Workspace Container
    ↓
Browser Terminal via WebSockets (PTY + xterm.js)
```

## Repository layout

```text
cmd/server                 # Go API entrypoint
internal/
  handlers                 # HTTP handlers
  services                 # Business logic
  repositories             # Data-access abstractions
  models                   # Domain models
  middleware               # HTTP middleware (JWT)
  config                   # Env-based configuration
  database                 # PostgreSQL setup
pkg/logger                 # Shared structured logger
frontend/                  # React + TypeScript + Tailwind dashboard
```

## Backend features included

- `/health` endpoint
- PostgreSQL connection pool setup (`pgxpool`)
- Environment variable configuration
- JWT auth (`/api/v1/auth/register`, `/api/v1/auth/login`, protected `/api/v1/auth/me`)
- Password hashing with bcrypt
- Environment lifecycle APIs (`/api/v1/environments`, start/stop/delete actions)
- Terraform provisioning API (`/api/v1/environments/:id/provision`)
- Terraform cloud-termination API (`/api/v1/environments/:id/destroy-cloud`)
- Async operation status API (`/api/v1/operations/:id`)
- Postgres-persisted operation tracking for async cloud workflows
- Local Docker workspace lifecycle via backend service
- PTY-backed browser terminal (xterm.js + WebSocket + resize) for running environments
- Structured JSON logging (`log/slog`)

## Local development

### Prerequisites

- Go 1.25+
- Node.js 20+
- Docker + Docker Compose

### 1) Start backend + PostgreSQL

```bash
docker compose up --build
```

This setup mounts the Docker socket into the backend container so environment lifecycle actions can manage local containers.

Backend runs at `http://localhost:8080`.

### 2) Run frontend

```bash
cd frontend
npm install
npm run dev
```

Frontend runs at `http://localhost:5173`.

## How to use

1. Start the backend and PostgreSQL with Docker Compose.
2. Start the frontend dev server from the `frontend/` folder.
3. Open `http://localhost:5173` in your browser.
4. Create an account on `/register`.
5. Sign in on `/login`.
6. After login, you will be redirected to `/dashboard`.
7. Use the dashboard to create and manage local Docker environments.
8. Open Terminal on a running environment to run shell commands from the browser.
9. Use Provision on an environment to trigger Terraform-based EC2 provisioning.
10. Use Terminate EC2 to destroy cloud resources while keeping the environment.
11. Use Delete to remove both the environment and provisioned cloud resources.
12. Long-running cloud actions run asynchronously; the dashboard tracks progress until completion.
13. Operation status is persisted in PostgreSQL, so polling state survives backend restarts.

Terminal tips:
- Copy selected terminal text with `Ctrl+Shift+C`.
- Paste with `Ctrl+Shift+V`.
- Use `Reconnect` in the terminal panel if the socket drops.

Current available product flow includes authentication, local environment lifecycle management, browser terminal access, and Terraform-based EC2 provisioning.

Destructive actions are confirmed through in-app modal dialogs rather than browser alerts.

### 3) Environment configuration

Copy and adjust:

```bash
cp .env.example .env
```

Used variables:

- `APP_ENV`
- `PORT`
- `DATABASE_URL`
- `JWT_SECRET`
- `JWT_TTL_MINUTES`
- `AWS_ACCESS_KEY_ID` (required for Terraform provisioning)
- `AWS_SECRET_ACCESS_KEY` (required for Terraform provisioning)
- `AWS_SESSION_TOKEN` (optional, if using temporary credentials)
- `AWS_DEFAULT_REGION` (optional, default `us-east-1`)

## Validation commands

Backend:

```bash
go test ./cmd/... ./internal/... ./pkg/...
```

Frontend:

```bash
cd frontend
npm run build
```
