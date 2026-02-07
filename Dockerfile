# Build stage
FROM golang:1.24-alpine AS builder

# Install git for go-git operations, ca-certificates, and CGO build dependencies
# gcc and musl-dev are required for CGO (sqlite-vec uses CGO)
RUN apk add --no-cache git ca-certificates tzdata gcc musl-dev

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with CGO enabled for sqlite-vec support
# Using -ldflags to reduce binary size (strip debug info and symbol table)
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o /medha cmd/server/main.go

# Runtime stage
FROM alpine:3.19

# Install git (required at runtime for go-git operations), ca-certificates,
# and libgcc for CGO-linked binaries
RUN apk add --no-cache git ca-certificates tzdata libgcc

# Create non-root user
RUN adduser -D -u 1000 medha

# Create data directories
RUN mkdir -p /home/medha/.medha/configs \
             /home/medha/.medha/db \
             /home/medha/.medha/store \
    && chown -R medha:medha /home/medha/.medha

# Copy binary from builder
COPY --from=builder /medha /usr/local/bin/medha

# Copy default config (optional)
COPY config.sample.json /home/medha/.medha/configs/config.json
RUN chown medha:medha /home/medha/.medha/configs/config.json

USER medha
WORKDIR /home/medha

# Environment variables
ENV MEDHA_HOME=/home/medha/.medha
ENV ENCRYPTION_KEY=""
ENV OPENAI_API_KEY=""

# Expose HTTP port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8080/health || exit 1

# Default to HTTP mode
ENTRYPOINT ["/usr/local/bin/medha"]
CMD ["--http"]
