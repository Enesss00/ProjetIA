# GHOST NET — top-level developer commands.
# Requires: Go >=1.24, Node >=20. `make dev` runs the whole game.

SERVER_DIR := server
WEB_DIR    := web
ADDR       ?= :8080

.PHONY: help dev build build-server build-web verify fuzz replay-check demo \
        test test-go test-web lint lint-go lint-web fmt e2e clean install

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

install: ## Install web dependencies
	cd $(WEB_DIR) && npm install

build: build-web build-server ## Build client then server

build-web: ## Build the web client into web/dist
	cd $(WEB_DIR) && npm run build

build-server: ## Build the Go server binary
	cd $(SERVER_DIR) && go build -o ghostnet ./cmd/ghostnet

dev: build-web build-server ## Build everything and run the game on $(ADDR)
	@echo "GHOST NET → http://localhost$(ADDR)"
	cd $(SERVER_DIR) && ./ghostnet serve -addr $(ADDR) -static ../$(WEB_DIR)/dist -db ./data

verify: lint test ## Full gate: lint + types + tests (both sides)

test: test-go test-web ## Run all tests

test-go: ## Run Go unit tests (race detector on)
	cd $(SERVER_DIR) && go test -race ./...

test-web: ## Run web unit tests
	cd $(WEB_DIR) && npm run test

e2e: build-web build-server ## Run the Playwright end-to-end test
	cd $(WEB_DIR) && npx playwright test e2e/game.spec.ts

fuzz: ## Run the Go fuzzers briefly (parser + WS decoder)
	cd $(SERVER_DIR) && go test ./internal/command/ -run xxx -fuzz FuzzParse -fuzztime 20s
	cd $(SERVER_DIR) && go test ./internal/server/ -run xxx -fuzz FuzzWSDecode -fuzztime 20s

replay-check: ## Prove determinism: replay seeded games and compare
	cd $(SERVER_DIR) && go run ./cmd/ghostnet replay -seeds 1,2,42,1337,99999 -runs 3

demo: build-server ## Headless: play one seed and print the incident report
	cd $(SERVER_DIR) && go run ./cmd/ghostnet verify -seed 1337 -profile apt

lint: lint-go lint-web ## Lint + typecheck both sides

lint-go: ## Go vet + golangci-lint
	cd $(SERVER_DIR) && go vet ./... && golangci-lint run ./...

lint-web: ## TypeScript typecheck + eslint + prettier check
	cd $(WEB_DIR) && npm run typecheck && npm run lint && npm run format:check

fmt: ## Format Go and web code
	cd $(SERVER_DIR) && gofmt -w cmd internal
	cd $(WEB_DIR) && npm run format

clean: ## Remove build artifacts
	rm -f $(SERVER_DIR)/ghostnet
	rm -rf $(SERVER_DIR)/data $(WEB_DIR)/dist $(WEB_DIR)/test-results $(WEB_DIR)/playwright-report
