# CV Evaluator

CV Evaluator is a Go project for CV-to-job matching. It extracts text from a PDF CV, stores CV and job embeddings in PostgreSQL (`pgvector`), and asks an LLM (DeepSeek) to evaluate the full CV against each job, producing a structured JSON result with evidence and per-criterion scores.

## Current status

- CLI entrypoints under `cmd/` are the main way to use the project.
- `cmd/api` serves the CV and match routes over HTTP, behind JWT auth. There are no HTTP routes yet to sign up or sign in, so tokens have to be minted outside the API for now.
- Core logic lives in service packages and is covered by unit and integration tests.

## How it works

1. **CV processing** (`users.Service.ProcessCV`): extracts text from the PDF, embeds the full CV, asks a chat LLM to parse it into sections (`title`, `summary`, `skills`, `experience`, `education`, `certifications`, `languages`, `projects`), and stores an embedding per non-empty section.
2. **Job ingest** (`jobs.Service.ProcessJob`): scrapes LinkedIn search results and job details, stores each posting once globally (unique on provider and job ID), and embeds its description once.
3. **Matching** (`matcher.Service`): sends DeepSeek the `jobmatch.tpl` prompt with the job description and the **full** CV text (a CV is short, so nothing is filtered out). The instructions, output schema and score scale go in the system message; the job (inside `<job_description>` tags) and the CV (inside `<cv>` tags) go in the user message, and the prompt treats tagged content as data, never instructions. Temperature is 0.2, so re-running a match gives stable scores. Job descriptions in other languages (for example Danish) are evaluated as written, with the output in English.

The criteria the LLM scores are defined in one list, `entities.EvaluationCriteria`: skills, experience, seniority, domain and education. Location is deliberately not scored: every search already filters by the location the user chose, and users may search far from where they live, so the prompt tells the LLM not to penalise it and only to mention it in the summary when the job needs on-site work or relocation. The prompt's response schema and instructions are generated from that list, and only those criteria are kept when parsing. To add a criterion, add it there and bump `pipeline.DefaultPromptVersion`.

The LLM fills the response in order: first the job's **requirements** (must-have or nice-to-have, whether the CV meets each, and the CV evidence), then the per-criterion **breakdown**, then the **overall score**, so it reasons before it scores. Every score uses one scale: 90-100 meets every must-have and most nice-to-haves, 70-89 meets every must-have with some gaps, 50-69 misses one must-have or is at the wrong level, 0-49 misses several. A missing must-have caps the overall score at 69, which the matcher also enforces in code.

The LLM response, which is also the `/match/*` response body, looks like this:

```json
{
  "requirements": [
    {"requirement": "5+ years of Go", "importance": "must", "met": "yes", "evidence": "6 years building Go microservices"},
    {"requirement": "Logistics domain", "importance": "nice", "met": "no", "evidence": ""}
  ],
  "breakdown": [
    {"criterion": "skills", "score": 85, "reason": "Strong Go and Postgres"},
    {"criterion": "domain", "score": 70, "reason": "Payments experience, new to logistics"}
  ],
  "overall_score": 78,
  "strengths": ["6 years of production Go", "Kafka event pipelines"],
  "missing_skills": ["Logistics domain"],
  "summary": "You meet every must-have ..."
}
```

Scores are integers from 0 to 100. A criterion the LLM could not assess is left out of `breakdown`; it is never reported as 0. `strengths` and `missing_skills` hold at most 5 items each. Matching without a CV returns `422` on the `/match/*` routes. The background worker stores `overall_score` on `evaluations` and one `evaluation_scores` row per criterion, indexed on `(criterion, score)` for filtering.

### Prompt evaluation

`make eval-prompt` runs 11 fictional CV and job pairs against the real DeepSeek API and checks that each overall score lands in its expected band. The cases cover a strong fit, a wrong discipline, a junior candidate, a missing must-have, a Danish job description, a prompt-injection attempt, and a candidate whose only Go experience is side projects. It also reports how much scores vary between runs. Cases live in `internal/services/matcher/testdata/prompt_eval.json`; run it before and after changing a prompt. It needs `DEEPSEEK_API_KEY`, makes 33 requests (`EVAL_RUNS=3` per case) and is excluded from normal test runs and CI by the `eval` build tag.

The same target runs the cover letter cases in `internal/services/coverletter/testdata/cover_letter_eval.json` (also alone with `make eval-cover-letter`): a strong fit, a missing must-have and a prompt-injection attempt in the job description. Each letter must pass every server-side check, use at least three facts from the CV and leave out what the injection asks for. `EVAL_SHOW=1` logs the letters.

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
  services/users/         signup/signin/profile/password (JWT + bcrypt) and CV upload/list/get/delete/process
  services/account/       account export and permanent deletion of everything a user owns
  services/jobs/          job ingest; each posting is stored and embedded once
  pipeline/               background search, embed and evaluate handlers and scheduler
  services/matcher/       retrieval, scoring and LLM match composition
  services/cleanup/       extract-then-cleanup helper (not yet used by any command)
  storage/jobs/           job repository helpers
  servers/                Gin server: tracing, recovery, request logging, /healthz, graceful shutdown
  servers/cv/             CV HTTP handlers
  servers/match/          match HTTP handlers
  servers/account/        account management handlers (PATCH/DELETE /me, password, export)
  servers/middleware/     Bearer JWT / API key auth middleware, with optional session validation
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
| POST   | `/match/cover-letter`     | Write a cover letter, see [Cover letters](#cover-letters) |

Accounts and API keys: `POST /auth/signup`, `POST /auth/signin`, `GET /me`, and `POST`/`GET /api-keys`, `DELETE /api-keys/:id`. The full reference, with request and response examples, is in [`api/openapi.yaml`](api/openapi.yaml).

### Cover letters

`POST /match/cover-letter` (`internal/servers/coverletter`, logic in `internal/services/coverletter`) writes a cover letter for a pasted job. The body is `{"job_description": "...", "evaluation": {...}}`, where `evaluation` is optional: the quick-match result the client already has, used as data only. The CV (the user's newest) and the name that signs the letter (from the profile) always come from the server. The response is `{"subject": "...", "letter": "...", "word_count": n}`; nothing is stored.

- **Prompt.** `internal/llm/prompts/coverletter.tpl` keeps the rules in the system message and the name, job, CV and evaluation in the user message, inside `<job_description>`, `<cv>` and `<evaluation>` tags that are stripped from every input first. It asks for a formal English letter of 250-350 words in 3-4 paragraphs, "Dear Hiring Manager," (unless the posting names a person), built around the 2-3 most important requirements the CV meets, each backed by a concrete CV fact, a gap mentioned only for an unmet must-have and never apologetically, and none of the banned clichés (`entities.CoverLetterBannedPhrases`). It is sent at temperature 0.4 and recorded with `llm.task=cover_letter`.
- **Checks.** Before a letter is returned the server checks it (`coverletter.Check`): 200-400 words, a one-line subject, a `Dear ...,` salutation, a closing (`Kind regards,` and similar) followed by the user's full name, and no profanity (word list in `profanity.go`, whole words only), banned clichés, exclamation marks, links, brackets or placeholders, or email addresses and phone numbers other than the user's own. A letter that fails is sent back to the LLM once with the list of failed checks; if the rewrite fails too the response is `502 cover_letter_failed`, and the names of the failed checks (never the letter) are logged.
- **Errors.** `400` for a missing or longer than 20,000 characters `job_description`, `422 cv_missing` without a CV, `429 rate_limited` (with `Retry-After`), `502 cover_letter_failed`, `504 timeout`.
- **Rate limit.** 5 letters per user per minute (`coverletter.DefaultLimit`), counted for well-formed requests only. The limiter (`internal/servers/ratelimit`) is in memory, so it applies per API process and resets on restart; running several API instances multiplies the limit, and a shared store would be needed then.
- **Reuse.** `Service.Write` takes the CV, candidate, job and evaluation directly, so a later `POST /results/:id/cover-letter` can pass a stored result's job, evaluation and CV.

```bash
curl -X POST localhost:8080/match/cover-letter \
  -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' \
  -d '{"job_description":"Senior Go engineer ..."}'
```

### Account management

Routes in `internal/servers/account`, authenticated with a session token or API key and always scoped to the caller:

| Method | Path           | Body                                     | Response |
|--------|----------------|------------------------------------------|----------|
| PATCH  | `/me`          | any of `first_name`, `last_name`, `email`, `phone` | 200, the user. Same trimming and email rules as signup; `409 email_already_used` / `phone_already_used` |
| POST   | `/me/password` | `current_password`, `new_password`        | 200, a fresh session (`token`). `400 incorrect_password` when the current password is wrong |
| GET    | `/me/export`   |                                          | 200, a JSON download (`cv-evaluator-export.json`) of everything stored about the user |
| DELETE | `/me`          | `password`                               | 204. `400 incorrect_password`, `404` if the account is already gone |

- **Password changes end other sessions.** `users.password_changed_at` is set on every change, and the auth middleware (`middleware.WithSessionValidator`, wired in `cmd/api`) rejects JWTs whose `iat` is before it with `401 invalid_token`. The response carries a fresh token so the current client stays signed in. JWT times are whole seconds, so a token issued in the same second as the change is still accepted. **API keys are not affected**; revoke them separately with `DELETE /api-keys/:id`.
- **Deletion is permanent.** It asks for the password even with an API key, then in one transaction hard-deletes (soft-deleted rows included) evaluation scores, evaluations, search results, job searches, CV sections, CVs, API keys, portal suggestions and the user, in that order. Job postings are shared and are kept. An audit line (`account deleted`, with the user id and row counts, no personal data) is logged.
- **The worker cannot resurrect a deleted account.** Deletion first locks the user's job searches and search results. A worker writing results takes a share lock on the job search and skips the insert when it is gone; one finishing an evaluation locks the search result and drops the outcome when it is gone. Whichever comes second sees the other's work.
- **The export** contains the profile, CVs (extracted text and sections), job searches, results with their job and evaluations (scores, strengths, missing skills, summary), API key metadata and portal suggestions, deleted and revoked items included. It never contains the password hash, API key secrets or hashes, embedding vectors or raw LLM output.

```bash
curl -X POST localhost:8080/me/password \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"current_password":"old-password","new_password":"a-new-password"}'
curl -OJ localhost:8080/me/export -H "Authorization: Bearer $TOKEN"
```

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

### Results

A result is a job one of the user's searches found, with its current evaluation against their CV (scored in the background by the worker). Results of deleted searches are not shown. Routes are in `internal/servers/results`, logic in `internal/services/results`.

| Method | Path                          | Description                                                       |
|--------|-------------------------------|-------------------------------------------------------------------|
| GET    | `/results`                    | The user's results across searches, a page at a time             |
| GET    | `/job-searches/:id/results`   | The same for one search (404 if it isn't the user's)             |
| GET    | `/results/summary`            | Dashboard counts: new this week, strong matches (80+), by status, by search |
| GET    | `/results/:id`                | One result with the job description and the full evaluation      |
| PATCH  | `/results/:id`                | Set the user's triage `status`: `new`, `saved`, `applied`, `dismissed` |
| POST   | `/results/:id/re-evaluate`    | Queue it for evaluation again, returns 202; 409 if already queued or running |

The lists take `min_score`, `status` and `evaluation_status` (repeat them or separate values with commas), `since` (RFC 3339), `criterion` with `min_criterion_score` (e.g. `criterion=skills&min_criterion_score=80`), `sort` (`score`, the default: best first and unscored last; or `newest`) and cursor pagination with `limit` (default 20, at most 100) and `starting_after` (the last `id` of the previous page). The response is `{"object":"list","data":[...],"has_more":...,"total_count":...}`. Each listed result has its search, the job (without its description) and, once it has a completed evaluation, `overall_score` and its top three missing skills. A single result adds the description and the evaluation's breakdown (in the criteria order), strengths, missing skills, summary, requirements (`null` for evaluations made before requirements were asked for), CV, model and prompt version.

```bash
curl "localhost:8080/results?min_score=70&status=new,saved&limit=10" -H "Authorization: Bearer $API_KEY"
```

So that filtering and sorting by score never scans evaluations, `search_results` keeps a pointer to its current evaluation (`evaluation_id`) and a copy of that evaluation's `overall_score` (null unless it completed). The worker sets both when it writes an evaluation, and migration backfills them for results evaluated before the columns existed. The composite indexes on `(user_id, overall_score DESC NULLS LAST, id DESC)`, `(job_search_id, overall_score DESC NULLS LAST, id DESC)`, `(user_id, created_at DESC, id DESC)` and `(user_id, status)` serve the two sort orders, per user and per search, and the counts.

## Background worker

`cmd/worker` (`make worker`) runs saved job searches on a schedule with no manual steps. It reads the same settings as the API through `internal/config`, opens the database, runs the migrations, then starts the scheduler and the embed and evaluate goroutines. On `SIGINT`/`SIGTERM` it stops claiming new work and gives in-flight steps `WORKER_SHUTDOWN_GRACE` to finish. Anything cut short is released and picked up again later.

There is no separate queue table. Progress is tracked on the rows themselves (`internal/pipeline`):

| Table            | State column        | Flow                                                                  |
|------------------|---------------------|-----------------------------------------------------------------------|
| `job_searches`   | `next_run_at`       | due → claimed (`next_run_at` pushed out as a lease) → run → next run |
| `jobs`           | `status`            | `new` → `described` → `embedded` (or `failed`); `processing` while a worker has it |
| `search_results` | `evaluation_status` | `pending` → `done` (or `failed`); `processing` while a worker has it  |

Each step goes **claim → release → work → write**:

1. **Claim**: one short statement picks one ready row with `FOR UPDATE SKIP LOCKED`, sets it to `processing` with `claimed_at`, and counts the attempt.
2. **Work**: the scrape, embedding or LLM call runs with no transaction or connection held.
3. **Write**: one short transaction stores the result and the final state. On failure, the row goes back to its ready state with `next_attempt_at` backed off exponentially, and it is marked `failed` after the last attempt.

If a worker dies, its row stays `processing`. Once it is older than `WORKER_STALE_TIMEOUT`, another worker claims it again. Every write is an idempotent upsert, so the rare double run is harmless.

Searches are claimed with a conditional update of `next_run_at`, so with several worker processes each run happens once. A claimed search runs all portals concurrently. Each portal has its own rate limit and a few retries, and a portal that keeps failing is logged without affecting the others.

Evaluations use the user's most recent CV. Users without a CV are checked again an hour later. Re-evaluating a result means setting its `evaluation_status` back to `pending`.

Settings (all optional):

| Variable                    | Default | Meaning                                                    |
|-----------------------------|---------|------------------------------------------------------------|
| `WORKER_SEARCH_CONCURRENCY` | `2`     | Searches run at once (each runs all portals)               |
| `WORKER_EMBED_CONCURRENCY`  | `4`     | Jobs described and embedded at once                        |
| `WORKER_EVAL_CONCURRENCY`   | `2`     | LLM evaluations at once                                    |
| `WORKER_STALE_TIMEOUT`      | `5m`    | When a `processing` row counts as abandoned (keep it above the 2m LLM timeout) |
| `WORKER_POLL_INTERVAL`      | `2s`    | Idle wait before looking for work again                    |
| `SCHEDULER_INTERVAL`        | `1m`    | How often due searches are looked for                      |
| `WORKER_SHUTDOWN_GRACE`     | `30s`   | Time in-flight steps get on shutdown                       |
| `WORKER_DB_MAX_OPEN_CONNS`  | `10`    | Database pool size. No connection is held during slow work, so this need not match the concurrency |

The worker reads due searches from `job_searches` and fetches from every portal in `internal/providers` (adapter: `internal/pipeline/sources`). It never issues or checks tokens, so it doesn't need `JWT_SECRET`.

## Web app

`web/` holds the browser app: Vue 3, Vite, Vue Router, Tailwind CSS v4 and TypeScript. It needs Node 24.

```bash
cd web
npm ci
npm run dev          # http://localhost:5173, proxies /api/* to the Go API
```

Start the API first (`make start`, on `:8080`). The app calls the API under `/api`, and the Vite dev server forwards those calls to `http://localhost:8080` without the prefix, so the Go server needs no CORS setup. Set `API_URL` to proxy somewhere else, or `VITE_API_BASE_URL` at build time when the API is served from another origin.

```bash
npm run build        # production build in web/dist
npm run preview      # serve the build (same /api proxy)
npm run lint         # ESLint + Prettier check
npm run typecheck    # vue-tsc
npx playwright install chromium   # once
npm run test:e2e     # Playwright, desktop and 375px mobile; the API is mocked
```

Job searches (`/searches`, `/searches/new`, `/searches/:id/edit`) list, create, edit, pause, run and delete saved searches, show the portals every search runs on (`GET /providers`) and let users suggest new portals (`/portal-suggestions`). Playwright mocks the API per test with `web/tests/support/mockApi.ts` (`mockApi` for the shell and overview, `mockJobSearchesApi` for job searches, providers and portal suggestions).

Quick match (`/quick-match`) can write a cover letter for the job it just scored (`POST /match/cover-letter`, via `web/src/api/coverLetter.ts`): a banner offers it for scores of 80 or more, a secondary button below that. The letter's subject and text are editable, Copy puts "Subject: …" and the letter on the clipboard (or selects it for Ctrl/Cmd+C when the browser refuses), and Regenerate asks first when the letter was edited. Letters, like quick matches, live only on the page. The three progress steps while it is written are time-based, since the API answers in one call (`mockCoverLetter` mocks it in tests).

Styles live in `web/src/styles` (design tokens in `tokens.css`, one file per component and page), not in `.vue` files. API calls go through `web/src/api`. CI for the web app is `.github/workflows/web.yaml`; it only runs when `web/` changes, and the Go workflows ignore `web/`.

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
