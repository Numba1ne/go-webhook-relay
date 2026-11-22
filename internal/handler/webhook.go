package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go-webhook-relay/internal/config"
	"go-webhook-relay/internal/processor"
)

// WebhookHandler handles inbound HTTP requests for health check and webhooks.
type WebhookHandler struct {
	cfg       *config.Config
	proc      *processor.Processor
	startTime time.Time
}

// New creates an instance of WebhookHandler.
func New(cfg *config.Config, proc *processor.Processor) *WebhookHandler {
	return &WebhookHandler{
		cfg:       cfg,
		proc:      proc,
		startTime: time.Now(),
	}
}

// HealthHandler responds with server status, uptime, and worker queue statistics.
func (h *WebhookHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	stats := h.proc.Stats()
	uptime := time.Since(h.startTime).Truncate(time.Second).String()

	resp := map[string]interface{}{
		"status":    "ok",
		"uptime":    uptime,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"processor": stats,
	}

	writeJSON(w, http.StatusOK, resp)
}

// WebhookHandler validates HMAC-SHA256 signature, parses payload, and forwards to processor.
func (h *WebhookHandler) WebhookHandler(w http.ResponseWriter, r *http.Request) {
	// Guard against memory exhaustion by bounding request body to 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Warn("Failed reading webhook body or payload exceeds limit", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Failed to read request body or payload exceeds 1MB limit",
		})
		return
	}

	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Request body cannot be empty",
		})
		return
	}

	// Signature verification
	sigHeader := extractSignature(r)
	if sigHeader == "" {
		slog.Warn("Webhook rejected: missing signature header")
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "Missing signature header (provide X-Signature-256)",
		})
		return
	}

	if !verifyHMACSHA256(body, sigHeader, h.cfg.WebhookSecret) {
		slog.Warn("Webhook rejected: invalid HMAC signature")
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "Invalid HMAC signature",
		})
		return
	}

	// Parse incoming JSON payload
	var event processor.WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		slog.Warn("Failed parsing webhook JSON payload", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON payload structure",
		})
		return
	}

	// Basic field validation
	if strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.Type) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "Missing mandatory fields: 'id' and 'type' are required",
		})
		return
	}

	// Ensure timestamp is populated
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	// Push event to concurrent worker pool
	if err := h.proc.Enqueue(event); err != nil {
		if errors.Is(err, processor.ErrQueueFull) {
			slog.Warn("Event queue saturated, returning 503", "event_id", event.ID)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "Server busy: event queue capacity reached. Please retry with backoff.",
			})
			return
		}

		slog.Error("Unexpected failure enqueuing event", "event_id", event.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Internal server error occurred",
		})
		return
	}

	slog.Info("Webhook accepted for processing",
		"event_id", event.ID,
		"event_type", event.Type,
		"source", event.Source,
	)

	// Return 202 Accepted for async event ingestion
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":   "accepted",
		"event_id": event.ID,
		"message":  "Event enqueued for processing",
	})
}

// extractSignature checks common header conventions for HMAC-SHA256 signatures.
func extractSignature(r *http.Request) string {
	if sig := r.Header.Get("X-Signature-256"); sig != "" {
		return sig
	}
	if sig := r.Header.Get("X-Hub-Signature-256"); sig != "" {
		return sig
	}
	if sig := r.Header.Get("X-Signature"); sig != "" {
		return sig
	}
	return ""
}

// verifyHMACSHA256 performs a constant-time HMAC-SHA256 signature verification.
func verifyHMACSHA256(message []byte, receivedSig, secret string) bool {
	// Normalize received signature (strip 'sha256=' prefix if supplied by GitHub/Stripe style clients)
	cleanSig := strings.TrimSpace(receivedSig)
	cleanSig = strings.TrimPrefix(cleanSig, "sha256=")

	// Compute expected HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(message)
	expectedMAC := mac.Sum(nil)
	expectedHex := hex.EncodeToString(expectedMAC)

	// Use constant-time comparison to prevent timing attacks
	return hmac.Equal([]byte(expectedHex), []byte(strings.ToLower(cleanSig)))
}

// writeJSON encodes data as JSON and sets the appropriate headers and status code.
func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}
