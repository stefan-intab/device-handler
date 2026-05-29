package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL                = "http://127.0.0.1:8080/api/v1"
	defaultUsername               = "service"
	defaultPassword               = "service"
	defaultServiceName            = "device_handler"
	defaultHTTPAddress            = ":8083"
	defaultPollInterval           = 60 * time.Second
	defaultPublishFlushAfter      = time.Second
	defaultPublishFlushCount      = 50
	defaultPublishSubject         = "channel.data.device-handler"
	defaultInternalDevicesPath    = "/devices/internal/"
	defaultInternalUpdatedPath    = "/devices/internal/updated/"
	defaultOutdatedPropertiesPath = "/devices/internal/properties/outdated/"
	defaultLoginPath              = "/auth/token/"
	defaultRequestTimeout         = 10 * time.Second
	defaultShutdownTimeout        = 10 * time.Second
	defaultJWTLeeway              = 30 * time.Second
	defaultFallbackTokenTTL       = 10 * time.Minute
	defaultLogLevel               = "DEBUG"
)

type Config struct {
	HTTP     HTTPConfig
	API      APIConfig
	Sync     SyncConfig
	Publish  PublishConfig
	LogLevel slog.Level
}

type HTTPConfig struct {
	Address         string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type APIConfig struct {
	BaseURL                string
	Username               string
	Password               string
	ServiceName            string
	LoginPath              string
	InternalDevicesPath    string
	InternalUpdatedPath    string
	OutdatedPropertiesPath string
	RequestTimeout         time.Duration
	JWTLeeway              time.Duration
	FallbackTokenTTL       time.Duration
}

type SyncConfig struct {
	PollInterval time.Duration
}

type PublishConfig struct {
	Subject    string
	FlushAfter time.Duration
	FlushCount int
}

func Load() (Config, error) {
	level, err := parseLogLevel(getenv("LOG_LEVEL", defaultLogLevel))
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTP: HTTPConfig{
			Address:         getenv("HTTP_ADDRESS", defaultHTTPAddress),
			ReadTimeout:     mustDuration("HTTP_READ_TIMEOUT", defaultRequestTimeout),
			WriteTimeout:    mustDuration("HTTP_WRITE_TIMEOUT", defaultRequestTimeout),
			ShutdownTimeout: mustDuration("HTTP_SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
		},
		API: APIConfig{
			BaseURL:                getenv("API_BASE_URL", defaultBaseURL),
			Username:               getenv("API_USERNAME", defaultUsername),
			Password:               getenv("API_PASSWORD", defaultPassword),
			ServiceName:            getenv("SERVICE_NAME", defaultServiceName),
			LoginPath:              getenv("API_LOGIN_PATH", defaultLoginPath),
			InternalDevicesPath:    getenv("API_INTERNAL_DEVICES_PATH", defaultInternalDevicesPath),
			InternalUpdatedPath:    getenv("API_INTERNAL_UPDATED_PATH", defaultInternalUpdatedPath),
			OutdatedPropertiesPath: getenv("API_OUTDATED_PROPERTIES_PATH", defaultOutdatedPropertiesPath),
			RequestTimeout:         mustDuration("API_REQUEST_TIMEOUT", defaultRequestTimeout),
			JWTLeeway:              mustDuration("API_JWT_LEEWAY", defaultJWTLeeway),
			FallbackTokenTTL:       mustDuration("API_FALLBACK_TOKEN_TTL", defaultFallbackTokenTTL),
		},
		Sync: SyncConfig{
			PollInterval: mustDuration("SYNC_POLL_INTERVAL", defaultPollInterval),
		},
		Publish: PublishConfig{
			Subject:    getenv("PUBLISH_SUBJECT", defaultPublishSubject),
			FlushAfter: mustDuration("PUBLISH_FLUSH_AFTER", defaultPublishFlushAfter),
			FlushCount: mustInt("PUBLISH_FLUSH_COUNT", defaultPublishFlushCount),
		},
		LogLevel: level,
	}

	if cfg.API.BaseURL == "" {
		return Config{}, fmt.Errorf("API_BASE_URL is required")
	}
	if cfg.API.Username == "" {
		return Config{}, fmt.Errorf("API_USERNAME is required")
	}
	if cfg.API.Password == "" {
		return Config{}, fmt.Errorf("API_PASSWORD is required")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func mustDuration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		panic(fmt.Sprintf("%s: %v", key, err))
	}
	return value
}

func mustInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		panic(fmt.Sprintf("%s: %v", key, err))
	}
	return value
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DEBUG":
		return slog.LevelDebug, nil
	case "INFO":
		return slog.LevelInfo, nil
	case "WARN":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported LOG_LEVEL %q", value)
	}
}
