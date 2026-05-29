package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"device-handler/internal/api"
	"device-handler/internal/cache"
	"device-handler/internal/device"
	"device-handler/internal/publish"
)

type deviceResolver interface {
	FetchDevices(context.Context) ([]cache.DeviceRecord, error)
	FetchUpdatedDevices(context.Context, time.Time) ([]cache.DeviceRecord, error)
}

type channelCreator interface {
	CreateDeploymentChannel(context.Context, uint64, api.CreateChannelRequest) (cache.ChannelMapping, error)
}

type Service struct {
	registry       *device.Registry
	cache          *cache.DeviceCache
	resolver       deviceResolver
	channelCreator channelCreator
	publisher      publish.EnqueuePublisher
	logger         *slog.Logger
}

func NewService(registry *device.Registry, cache *cache.DeviceCache, resolver deviceResolver, channelCreator channelCreator, publisher publish.EnqueuePublisher, logger *slog.Logger) *Service {
	return &Service{
		registry:       registry,
		cache:          cache,
		resolver:       resolver,
		channelCreator: channelCreator,
		publisher:      publisher,
		logger:         logger,
	}
}

func (s *Service) Handle(ctx context.Context, req device.Request) (device.Response, error) {
	// Parser selection is entirely driven by the URL parameters so new device
	// types can be added by registering one more parser implementation.
	parser, ok := s.registry.Lookup(req.Manufacturer, req.Model)
	if !ok {
		return device.Response{}, device.ErrUnsupportedDevice
	}

	// Each parser knows how to turn its raw protocol payload into a normalized
	// internal representation with serial, timestamp, and channel values.
	parsed, err := parser.Parse(ctx, req)
	if err != nil {
		return device.Response{}, fmt.Errorf("parse device payload: %w", err)
	}

	// The cache is our source of truth for mapping a physical device upload to
	// internal IDs like deployment_id and channel_id.
	record, ok := s.cache.Lookup(req.Manufacturer, req.Model, parsed.Serial)
	if !ok {
		var err error
		record, ok, err = s.refreshAndLookupDevice(ctx, req.Manufacturer, req.Model, parsed.Serial)
		if err != nil {
			return device.Response{}, err
		}
		if !ok {
			return device.Response{}, fmt.Errorf("%w for serial %s", device.ErrDeviceNotFound, parsed.Serial)
		}
	}

	record, err = s.ensureChannels(ctx, req, record, parsed)
	if err != nil {
		return device.Response{}, err
	}

	// BuildTelemetry converts the parsed payload into the shared telemetry shape
	// that later gets encoded as protobuf and sent to JetStream.
	sample, err := device.BuildTelemetry(record, parsed)
	if err != nil {
		return device.Response{}, fmt.Errorf("build telemetry: %w", err)
	}

	// Enqueue is intentionally non-blocking with respect to downstream publish:
	// items are collected into short batches to reduce message pressure.
	envelope := publish.TelemetryEnvelope{
		DeploymentSample: sample,
		ReceivedAt:       time.Now().UTC(),
	}
	if err := s.publisher.Enqueue(ctx, envelope); err != nil {
		return device.Response{}, fmt.Errorf("enqueue telemetry: %w", err)
	}

	s.logger.Info("processed device payload",
		"manufacturer", req.Manufacturer,
		"model", req.Model,
		"serial", parsed.Serial,
		"secret", req.Secret,
		"channel_count", len(parsed.ChannelMeasurements),
	)

	return parsed.Response, nil
}

func (s *Service) refreshAndLookupDevice(ctx context.Context, manufacturer, model, serial string) (cache.DeviceRecord, bool, error) {
	s.logger.Warn("device not found in cache, refreshing cache before rejecting payload",
		"manufacturer", manufacturer,
		"model", model,
		"serial", serial,
	)

	updatedDevices, err := s.resolver.FetchUpdatedDevices(ctx, s.cache.UpdatedAt())
	if err != nil {
		return cache.DeviceRecord{}, false, fmt.Errorf("refresh updated devices after cache miss: %w", err)
	}
	if len(updatedDevices) > 0 {
		s.cache.UpsertMany(updatedDevices)
	}
	if record, ok := s.cache.Lookup(manufacturer, model, serial); ok {
		return record, true, nil
	}

	allDevices, err := s.resolver.FetchDevices(ctx)
	if err != nil {
		return cache.DeviceRecord{}, false, fmt.Errorf("refresh full device cache after cache miss: %w", err)
	}
	s.cache.UpsertMany(allDevices)
	record, ok := s.cache.Lookup(manufacturer, model, serial)
	return record, ok, nil
}

func (s *Service) ensureChannels(ctx context.Context, req device.Request, record cache.DeviceRecord, parsed device.ParseResult) (cache.DeviceRecord, error) {
	for _, measurement := range parsed.ChannelMeasurements {
		if _, err := cache.ResolveChannelIDByTag(record, measurement.Name); err == nil {
			continue
		}

		channelTag := strings.TrimSpace(measurement.Name)
		if channelTag == "" {
			continue
		}

		createReq := api.CreateChannelRequest{
			Tag:  channelTag,
			Name: channelTag,
			Unit: normalizeUnit(measurement.Unit),
		}
		mapping, err := s.channelCreator.CreateDeploymentChannel(ctx, record.DeploymentID, createReq)
		if err != nil {
			return cache.DeviceRecord{}, fmt.Errorf("create channel for deployment %d tag %q: %w", record.DeploymentID, channelTag, err)
		}

		updatedRecord, ok := s.cache.UpsertChannelMapping(req.Manufacturer, req.Model, parsed.Serial, mapping)
		if !ok {
			return cache.DeviceRecord{}, fmt.Errorf("update cache with new channel mapping for serial %s", parsed.Serial)
		}
		record = updatedRecord
		s.logger.Info("created channel for device",
			"manufacturer", req.Manufacturer,
			"model", req.Model,
			"serial", parsed.Serial,
			"deployment_id", record.DeploymentID,
			"channel_id", mapping.ChannelID,
			"tag", mapping.Tag,
			"unit", createReq.Unit,
		)
	}

	return record, nil
}

func normalizeUnit(raw string) string {
	switch strings.TrimSpace(raw) {
	case "C", "°C", "�C":
		return "°C"
	case "F", "°F":
		return "°F"
	case "%":
		return "%"
	case "%RH":
		return "%RH"
	default:
		return strings.TrimSpace(raw)
	}
}
