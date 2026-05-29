package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"device-handler/internal/api"
	"device-handler/internal/cache"
	"device-handler/internal/config"
	"device-handler/internal/device"
	cometsoaptx "device-handler/internal/device/comet/soaptx"
	cometulogger "device-handler/internal/device/comet/ulogger"
	cometwebsensor "device-handler/internal/device/comet/websensor"
	"device-handler/internal/ingest"
	"device-handler/internal/metrics"
	"device-handler/internal/publish"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	appMetrics := metrics.NewAppMetrics()
	apiClient := api.NewJWTClient(cfg.API, logger)
	deviceCache := cache.NewDeviceCache()
	updateStore := cache.NewDeviceUpdateStore()
	syncer := cache.NewSyncer(apiClient, deviceCache, updateStore, cfg.Sync, appMetrics, logger)

	logger.Debug("loaded runtime config",
		"http_address", cfg.HTTP.Address,
		"api_base_url", cfg.API.BaseURL,
		"service_name", cfg.API.ServiceName,
		"api_login_path", cfg.API.LoginPath,
		"api_internal_devices_path", cfg.API.InternalDevicesPath,
		"api_internal_updated_path", cfg.API.InternalUpdatedPath,
		"api_outdated_properties_path", cfg.API.OutdatedPropertiesPath,
		"api_username", cfg.API.Username,
		"sync_poll_interval", cfg.Sync.PollInterval.String(),
		"nats_url", cfg.Publish.NATSURL,
		"publish_subject", cfg.Publish.Subject,
		"publish_retry_backoff", cfg.Publish.RetryBackoff.String(),
		"publish_max_retries", cfg.Publish.MaxRetries,
		"log_level", cfg.LogLevel.String(),
	)

	if err := syncer.InitialLoad(ctx); err != nil {
		logger.Error("initial cache sync failed", "error", err)
		os.Exit(1)
	}

	registry := device.NewRegistry()
	registry.Register(cometulogger.New())
	for _, model := range cometwebsensor.SupportedModels() {
		registry.Register(cometwebsensor.New(model))
	}
	for _, model := range cometsoaptx.SupportedModels() {
		registry.Register(cometsoaptx.New(model))
	}

	basePublisher, err := publish.NewJetStreamPublisher(cfg.Publish, cfg.API.ServiceName, logger)
	if err != nil {
		logger.Error("connect jetstream publisher", "error", err)
		os.Exit(1)
	}
	defer basePublisher.Close()
	bufferedPublisher := publish.NewBufferedPublisher(basePublisher, cfg.Publish, appMetrics, logger)
	bufferedPublisher.Start(ctx)

	service := ingest.NewService(registry, deviceCache, apiClient, apiClient, bufferedPublisher, appMetrics, logger)
	server := ingest.NewHTTPServer(cfg.HTTP, service, func() (bool, []string) {
		var reasons []string

		if ready, reason := syncer.ReadyStatus(); !ready {
			reasons = append(reasons, "cache/api: "+reason)
		}
		if ready, reason := basePublisher.ReadyStatus(); !ready {
			reasons = append(reasons, "nats: "+reason)
		}

		return len(reasons) == 0, reasons
	}, appMetrics.Registry, logger)

	go syncer.Start(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("http server shutdown failed", "error", err)
		}
	}()

	logger.Info("device handler starting",
		"addr", cfg.HTTP.Address,
		"subject", cfg.Publish.Subject,
		"cache_poll_interval", cfg.Sync.PollInterval.String(),
	)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server failed", "error", err)
		os.Exit(1)
	}
}
