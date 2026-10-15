# CV Evaluator

CV Evaluator is a Go project for CV-to-job matching. It extracts text from a PDF CV, stores CV and job embeddings in PostgreSQL (`pgvector`), retrieves the most relevant CV sections by vector similarity, and asks an LLM (DeepSeek) to produce a structured JSON match result.

## Current status

- CLI entrypoints under `cmd/` are the main way to use the project.
- `cmd/api` serves the CV and match routes over HTTP, behind JWT auth. There are no HTTP routes yet to sign up or sign in, so tokens have to be minted outside the API for now.
- Core logic lives in service packages and is covered by unit and integration tests.

## How it works

1. **CV processing** (`users.Service.ProcessCV`): extracts text from the PDF, embeds the full CV, asks a chat LLM to split it into sections (`summary`, `experience`, `skills`, `education`, `title`), and stores an embedding per section.
2. **Job ingest** (`jobs.Service.ProcessJob`): scrapes LinkedIn search results and job details, stores each posting once globally (unique on provider and job ID), and embeds its description once.
3. **Matching** (`matcher.Service`): embeds the job description, fetches the user's 10 closest CV sections by cosine distance, computes weighted section scores, builds a CV context and sends it to DeepSeek with the `jobmatch.tpl` prompt.

Section weights used for scoring:

| Section    | Weight |
|------------|--------|
| skills     | 0.40   |
| experience | 0.25   |
| education  | 0.15   |
| summary    | 0.10   |
| title      | 0.10   |

The LLM response is decoded into:

```json
{
  "overall_score": 0,
  "skills_score": 0,
  "experience_score": 0,
  "seniority_score": 0,
  "domain_score": 0,
  "location_score": 0,
  "strengths": [],
  "missing_skills": [],
  "summary": ""
}
```

## Models used

- **Embeddings**: Ollama `nomic-embed-text` (768 dimensions, matching the `vector(768)` columns).
- **Job matching**: DeepSeek `deepseek-chat` with JSON response format.
- **CV cleanup / sectioning**: DeepSeek by default, or Ollama `qwen3:8b` with `CV_CLEANUP_PROVIDER=ollama`.

## Project layout

```
cmd/
  api/        HTTP API server
  worker/     background scheduler and task workers
  seed/       create (or reuse) a user and process a CV
  loader/     scrape LinkedIn jobs and store them with embeddings
  byjobs/     match a user's CV against a stored job
  local/      match a user's CV against data/senior-software-engineer.txt
db/           Postgres/GORM connection from env vars
internal/
  config/                 environment-based configuration and validation
  entities/               DTOs shared between scrapers, services and LLMs
  models/                 GORM models: User, CV, SectionEmbedding, Job
  migrator/               enables the vector extension and auto-migrates models
  integrations/extractor/ PDF text extraction
  llm/                    CV cleanup provider selection
  llm/ollama/             embeddings and CV cleanup via Ollama
  llm/deepseek/           CV cleanup and job matching via DeepSeek
  llm/prompts/            embedded prompt files
  scraper/linkedin/       LinkedIn search and job detail scraping
  scraper/jobindex/       Jobindex.dk scraper (not yet used by any command)
  services/users/         signup/signin/profile (JWT + bcrypt) and CV upload/list/get/delete/process
  services/jobs/          job ingest; each posting is stored and embedded once
  queue/                  Postgres task queue (SKIP LOCKED, leases, retries)
  pipeline/               background search, embed and evaluate handlers and scheduler
  services/matcher/       retrieval, scoring and LLM match composition
  services/cleanup/       extract-then-cleanup helper (not yet used by any command)
  storage/jobs/           job repository helpers
  servers/                Gin server: tracing, recovery, request logging, /healthz, graceful shutdown
  servers/cv/             CV HTTP handlers
  servers/match/          match HTTP handlers
  servers/middleware/     Bearer JWT auth middleware
  telemetry/              OpenTelemetry setup, run and external request metrics
  telemetry/telemetrytest/ in-memory providers for asserting spans and metrics in tests
  testutil/postgres/      pgvector testcontainer helper
```

## Prerequisites

- Go `1.25+` (CI runs on Go `1.26`)
- Docker (for local Postgres and for the integration tests, which use testcontainers)
- Ollama, reachable via `OLLAMA_BASE_URL`
- A DeepSeek API key (needed for matching)
- Optional tools:
  - `golangci-lint` (for `make lint`)
  - `sift` (for `make fancy-test`)

## Configuration

Copy `.env.example`:

```bash
cp .env.example .env
```

Env keys:

```dotenv
ENVIRONMENT=development
PORT=8080
DEEPSEEK_API_KEY=your-deepseek-api-key
OLLAMA_BASE_URL=http://localhost:11434
CV_CLEANUP_PROVIDER=deepseek
DB_HOST=localhost
DB_PORT=5432
DB_NAME=cv_evaluator
DB_USER=postgres
DB_PASSWORD=postgres
JWT_SECRET=change-me-with-a-long-random-value
OTEL_EXPORTER_OTLP_ENDPOINT=
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
SERVICE_VERSION=dev
```

Notes:

- `cmd/api` and `cmd/seed` read settings through `internal/config`, which fails at startup if a required value is missing. The other CLI entrypoints load `.env` with `godotenv.Load()` and exit if it is missing; `cmd/api` treats `.env` as optional.
- `ENVIRONMENT` defaults to `production`. Outside `development`, `JWT_SECRET` is required; in `development` a random secret is generated when it is empty, and tokens will not survive a restart. The worker (`cmd/worker`) never issues or checks tokens, so it doesn't need `JWT_SECRET`.
- `CV_CLEANUP_PROVIDER` is `deepseek` (default) or `ollama`.
- `PORT` defaults to `8080`.
- The `.env.example` DB values match `docker-compose.yml`.
- Telemetry is off unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set (see [Observability](#observability)).

## Quick start

1. Start local Postgres with pgvector:

   ```bash
   docker compose up -d
   ```

2. Pull the embedding model:

   ```bash
   ollama pull nomic-embed-text
   # optional, only needed with CV_CLEANUP_PROVIDER=ollama
   ollama pull qwen3:8b
   ```

3. Create `.env` and fill in real values:

   ```bash
   cp .env.example .env
   ```

4. Seed a user and process a CV. This also runs the database migrations. Note the user ULID it prints.

   ```bash
   make seed-cv ARGS='-cv ./data/dummy-resume.pdf -email you@example.com -first-name Your -last-name Name -phone +1234567890 -password strongpassword'
   ```

   If the user already has a CV, processing is skipped; pass `-force` to process another one. Without flags, the seed uses the defaults hardcoded in `cmd/seed/main.go`.

5. Load jobs. `cmd/loader` has a hardcoded user ID and LinkedIn search URL, so edit `cmd/loader/main.go` to use the ULID from step 4 first:

   ```bash
   go run ./cmd/loader
   ```

6. Match against a stored job. `-job-id` is the provider's job ID (the `job_id` column), not the row ULID:

   ```bash
   make by-job userId=<USER_ULID> jobId=<JOB_ID>
   # or
   go run ./cmd/byjobs -user-id=<USER_ULID> -job-id=<JOB_ID>
   ```

## Common commands

```bash
# HTTP API on :8080 (ENVIRONMENT=development)
make start
curl localhost:8080/healthz

# Seed a user and process a CV
go run ./cmd/seed -cv ./data/dummy-resume.pdf -email you@example.com

# Scrape and store LinkedIn jobs
go run ./cmd/loader

# Match by stored job
make by-job jobId=<JOB_ID> userId=<USER_ULID>

# Match against data/senior-software-engineer.txt (user ID is hardcoded in cmd/local/main.go)
go run ./cmd/local
```

## Testing and quality

Tests that touch the database start a `pgvector/pgvector:pg16` container through testcontainers, so Docker must be running.

```bash
make test             # go test ./... --cover
make test-with-race   # with -race
make shuffle-test     # -shuffle=on --count=2
make fancy-test       # pipes JSON output through sift
make lint             # golangci-lint
make clean            # clears the test cache
make                  # clean + lint + test
```

CI (`.github/workflows`) runs lint and tests on pushes to `main`, `staging`, `testing` and `develop`, and on pull requests to `main`, `develop` and `testing`.

## HTTP API

`make start` runs the server. `GET /healthz` returns `{"status":"ok"}` when the database is reachable, and `503` otherwise. On `SIGINT`/`SIGTERM` the server stops accepting requests and lets in-flight ones finish for up to 10 seconds.

CV routes (`internal/servers/cv`), all authenticated with a Bearer session token (JWT) or API key:

| Method | Path               | Description                                    |
|--------|--------------------|------------------------------------------------|
| POST   | `/cv/`             | Upload a CV (multipart field `cv`), returns 202 |
| GET    | `/cv/`             | List the user's CVs                            |
| GET    | `/cv/download/:id` | Download the extracted CV text                 |
| PATCH  | `/cv/`             | Not implemented (501)                          |
| DELETE | `/cv/:id`          | Delete a CV, returns 204                       |

Match routes (`internal/servers/match`), authenticated the same way:

| Method | Path                      | Description                                  |
|--------|---------------------------|----------------------------------------------|
| POST   | `/match/job-description`  | Body `{"job_description": "..."}`            |
| POST   | `/match/job/:id`          | Match against a stored job                   |
| POST   | `/match/job-link`         | Not implemented (501)                        |

Accounts and API keys: `POST /auth/signup`, `POST /auth/signin`, `GET /me`, and `POST`/`GET /api-keys`, `DELETE /api-keys/:id`. The full reference, with request and response examples, is in [`api/openapi.yaml`](api/openapi.yaml).

### Job searches

A job search is a saved query (`query`, optional `location`) that runs on a schedule (`hourly`, `daily` or `weekly`) against **every** supported job portal. Users don't pick a portal. A new or re-enabled search is due straight away, and disabling a search clears its `next_run_at`. Runs happen in the background: the API only sets `next_run_at`, and the scheduler picks up searches that are due.

| Method | Path                       | Description                                              |
|--------|----------------------------|----------------------------------------------------------|
| POST   | `/job-searches`            | Create a search, returns 201                             |
| GET    | `/job-searches`            | List the user's searches (`{"object":"list","data":[...]}`) |
| GET    | `/job-searches/:id`        | Retrieve a search                                        |
| PATCH  | `/job-searches/:id`        | Update the fields sent (e.g. `{"enabled": false}`)       |
| DELETE | `/job-searches/:id`        | Delete a search                                          |
| POST   | `/job-searches/:id/run`    | Queue an enabled search to run now, returns 202          |
| GET    | `/providers`               | Read-only list of supported portals (LinkedIn, Jobindex) |
| POST   | `/portal-suggestions`      | Suggest a new portal (`url`, optional `name`, `note`); 409 if already suggested or supported |
| GET    | `/portal-suggestions`      | List the user's suggestions and their review `status`    |

```bash
curl -X POST localhost:8080/job-searches \
  -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' \
  -d '{"query":"senior go engineer","location":"Copenhagen","frequency":"daily"}'
```

Portals are implemented in `internal/providers` on top of the scrapers in `internal/scraper`. Each provider builds its search URL from the query and location.

## Background worker

`cmd/worker` (`make worker`) runs saved job searches on a schedule with no manual steps. It reads the same settings as the API through `internal/config`, opens the database, runs the migrations, then starts a scheduler and a pool of workers. `SIGINT`/`SIGTERM` stops claiming new tasks and gives in-flight ones `WORKER_SHUTDOWN_GRACE` to finish; anything cut short is retried later.

Work goes through a Postgres queue (`internal/queue`, table `queue_tasks`). Workers claim tasks with `SELECT ... FOR UPDATE SKIP LOCKED` and hold a renewable lease, so a crashed worker's task is picked up again once its lease expires. Failed tasks retry with exponential backoff and are marked `dead` after their last attempt.

The flow (`internal/pipeline`):

1. The scheduler finds due searches and queues one `run_search` task per search and portal, then sets the search's next run from its frequency (hourly, daily or weekly).
2. `run_search` fetches postings from that portal. Each posting is stored once, globally, on (provider, job id), and linked to the search as a search result.
3. `embed_job` fetches a job's description if it is missing and embeds it, once per job.
4. `evaluate_job` evaluates the job against the user's most recent CV and stores the scores, strengths, missing skills, summary and raw LLM output. Users without a CV are skipped.

Re-running any task never duplicates jobs, results or evaluations. Portal calls are rate limited per portal and back off after failures, and LLM evaluations are capped per process.

Settings (all optional):

| Variable                 | Default | Meaning                                  |
|--------------------------|---------|------------------------------------------|
| `WORKER_CONCURRENCY`     | `4`     | Tasks processed at once                  |
| `WORKER_MAX_EVALUATIONS` | `2`     | LLM evaluations running at once          |
| `WORKER_POLL_INTERVAL`   | `1s`    | Idle wait between queue polls            |
| `SCHEDULER_INTERVAL`     | `1m`    | How often due searches are looked for    |
| `WORKER_SHUTDOWN_GRACE`  | `30s`   | Time in-flight tasks get on shutdown     |

The worker does not run any searches yet: saved searches and the portal providers are not wired in, so the scheduler finds nothing to run.

## Observability

`cmd/api` and `cmd/seed` set up OpenTelemetry at startup (`internal/telemetry`). Traces and metrics are exported over OTLP/HTTP when `OTEL_EXPORTER_OTLP_ENDPOINT` is set; otherwise no-op providers are used, so local runs and tests need nothing running.

| Setting | Default | Notes |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | empty (export off) | Base URL, e.g. `http://localhost:4318`. Must be `http(s)://host[:port]`. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | Only `http/protobuf` is supported. |
| `SERVICE_VERSION` | `dev` | Reported as `service.version`. |
| `OTEL_SDK_DISABLED` | `false` | `true` turns export off even with an endpoint set. |

The exporters also honour the other standard variables, such as `OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_EXPORTER_OTLP_TIMEOUT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`, `OTEL_METRIC_EXPORT_INTERVAL` and `OTEL_RESOURCE_ATTRIBUTES`. The resource carries `service.name=cv-evaluator`, `service.version` and `deployment.environment(.name)` from `ENVIRONMENT`.

What is recorded:

- **HTTP requests**: a server span per request from `otelgin` (`/healthz` is skipped), plus otelgin's standard HTTP server metrics. Request logs include `trace_id`.
- **`cv_evaluator.background_run.duration`** (histogram, seconds), attributes `run.kind` and `run.outcome` (`success`|`error`), with a span `background_run <kind>`. Use `telemetry.StartRun(ctx, kind)`, or `telemetry.RunObserver{}` where an observer hook (`RunStarted`/`RunFinished`) is expected. `cmd/seed` records `seed.process_cv`.
- **`cv_evaluator.external_request.duration`** (histogram, seconds), attributes `peer.service`, `operation`, `outcome` (and `llm.task` for DeepSeek), with a client span `<peer.service> <operation>`. Ollama (`embedding`, `cv_cleanup`) and DeepSeek (`chat_completion`, `llm.task=cv_cleanup|job_match`) are instrumented. HTTP clients use `telemetry.NewHTTPClient(peer, client)` with `telemetry.WithOperation(ctx, op)`; other SDK calls use `telemetry.TrackExternal(ctx, peer, op)`. `telemetry.NewJobBoardTransport(nil)` is one transport for the scrapers that labels `peer.service` (`linkedin`, `jobindex`) from the host and `operation` (`search`, `details`) from the path.

### Viewing locally

`docker compose up -d` starts `otel-lgtm`, a single Grafana container that receives traces and metrics. Point the app at it:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 make start
```

Open Grafana at <http://localhost:3000>: traces are under Explore → Tempo (service `cv-evaluator`), and the histograms under Explore → Prometheus as `cv_evaluator_background_run_duration_seconds` and `cv_evaluator_external_request_duration_seconds`.

In a deployed environment, set `OTEL_EXPORTER_OTLP_ENDPOINT` (and `OTEL_EXPORTER_OTLP_HEADERS` for the API key) to your hosted OpenTelemetry provider instead; nothing else changes.

Metrics are pushed every 60 seconds by default; set `OTEL_METRIC_EXPORT_INTERVAL=5000` (milliseconds) to see them sooner.

## Limitations

- CV processing runs inside the upload request and can take minutes.
- `cmd/loader` and `cmd/local` use hardcoded user IDs, and `cmd/loader` uses a hardcoded LinkedIn search (Senior Software Engineer, Copenhagen, 1 page). `cmd/loader` does not run migrations.
- LinkedIn scraping depends on page structure and may break over time.
