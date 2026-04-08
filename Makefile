# hello-kupe Makefile

GO ?= go
BINARY_NAME ?= hello-kupe
GOCACHE ?= $(PWD)/.tmp/go-build
SERVICE_NAME ?= hello-kupe
TENANT ?= example
PUBLIC_URL ?= http://localhost:8080
PORT ?= 8080
LOG_INTERVAL_SECONDS ?= 5
POD_NAME ?= local
POD_NAMESPACE ?= default

.PHONY: run build test fmt help

run: ## Run hello-kupe locally with sensible defaults
	SERVICE_NAME="$(SERVICE_NAME)" \
	TENANT="$(TENANT)" \
	PUBLIC_URL="$(PUBLIC_URL)" \
	PORT="$(PORT)" \
	LOG_INTERVAL_SECONDS="$(LOG_INTERVAL_SECONDS)" \
	POD_NAME="$(POD_NAME)" \
	POD_NAMESPACE="$(POD_NAMESPACE)" \
	$(GO) run ./cmd/hello-kupe

build: ## Build the hello-kupe binary
	GOCACHE="$(GOCACHE)" $(GO) build -o $(BINARY_NAME) ./cmd/hello-kupe

test: ## Run Go tests
	GOCACHE="$(GOCACHE)" $(GO) test ./...

fmt: ## Format Go code
	GOCACHE="$(GOCACHE)" $(GO) fmt ./...

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'
