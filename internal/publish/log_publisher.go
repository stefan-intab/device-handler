package publish

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log/slog"

	"device-handler/internal/telemetry"
)

type LogPublisher struct {
	logger *slog.Logger
}

func NewLogPublisher(logger *slog.Logger) *LogPublisher {
	return &LogPublisher{logger: logger}
}

func (p *LogPublisher) Publish(_ context.Context, subject string, batch telemetry.Batch) error {
	// This is a temporary stand-in for the real JetStream publisher. We still
	// marshal the batch so we exercise roughly the same payload shape in dev.
	payload, err := json.Marshal(batch)
	if err != nil {
		return err
	}

	p.logger.Info("telemetry batch ready for publish",
		"subject", subject,
		"transmission_id", hex.EncodeToString(batch.TransmissionID),
		"deployment_samples", len(batch.DeploymentSamples),
		"payload_bytes", len(payload),
	)
	return nil
}
