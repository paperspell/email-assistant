GONOSUMDB=github.com/paperspell
GOLANGCI_LINT_VERSION=v2.10.1
BINARY=bin/email-agent
# The version baked into the binary: the nearest tag, or the commit when
# untagged, with -dirty for uncommitted changes. goreleaser sets the same
# variable from the release tag.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := help

check: tidy lint-fix test test-migrations ## Run all local checks: tidy, lint, unit tests, migration tests.

build: ## Build the binary.
	go build -o $(BINARY) -trimpath -ldflags "$(LDFLAGS)" ./cmd/email-agent

run: build ## Build and run the daemon (requires config.yaml).
	$(BINARY) run

.PHONY: test
test: ## Run unit tests with race detector and coverage.
	go test -race -cover -failfast ./...

test-migrations: ## Run migration tests.
	go test -v -tags=migration ./internal/db/...

.PHONY: lint
lint: ## Run golangci-lint.
	golangci-lint run -v

lint-fix: ## Run golangci-lint in fix mode.
	golangci-lint run -v --fix

fmt: ## Run go fmt.
	go fmt ./...

generate: ## Run go generate.
	go generate ./...

mock: ## Regenerate mocks with mockery.
	mockery

tidy: ## Run go mod tidy.
	go mod tidy

setup: ## Install development prerequisites (macOS).
	brew install go
	brew install golangci-lint
	brew install mockery

.PHONY: help
help: ## Print available make targets.
	@grep -E '^[0-9a-zA-Z_-]+:.*?## .*$$' $(firstword $(MAKEFILE_LIST)) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
