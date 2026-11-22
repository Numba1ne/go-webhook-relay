package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"go-webhook-relay/internal/config"
	"go-webhook-relay/internal/handler"
	"go-webhook-relay/internal/processor"
)

func main() {
	// 1. Load configuration
	cfg := config.Load()

	// 2. Setup structured logging (slog)
	setupLogger(cfg.LogLevel)
	slog.Info("Configuration initialized",
		"port", cfg.Port,
		"worker_count", cfg.WorkerCount,
		"queue_capacity", cfg.QueueCapacity,
		"log_level", cfg.LogLevel,
	)

	// 3. Initialize & start concurrent worker pool
	proc := processor.New(cfg.WorkerCount, cfg.QueueCapacity)
	proc.Start()

	// 4. Initialize HTTP handlers
	webhookHandler := handler.New(cfg, proc)

	// 5. Configure Chi router & middlewares
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(structuredLoggingMiddleware())
	r.Use(middleware.Timeout(30 * time.Second))

	// Endpoints
	r.Get("/health", webhookHandler.HealthHandler)
	r.Post("/webhook", webhookHandler.WebhookHandler)

	// 6. Setup HTTP server with production-grade timeouts
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 7. Start server asynchronously
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("Starting HTTP server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- fmt.Errorf("server error: %w", err)
		}
	}()

	// 8. Graceful shutdown listener
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-serverErrors:
		slog.Error("Fatal server error occurred", "error", err)
		os.Exit(1)

	case sig := <-shutdown:
		slog.Info("Shutdown signal received", "signal", sig.String())

		// Create timeout context for server drainage
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// Stop accepting new HTTP connections
		slog.Info("Shutting down HTTP server...")
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("Error during server shutdown", "error", err)
			_ = server.Close()
		}

		// Gracefully drain worker pool
		if err := proc.Stop(10 * time.Second); err != nil {
			slog.Error("Error stopping worker pool", "error", err)
		}

		slog.Info("Graceful shutdown completed successfully. Exiting.")
	}
}

// setupLogger initializes slog default logger based on the configured log level.
func setupLogger(levelStr string) {
	var level slog.Level
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	// Use JSON formatted logs in production environments
	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))
	slog.SetDefault(logger)
}

// structuredLoggingMiddleware logs inbound requests with duration, status, and request ID.
func structuredLoggingMiddleware() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				slog.Info("Inbound HTTP request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", ww.Status(),
					"bytes", ww.BytesWritten(),
					"duration_ms", time.Since(start).Milliseconds(),
					"client_ip", r.RemoteAddr,
					"request_id", middleware.GetReqID(r.Context()),
				)
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
