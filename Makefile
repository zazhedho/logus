GOLANGCI_LINT ?= $(shell go env GOPATH)/bin/golangci-lint
GOLANGCI_LINT_VERSION := v2.14.0

.PHONY: lint lint-install test

lint:
	@echo "Running golangci-lint..."
	@if [ ! -x "$(GOLANGCI_LINT)" ]; then \
		echo "golangci-lint is not installed. Run: make lint-install"; \
		exit 1; \
	fi
	@$(GOLANGCI_LINT) run --config .golangci.yml --timeout=5m ./...
	@echo "golangci-lint passed."

lint-install:
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

test:
	@go test -race ./...
