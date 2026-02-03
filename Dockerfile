# Build stage
FROM golang:1.24-alpine AS builder

# Install git for go-git operations and ca-certificates
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /mimir cmd/server/main.go

# Runtime stage
FROM alpine:3.19

# Install git (required at runtime for go-git operations) and ca-certificates
RUN apk add --no-cache git ca-certificates tzdata

# Create non-root user
RUN adduser -D -u 1000 mimir

# Create data directories
RUN mkdir -p /home/mimir/.mimir/configs \
             /home/mimir/.mimir/db \
             /home/mimir/.mimir/store \
    && chown -R mimir:mimir /home/mimir/.mimir

# Copy binary from builder
COPY --from=builder /mimir /usr/local/bin/mimir

# Copy default config (optional)
COPY config.sample.json /home/mimir/.mimir/configs/config.json
RUN chown mimir:mimir /home/mimir/.mimir/configs/config.json

USER mimir
WORKDIR /home/mimir

# Environment variables
ENV MIMIR_HOME=/home/mimir/.mimir
ENV ENCRYPTION_KEY=""

# Expose HTTP port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8080/health || exit 1

# Default to HTTP mode
ENTRYPOINT ["/usr/local/bin/mimir"]
CMD ["--http"]
