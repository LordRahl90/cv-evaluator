all: clean lint test
test:
	@go test ./... --cover

fancy-test:
	@go test ./... -v -json | sift

test-with-race:
	@go test -race ./... --cover

shuffle-test:
	@go test -shuffle=on --count=2 ./... -v

lint:
	golangci-lint run ./... --timeout=3m

start:
	ENVIRONMENT=development go run ./cmd/api/

worker:
	ENVIRONMENT=development go run ./cmd/worker/

clean:
	go clean --testcache

by-job:
	@go run ./cmd/byjobs -job-id=$(jobId) -user-id=$(userId)

seed-cv:
	@go run ./cmd/seed $(ARGS)

eval-prompt:
	@go test -tags eval -run 'TestPromptEval|TestCoverLetterEval' -count=1 -v ./internal/services/matcher/ ./internal/services/coverletter/

eval-cover-letter:
	@go test -tags eval -run TestCoverLetterEval -count=1 -v ./internal/services/coverletter/
