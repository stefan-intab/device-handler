package cache

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"device-handler/internal/config"
	"device-handler/internal/metrics"
)

type SyncClient interface {
	FetchDevices(context.Context) ([]DeviceRecord, error)
	FetchUpdatedDevices(context.Context, time.Time) ([]DeviceRecord, error)
	FetchDeviceUpdates(context.Context) ([]DeviceUpdate, error)
}

type Syncer struct {
	client      SyncClient
	deviceCache *DeviceCache
	updateStore *DeviceUpdateStore
	cfg         config.SyncConfig
	logger      *slog.Logger

	statusMu           sync.RWMutex
	initialLoadDone    bool
	lastSuccessfulSync time.Time
	lastSyncError      string
	metrics            *metrics.AppMetrics
}

func NewSyncer(client SyncClient, deviceCache *DeviceCache, updateStore *DeviceUpdateStore, cfg config.SyncConfig, appMetrics *metrics.AppMetrics, logger *slog.Logger) *Syncer {
	return &Syncer{
		client:      client,
		deviceCache: deviceCache,
		updateStore: updateStore,
		cfg:         cfg,
		metrics:     appMetrics,
		logger:      logger,
	}
}

func (s *Syncer) InitialLoad(ctx context.Context) error {
	// Startup loads the full device set first so the ingest path can resolve
	// serial numbers immediately once the HTTP server starts accepting traffic.
	s.logger.Debug("initial cache sync started")
	devices, err := s.client.FetchDevices(ctx)
	if err != nil {
		s.metrics.CacheSyncFailures.Inc()
		s.logger.Error("initial device fetch failed", "error", err)
		return err
	}
	s.deviceCache.UpsertMany(devices)
	s.logger.Info("loaded devices into cache", "count", len(devices))

	// Device updates are stored separately because they are not required for
	// telemetry identity resolution, but they may drive future config responses.
	updates, err := s.client.FetchDeviceUpdates(ctx)
	if err != nil {
		s.metrics.CacheSyncFailures.Inc()
		s.logger.Error("initial device update fetch failed", "error", err)
		return err
	}
	s.updateStore.AddMany(updates)
	s.logger.Info("loaded device updates", "count", len(updates))
	s.logger.Debug("initial cache sync completed",
		"device_count", len(devices),
		"update_count", len(updates),
	)
	s.markSyncSuccess()

	return nil
}

func (s *Syncer) Start(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollOnce(ctx)
		}
	}
}

func (s *Syncer) pollOnce(ctx context.Context) {
	// Polling keeps the cache fresh even before NATS-based invalidation is added.
	s.logger.Debug("cache poll started")
	devices, err := s.client.FetchUpdatedDevices(ctx, s.deviceCache.UpdatedAt())
	pollErr := false
	if err != nil {
		pollErr = true
		s.logger.Error("poll updated devices", "error", err)
	} else if len(devices) > 0 {
		s.deviceCache.UpsertMany(devices)
		s.logger.Info("applied updated devices", "count", len(devices))
	}

	// Updates are kept even if there are no immediate consumers so the state is
	// available once two-way communication is introduced for supported devices.
	updates, err := s.client.FetchDeviceUpdates(ctx)
	if err != nil {
		pollErr = true
		s.logger.Error("poll device updates", "error", err)
	} else if len(updates) > 0 {
		s.updateStore.AddMany(updates)
		s.logger.Info("applied device updates", "count", len(updates))
	}
	if pollErr {
		s.markSyncError("one or more cache poll requests failed")
	} else {
		s.markSyncSuccess()
	}
	s.logger.Debug("cache poll completed")
}

func (s *Syncer) ReadyStatus() (bool, string) {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()

	if !s.initialLoadDone {
		return false, "initial cache sync not completed"
	}
	if !s.lastSuccessfulSync.IsZero() && time.Since(s.lastSuccessfulSync) > 2*s.cfg.PollInterval {
		if s.lastSyncError != "" {
			return false, s.lastSyncError
		}
		return false, "cache sync stale"
	}
	return true, ""
}

func (s *Syncer) markSyncSuccess() {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.initialLoadDone = true
	s.lastSuccessfulSync = time.Now().UTC()
	s.lastSyncError = ""
	s.metrics.CacheSyncSuccess.Inc()
	s.metrics.CacheLastSuccessTimestamp.Set(s.lastSuccessfulSync.Unix())
}

func (s *Syncer) markSyncError(message string) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.lastSyncError = message
	s.metrics.CacheSyncFailures.Inc()
}
