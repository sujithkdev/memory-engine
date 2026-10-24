# Memory Engine

A fast, privacy-focused long-term memory system for local LLMs.  
Built with Go + PostgreSQL + pgvector.

## Chat interface — Memory Studio

The `ui/` directory contains a responsive local chat interface with Ollama model
selection, streaming responses, saved conversations, and a memory context panel.
Run `node ui/server.mjs` and open `http://127.0.0.1:3000`.

### ⚡ OpenAI Drop-In Proxy (`/v1/chat/completions`)
Memory Engine provides an OpenAI-compatible proxy at `http://127.0.0.1:3000/v1`. Connect **Cursor**, **Continue.dev**, **Open WebUI**, **LibreChat**, or **Cline** directly to it—any client gets automatic long-term memory retrieval without modifying application code! See [UI setup and behavior](ui/README.md).
The implementation has progressed beyond the original Phase 1 overview below;
the Go module currently specifies Go 1.26.2.

---

## Architecture

```
memory-engine/
├── cmd/server/          ← Entry point (main.go)
├── internal/
│   ├── api/             ← HTTP handlers, router, response helpers
│   ├── config/          ← Config loading (YAML + env overrides)
│   ├── database/        ← pgxpool, migrations, query tracer
│   └── middleware/      ← Request logger, recoverer
├── migrations/          ← Versioned SQL (golang-migrate format)
├── configs/             ← config.yaml
├── tests/               ← Integration tests
└── docker-compose.yml   ← PostgreSQL + pgvector
```

---

## Phase 1 — What's Built

| Component | Status |
|---|---|
| Go project scaffold | ✅ |
| Config loading (YAML + env) | ✅ |
| PostgreSQL connection pool (pgxpool) | ✅ |
| Slow query tracer | ✅ |
| golang-migrate runner | ✅ |
| Database schema (users, conversations, messages) | ✅ |
| Structured logging (zerolog) | ✅ |
| Request/response logging middleware | ✅ |
| Panic recovery middleware | ✅ |
| `GET /health` — liveness probe | ✅ |
| `GET /ready` — readiness probe | ✅ |
| `GET /api/v1/` — API root | ✅ |
| JSON response envelope | ✅ |
| Graceful shutdown | ✅ |
| Integration tests | ✅ |

---

## Prerequisites

- Go 1.21+
- PostgreSQL 17 with pgvector extension  
  **Option A**: Use Docker (recommended)  
  **Option B**: Use local PostgreSQL (already installed)

---

## Quick Start

### Option A — Docker (easiest)

```bash
# Start PostgreSQL with pgvector
docker compose up -d postgres

# Wait for healthy status
docker compose ps

# Run the server
go run ./cmd/server --config configs/config.yaml
```

### Option B — Local PostgreSQL (Windows, already installed)

**Step 1** — Start PostgreSQL as Administrator:

Open PowerShell **as Administrator** and run:
```powershell
Start-Service postgresql-x64-17
```

Or use Windows Services (`services.msc`) → right-click `postgresql-x64-17` → Start.

**Step 2** — Create the database:
```powershell
# Add psql to PATH (adjust version if needed)
$env:Path += ";C:\Program Files\PostgreSQL\17\bin"

# Create database
psql -U postgres -c "CREATE DATABASE memory_engine;"

# Verify
psql -U postgres -c "\l"
```

**Step 3** — Run the server:
```powershell
go run ./cmd/server --config configs/config.yaml
```

---

## Configuration

All settings in `configs/config.yaml`. Override any value with environment variables:

| Env Var | Config Key | Example |
|---|---|---|
| `DB_HOST` | database.host | `localhost` |
| `DB_PORT` | database.port | `5432` |
| `DB_NAME` | database.name | `memory_engine` |
| `DB_USER` | database.user | `postgres` |
| `DB_PASSWORD` | database.password | `postgres` |
| `SERVER_PORT` | server.port | `8080` |
| `LOG_LEVEL` | log.level | `debug` / `info` |
| `LOG_FORMAT` | log.format | `pretty` / `json` |
| `APP_ENV` | app.environment | `development` / `production` |

---

## API Endpoints (Phase 1)

### Liveness — `GET /health`

Returns 200 as long as the process is running.

```bash
curl http://localhost:8080/health
```

```json
{
  "status": "ok",
  "name": "memory-engine",
  "version": "1.0.0",
  "timestamp": "2026-08-21T18:00:00Z"
}
```

---

### Readiness — `GET /ready`

Returns 200 when database is reachable. Returns 503 if degraded.

```bash
curl http://localhost:8080/ready
```

```json
{
  "status": "ok",
  "name": "memory-engine",
  "version": "1.0.0",
  "timestamp": "2026-08-21T18:00:00Z",
  "checks": {
    "database": { "status": "ok" },
    "pool": { "status": "ok", "message": "total=1 idle=1 acquired=0" }
  }
}
```

---

### API Root — `GET /api/v1/`

```bash
curl http://localhost:8080/api/v1/
```

```json
{
  "success": true,
  "data": {
    "name": "memory-engine",
    "status": "running",
    "version": "1.0.0"
  }
}
```

---

## Running Tests

Tests require a PostgreSQL instance. Uses a separate `memory_engine_test` database.

### With Docker:
```bash
docker compose up -d postgres-test

# Run tests against test DB (port 5433)
$env:TEST_DB_PORT = "5433"
go test ./tests/... -v
```

### With local PostgreSQL:
```powershell
# Create test database first
psql -U postgres -c "CREATE DATABASE memory_engine_test;"

go test ./tests/... -v
```

### Test environment variables:
| Var | Default |
|---|---|
| `TEST_DB_HOST` | `localhost` |
| `TEST_DB_NAME` | `memory_engine_test` |
| `TEST_DB_USER` | `postgres` |
| `TEST_DB_PASSWORD` | `postgres` |

---

## Database Schema (Phase 1)

```sql
users (id, username, email, display_name, is_active, created_at, updated_at)
conversations (id, user_id, title, namespace, is_archived, created_at, updated_at)
messages (id, conversation_id, role, content, token_count, sequence_number, created_at, source_model)
```

pgvector extension is enabled at migration time — ready for Phase 4 embeddings.

---

## What's Coming

| Phase | Feature |
|---|---|
| Phase 2 | Users, Conversations, Messages CRUD API |
| Phase 3 | Memory CRUD + lifecycle state machine + remember/forget |
| Phase 4 | Metadata: importance, confidence, salience, provenance, versioning |
| Phase 5 | Deduplication + conflict detection + audit log |
| Phase 6 | PostgreSQL full-text search |
| Phase 7 | pgvector + embedding interface |
| Phase 8 | Hybrid retrieval + reranking + token budget |
| Phase 9 | Background workers: decay, consolidation |
| Phase 10 | Caching + performance |
| Phase 11 | Local LLM (Ollama/Qwen) integration |
