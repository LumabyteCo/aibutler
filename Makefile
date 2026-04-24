.PHONY: test test-race test-integration test-all coverage lint build build-all clean check

# Default: run all unit tests
test:
	go test ./...

# Run tests with race detector
test-race:
	go test -race ./...

# Run integration tests (requires build tag)
test-integration:
	go test -tags=integration -race -count=1 ./...

# Run all tests (unit + integration, race-free)
test-all: test-race test-integration

# Run linter
lint:
	go vet ./...

# Build single binary (CGO-free)
build:
	CGO_ENABLED=0 go build -o aibutler .

# Cross-platform builds
build-all:
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/aibutler-linux-amd64 .
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o dist/aibutler-linux-arm64 .
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o dist/aibutler-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o dist/aibutler-darwin-arm64 .

# Clean build artifacts
clean:
	rm -f aibutler
	rm -rf dist/aibutler-*

# Run tests with coverage report
coverage:
	go test -race -coverprofile=cover.out ./...
	go tool cover -func=cover.out | tail -1

# Run all checks (vet + race tests)
check: lint test-race
