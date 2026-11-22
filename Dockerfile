# ==========================================
# Stage 1: Build binary
# ==========================================
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install git, ca-certificates, and timezone database
RUN apk add --no-cache git ca-certificates tzdata

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build statically compiled binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /app/bin/server ./cmd/server

# ==========================================
# Stage 2: Minimal runtime image
# ==========================================
FROM alpine:3.20

# Create non-root user for security
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /home/appuser

# Copy certificates and compiled binary
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /app/bin/server /usr/local/bin/server

# Set ownership
USER appuser

# Expose default service port
EXPOSE 8080

# Environment variables
ENV PORT=8080 \
    LOG_LEVEL=INFO \
    WORKER_COUNT=5 \
    QUEUE_CAPACITY=100

# Health check probe
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["server"]
