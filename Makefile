.PHONY: build test test-unit test-integration test-functional run clean install deps lint

# Build the server binary
build:
	@echo "Building Mimir MCP server..."
	@go build -o bin/mimir cmd/server/main.go
	@echo "Build complete: bin/mimir"

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

# Quick start (stdio mode, no config needed)
run-quick:
	@echo "Starting stdio mode with defaults..."
	@./bin/mimir

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.txt
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

# Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	@GOOS=linux GOARCH=amd64 go build -o bin/mimir-linux-amd64 cmd/server/main.go
	@GOOS=darwin GOARCH=amd64 go build -o bin/mimir-darwin-amd64 cmd/server/main.go
	@GOOS=darwin GOARCH=arm64 go build -o bin/mimir-darwin-arm64 cmd/server/main.go
	@GOOS=windows GOARCH=amd64 go build -o bin/mimir-windows-amd64.exe cmd/server/main.go
	@echo "Multi-platform build complete"

# Docker commands
docker-build:
	@echo "Building Docker image..."
	@docker build -t mimir-mcp .

docker-run:
	@echo "Running Mimir in Docker..."
	@docker run -d -p 8080:8080 \
		-e ENCRYPTION_KEY=$${ENCRYPTION_KEY:-your-32-char-encryption-key} \
		-v mimir-data:/home/mimir/.mimir \
		--name mimir \
		mimir-mcp

docker-stop:
	@echo "Stopping Mimir container..."
	@docker stop mimir && docker rm mimir

docker-logs:
	@docker logs -f mimir

docker-compose-up:
	@echo "Starting with docker-compose..."
	@docker-compose up -d

docker-compose-down:
	@echo "Stopping docker-compose..."
	@docker-compose down

# Help
help:
	@echo "Mimir MCP - Available commands:"
	@echo ""
	@echo "Build & Run:"
	@echo "  make build              - Build the server binary"
	@echo "  make run                - Run in stdio mode (for Cursor MCP)"
	@echo "  make run-http           - Run in HTTP server mode"
	@echo "  make run-http-dev       - Run HTTP on port 9000"
	@echo "  make run-quick          - Quick start stdio mode"
	@echo ""
	@echo "Testing:"
	@echo "  make test               - Run all unit tests"
	@echo "  make test-integration   - Run integration tests"
	@echo "  make test-functional    - Run functional tests"
	@echo "  make coverage           - View test coverage"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build       - Build Docker image"
	@echo "  make docker-run         - Run container"
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
	@echo "  make build-all          - Build for all platforms"
