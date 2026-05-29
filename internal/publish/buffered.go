package publish

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"device-handler/internal/config"
	"device-handler/internal/metrics"
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

type bufferedItem struct {
	envelope   TelemetryEnvelope
	retryCount int
	notBefore  time.Time
}

type BufferedPublisher struct {
	downstream Publisher
	cfg        config.PublishConfig
	metrics    *metrics.AppMetrics
	logger     *slog.Logger

	mu       sync.Mutex
	buffer   []bufferedItem
	flushing bool
}

func NewBufferedPublisher(downstream Publisher, cfg config.PublishConfig, appMetrics *metrics.AppMetrics, logger *slog.Logger) *BufferedPublisher {
	return &BufferedPublisher{
		downstream: downstream,
		cfg:        cfg,
		metrics:    appMetrics,
		logger:     logger,
		buffer:     make([]bufferedItem, 0, cfg.FlushCount),
	}
}

func (p *BufferedPublisher) Start(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.FlushAfter)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				p.flush(ctx)
				return
			case <-ticker.C:
				p.flush(ctx)
			}
		}
	}()
}

func (p *BufferedPublisher) Enqueue(ctx context.Context, envelope TelemetryEnvelope) error {
	p.mu.Lock()
	p.buffer = append(p.buffer, bufferedItem{envelope: envelope})
	queueDepth := len(p.buffer)
	shouldFlush := queueDepth >= p.cfg.FlushCount && !p.flushing
	p.mu.Unlock()

	p.metrics.PublishQueueDepth.Set(int64(queueDepth))
	if shouldFlush {
		go p.flush(context.Background())
	}
	return nil
}

func (p *BufferedPublisher) flush(ctx context.Context) {
	items, subject, batch, err := p.dequeueReadyBatch()
	if err != nil {
		p.logger.Error("generate transmission id", "error", err)
		return
	}
	if len(items) == 0 {
		return
	}

	if err := p.downstream.Publish(ctx, subject, batch); err != nil {
		p.metrics.PublishFailures.Inc()
		p.handlePublishFailure(items, err)
		return
	}

	p.metrics.PublishBatches.Inc()
	p.logger.Debug("published telemetry batch", "item_count", len(items), "subject", subject)
	p.finishFlush()
}

func (p *BufferedPublisher) dequeueReadyBatch() ([]bufferedItem, string, telemetry.Batch, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.flushing || len(p.buffer) == 0 {
		return nil, "", telemetry.Batch{}, nil
	}

	now := time.Now().UTC()
	readyCount := 0
	for readyCount < len(p.buffer) && readyCount < p.cfg.FlushCount {
		if p.buffer[readyCount].notBefore.After(now) {
			break
		}
		readyCount++
	}
	if readyCount == 0 {
		return nil, "", telemetry.Batch{}, nil
	}

	p.flushing = true
	items := make([]bufferedItem, readyCount)
	copy(items, p.buffer[:readyCount])
	p.buffer = append([]bufferedItem(nil), p.buffer[readyCount:]...)
	p.metrics.PublishQueueDepth.Set(int64(len(p.buffer)))

	transmissionID, err := newTransmissionID()
	if err != nil {
		p.flushing = false
		return nil, "", telemetry.Batch{}, err
	}

	subject := p.cfg.Subject
	if items[0].envelope.Subject != "" {
		subject = items[0].envelope.Subject
	}

	batch := telemetry.Batch{
		TransmissionID:    transmissionID,
		DeploymentSamples: make([]telemetry.DeploymentSamples, 0, len(items)),
	}
	for _, item := range items {
		batch.DeploymentSamples = append(batch.DeploymentSamples, item.envelope.DeploymentSample)
	}

	return items, subject, batch, nil
}

func (p *BufferedPublisher) handlePublishFailure(items []bufferedItem, publishErr error) {
	now := time.Now().UTC()
	requeued := make([]bufferedItem, 0, len(items))
	droppedCount := 0

	for _, item := range items {
		if item.retryCount >= p.cfg.MaxRetries {
			droppedCount++
			continue
		}

		item.retryCount++
		item.notBefore = now.Add(backoffDelay(p.cfg.RetryBackoff, item.retryCount))
		requeued = append(requeued, item)
	}

	p.mu.Lock()
	p.buffer = append(requeued, p.buffer...)
	p.flushing = false
	queueDepth := len(p.buffer)
	p.mu.Unlock()

	if len(requeued) > 0 {
		p.metrics.PublishRetries.Add(uint64(len(requeued)))
	}
	if droppedCount > 0 {
		p.metrics.PublishDroppedItems.Add(uint64(droppedCount))
	}
	p.metrics.PublishQueueDepth.Set(int64(queueDepth))

	p.logger.Error("publish telemetry batch failed",
		"error", publishErr,
		"requeued_items", len(requeued),
		"dropped_items", droppedCount,
	)
}

func (p *BufferedPublisher) finishFlush() {
	p.mu.Lock()
	p.flushing = false
	p.mu.Unlock()
}

func newTransmissionID() ([]byte, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("read random bytes: %w", err)
	}
	return buf, nil
}

func backoffDelay(base time.Duration, retryCount int) time.Duration {
	if base <= 0 {
		return 0
	}

	shift := retryCount - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 6 {
		shift = 6
	}
	return base * time.Duration(1<<shift)
}
