# Build stage
FROM golang:1.24-bookworm AS builder

# Install git, ca-certificates, CGO build deps, and SQLite dev (sqlite-vec)
RUN apt-get update && apt-get install -y --no-install-recommends \
    git ca-certificates tzdata gcc libc6-dev libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with CGO enabled for sqlite-vec support
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o /medha cmd/server/main.go

# Runtime stage
FROM debian:bookworm-slim

# Install git (runtime), ca-certificates, tzdata, libgcc for CGO binary, wget for healthcheck
RUN apt-get update && apt-get install -y --no-install-recommends \
    git ca-certificates tzdata libgcc-s1 wget \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN adduser --disabled-password --gecos '' --uid 1000 medha

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
