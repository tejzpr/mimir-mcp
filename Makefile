.PHONY: build test test-unit test-integration test-functional run clean install deps lint

# CGO is required for sqlite-vec support
# Ensure you have gcc installed (xcode-select --install on macOS, build-essential on Linux)
CGO_ENABLED := 1
export CGO_ENABLED

# Build the server binary
build:
	@echo "Building Medha MCP server (CGO enabled for sqlite-vec)..."
	@go build -o bin/medha cmd/server/main.go
	@echo "Build complete: bin/medha"

# Run all tests
test: test-unit

# Run unit tests with coverage
test-unit:
	@echo "Running unit tests..."
	@go test -v -race -coverprofile=coverage.txt ./internal/... ./pkg/...

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	@go test -v ./tests/integration/...

# Run functional/E2E tests  
test-functional:
	@echo "Running functional tests..."
	@go test -v ./tests/functional/...

# Run all tests (unit + integration + functional)
test-all: test-unit test-integration test-functional

# Run the server in stdio mode (for Cursor MCP)
run:
	@echo "Starting in stdio mode (MCP)..."
	@go run cmd/server/main.go

# Run in HTTP server mode
run-http:
	@echo "Starting HTTP server mode..."
	@go run cmd/server/main.go --http

# Run HTTP mode with custom port
run-http-dev:
	@echo "Starting HTTP server on port 9000..."
	@go run cmd/server/main.go --http --port=9000

# Run with embeddings enabled
run-with-embeddings:
	@echo "Starting with embeddings enabled..."
	@go run cmd/server/main.go --enable-embeddings

# Quick start (stdio mode, no config needed)
run-quick:
	@echo "Starting stdio mode with defaults..."
	@./bin/medha

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.txt coverage.out
	@echo "Clean complete"

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy
	@echo "Dependencies installed"

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run ./...

# Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

# Install tools
install-tools:
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Tools installed"

# Run setup script
setup:
	@echo "Running setup..."
	@bash scripts/setup.sh

# View coverage report
coverage:
	@go test -coverprofile=coverage.out ./internal/... ./pkg/...
	@go tool cover -html=coverage.out

# Build for current platform only (CGO prevents easy cross-compilation)
# For cross-platform builds, use Docker or platform-specific CI
build-native:
	@echo "Building for current platform (CGO enabled)..."
	@go build -ldflags="-w -s" -o bin/medha cmd/server/main.go
	@echo "Build complete: bin/medha"

# Docker commands
docker-build:
	@echo "Building Docker image..."
	@docker build -t medha-mcp .

docker-run:
	@echo "Running Medha in Docker..."
	@docker run -d -p 8080:8080 \
		-e ENCRYPTION_KEY=$${ENCRYPTION_KEY:-your-32-char-encryption-key} \
		-e OPENAI_API_KEY=$${OPENAI_API_KEY:-} \
		-v medha-data:/home/medha/.medha \
		--name medha \
		medha-mcp

docker-run-with-embeddings:
	@echo "Running Medha in Docker with embeddings..."
	@docker run -d -p 8080:8080 \
		-e ENCRYPTION_KEY=$${ENCRYPTION_KEY:-your-32-char-encryption-key} \
		-e OPENAI_API_KEY=$${OPENAI_API_KEY} \
		-e MEDHA_EMBEDDINGS_ENABLED=true \
		-v medha-data:/home/medha/.medha \
		--name medha \
		medha-mcp

docker-stop:
	@echo "Stopping Medha container..."
	@docker stop medha && docker rm medha

docker-logs:
	@docker logs -f medha

docker-compose-up:
	@echo "Starting with docker-compose..."
	@docker-compose up -d

docker-compose-down:
	@echo "Stopping docker-compose..."
	@docker-compose down

# Help
help:
	@echo "Medha MCP - Available commands:"
	@echo ""
	@echo "NOTE: CGO is required (sqlite-vec). Ensure gcc is installed."
	@echo ""
	@echo "Build & Run:"
	@echo "  make build              - Build the server binary"
	@echo "  make build-native       - Build optimized for current platform"
	@echo "  make run                - Run in stdio mode (for Cursor MCP)"
	@echo "  make run-http           - Run in HTTP server mode"
	@echo "  make run-http-dev       - Run HTTP on port 9000"
	@echo "  make run-with-embeddings- Run with semantic search enabled"
	@echo "  make run-quick          - Quick start stdio mode"
	@echo ""
	@echo "Testing:"
	@echo "  make test               - Run unit tests"
	@echo "  make test-integration   - Run integration tests"
	@echo "  make test-functional    - Run functional tests"
	@echo "  make test-all           - Run all tests"
	@echo "  make coverage           - View test coverage"
	@echo "  make vet                - Run go vet"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build       - Build Docker image"
	@echo "  make docker-run         - Run container"
	@echo "  make docker-run-with-embeddings - Run with embeddings"
	@echo "  make docker-stop        - Stop and remove container"
	@echo "  make docker-logs        - View container logs"
	@echo "  make docker-compose-up  - Start with docker-compose"
	@echo "  make docker-compose-down- Stop docker-compose"
	@echo ""
	@echo "Other:"
	@echo "  make clean              - Clean build artifacts"
	@echo "  make deps               - Install dependencies"
	@echo "  make lint               - Run linter"
	@echo "  make setup              - Run initial setup"
