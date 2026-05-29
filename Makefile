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

clean:
	go clean --testcache
