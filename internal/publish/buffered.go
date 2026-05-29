package publish

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"device-handler/internal/config"
	"device-handler/internal/telemetry"
)

const DefaultSubject = "channel.data.device-handler"

type EnqueuePublisher interface {
	Enqueue(context.Context, TelemetryEnvelope) error
}

type Publisher interface {
	Publish(context.Context, string, telemetry.Batch) error
}

type TelemetryEnvelope struct {
	Subject          string
	DeploymentSample telemetry.DeploymentSamples
	ReceivedAt       time.Time
}

type BufferedPublisher struct {
	downstream Publisher
	cfg        config.PublishConfig
	logger     *slog.Logger

	mu       sync.Mutex
	buffer   []TelemetryEnvelope
	flushing bool
}

func NewBufferedPublisher(downstream Publisher, cfg config.PublishConfig, logger *slog.Logger) *BufferedPublisher {
	return &BufferedPublisher{
		downstream: downstream,
		cfg:        cfg,
		logger:     logger,
		buffer:     make([]TelemetryEnvelope, 0, cfg.FlushCount),
	}
}

func (p *BufferedPublisher) Start(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.FlushAfter)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				// Flush once more during shutdown so the last short batch is not lost
				// just because it did not hit the time/count threshold yet.
				p.flush(ctx)
				return
			case <-ticker.C:
				// Time-based flushing caps end-to-end latency when traffic is low.
				p.flush(ctx)
			}
		}
	}()
}

func (p *BufferedPublisher) Enqueue(ctx context.Context, envelope TelemetryEnvelope) error {
	p.mu.Lock()
	p.buffer = append(p.buffer, envelope)
	// Count-based flushing protects us when traffic is high: we publish sooner
	// once the batch is large enough instead of waiting for the timer.
	shouldFlush := len(p.buffer) >= p.cfg.FlushCount && !p.flushing
	p.mu.Unlock()

	if shouldFlush {
		go p.flush(context.Background())
	}
	return nil
}

func (p *BufferedPublisher) flush(ctx context.Context) {
	p.mu.Lock()
	if p.flushing || len(p.buffer) == 0 {
		p.mu.Unlock()
		return
	}

	p.flushing = true
	// Copy the current batch so new incoming payloads can continue to queue while
	// the downstream publish call is in flight.
	items := make([]TelemetryEnvelope, len(p.buffer))
	copy(items, p.buffer)
	p.buffer = p.buffer[:0]
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.flushing = false
		p.mu.Unlock()
	}()

	transmissionID, err := newTransmissionID()
	if err != nil {
		p.logger.Error("generate transmission id", "error", err)
		return
	}

	subject := p.cfg.Subject
	if items[0].Subject != "" {
		// The first envelope may override the default subject when we later need
		// different internal routing without changing the batching logic.
		subject = items[0].Subject
	}

	batch := telemetry.Batch{
		TransmissionID:    transmissionID,
		DeploymentSamples: make([]telemetry.DeploymentSamples, 0, len(items)),
	}
	for _, item := range items {
		// Every HTTP upload currently becomes one DeploymentSamples entry.
		batch.DeploymentSamples = append(batch.DeploymentSamples, item.DeploymentSample)
	}

	if err := p.downstream.Publish(ctx, subject, batch); err != nil {
		p.logger.Error("publish telemetry batch", "error", err, "item_count", len(items))
		return
	}

	p.logger.Debug("published telemetry batch", "item_count", len(items), "subject", subject)
}

func newTransmissionID() ([]byte, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("read random bytes: %w", err)
	}
	return buf, nil
}
