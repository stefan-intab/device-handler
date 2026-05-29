package ingest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"device-handler/internal/config"
	"device-handler/internal/device"
)

type HTTPServer struct {
	server  *http.Server
	service *Service
	logger  *slog.Logger
}

func NewHTTPServer(cfg config.HTTPConfig, service *Service, logger *slog.Logger) *HTTPServer {
	s := &HTTPServer{
		service: service,
		logger:  logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	// Devices post to a single endpoint and identify their parser through query
	// parameters, which keeps the HTTP surface small even as device support grows.
	mux.HandleFunc("/devices", s.handleDevices)

	s.server = &http.Server{
		Addr:         cfg.Address,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return s
}

func (s *HTTPServer) ListenAndServe() error {
	return s.server.ListenAndServe()
}

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *HTTPServer) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Manufacturer/model decide parser routing. Secret is carried through for
	// logging now and can later become a validation step.
	manufacturer := strings.TrimSpace(r.URL.Query().Get("manufacturer"))
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	secret := r.URL.Query().Get("secret")
	if manufacturer == "" || model == "" {
		http.Error(w, "manufacturer and model are required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read request body", http.StatusBadRequest)
		return
	}

	response, err := s.service.Handle(r.Context(), device.Request{
		Manufacturer: manufacturer,
		Model:        model,
		Secret:       secret,
		Body:         body,
		ContentType:  r.Header.Get("Content-Type"),
		ReceivedAt:   time.Now().UTC(),
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, device.ErrUnsupportedDevice) {
			// Unsupported device types are a routing problem, so return 404 rather
			// than pretending the payload itself was malformed.
			status = http.StatusNotFound
		} else if errors.Is(err, device.ErrDeviceNotFound) {
			// A missing serial is usually a cache propagation problem, so return a
			// retryable status instead of treating the payload as malformed.
			status = http.StatusServiceUnavailable
		}
		s.logger.Error("handle device payload", "error", err, "manufacturer", manufacturer, "model", model)
		http.Error(w, err.Error(), status)
		return
	}

	if response.ContentType != "" {
		w.Header().Set("Content-Type", response.ContentType)
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(response.Body)
}
