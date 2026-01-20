FROM golang:1.23-alpine AS builder

WORKDIR /build

# Install dependencies
RUN apk add --no-cache git gcc musl-dev

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source
COPY . .

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o indexer ./cmd/indexer

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/indexer .

# Create data directory
RUN mkdir -p /app/data

# Default environment
ENV LOG_LEVEL=info
ENV API_ADDR=:8080
ENV METRICS_ADDR=:9090
ENV DB_PATH=/app/data/indexer.db
ENV BACKFILL=true
ENV WORKERS=8
ENV MAX_BLOCK_RANGE=500
ENV ROLLBACK_WINDOW=128
ENV CHECKPOINT_INTERVAL=30s

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget -q -O- http://localhost:8080/v1/health || exit 1

EXPOSE 8080 9090

ENTRYPOINT ["./indexer"]
