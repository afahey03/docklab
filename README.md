# DockLab

Cloud-based remote development environment platform starter.

## Architecture

```text
React Frontend
    ↓
Go API Server
    ↓
Terraform Runner Service (planned)
    ↓
AWS EC2 Instance (planned)
    ↓
Docker Workspace Container (planned)
    ↓
Browser Terminal via WebSockets (planned)
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

Current available product flow is authentication and dashboard access.

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
