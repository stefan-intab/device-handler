package metrics

type AppMetrics struct {
	Registry *Registry

	IngestRequests           *Counter
	IngestSuccess            *Counter
	IngestParseFailures      *Counter
	IngestUnsupportedDevices *Counter
	IngestDeviceNotFound     *Counter
	IngestCacheMisses        *Counter

	ChannelCreateSuccess  *Counter
	ChannelCreateFailures *Counter

	CacheSyncSuccess          *Counter
	CacheSyncFailures         *Counter
	CacheLastSuccessTimestamp *Gauge

	PublishBatches      *Counter
	PublishFailures     *Counter
	PublishRetries      *Counter
	PublishDroppedItems *Counter
	PublishQueueDepth   *Gauge
}

func NewAppMetrics() *AppMetrics {
	registry := NewRegistry()

	return &AppMetrics{
		Registry: registry,

		IngestRequests:           registry.NewCounter("device_handler_ingest_requests_total", "Total incoming device publish requests."),
		IngestSuccess:            registry.NewCounter("device_handler_ingest_success_total", "Total successfully processed device publish requests."),
		IngestParseFailures:      registry.NewCounter("device_handler_ingest_parse_failures_total", "Total device payload parse failures."),
		IngestUnsupportedDevices: registry.NewCounter("device_handler_ingest_unsupported_devices_total", "Total requests rejected because manufacturer/model is unsupported."),
		IngestDeviceNotFound:     registry.NewCounter("device_handler_ingest_device_not_found_total", "Total requests that could not be matched to a cached device."),
		IngestCacheMisses:        registry.NewCounter("device_handler_ingest_cache_misses_total", "Total requests that missed the local device cache and triggered a refresh."),

		ChannelCreateSuccess:  registry.NewCounter("device_handler_channel_create_success_total", "Total channels auto-created by the handler."),
		ChannelCreateFailures: registry.NewCounter("device_handler_channel_create_failures_total", "Total failed automatic channel creation attempts."),

		CacheSyncSuccess:          registry.NewCounter("device_handler_cache_sync_success_total", "Total successful cache sync cycles, including initial sync."),
		CacheSyncFailures:         registry.NewCounter("device_handler_cache_sync_failures_total", "Total failed cache sync cycles."),
		CacheLastSuccessTimestamp: registry.NewGauge("device_handler_cache_last_success_timestamp_seconds", "Unix timestamp of the last successful cache sync."),

		PublishBatches:      registry.NewCounter("device_handler_publish_batches_total", "Total successfully published telemetry batches."),
		PublishFailures:     registry.NewCounter("device_handler_publish_failures_total", "Total failed telemetry batch publish attempts."),
		PublishRetries:      registry.NewCounter("device_handler_publish_retries_total", "Total telemetry items requeued for retry after publish failure."),
		PublishDroppedItems: registry.NewCounter("device_handler_publish_dropped_items_total", "Total telemetry items dropped after exhausting publish retries."),
		PublishQueueDepth:   registry.NewGauge("device_handler_publish_queue_depth", "Current number of telemetry items waiting in the publish buffer."),
	}
}
