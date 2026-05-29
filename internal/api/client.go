package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"device-handler/internal/cache"
	"device-handler/internal/config"
)

type queryValues map[string]string

type CreateChannelRequest struct {
	Tag  string `json:"tag"`
	Name string `json:"name"`
	Unit string `json:"unit"`
}

type JWTClient struct {
	cfg    config.APIConfig
	client *http.Client
	logger *slog.Logger

	mu           sync.Mutex
	token        string
	tokenExpires time.Time
}

func NewJWTClient(cfg config.APIConfig, logger *slog.Logger) *JWTClient {
	return &JWTClient{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
		logger: logger,
	}
}

func (c *JWTClient) FetchDevices(ctx context.Context) ([]cache.DeviceRecord, error) {
	c.logger.Debug("fetch devices started", "path", c.cfg.InternalDevicesPath)
	req, err := c.newRequest(ctx, http.MethodGet, c.cfg.InternalDevicesPath, queryValues{
		"include_channels": "true",
		"device_owner":     c.cfg.ServiceName,
	}, nil)
	if err != nil {
		return nil, err
	}

	var response deviceListResponse
	if err := c.doJSON(req, &response); err != nil {
		return nil, err
	}

	devices := response.devices()
	c.logger.Debug("fetch devices completed", "count", len(devices), "path", c.cfg.InternalDevicesPath)
	return devices, nil
}

func (c *JWTClient) FetchUpdatedDevices(ctx context.Context, updatedSince time.Time) ([]cache.DeviceRecord, error) {
	c.logger.Debug("fetch updated devices started",
		"path", c.cfg.InternalUpdatedPath,
		"updated_since", formatUpdatedSince(updatedSince),
	)
	req, err := c.newRequest(ctx, http.MethodGet, c.cfg.InternalUpdatedPath, queryValues{
		"include_channels": "true",
		"device_owner":     c.cfg.ServiceName,
		"updated_since":    formatUpdatedSince(updatedSince),
	}, nil)
	if err != nil {
		return nil, err
	}

	var response deviceListResponse
	if err := c.doJSON(req, &response); err != nil {
		return nil, err
	}

	devices := response.devices()
	c.logger.Debug("fetch updated devices completed", "count", len(devices), "path", c.cfg.InternalUpdatedPath)
	return devices, nil
}

func (c *JWTClient) FetchDeviceUpdates(ctx context.Context) ([]cache.DeviceUpdate, error) {
	c.logger.Debug("fetch outdated properties started", "path", c.cfg.OutdatedPropertiesPath)
	req, err := c.newRequest(ctx, http.MethodGet, c.cfg.OutdatedPropertiesPath, queryValues{
		"device_owner": c.cfg.ServiceName,
	}, nil)
	if err != nil {
		return nil, err
	}

	var response deviceUpdateListResponse
	if err := c.doJSON(req, &response); err != nil {
		return nil, err
	}

	updates := response.updates()
	c.logger.Debug("fetch outdated properties completed", "count", len(updates), "path", c.cfg.OutdatedPropertiesPath)
	return updates, nil
}

func (c *JWTClient) CreateDeploymentChannel(ctx context.Context, deploymentID uint64, input CreateChannelRequest) (cache.ChannelMapping, error) {
	path := fmt.Sprintf("/device-deployments/%d/channels/", deploymentID)
	c.logger.Debug("create deployment channel started",
		"deployment_id", deploymentID,
		"tag", input.Tag,
		"unit", input.Unit,
	)
	req, err := c.newRequest(ctx, http.MethodPost, path, nil, input)
	if err != nil {
		return cache.ChannelMapping{}, err
	}

	var response struct {
		ID  uint64 `json:"id"`
		Tag string `json:"tag"`
	}
	if err := c.doJSON(req, &response); err != nil {
		return cache.ChannelMapping{}, err
	}

	mapping := cache.ChannelMapping{
		ChannelID: response.ID,
		Tag:       response.Tag,
	}
	if strings.TrimSpace(mapping.Tag) == "" {
		mapping.Tag = input.Tag
	}

	c.logger.Debug("create deployment channel completed",
		"deployment_id", deploymentID,
		"channel_id", mapping.ChannelID,
		"tag", mapping.Tag,
	)
	return mapping, nil
}

func (c *JWTClient) newRequest(ctx context.Context, method, path string, query queryValues, body any) (*http.Request, error) {
	fullURL, err := url.JoinPath(c.cfg.BaseURL, path)
	if err != nil {
		return nil, fmt.Errorf("build request url: %w", err)
	}

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, &buf)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if len(query) > 0 {
		values := req.URL.Query()
		for key, value := range query {
			values.Set(key, value)
		}
		req.URL.RawQuery = values.Encode()
	}

	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	token, err := c.ensureToken(ctx)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	c.logger.Debug("api request prepared",
		"method", method,
		"url", req.URL.String(),
		"has_body", body != nil,
	)

	return req, nil
}

func (c *JWTClient) doJSON(req *http.Request, out any) error {
	startedAt := time.Now()
	c.logger.Debug("api request sending", "method", req.Method, "url", req.URL.String())
	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Error("api request failed",
			"method", req.Method,
			"url", req.URL.String(),
			"duration", time.Since(startedAt).String(),
			"error", err,
		)
		return fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()
	c.logger.Debug("api response received",
		"method", req.Method,
		"url", req.URL.String(),
		"status", resp.Status,
		"duration", time.Since(startedAt).String(),
	)

	if resp.StatusCode == http.StatusUnauthorized {
		c.logger.Warn("api request unauthorized, refreshing token",
			"method", req.Method,
			"url", req.URL.String(),
		)
		if err := c.clearToken(); err != nil {
			return err
		}
		retryReq := req.Clone(req.Context())
		token, err := c.ensureToken(req.Context())
		if err != nil {
			return err
		}
		retryReq.Header.Set("Authorization", "Bearer "+token)

		resp, err = c.client.Do(retryReq)
		if err != nil {
			return fmt.Errorf("perform retry request: %w", err)
		}
		defer resp.Body.Close()
		c.logger.Debug("api retry response received",
			"method", retryReq.Method,
			"url", retryReq.URL.String(),
			"status", resp.Status,
			"duration", time.Since(startedAt).String(),
		)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logger.Error("api request returned unexpected status",
			"method", req.Method,
			"url", req.URL.String(),
			"status", resp.Status,
			"response_body", previewBody(body),
		)
		return fmt.Errorf("unexpected status %s: %s", resp.Status, previewBody(body))
	}

	if err := json.Unmarshal(body, out); err == nil {
		c.logger.Debug("api response decoded",
			"method", req.Method,
			"url", req.URL.String(),
			"response_bytes", len(body),
		)
		return nil
	} else if rawListDecoder, ok := out.(interface{ decodeRawList(json.RawMessage) error }); ok {
		var raw json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			return fmt.Errorf("decode raw response: %w", err)
		}
		if err := rawListDecoder.decodeRawList(raw); err != nil {
			return err
		}
		c.logger.Debug("api raw-list response decoded",
			"method", req.Method,
			"url", req.URL.String(),
			"response_bytes", len(body),
		)
		return nil
	} else {
		c.logger.Error("api response decode failed",
			"method", req.Method,
			"url", req.URL.String(),
			"response_body", previewBody(body),
			"error", err,
		)
		return fmt.Errorf("decode response body: %w", err)
	}
}

func (c *JWTClient) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Until(c.tokenExpires) > c.cfg.JWTLeeway {
		c.logger.Debug("reusing cached api token", "expires_at", c.tokenExpires.UTC().Format(time.RFC3339))
		return c.token, nil
	}

	body := map[string]string{
		"username": c.cfg.Username,
		"password": c.cfg.Password,
	}

	fullURL, err := url.JoinPath(c.cfg.BaseURL, c.cfg.LoginPath)
	if err != nil {
		return "", fmt.Errorf("build login url: %w", err)
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return "", fmt.Errorf("encode login body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, &buf)
	if err != nil {
		return "", fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	c.logger.Debug("api login started",
		"url", fullURL,
		"username", c.cfg.Username,
	)

	startedAt := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Error("api login failed",
			"url", fullURL,
			"username", c.cfg.Username,
			"duration", time.Since(startedAt).String(),
			"error", err,
		)
		return "", fmt.Errorf("perform login request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read login response body: %w", err)
	}
	c.logger.Debug("api login response received",
		"url", fullURL,
		"status", resp.Status,
		"duration", time.Since(startedAt).String(),
	)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logger.Error("api login returned unexpected status",
			"url", fullURL,
			"status", resp.Status,
			"response_body", previewBody(respBody),
		)
		return "", fmt.Errorf("login returned %s: %s", resp.Status, previewBody(respBody))
	}

	var loginResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &loginResp); err != nil {
		c.logger.Error("decode login response failed",
			"url", fullURL,
			"response_body", previewBody(respBody),
			"error", err,
		)
		return "", fmt.Errorf("decode login response: %w", err)
	}

	token := strings.TrimSpace(loginResp.Token)
	if token == "" {
		token = strings.TrimSpace(loginResp.AccessToken)
	}
	if token == "" {
		return "", fmt.Errorf("login response did not include token")
	}

	c.token = token
	c.tokenExpires = tokenExpiry(token, c.cfg.FallbackTokenTTL, loginResp.ExpiresIn)
	c.logger.Debug("api login succeeded",
		"url", fullURL,
		"token_preview", previewToken(token),
		"expires_at", c.tokenExpires.UTC().Format(time.RFC3339),
	)
	return c.token, nil
}

func (c *JWTClient) clearToken() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
	c.tokenExpires = time.Time{}
	return nil
}

var _ interface {
	FetchDevices(context.Context) ([]cache.DeviceRecord, error)
	FetchUpdatedDevices(context.Context, time.Time) ([]cache.DeviceRecord, error)
	FetchDeviceUpdates(context.Context) ([]cache.DeviceUpdate, error)
	CreateDeploymentChannel(context.Context, uint64, CreateChannelRequest) (cache.ChannelMapping, error)
} = (*JWTClient)(nil)

func tokenExpiry(token string, fallback time.Duration, expiresInSeconds int64) time.Time {
	if expiresInSeconds > 0 {
		return time.Now().Add(time.Duration(expiresInSeconds) * time.Second)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Now().Add(fallback)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Now().Add(fallback)
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Now().Add(fallback)
	}
	return time.Unix(claims.Exp, 0)
}

type deviceListResponse struct {
	Items   []cache.DeviceRecord `json:"items"`
	Devices []cache.DeviceRecord `json:"devices"`
}

func (r deviceListResponse) devices() []cache.DeviceRecord {
	if len(r.Devices) > 0 {
		return r.Devices
	}
	return r.Items
}

func (r *deviceListResponse) decodeRawList(raw json.RawMessage) error {
	return json.Unmarshal(raw, &r.Items)
}

type deviceUpdateListResponse struct {
	Items   []cache.DeviceUpdate `json:"items"`
	Updates []cache.DeviceUpdate `json:"updates"`
}

func (r deviceUpdateListResponse) updates() []cache.DeviceUpdate {
	if len(r.Updates) > 0 {
		return r.Updates
	}
	return r.Items
}

func (r *deviceUpdateListResponse) decodeRawList(raw json.RawMessage) error {
	return json.Unmarshal(raw, &r.Items)
}

func previewBody(body []byte) string {
	const maxLen = 512
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) <= maxLen {
		return trimmed
	}
	return trimmed[:maxLen] + "...(truncated)"
}

func previewToken(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:6] + "..." + token[len(token)-6:]
}

func formatUpdatedSince(value time.Time) string {
	if value.IsZero() {
		return time.Now().UTC().Format(time.RFC3339)
	}
	return value.UTC().Format(time.RFC3339)
}
