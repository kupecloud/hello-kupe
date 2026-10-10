# Managed database sample Makefile

GO ?= go
GOCACHE ?= $(PWD)/.tmp/go-build
HELM_SET ?= --set tenant=example-tenant --set cluster=example \
	--set api.image.repository=ghcr.io/example/todo-api --set api.image.tag=0.1.0 \
	--set web.image.repository=ghcr.io/example/todo-web --set web.image.tag=0.1.0

.PHONY: test web-test web-build gosec govulncheck helm-lint smoke help

test: ## Run Go tests
	GOCACHE="$(GOCACHE)" $(GO) test -timeout 120s ./...

web-test: ## Type-check and test the frontend
	cd web && npm ci && npx tsc && npm test

web-build: ## Build the frontend into web/dist
	cd web && npm ci && npm run build

gosec: ## Run gosec against the Go code
	GOCACHE="$(GOCACHE)" GOWORK=off $(GO) run github.com/securego/gosec/v2/cmd/gosec@v2.25.0 -exclude-generated ./...

govulncheck: ## Run govulncheck against the Go code
	GOCACHE="$(GOCACHE)" GOWORK=off $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

helm-lint: ## Lint the Helm chart
	helm lint ./chart $(HELM_SET)

smoke: ## End-to-end check in Docker: TLS PostgreSQL, migration, API, frontend
	scripts/local-smoke.sh

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'
