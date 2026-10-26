# hello-kupe Makefile

.DEFAULT_GOAL := run

GO ?= go
BINARY_NAME ?= hello-kupe
GOCACHE ?= $(PWD)/.tmp/go-build
SERVICE_NAME ?= hello-kupe
TENANT ?= example
PUBLIC_URL ?= http://localhost:8080
PORT ?= 8080
WEB_PORT ?= 3000
TF ?= tofu
POD_NAME ?= local
POD_NAMESPACE ?= default

.PHONY: dev run build test gosec govulncheck helm-lint fmt help web-install web-build web-test db-up migrate infra-check kind-up kind-forward kind-dev kind-status kind-down
.PHONY: cloud-init cloud-plan cloud-apply cloud-kubeconfig cloud-deploy cloud-status cloud-test

dev: ## Start the Go API and Next.js frontend with hot reload
	GO="$(GO)" GOCACHE="$(GOCACHE)" PORT="$(PORT)" WEB_PORT="$(WEB_PORT)" \
	SERVICE_NAME="$(SERVICE_NAME)" TENANT="$(TENANT)" \
	PUBLIC_URL="http://localhost:$(WEB_PORT)" \
	node scripts/dev.mjs

run: web-build ## Run hello-kupe locally with sensible defaults
	SERVICE_NAME="$(SERVICE_NAME)" \
	TENANT="$(TENANT)" \
	PUBLIC_URL="$(PUBLIC_URL)" \
	PORT="$(PORT)" \
	POD_NAME="$(POD_NAME)" \
	POD_NAMESPACE="$(POD_NAMESPACE)" \
	$(GO) run ./cmd/hello-kupe

build: ## Build the hello-kupe binary
	GOCACHE="$(GOCACHE)" $(GO) build -o $(BINARY_NAME) ./cmd/hello-kupe

test: ## Run Go tests
	GOCACHE="$(GOCACHE)" $(GO) test ./...

gosec: ## Run gosec against the codebase
	GOCACHE="$(GOCACHE)" GOWORK=off $(GO) run github.com/securego/gosec/v2/cmd/gosec@v2.25.0 -exclude-generated ./...

govulncheck: ## Run govulncheck against the codebase
	GOCACHE="$(GOCACHE)" GOWORK=off $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

helm-lint: ## Lint the Helm chart
	helm lint ./chart --set tenant=example-tenant --set cluster=example
	helm lint ./chart --set tenant=example-tenant --set cluster=example --set database.enabled=true

fmt: ## Format Go code
	GOCACHE="$(GOCACHE)" $(GO) fmt ./...

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'

web-install: ## Install frontend dependencies
	cd web && npm ci

web-build: ## Build the Next.js static frontend
	cd web && npm run build

web-test: ## Check frontend types and tests
	cd web && npm run typecheck && npm test

db-up: ## Start local PostgreSQL with separate owner and application roles
	docker compose up -d --wait postgres

migrate: ## Apply schema migrations using the owner's database connection
	GOCACHE="$(GOCACHE)" $(GO) run ./cmd/hello-kupe migrate

infra-check: ## Validate Terraform/OpenTofu infrastructure without applying
	$(TF) fmt -check infra
	$(TF) -chdir=infra validate

cloud-init: ## Prepare a fresh demo: make cloud-init TENANT=your-tenant
	DEMO_TENANT="$(TENANT)" TF="$(TF)" node scripts/cloud.mjs init

cloud-plan: ## Review the proposed cloud infrastructure
	TF="$(TF)" node scripts/cloud.mjs plan

cloud-apply: ## Provision infrastructure after Terraform's confirmation
	TF="$(TF)" node scripts/cloud.mjs apply

cloud-kubeconfig: ## Generate a private repo-local kubeconfig from Terraform outputs
	TF="$(TF)" node scripts/cloud.mjs kubeconfig

cloud-deploy: ## Build, push, migrate and deploy the game to the provisioned cluster
	TF="$(TF)" node scripts/cloud.mjs deploy

cloud-status: ## Show the cloud deployment and its public URL
	TF="$(TF)" node scripts/cloud.mjs status

cloud-test: ## Check cloud workflow targeting and failure handling without cloud access
	node --test scripts/cloud.test.mjs

kind-up: ## Build and deploy Hello Kupe with PostgreSQL to isolated local kind
	node scripts/kind.mjs up

kind-forward: ## Open the kind deployment at http://localhost:18080
	node scripts/kind.mjs forward

kind-dev: kind-up ## Set up the kind deployment and keep it accessible locally
	node scripts/kind.mjs forward

kind-status: ## Show the local kind pods and database storage
	node scripts/kind.mjs status

kind-down: ## Delete the local kind cluster and its database data
	node scripts/kind.mjs down
