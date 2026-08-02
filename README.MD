# CV Evaluator

CV Evaluator is a Go project for CV-to-job matching. It stores CV and job embeddings in PostgreSQL (`pgvector`), retrieves relevant CV context by vector similarity, and uses an LLM to produce a structured match result.

## Current status

- CLI-first workflows are currently the main way to use the project.
- `cmd/api` exists, but it is still a placeholder entrypoint.
- Core matching logic lives in service packages and is covered by tests.

## What it does

- Processes CV content into full and section-level embeddings.
- Scrapes and stores job listings/details with embeddings.
- Computes match relevance with vector similarity and weighted scoring.
- Generates final match output using DeepSeek.

## Architecture

- `internal/services/users`: user creation/auth and CV processing.
- `internal/services/jobs`: job ingest, dedupe checks, embedding generation.
- `internal/services/matcher`: retrieval, scoring, and LLM response composition.
- `internal/scraper/linkedin`: LinkedIn search/detail scraping.
- `internal/llm/ollama`: embedding and cleanup model integration.
- `internal/llm/deepseek`: final job-match completion.
- `internal/migrator`: `vector` extension and model migrations.

## Prerequisites

- Go `1.25+`
- Docker
- Ollama (reachable via `OLLAMA_BASE_URL`)
- DeepSeek API key
- Optional tools:
  - `golangci-lint` (for `make lint`)
  - `sift` (for `make fancy-test`)

## Configuration

Copy `.env.example`:

```bash
cp .env.example .env
```

Current env keys:

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

Important notes:

- CLI entrypoints load `.env` with `godotenv.Load()` and fail if missing.
- If `JWT_SECRET` is empty, the users service generates a random in-memory secret.

## Quick start (recommended flow)

1. Start local Postgres/pgvector:

```bash
docker compose up -d
```

2. Pull required Ollama models:

```bash
ollama pull nomic-embed-text
ollama pull qwen3:8b
```

3. Create `.env` and fill real values:

```bash
cp .env.example .env
```

4. Seed a user and process a CV (recommended first run):

```bash
make seed-cv ARGS='-cv ./data/alugbin-abiodun-resume.pdf -email you@example.com -first-name Your -last-name Name -phone +1234567890 -password strongpassword'
```

5. Load jobs:

```bash
go run ./cmd/loader
```

6. Match against a stored job ID:

```bash
go run ./cmd/byjobs -user-id=<USER_ULID> -job-id=<JOB_ID>
```

## Common commands

```bash
# API placeholder
make start

# Seed CV directly
go run ./cmd/seed -cv ./data/alugbin-abiodun-resume.pdf -email you@example.com

# Load jobs
go run ./cmd/loader

# Match by stored job
make by-job jobId=<JOB_ID> userId=<USER_ULID>

# Match local text file (data/senior-software-engineer.txt)
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

## API and server notes

- `cmd/api` currently prints `Hello world!` and does not wire the Gin server yet.
- Server handlers exist under `internal/servers/cv` and `internal/servers/match`.
- Some handler endpoints are intentionally not implemented yet (for example CV update and match-by-link endpoint).

## Limitations

- LinkedIn scraping depends on page structure and may break over time.
- Some CLI flows assume existing seeded user/CV data (for example hardcoded IDs in `loader`/`local`).
