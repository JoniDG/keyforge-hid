.PHONY: help build test cover lint lint-install probe clean

COVERAGE_MIN := 95

# golangci-lint always tracks the latest release, locally and in CI (the
# workflow uses `version: latest`). It is called by path, not through PATH, so
# another install (e.g. Homebrew's) can never shadow the one lint-install puts
# here.
GOLANGCI_LINT := $(firstword $(subst :, ,$(shell go env GOPATH)))/bin/golangci-lint
GOLANGCI_LINT_RELEASES := https://api.github.com/repos/golangci/golangci-lint/releases/latest

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

build: ## Compile cmd/probe
	go build -o bin/probe ./cmd/probe

test: ## Run unit tests with coverage (root package + internal/, excludes cmd/)
	go test -race -coverprofile=coverage.out -covermode=atomic . ./internal/...
	@echo "---"
	@go tool cover -func=coverage.out | tail -1

cover: test ## Generate HTML coverage report
	go tool cover -html=coverage.out -o coverage.html
	@echo "Open coverage.html"

lint: ## Run golangci-lint (fails if it is not the latest release, as used by CI)
	@test -x "$(GOLANGCI_LINT)" || { echo "ERROR: golangci-lint not found at $(GOLANGCI_LINT). Run: make lint-install"; exit 1; }
	@latest="$$(curl -sSfL -m 5 "$(GOLANGCI_LINT_RELEASES)" 2>/dev/null | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')"; \
	installed="$$("$(GOLANGCI_LINT)" version 2>&1 | head -1)"; \
	if [ -z "$$latest" ]; then \
		echo "WARNING: could not check the latest golangci-lint release (offline or rate-limited?); running the installed one: $$installed"; \
	elif ! echo "$$installed" | grep -qE "version v?$$(echo "$$latest" | sed 's/\./\\./g') "; then \
		echo "ERROR: golangci-lint $$latest is out (CI uses it), installed: $$installed. Run: make lint-install"; exit 1; \
	fi
	"$(GOLANGCI_LINT)" run ./...

lint-install: ## Install the latest golangci-lint release into $(go env GOPATH)/bin
	@# Downloaded to a file rather than piped into sh: in a pipe a failed curl
	@# goes unnoticed, and pipefail is not portable (Ubuntu's /bin/sh is dash).
	tmp="$$(mktemp)" && curl -sSfL -o "$$tmp" https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh \
		&& sh "$$tmp" -b "$(dir $(GOLANGCI_LINT))"; rc=$$?; rm -f "$$tmp"; exit $$rc

probe: build ## Run the probe binary
	./bin/probe

clean: ## Remove build artifacts and coverage files
	rm -rf bin/ coverage.out coverage.html
