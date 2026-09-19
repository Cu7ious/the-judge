# LLM Evaluation Runner (the-judge)

Small production-minded Go service that runs evaluation suites against multiple LLM providers, stores results in Postgres, and executes work asynchronously via Redis/asynq.

## Stack

- Go 1.27+ API (`cmd/api`) + worker (`cmd/worker`)
- PostgreSQL (persistence; SQL migrations applied on API startup)
- Redis + asynq (job queue, retries)
- Providers: LM Studio (OpenAI-compatible), Google Gemini
- Deterministic validators only (exact, contains, regex, JSON, JSON schema, no_error, no_timeout)

## Quick start

```bash
cp .env.example .env
docker compose up -d --build
# API: http://localhost:8080
```

Or run infra only and binaries locally:

```bash
docker compose up -d postgres redis
export $(grep -v '^#' .env.example | xargs)
go run ./cmd/api
go run ./cmd/worker
```

## API

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/v1/suites` | Create suite |
| `GET` | `/v1/suites` | List suites |
| `GET` | `/v1/suites/{id}` | Get suite + cases |
| `POST` | `/v1/suites/{id}/cases` | Add test case |
| `POST` | `/v1/runs` | Submit run (`suite_id` + `models[]`) |
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

1. Select the **Local** environment (`baseUrl` defaults to `http://localhost:8080`)
2. Run **Health → Readyz**, then **Suites → Create Suite → Create Case → Runs → Create Run**
3. `suiteId` / `runId` are saved into the environment by post-response scripts

```bash
make unit          # domain + validators (no infra)
make test          # all tests; store/runner tests skip if Postgres is down
make upgrade       # bump module deps, tidy, and re-run tests
```

## Layout

See `internal/` for domain, API, store, providers, validators, jobs, and runner. Observability hooks live in `internal/obs` (slog now; OpenTelemetry later).

## V1 non-goals

Auth, UI, streaming, LLM-as-judge, exact-once provider calls. Those are intentional seams for later.
