# go-webhook-relay 🚀

A production-style, high-performance Go backend service designed to ingest inbound webhooks (e.g., payment gateways, blockchain notifications, or external SaaS events), validate cryptographic signatures, and process them concurrently using a resilient worker pool.

Built with Go's standard library and the lightweight, standard-library-compatible [Chi](https://github.com/go-chi/chi) router.

---

## 🌟 Key Features

- **Standard Library + Chi**: Clean, zero-bloat architecture using `net/http` idioms and `go-chi/chi/v5`.
- **HMAC-SHA256 Signature Verification**: Constant-time signature comparison (`hmac.Equal`) preventing timing attacks. Compatible with `X-Signature-256`, `X-Hub-Signature-256`, and `X-Signature` headers (with or without `sha256=` prefixes).
- **Concurrent Worker Pool**: Bounded channel buffer with a configurable pool of concurrent worker goroutines for asynchronous event processing.
- **Load Shedding**: Immediate HTTP `503 Service Unavailable` response when internal queues are saturated, preventing out-of-memory cascading failures.
- **Structured Logging (`log/slog`)**: Production-ready JSON structured logs containing request IDs, IP addresses, durations, and worker telemetry.
- **Graceful Shutdown**: Intercepts `SIGINT` / `SIGTERM`, drains pending HTTP requests, closes event channels, and allows active workers to finish cleanly before exiting.
- **Multi-Stage Dockerfile**: Secure, ultra-lightweight Alpine image running as an unprivileged non-root user with built-in health checks.

---

## 📁 Project Structure

```
go-webhook-relay/
├── cmd/
│   └── server/
│       └── main.go           # Application entrypoint & graceful shutdown orchestration
├── internal/
│   ├── config/
│   │   └── config.go         # Environment-based configuration loader
│   ├── handler/
│   │   └── webhook.go        # HTTP handlers (/health, /webhook) & HMAC validation
│   └── processor/
│       └── processor.go      # Worker pool, bounded event queue & simulated business logic
├── .env.example              # Environment variables template
├── Dockerfile                # Multi-stage production container build
├── go.mod                    # Go module definition
├── go.sum                    # Dependency checksums
└── README.md                 # Documentation and API guide
```

---

## ⚙️ Configuration

The service is configured using standard environment variables:

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | Port for the HTTP server to listen on |
| `WEBHOOK_SECRET` | `supersecretkey_change_me_in_production` | Shared secret key for HMAC-SHA256 signature verification |
| `WORKER_COUNT` | `5` | Number of concurrent worker goroutines |
| `QUEUE_CAPACITY` | `100` | Maximum number of buffered events in the channel |
| `LOG_LEVEL` | `INFO` | Logging level (`DEBUG`, `INFO`, `WARN`, `ERROR`) |

To start, copy the example environment file:
```bash
cp .env.example .env
```

---

## 🚀 Getting Started

### Prerequisites

- [Go 1.22+](https://go.dev/dl/) or [Docker](https://www.docker.com/)

### Running Locally

1. **Clone and enter the directory**:
   ```bash
   cd go-webhook-relay
   ```

2. **Download dependencies**:
   ```bash
   go mod download
   ```

3. **Run the server**:
   ```bash
   go run ./cmd/server
   ```
   Or build a binary:
   ```bash
   go build -o bin/server ./cmd/server
   ./bin/server
   ```

---

## 🐳 Running with Docker

### Build Image
```bash
docker build -t go-webhook-relay:latest .
```

### Run Container
```bash
docker run -d \
  --name webhook-relay \
  -p 8080:8080 \
  -e WEBHOOK_SECRET="my_custom_secret" \
  -e WORKER_COUNT=5 \
  go-webhook-relay:latest
```

### Inspect Container Logs
```bash
docker logs -f webhook-relay
```

---

## 📡 API Endpoints

### 1. Health Check & Telemetry

Returns server status, uptime, and real-time worker pool statistics.

- **URL**: `GET /health`
- **Response**: `200 OK`

#### Example Request:
```bash
curl -i http://localhost:8080/health
```

#### Example Response:
```json
{
  "status": "ok",
  "uptime": "2m34s",
  "timestamp": "2026-11-22T08:30:00Z",
  "processor": {
    "worker_count": 5,
    "queued_events": 0,
    "queue_capacity": 100,
    "processed_count": 42,
    "dropped_count": 0
  }
}
```

---

### 2. Inbound Webhook Ingestion

Ingests an event, verifies the cryptographic signature, and dispatches it to the worker pool.

- **URL**: `POST /webhook`
- **Headers**:
  - `Content-Type: application/json`
  - `X-Signature-256: <hex_digest>` (or `X-Hub-Signature-256: sha256=<hex_digest>`)
- **Response**: `202 Accepted` (when successfully enqueued)

#### Expected Payload Format:
```json
{
  "id": "evt_payment_981247",
  "type": "payment.succeeded",
  "source": "stripe",
  "timestamp": "2026-11-22T08:30:00Z",
  "payload": {
    "amount": 4900,
    "currency": "usd",
    "customer": "cust_8234"
  }
}
```

---

## 🧪 Testing Webhooks with HMAC Signatures

To send a valid request to `/webhook`, calculate the HMAC-SHA256 hex digest of the exact request body using `WEBHOOK_SECRET`.

### Option A: Using cURL + OpenSSL (Bash / Linux / macOS)

```bash
# 1. Define secret and JSON payload
SECRET="supersecretkey_change_me_in_production"
BODY='{"id":"evt_101","type":"payment.succeeded","source":"stripe","payload":{"amount":2500,"currency":"usd"}}'

# 2. Compute HMAC-SHA256 hex signature
SIGNATURE=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | sed 's/^.* //')

# 3. Send signed POST request
curl -i -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-Signature-256: $SIGNATURE" \
  -d "$BODY"
```

### Option B: Using Windows PowerShell

```powershell
$Secret = "supersecretkey_change_me_in_production"
$Body = '{"id":"evt_101","type":"payment.succeeded","source":"stripe","payload":{"amount":2500,"currency":"usd"}}'

# Compute HMAC-SHA256
$hmac = New-Object System.Security.Cryptography.HMACSHA256
$hmac.Key = [System.Text.Encoding]::UTF8.GetBytes($Secret)
$hash = $hmac.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($Body))
$signature = -join ($hash | ForEach-Object { "{0:x2}" -f $_ })

# Send Request
Invoke-RestMethod -Uri "http://localhost:8080/webhook" `
  -Method POST `
  -Headers @{
      "Content-Type" = "application/json"
      "X-Signature-256" = $signature
  } `
  -Body $Body
```

### Option C: Using Python (`test_webhook.py`)

```python
import hmac
import hashlib
import json
import requests

SECRET = b"supersecretkey_change_me_in_production"
URL = "http://localhost:8080/webhook"

payload = {
    "id": "evt_tx_0x9a8f",
    "type": "blockchain.tx_confirmed",
    "source": "ethereum",
    "payload": {
        "tx_hash": "0x4b7f8c9d2e1a3b5c...",
        "block_number": 19283746,
        "confirmations": 12
    }
}

body = json.dumps(payload, separators=(',', ':')).encode('utf-8')
signature = hmac.new(SECRET, body, hashlib.sha256).hexdigest()

headers = {
    "Content-Type": "application/json",
    "X-Signature-256": signature
}

response = requests.post(URL, data=body, headers=headers)
print("Status Code:", response.status_code)
print("Response:", response.json())
```

---

## 🔒 Security & Resilience Design

1. **Constant-Time Verification**:
   The signature check utilizes `crypto/hmac.Equal`, which performs a constant-time byte comparison to eliminate vulnerability to timing attacks.

2. **DoS Protection via Payload Limits**:
   The request body reader is wrapped with `http.MaxBytesReader(w, r.Body, 1024*1024)`, enforcing a 1MB limit to protect against memory exhaustion.

3. **Non-Blocking Load Shedding**:
   The worker queue uses a non-blocking `select` on the event channel. When `QUEUE_CAPACITY` is reached, requests return `503 Service Unavailable` with `Retry-After` semantics rather than blocking HTTP connection threads.

4. **Graceful Worker Drain**:
   Upon receiving `SIGINT` or `SIGTERM`, the HTTP listener is closed first to reject new traffic. The event queue channel is then closed, allowing active workers to finish processing pending items before the application exits.

---

## 📈 Ready to Expand

This service provides a solid foundation for enterprise-grade extensions:
- **Distributed Queues**: Replace the in-memory Go channel with Redis Streams, RabbitMQ, or AWS SQS.
- **Dead-Letter Queue (DLQ)**: Add automatic retry logic with exponential backoff and route unrecoverable failures to a persistent DLQ.
- **Database Persistence**: Store event receipts and idempotency keys in PostgreSQL, MySQL, or SQLite.
- **Observability**: Add Prometheus metrics (`/metrics`) and OpenTelemetry traces across workers.
