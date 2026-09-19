# LLM Evaluation Runner (the-judge)

Small production-minded Go service that runs evaluation suites against multiple LLM providers, stores results in Postgres, and executes work asynchronously via Redis/asynq.

## Stack

- Go 1.27+ API (`cmd/api`) + worker (`cmd/worker`)
- PostgreSQL (persistence; SQL migrations applied on API startup)
- Redis + asynq (job queue, retries)
- Providers: LM Studio (OpenAI-compatible), Google Gemini
- Deterministic validators only (exact, contains, regex, JSON, JSON schema, no_error, no_timeout)

## How to run (pick one mode)

### Mode A — Local-friendly (recommended while coding)

Postgres + Redis in Docker; **API and worker as normal Go processes** on your Mac.

```bash
cp .env.example .env          # once; edit LM Studio URL/key
make infra                    # docker: postgres + redis only
make local-api                # terminal 1 — loads .env, go run ./cmd/api
make local-worker             # terminal 2 — loads .env, go run ./cmd/worker
```

| You change… | What to do |
|-------------|------------|
| **Go code** | Stop the `go run` process (Ctrl+C), run `make local-api` / `make local-worker` again. New code is compiled on start. |
| **`.env`** | Same: restart that `go run` process. Env is read at process start only. |
| **Migrations** | Applied automatically when the API starts. |

Use `LMSTUDIO_BASE_URL=http://127.0.0.1:PORT/v1` (host loopback). Bruno stays at `http://localhost:8080`.

### Mode B — Everything in Docker

```bash
docker compose up -d --build
# or: make docker
```

| You change… | What to do |
|-------------|------------|
| **Go code** | `docker compose up -d --build api worker` (rebuild images). |
| **`.env`** | `make restart-apps` (recreate containers so they get new env). Compose reads `.env` only when creating containers. |
| **LM Studio from Docker** | Use `LMSTUDIO_BASE_URL=http://host.docker.internal:PORT/v1` — inside a container, `localhost` is the container, not your Mac. |

`the-judge-api-1` never hot-reloads `.env` or Go source. Recreate/rebuild is required.

### Mental model

```text
Bruno / curl  →  API (:8080)  →  Postgres + Redis
                      ↓ enqueue
                   Worker  →  LM Studio (your Mac)
```

The **worker** calls LM Studio. The API only accepts HTTP and enqueues jobs.

## API

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/v1/suites` | Create suite |
| `GET` | `/v1/suites` | List suites |
| `GET` | `/v1/suites/{id}` | Get suite + cases |
| `POST` | `/v1/suites/{id}/cases` | Add test case |
| `POST` | `/v1/runs` | Submit run (`suite_id` + `models[]`) |
| `GET` | `/v1/runs` | List recent runs (summaries) |
| `GET` | `/v1/runs/{id}` | Run status + counts |
| `GET` | `/v1/runs/{id}/results` | Compare results |
| `POST` | `/v1/runs/{id}/cancel` | Cancel run |
| `GET` | `/healthz` | Liveness |
| `GET` | `/readyz` | Postgres + Redis |

### Example

```bash
# Create suite
curl -s localhost:8080/v1/suites -H 'Content-Type: application/json' \
  -d '{"name":"capitals","description":"geo smoke"}'

# Add case (use suite id from response)
curl -s localhost:8080/v1/suites/$SUITE_ID/cases -H 'Content-Type: application/json' \
  -d '{
    "name":"france",
    "prompt":"What is the capital of France? Reply with one word.",
    "validators":[{"type":"contains","value":"Paris"}],
    "timeout_ms":20000
  }'

# Run against LM Studio and/or Gemini
curl -s localhost:8080/v1/runs -H 'Content-Type: application/json' \
  -d '{
    "suite_id":"'"$SUITE_ID"'",
    "models":[
      {"provider":"lmstudio","model":"local-model"},
      {"provider":"gemini","model":"gemini-2.0-flash"}
    ]
  }'
```

Set `GEMINI_API_KEY` for Gemini. Point `LMSTUDIO_BASE_URL` at your local OpenAI-compatible server (`http://localhost:1234/v1`, or `http://host.docker.internal:1234/v1` from containers).

## Run lifecycle

`pending` → `running` → `completed` | `failed` | `cancelled`

Each `(test_case × model)` becomes a `case_run` job. Transient provider errors retry via asynq; validation failures do not.

## Bruno collection

Open [`bruno/The Judge`](bruno/The%20Judge) in Bruno v3+/v4 (OpenCollection YAML).

1. Select the **Local** environment (`baseUrl` → `http://localhost:8080`, `lmstudioModel` → your LM Studio model id)
2. Prefer the **Smoke Parallel LM Studio** folder: run requests **01 → 08** in order
3. After **06 Create Run**, repeat **07 Poll** until `completed`/`failed`/`cancelled`, then **08 Get Run Results**
4. `suiteId` / `runId` are saved into the environment by post-response scripts

Do not “Run Folder” on **runs** if it includes **Cancel Run** — that will abort a fresh run.

```bash
make unit          # domain + validators (no infra)
make test          # all tests; store/runner tests skip if Postgres is down
make upgrade       # bump module deps, tidy, and re-run tests
```

## Layout

See `internal/` for domain, API, store, providers, validators, jobs, and runner. Observability hooks live in `internal/obs` (slog now; OpenTelemetry later).

## V1 non-goals

Auth, UI, streaming, LLM-as-judge, exact-once provider calls. Those are intentional seams for later.
