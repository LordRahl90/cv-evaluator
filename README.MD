# CV Solution

CV Solution is a Go project for CV-to-job matching. It stores vector embeddings in PostgreSQL (`pgvector`), retrieves the most relevant CV context for a job, and returns an LLM-generated match result.

## Current status

- CLI flows are the primary working interface: `cmd/loader`, `cmd/byjobs`, and `cmd/local`.
- `cmd/api` exists but is currently a stub entrypoint.
- Scraper support is currently focused on LinkedIn in the main loader flow.

## What it does

- Parses and stores CV content as embeddings (full CV + section-level vectors).
- Scrapes jobs, fetches details, and stores job embeddings.
- Uses vector similarity in Postgres (`pgvector`) to find best CV sections for a job.
- Uses DeepSeek to generate structured match output from retrieved context.

## Architecture at a glance

- `internal/services/users`: user auth/profile and CV processing pipeline.
- `internal/services/jobs`: job ingestion, dedupe checks, and embedding persistence.
- `internal/services/matcher`: retrieval, weighted scoring, and LLM match generation.
- `internal/scraper/linkedin`: listing + details scraping.
- `internal/llm/ollama`: embedding/cleanup model integration.
- `internal/llm/deepseek`: final job-match generation.
- `internal/migrator`: schema migration + `vector` extension setup.

## Prerequisites

- Go `1.25+`
- Docker (for local Postgres/pgvector)
- Ollama running locally (or reachable via `OLLAMA_BASE_URL`)
- DeepSeek API key
- Optional:
  - `golangci-lint` for `make lint`
  - `sift` for `make fancy-test`

## Configuration

Copy the template and update values:

```bash
cp .env.example .env
```

Current `.env.example` keys:

```dotenv
DEEPSEEK_API_KEY=your-deepseek-api-key
OLLAMA_BASE_URL=http://localhost:11434
DB_HOST=localhost
DB_PORT=5432
DB_NAME=cv_evaluator
DB_USER=postgres
DB_PASSWORD=postgres
JWT_SECRET=change-me-with-a-long-random-value
```

> Note: the CLI entrypoints call `godotenv.Load()` and fail if `.env` is missing.
> Use a unique local/dev password and a long random `JWT_SECRET`; never commit real secrets.

## Quick start

1. Start local Postgres with pgvector:

```bash
docker compose up -d
```

2. Ensure Ollama is running and required models are available.

```bash
ollama pull nomic-embed-text
ollama pull qwen3:8b
```

3. Create and update `.env`:

```bash
cp .env.example .env
```

4. Run tests:

```bash
make test
```

## Run commands

Run job loader (scrape + persist jobs for `userID=1`):

```bash
go run ./cmd/loader
```

Match against an already stored job ID:

```bash
go run ./cmd/byjobs -user-id=1 -job-id=<linkedin_job_id>
```

Equivalent Make target:

```bash
make by-job jobId=<linkedin_job_id> userId=1
```

Run local matching against text in `data/senior-software-engineer.txt`:

```bash
go run ./cmd/local
```

## Testing and quality

```bash
make test
make test-with-race
make shuffle-test
make fancy-test
make lint
make clean
```

## Important behavior notes

- `cmd/loader` runs migrations automatically.
- `cmd/byjobs` and `cmd/local` assume required DB data already exists (for example `userID=1` and a processed CV).
- `cmd/local` reads `data/senior-software-engineer.txt` by default.
- `make start` runs `go run ./cmd/api/`, which currently prints a placeholder message.

## Limitations

- LinkedIn scraping depends on page structure and may break when markup changes.
- HTTP server handlers under `internal/servers/*` are scaffold-level and not fully implemented.
