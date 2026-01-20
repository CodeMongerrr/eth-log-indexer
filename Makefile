.PHONY: build run test clean docker-build docker-up docker-down fmt lint help

# Variables
BINARY_NAME=indexer
BUILD_DIR=bin
CMD_PATH=cmd/indexer

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=1 go build -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_PATH)
	@echo "✓ Binary built: $(BUILD_DIR)/$(BINARY_NAME)"

# Run the indexer locally
run: build
	@echo "Running indexer..."
	@./$(BUILD_DIR)/$(BINARY_NAME) \
		-rpc $(RPC_URL) \
		-contract $(CONTRACT_ADDR) \
		-topic $(EVENT_TOPIC) \
		-log-level debug

# Run with default test params (requires env vars)
run-dev: build
	@echo "Running indexer in dev mode..."
	@./$(BUILD_DIR)/$(BINARY_NAME) \
		-rpc $(RPC_URL) \
		-contract $(CONTRACT_ADDR) \
		-topic $(EVENT_TOPIC) \
		-start 19000000 \
		-end 19010000 \
		-workers 8 \
		-log-level debug \
		-db /tmp/test.db

# Run tests
test:
	@echo "Running tests..."
	@go test -v -race -cover ./...

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@goimports -w .

# Lint code
lint:
	@echo "Linting code..."
	@golangci-lint run ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f /tmp/test.db*
	@go clean

# Dependency management
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

# Docker build
docker-build:
	@echo "Building Docker image..."
	@docker build -t eth-log-indexer:latest .

# Docker compose up
docker-up:
	@echo "Starting Docker containers..."
	@docker-compose up -d
	@echo "Services starting:"
	@echo "  - Indexer: http://localhost:8080"
	@echo "  - Metrics: http://localhost:9090"
	@echo "  - Prometheus: http://localhost:9091"
	@echo "  - Grafana: http://localhost:3000"

# Docker compose down
docker-down:
	@echo "Stopping Docker containers..."
	@docker-compose down

# View logs
logs:
	@docker-compose logs -f indexer

# Database status
db-status:
	@echo "Database status..."
	@curl -s http://localhost:8080/v1/status | jq .

# Health check
health:
	@curl -s http://localhost:8080/v1/health | jq .

# Example queries
examples:
	@echo "Health check:"
	@echo "  curl http://localhost:8080/v1/health"
	@echo ""
	@echo "Status:"
	@echo "  curl http://localhost:8080/v1/status"
	@echo ""
	@echo "Get latest 10 logs:"
	@echo "  curl 'http://localhost:8080/v1/logs?limit=10'"
	@echo ""
	@echo "Get logs by block:"
	@echo "  curl 'http://localhost:8080/v1/logs?blockNumber=19000100'"
	@echo ""
	@echo "WebSocket stream:"
	@echo "  wscat -c ws://localhost:8080/v1/ws"

# Help
help:
	@echo "Ethereum Log Indexer - Make targets:"
	@echo ""
	@echo "  build              Build the binary"
	@echo "  run                Run the indexer (requires env vars)"
	@echo "  run-dev            Run with test parameters"
	@echo "  test               Run tests"
	@echo "  fmt                Format code"
	@echo "  lint               Lint code"
	@echo "  clean              Clean artifacts"
	@echo "  deps               Download dependencies"
	@echo "  docker-build       Build Docker image"
	@echo "  docker-up          Start Docker services"
	@echo "  docker-down        Stop Docker services"
	@echo "  logs               View service logs"
	@echo "  health             Check service health"
	@echo "  db-status          Get indexer status"
	@echo "  examples           Show curl examples"
	@echo "  help               Show this message"
