package cache

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type DeviceRecord struct {
	DeviceID            uint64           `json:"device_id"`
	DeploymentID        uint64           `json:"id"`
	GatewayDeploymentID *uint64          `json:"gateway_deployment_id,omitempty"`
	Serial              string           `json:"serial_number"`
	Manufacturer        string           `json:"manufacturer"`
	Model               string           `json:"model"`
	ChannelMappings     []ChannelMapping `json:"channels,omitempty"`
	UpdatedAt           time.Time        `json:"sync_updated_at,omitempty"`
}

type ChannelMapping struct {
	ChannelID uint64 `json:"id"`
	Tag       string `json:"tag"`
}

type DeviceCache struct {
	mu        sync.RWMutex
	byKey     map[string]DeviceRecord
	updatedAt time.Time
}

func NewDeviceCache() *DeviceCache {
	return &DeviceCache{
		byKey: make(map[string]DeviceRecord),
	}
}

func (c *DeviceCache) UpsertMany(records []DeviceRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, record := range records {
		if record.UpdatedAt.IsZero() {
			record.UpdatedAt = time.Now().UTC()
		}
		c.byKey[key(record.Manufacturer, record.Model, record.Serial)] = record
		if record.UpdatedAt.After(c.updatedAt) {
			c.updatedAt = record.UpdatedAt
		}
	}
}

func (c *DeviceCache) Lookup(manufacturer, model, serial string) (DeviceRecord, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	record, ok := c.byKey[key(manufacturer, model, serial)]
	return record, ok
}

func (c *DeviceCache) Snapshot() []DeviceRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]DeviceRecord, 0, len(c.byKey))
	for _, record := range c.byKey {
		result = append(result, record)
	}
	return result
}

func (c *DeviceCache) UpdatedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.updatedAt
}

func (c *DeviceCache) UpsertChannelMapping(manufacturer, model, serial string, mapping ChannelMapping) (DeviceRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cacheKey := key(manufacturer, model, serial)
	record, ok := c.byKey[cacheKey]
	if !ok {
		return DeviceRecord{}, false
	}

	for i := range record.ChannelMappings {
		if normalizeChannelTag(record.ChannelMappings[i].Tag) == normalizeChannelTag(mapping.Tag) {
			record.ChannelMappings[i] = mapping
			c.byKey[cacheKey] = record
			return record, true
		}
	}

	record.ChannelMappings = append(record.ChannelMappings, mapping)
	c.byKey[cacheKey] = record
	return record, true
}

func key(manufacturer, model, serial string) string {
	return strings.ToLower(strings.TrimSpace(manufacturer)) + "|" +
		strings.ToLower(strings.TrimSpace(model)) + "|" +
		strings.TrimSpace(serial)
}

func DebugKey(manufacturer, model, serial string) string {
	return key(manufacturer, model, serial)
}

func (c *DeviceCache) FindBySerial(serial string) []DeviceRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()

	normalizedSerial := strings.TrimSpace(serial)
	var matches []DeviceRecord
	for _, record := range c.byKey {
		if strings.TrimSpace(record.Serial) == normalizedSerial {
			matches = append(matches, record)
		}
	}
	return matches
}

func (c *DeviceCache) FindByManufacturerAndSerial(manufacturer, serial string) []DeviceRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()

	normalizedManufacturer := strings.ToLower(strings.TrimSpace(manufacturer))
	normalizedSerial := strings.TrimSpace(serial)
	var matches []DeviceRecord
	for _, record := range c.byKey {
		if strings.ToLower(strings.TrimSpace(record.Manufacturer)) == normalizedManufacturer &&
			strings.TrimSpace(record.Serial) == normalizedSerial {
			matches = append(matches, record)
		}
	}
	return matches
}

type DeviceUpdate struct {
	DeviceID     uint64          `json:"device_id"`
	DeploymentID uint64          `json:"id"`
	Serial       string          `json:"serial_number"`
	Manufacturer string          `json:"manufacturer"`
	Model        string          `json:"model"`
	Type         string          `json:"sync_action"`
	UpdatedAt    time.Time       `json:"properties_updated_at"`
	Raw          json.RawMessage `json:"raw,omitempty"`
}

type DeviceUpdateStore struct {
	mu        sync.RWMutex
	updates   []DeviceUpdate
	updatedAt time.Time
}

func NewDeviceUpdateStore() *DeviceUpdateStore {
	return &DeviceUpdateStore{}
}

func (s *DeviceUpdateStore) AddMany(updates []DeviceUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, update := range updates {
		s.updates = append(s.updates, update)
		if update.UpdatedAt.After(s.updatedAt) {
			s.updatedAt = update.UpdatedAt
		}
	}
}

func (s *DeviceUpdateStore) Snapshot() []DeviceUpdate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]DeviceUpdate, len(s.updates))
	copy(result, s.updates)
	return result
}

func (s *DeviceUpdateStore) UpdatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

func ResolveChannelID(record DeviceRecord, channelNumber int) (uint64, error) {
	return 0, fmt.Errorf("channel mapping by number is not configured for this device family: %d", channelNumber)
}

func ResolveChannelIDByTag(record DeviceRecord, tag string) (uint64, error) {
	normalizedTag := normalizeChannelTag(tag)
	for _, mapping := range record.ChannelMappings {
		if normalizeChannelTag(mapping.Tag) == normalizedTag {
			return mapping.ChannelID, nil
		}
	}
	return 0, fmt.Errorf("channel mapping missing for tag %q", tag)
}

func normalizeChannelTag(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (r *DeviceRecord) UnmarshalJSON(data []byte) error {
	type rawDeviceRecord struct {
		DeviceID            uint64           `json:"device_id"`
		DeploymentID        uint64           `json:"id"`
		GatewayDeploymentID *uint64          `json:"gateway_deployment_id"`
		SerialNumber        string           `json:"serial_number"`
		Manufacturer        string           `json:"manufacturer"`
		Model               string           `json:"model"`
		Channels            []ChannelMapping `json:"channels"`
		SyncUpdatedAt       *time.Time       `json:"sync_updated_at"`
		PropertiesUpdatedAt *time.Time       `json:"properties_updated_at"`
	}

	var raw rawDeviceRecord
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	r.DeviceID = raw.DeviceID
	r.DeploymentID = raw.DeploymentID
	r.GatewayDeploymentID = raw.GatewayDeploymentID
	r.Serial = raw.SerialNumber
	r.Manufacturer = raw.Manufacturer
	r.Model = raw.Model
	r.ChannelMappings = raw.Channels
	if raw.SyncUpdatedAt != nil {
		r.UpdatedAt = raw.SyncUpdatedAt.UTC()
	} else if raw.PropertiesUpdatedAt != nil {
		r.UpdatedAt = raw.PropertiesUpdatedAt.UTC()
	}

	return nil
}

func (u *DeviceUpdate) UnmarshalJSON(data []byte) error {
	type rawDeviceUpdate struct {
		DeviceID            uint64     `json:"device_id"`
		DeploymentID        uint64     `json:"id"`
		SerialNumber        string     `json:"serial_number"`
		Manufacturer        string     `json:"manufacturer"`
		Model               string     `json:"model"`
		SyncAction          string     `json:"sync_action"`
		PropertiesUpdatedAt *time.Time `json:"properties_updated_at"`
		SyncUpdatedAt       *time.Time `json:"sync_updated_at"`
	}

	var raw rawDeviceUpdate
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	u.DeviceID = raw.DeviceID
	u.DeploymentID = raw.DeploymentID
	u.Serial = raw.SerialNumber
	u.Manufacturer = raw.Manufacturer
	u.Model = raw.Model
	u.Type = raw.SyncAction
	u.Raw = append(u.Raw[:0], data...)
	if raw.PropertiesUpdatedAt != nil {
		u.UpdatedAt = raw.PropertiesUpdatedAt.UTC()
	} else if raw.SyncUpdatedAt != nil {
		u.UpdatedAt = raw.SyncUpdatedAt.UTC()
	}

	return nil
}
