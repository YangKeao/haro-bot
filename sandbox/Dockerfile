FROM golang:1.22-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/haro-sandboxd ./cmd/haro-sandboxd

FROM golang:1.22-bookworm AS go-runtime

FROM node:22-bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    curl \
    default-mysql-client \
    file \
    git \
    jq \
    procps \
    python3 \
    python3-pip \
    python3-venv \
    ripgrep \
    unzip \
    wget \
    && rm -rf /var/lib/apt/lists/*

COPY --from=go-runtime /usr/local/go /usr/local/go
COPY --from=builder /out/haro-sandboxd /usr/local/bin/haro-sandboxd
RUN ln -s /usr/local/go/bin/go /usr/local/bin/go \
    && ln -s /usr/local/go/bin/gofmt /usr/local/bin/gofmt

ENV PATH="/usr/local/go/bin:${PATH}" \
    HOME=/workspace/home \
    LANG=C.UTF-8
WORKDIR /workspace

# Agent Sandbox overrides the entrypoint with the injected runtime. This default
# keeps the image useful for local inspection as well.
ENTRYPOINT ["/usr/local/bin/haro-sandboxd"]
