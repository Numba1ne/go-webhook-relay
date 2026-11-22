package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrQueueFull is returned when the internal buffer is saturated.
	ErrQueueFull = errors.New("event queue is full")
	// ErrStopped is returned when attempting to enqueue to a terminated processor.
	ErrStopped = errors.New("processor has been stopped")
)

// WebhookEvent represents an incoming webhook payload (e.g. payments, blockchain transactions).
type WebhookEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`      // e.g. "payment.succeeded", "blockchain.tx_confirmed"
	Source    string          `json:"source"`    // e.g. "stripe", "ethereum", "paypal"
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`   // Flexible JSON body for specific event data
}

// Stats provides real-time snapshot metrics of the worker pool.
type Stats struct {
	WorkerCount    int    `json:"worker_count"`
	QueuedEvents   int    `json:"queued_events"`
	QueueCapacity  int    `json:"queue_capacity"`
	ProcessedCount uint64 `json:"processed_count"`
	DroppedCount   uint64 `json:"dropped_count"`
}

// Processor manages an in-memory worker pool for processing events concurrently.
type Processor struct {
	workerCount int
	queue       chan WebhookEvent
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	stopped     atomic.Bool

	// Atomic counters for telemetry
	processedCount atomic.Uint64
	droppedCount   atomic.Uint64
}

// New initializes a Processor with a given worker pool size and buffer capacity.
func New(workerCount, queueCapacity int) *Processor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Processor{
		workerCount: workerCount,
		queue:       make(chan WebhookEvent, queueCapacity),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start spawns the worker pool goroutines.
func (p *Processor) Start() {
	slog.Info("Starting event processor worker pool",
		"worker_count", p.workerCount,
		"queue_capacity", cap(p.queue),
	)

	for i := 1; i <= p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// Enqueue puts an event into the channel queue without blocking.
// If the buffer is full, it immediately returns ErrQueueFull for load shedding.
func (p *Processor) Enqueue(event WebhookEvent) error {
	if p.stopped.Load() {
		return ErrStopped
	}

	select {
	case p.queue <- event:
		return nil
	default:
		p.droppedCount.Add(1)
		return ErrQueueFull
	}
}

// Stop initiates a graceful drain of the queue and waits for workers to finish.
func (p *Processor) Stop(timeout time.Duration) error {
	slog.Info("Initiating graceful shutdown of event processor...")
	p.stopped.Store(true)

	// Closing the channel signals workers to finish remaining buffered items and exit
	close(p.queue)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.cancel()
		slog.Info("All workers terminated cleanly",
			"total_processed", p.processedCount.Load(),
			"total_dropped", p.droppedCount.Load(),
		)
		return nil
	case <-time.After(timeout):
		p.cancel()
		return fmt.Errorf("processor shutdown timed out after %s", timeout)
	}
}

// worker loops over the event queue until the channel is closed.
func (p *Processor) worker(workerID int) {
	defer p.wg.Done()
	slog.Debug("Worker started", "worker_id", workerID)

	for event := range p.queue {
		p.processEvent(workerID, event)
	}

	slog.Debug("Worker finished draining", "worker_id", workerID)
}

// processEvent simulates domain logic execution (e.g. database transactions, external webhooks).
func (p *Processor) processEvent(workerID int, event WebhookEvent) {
	start := time.Now()

	slog.Info("Processing event",
		"worker_id", workerID,
		"event_id", event.ID,
		"event_type", event.Type,
		"source", event.Source,
	)

	// Simulate event-specific domain logic and network/db latency
	switch event.Type {
	case "payment.succeeded":
		// Example: Credit customer balance, dispatch invoice email
		time.Sleep(40 * time.Millisecond)

	case "payment.failed":
		// Example: Mark order status pending, alert risk system
		time.Sleep(30 * time.Millisecond)

	case "blockchain.tx_confirmed":
		// Example: Confirm on-chain deposit, mint internal tokens
		time.Sleep(60 * time.Millisecond)

	default:
		// Default handler for general webhooks
		time.Sleep(25 * time.Millisecond)
	}

	duration := time.Since(start)
	p.processedCount.Add(1)

	slog.Info("Event processed successfully",
		"worker_id", workerID,
		"event_id", event.ID,
		"event_type", event.Type,
		"duration_ms", duration.Milliseconds(),
	)
}

// Stats returns current pool health and queue telemetry.
func (p *Processor) Stats() Stats {
	return Stats{
		WorkerCount:    p.workerCount,
		QueuedEvents:   len(p.queue),
		QueueCapacity:  cap(p.queue),
		ProcessedCount: p.processedCount.Load(),
		DroppedCount:   p.droppedCount.Load(),
	}
}
