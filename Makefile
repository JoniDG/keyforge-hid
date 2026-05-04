.PHONY: help build test cover lint probe clean

COVERAGE_MIN := 95

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

build: ## Compile cmd/probe
	go build -o bin/probe ./cmd/probe

test: ## Run unit tests with coverage (excludes cmd/)
	go test -race -coverprofile=coverage.out -covermode=atomic ./internal/...
	@echo "---"
	@go tool cover -func=coverage.out | tail -1

cover: test ## Generate HTML coverage report
	go tool cover -html=coverage.out -o coverage.html
	@echo "Open coverage.html"

lint: ## Run golangci-lint
	@command -v golangci-lint >/dev/null 2>&1 || { echo "ERROR: golangci-lint not found. Run: brew install golangci-lint"; exit 1; }
	golangci-lint run ./...

probe: build ## Run the probe binary
	./bin/probe

clean: ## Remove build artifacts and coverage files
	rm -rf bin/ coverage.out coverage.html
