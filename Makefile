# Platbor developer tasks. `make build` produces a self-contained binary with
# the UI embedded; `make dev` runs the API and Vite dev server side by side.

.PHONY: help build ui run dev test test-go test-web lint lint-go lint-web fmt image clean

BINARY := platbor
IMAGE := platbor

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

ui: ## Build the frontend into web/dist
	npm --prefix web ci
	npm --prefix web run build

build: ui ## Build the frontend and the binary
	go build -o $(BINARY) ./cmd/platbor

run: ## Run the API (serves whatever is currently in web/dist)
	go run ./cmd/platbor

dev: ## Reminder for the two-process dev loop
	@echo "Run in two terminals:"
	@echo "  1) go run ./cmd/platbor"
	@echo "  2) npm --prefix web run dev"

test: test-go test-web ## Run all tests

test-go: ## Run Go tests with the race detector
	go test -race ./...

test-web: ## Run frontend tests
	npm --prefix web run test

lint: lint-go lint-web ## Lint everything

lint-go: ## Lint Go (golangci-lint) + gofmt check
	gofmt -l .
	golangci-lint run

lint-web: ## Lint + typecheck the frontend
	npm --prefix web run lint
	npm --prefix web run typecheck

fmt: ## Format Go (gofumpt) and the frontend (prettier)
	gofumpt -w .
	npm --prefix web exec prettier -- --write "src/**/*.{ts,tsx,css}"

image: ## Build the Docker image (SPA + static binary, distroless runtime)
	docker build -t $(IMAGE) .

clean: ## Remove build output and runtime data
	rm -f $(BINARY) $(BINARY).exe
	rm -rf platbor-data web/dist/assets
