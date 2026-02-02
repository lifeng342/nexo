# Nexo IM Makefile
# Automated build, test, and deployment

.PHONY: all build test test-unit test-integration test-e2e clean \
        docker-up docker-down docker-logs \
        server server-bg server-stop \
        lint fmt vet help

# Configuration
APP_NAME := nexo
SERVER_BIN := ./bin/$(APP_NAME)
SERVER_PID := /tmp/$(APP_NAME).pid
CONFIG_FILE := config/config.yaml
TEST_TIMEOUT := 120s

# Colors for output
GREEN := \033[0;32m
YELLOW := \033[0;33m
RED := \033[0;31m
NC := \033[0m # No Color

# Default target
all: build

#==============================================================================
# Build
#==============================================================================

## build: Build the server binary
build:
	@echo "$(GREEN)Building $(APP_NAME)...$(NC)"
	@mkdir -p bin
	go build -o $(SERVER_BIN) ./cmd/server/
	@echo "$(GREEN)Build complete: $(SERVER_BIN)$(NC)"

## clean: Remove build artifacts
clean:
	@echo "$(YELLOW)Cleaning...$(NC)"
	rm -rf bin/
	rm -f $(SERVER_PID)
	@echo "$(GREEN)Clean complete$(NC)"

#==============================================================================
# Docker
#==============================================================================

## docker-up: Start MySQL and Redis containers
docker-up:
	@echo "$(GREEN)Starting Docker containers...$(NC)"
	docker-compose up -d
	@echo "$(YELLOW)Waiting for services to be healthy...$(NC)"
	@./scripts/wait-for-services.sh || (echo "$(RED)Services failed to start$(NC)" && exit 1)
	@echo "$(GREEN)Docker containers ready$(NC)"

## docker-down: Stop and remove containers
docker-down:
	@echo "$(YELLOW)Stopping Docker containers...$(NC)"
	docker-compose down -v
	@echo "$(GREEN)Docker containers stopped$(NC)"

## docker-logs: Show container logs
docker-logs:
	docker-compose logs -f

## docker-status: Check container status
docker-status:
	@docker-compose ps

#==============================================================================
# Server
#==============================================================================

## server: Run the server in foreground
server: build
	@echo "$(GREEN)Starting server...$(NC)"
	$(SERVER_BIN) -c $(CONFIG_FILE)

## server-bg: Start server in background
server-bg: build
	@echo "$(GREEN)Starting server in background...$(NC)"
	@if [ -f $(SERVER_PID) ] && kill -0 $$(cat $(SERVER_PID)) 2>/dev/null; then \
		echo "$(YELLOW)Server already running (PID: $$(cat $(SERVER_PID)))$(NC)"; \
	else \
		$(SERVER_BIN) -c $(CONFIG_FILE) > /tmp/$(APP_NAME).log 2>&1 & \
		echo $$! > $(SERVER_PID); \
		sleep 2; \
		if curl -s http://localhost:8080/health > /dev/null; then \
			echo "$(GREEN)Server started (PID: $$(cat $(SERVER_PID)))$(NC)"; \
		else \
			echo "$(RED)Server failed to start. Check /tmp/$(APP_NAME).log$(NC)"; \
			exit 1; \
		fi \
	fi

## server-stop: Stop background server
server-stop:
	@echo "$(YELLOW)Stopping server...$(NC)"
	@if [ -f $(SERVER_PID) ]; then \
		kill $$(cat $(SERVER_PID)) 2>/dev/null || true; \
		rm -f $(SERVER_PID); \
		echo "$(GREEN)Server stopped$(NC)"; \
	else \
		pkill -f "$(APP_NAME)" 2>/dev/null || true; \
		echo "$(GREEN)Server stopped$(NC)"; \
	fi

## server-restart: Restart the server
server-restart: server-stop server-bg

#==============================================================================
# Testing
#==============================================================================

## test: Run all tests (starts docker and server automatically)
test: docker-up server-bg
	@echo "$(GREEN)Running all tests...$(NC)"
	@cd tests && go test -v -timeout $(TEST_TIMEOUT) ./... ; \
	TEST_EXIT=$$?; \
	cd ..; \
	$(MAKE) server-stop; \
	exit $$TEST_EXIT

## test-quick: Run tests without restarting docker (assumes services are running)
test-quick: server-restart
	@echo "$(GREEN)Running tests (quick mode)...$(NC)"
	@cd tests && go test -v -timeout $(TEST_TIMEOUT) ./... ; \
	TEST_EXIT=$$?; \
	cd ..; \
	$(MAKE) server-stop; \
	exit $$TEST_EXIT

## test-unit: Run unit tests only (no external dependencies)
test-unit:
	@echo "$(GREEN)Running unit tests...$(NC)"
	go test -v -short ./internal/... ./pkg/...

## test-specific: Run specific test (usage: make test-specific TEST=TestAuth)
test-specific: server-bg
	@echo "$(GREEN)Running test: $(TEST)...$(NC)"
	@cd tests && go test -v -timeout $(TEST_TIMEOUT) -run "$(TEST)" ./... ; \
	TEST_EXIT=$$?; \
	cd ..; \
	$(MAKE) server-stop; \
	exit $$TEST_EXIT

## test-coverage: Run tests with coverage report
test-coverage: docker-up server-bg
	@echo "$(GREEN)Running tests with coverage...$(NC)"
	@mkdir -p coverage
	go test -v -coverprofile=coverage/coverage.out -timeout $(TEST_TIMEOUT) ./internal/... ./pkg/...
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "$(GREEN)Coverage report: coverage/coverage.html$(NC)"
	$(MAKE) server-stop

## test-ci: Run tests in CI mode (clean environment)
test-ci: clean docker-down docker-up build server-bg
	@echo "$(GREEN)Running CI tests...$(NC)"
	@sleep 3
	@cd tests && go test -v -timeout $(TEST_TIMEOUT) ./... 2>&1 | tee /tmp/test-results.log ; \
	TEST_EXIT=$${PIPESTATUS[0]}; \
	cd ..; \
	$(MAKE) server-stop; \
	$(MAKE) docker-down; \
	exit $$TEST_EXIT

#==============================================================================
# Code Quality
#==============================================================================

## fmt: Format code
fmt:
	@echo "$(GREEN)Formatting code...$(NC)"
	go fmt ./...
	@echo "$(GREEN)Format complete$(NC)"

## vet: Run go vet
vet:
	@echo "$(GREEN)Running go vet...$(NC)"
	go vet ./...
	@echo "$(GREEN)Vet complete$(NC)"

## lint: Run linter (requires golangci-lint)
lint:
	@echo "$(GREEN)Running linter...$(NC)"
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run ./...; \
	else \
		echo "$(YELLOW)golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest$(NC)"; \
	fi

## check: Run all code quality checks
check: fmt vet lint

#==============================================================================
# Database
#==============================================================================

## db-migrate: Run database migrations
db-migrate:
	@echo "$(GREEN)Running migrations...$(NC)"
	@docker exec -i nexo_mysql mysql -uroot nexo_im < migrations/001_init_schema.sql
	@echo "$(GREEN)Migrations complete$(NC)"

## db-reset: Reset database (drop and recreate)
db-reset:
	@echo "$(YELLOW)Resetting database...$(NC)"
	@docker exec -i nexo_mysql mysql -uroot -e "DROP DATABASE IF EXISTS nexo_im; CREATE DATABASE nexo_im;"
	@$(MAKE) db-migrate
	@echo "$(GREEN)Database reset complete$(NC)"

## db-shell: Open MySQL shell
db-shell:
	docker exec -it nexo_mysql mysql -uroot nexo_im

## redis-shell: Open Redis CLI
redis-shell:
	docker exec -it nexo_redis redis-cli

#==============================================================================
# Development
#==============================================================================

## dev: Start development environment (docker + server with hot reload)
dev: docker-up
	@echo "$(GREEN)Starting development server...$(NC)"
	@if command -v air > /dev/null; then \
		air -c .air.toml; \
	else \
		echo "$(YELLOW)air not installed. Running without hot reload...$(NC)"; \
		$(MAKE) server; \
	fi

## setup: Initial project setup
setup:
	@echo "$(GREEN)Setting up project...$(NC)"
	go mod download
	go mod tidy
	mkdir -p bin coverage
	chmod +x scripts/*.sh 2>/dev/null || true
	@echo "$(GREEN)Setup complete$(NC)"

#==============================================================================
# Help
#==============================================================================

## help: Show this help message
help:
	@echo "$(GREEN)Nexo IM - Available Commands$(NC)"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/  /'
	@echo ""
	@echo "Examples:"
	@echo "  make test              # Run all tests (auto-starts docker & server)"
	@echo "  make test-quick        # Run tests (assumes docker is running)"
	@echo "  make test-specific TEST=TestAuth  # Run specific test"
	@echo "  make docker-up         # Start MySQL and Redis"
	@echo "  make server-bg         # Start server in background"
