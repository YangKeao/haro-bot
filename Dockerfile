FROM node:22-bookworm-slim AS web-builder

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Build stage
FROM golang:1.22-bookworm AS builder

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /agentd ./cmd/agentd

# Runtime stage
FROM debian:bookworm-slim

# Install common tools that are frequently needed
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    # Version control
    git \
    # Network tools
    curl \
    wget \
    # Text processing
    ripgrep \
    # File utilities
    file \
    # Archive utilities
    unzip \
    # Process utilities
    procps \
    # GitHub CLI
    gh \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /agentd /app/agentd
COPY --from=web-builder /src/web/dist /app/web/dist

# Create non-root user for security
RUN useradd -r -s /bin/false appuser && \
    chown -R appuser:appuser /app

USER appuser

# Default config path (can be overridden)
ENV CONFIG_PATH=/app/config.toml

ENTRYPOINT ["/app/agentd"]
CMD ["-config", "/app/config.toml"]
