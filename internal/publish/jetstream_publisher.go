package publish

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"device-handler/internal/config"
	"device-handler/internal/telemetry"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
)

type JetStreamPublisher struct {
	cfg    config.PublishConfig
	logger *slog.Logger
	nc     *nats.Conn
	js     nats.JetStreamContext
}

func NewJetStreamPublisher(cfg config.PublishConfig, serviceName string, logger *slog.Logger) (*JetStreamPublisher, error) {
	nc, err := nats.Connect(
		cfg.NATSURL,
		nats.Name(serviceName),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			logger.Warn("nats disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(conn *nats.Conn) {
			logger.Info("nats reconnected", "connected_url", conn.ConnectedUrl())
		}),
		nats.ClosedHandler(func(conn *nats.Conn) {
			logger.Warn("nats connection closed", "last_error", conn.LastError())
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("create jetstream context: %w", err)
	}

	logger.Info("jetstream publisher connected",
		"nats_url", cfg.NATSURL,
		"publish_subject", cfg.Subject,
	)

	return &JetStreamPublisher{
		cfg:    cfg,
		logger: logger,
		nc:     nc,
		js:     js,
	}, nil
}

func (p *JetStreamPublisher) Publish(ctx context.Context, subject string, batch telemetry.Batch) error {
	payload, err := proto.Marshal(toProtoBatch(batch))
	if err != nil {
		return fmt.Errorf("marshal telemetry batch as protobuf: %w", err)
	}

	msg := nats.NewMsg(subject)
	msg.Data = payload
	msg.Header.Set("Content-Type", "application/x-protobuf")
	msg.Header.Set("X-Proto-Schema", "telemetry.v1.Batch")
	msg.Header.Set("X-Ingest-Source", "device-handler")
	msg.Header.Set("X-Transmission-Id", hex.EncodeToString(batch.TransmissionID))

	pubCtx := ctx
	cancel := func() {}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && p.cfg.Timeout > 0 {
		pubCtx, cancel = context.WithTimeout(ctx, p.cfg.Timeout)
	}
	defer cancel()

	ack, err := p.js.PublishMsg(msg, nats.Context(pubCtx))
	if err != nil {
		return fmt.Errorf("jetstream publish on %s: %w", subject, err)
	}

	p.logger.Debug("jetstream publish acknowledged",
		"subject", subject,
		"stream", ack.Stream,
		"sequence", ack.Sequence,
		"payload_bytes", len(payload),
	)
	return nil
}

func (p *JetStreamPublisher) Close() {
	if p.nc != nil && !p.nc.IsClosed() {
		p.nc.Drain()
		p.nc.Close()
	}
}

func (p *JetStreamPublisher) ReadyStatus() (bool, string) {
	if p.nc == nil {
		return false, "nats connection not initialized"
	}

	switch p.nc.Status() {
	case nats.CONNECTED:
		return true, ""
	case nats.RECONNECTING:
		return false, "nats reconnecting"
	case nats.CLOSED:
		return false, "nats connection closed"
	default:
		if err := p.nc.LastError(); err != nil {
			return false, err.Error()
		}
		return false, p.nc.Status().String()
	}
}
