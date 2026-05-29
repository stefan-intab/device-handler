package ulogger

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"device-handler/internal/device"
)

const secondsSince2000ToUnix = 946684800

type Parser struct{}

func New() *Parser {
	return &Parser{}
}

func (p *Parser) Manufacturer() string { return "comet" }
func (p *Parser) Model() string        { return "UxxxxM" }

func (p *Parser) Parse(_ context.Context, req device.Request) (device.ParseResult, error) {
	body := bytes.TrimPrefix(req.Body, []byte("\xef\xbb\xbf"))

	var raw []any
	if err := json.Unmarshal(body, &raw); err != nil {
		return device.ParseResult{}, fmt.Errorf("decode comet payload: %w", err)
	}

	if len(raw) < 22 {
		return device.ParseResult{}, fmt.Errorf("unexpected top-level array length %d", len(raw))
	}

	serial, err := asString(raw[6])
	if err != nil {
		return device.ParseResult{}, fmt.Errorf("read serial: %w", err)
	}

	occurredAt, err := parseDeviceTime(raw[9])
	if err != nil {
		return device.ParseResult{}, fmt.Errorf("read device time: %w", err)
	}

	rssi, err := asFloat32(raw[2])
	if err != nil {
		return device.ParseResult{}, fmt.Errorf("read rssi: %w", err)
	}

	battery, err := asFloat32(raw[20])
	if err != nil {
		return device.ParseResult{}, fmt.Errorf("read battery percentage: %w", err)
	}

	channelItems, ok := raw[21].([]any)
	if !ok {
		return device.ParseResult{}, fmt.Errorf("channels must be an array")
	}

	channels := make([]device.ChannelMeasurement, 0, len(channelItems))
	for _, item := range channelItems {
		channel, err := parseChannel(item)
		if err != nil {
			return device.ParseResult{}, err
		}
		if channel != nil {
			channels = append(channels, *channel)
		}
	}

	return device.ParseResult{
		Serial:              serial,
		OccurredAt:          occurredAt,
		ChannelMeasurements: channels,
		BatteryPercent:      &battery,
		SignalStrength:      &rssi,
		Response:            device.DefaultSuccessResponse(),
		Metadata: map[string]any{
			"secret":       req.Secret,
			"content_type": req.ContentType,
		},
	}, nil
}

func parseChannel(item any) (*device.ChannelMeasurement, error) {
	values, ok := item.([]any)
	if !ok || len(values) < 5 {
		return nil, fmt.Errorf("channel entry has invalid shape")
	}

	number, err := asInt(values[0])
	if err != nil {
		return nil, fmt.Errorf("read channel number: %w", err)
	}

	isBinary, err := asInt(values[1])
	if err != nil {
		return nil, fmt.Errorf("read channel kind: %w", err)
	}

	name, err := asString(values[2])
	if err != nil {
		return nil, fmt.Errorf("read channel name: %w", err)
	}

	if isBinary == 1 {
		state, err := asFloat32(values[4])
		if err != nil {
			return nil, fmt.Errorf("read binary state for channel %d: %w", number, err)
		}
		return &device.ChannelMeasurement{
			Number: number,
			Name:   name,
			Unit:   "",
			Value:  state,
		}, nil
	}

	measurement, err := parseHexFloat(values[3])
	if err != nil {
		return nil, fmt.Errorf("read analog value for channel %d: %w", number, err)
	}

	unit, err := asString(values[4])
	if err != nil {
		return nil, fmt.Errorf("read unit for channel %d: %w", number, err)
	}

	return &device.ChannelMeasurement{
		Number: number,
		Name:   name,
		Unit:   unit,
		Value:  measurement,
	}, nil
}

func parseDeviceTime(value any) (time.Time, error) {
	raw, err := asString(value)
	if err != nil {
		return time.Time{}, err
	}

	seconds, err := strconv.ParseInt(raw, 16, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse hex seconds: %w", err)
	}

	return time.Unix(secondsSince2000ToUnix+seconds, 0).UTC(), nil
}

func parseHexFloat(value any) (float32, error) {
	raw, err := asString(value)
	if err != nil {
		return 0, err
	}
	raw = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "0x")
	if strings.HasPrefix(raw, "ff8100") {
		return 0, fmt.Errorf("device reported measurement error %s", raw)
	}

	bytesValue, err := hex.DecodeString(raw)
	if err != nil {
		return 0, fmt.Errorf("decode hex float: %w", err)
	}
	if len(bytesValue) != 4 {
		return 0, fmt.Errorf("expected 4 bytes, got %d", len(bytesValue))
	}

	bits := uint32(bytesValue[0])<<24 |
		uint32(bytesValue[1])<<16 |
		uint32(bytesValue[2])<<8 |
		uint32(bytesValue[3])
	return math.Float32frombits(bits), nil
}

func asString(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case float64:
		return strconv.FormatInt(int64(typed), 10), nil
	default:
		return "", fmt.Errorf("expected string-compatible value, got %T", value)
	}
}

func asInt(value any) (int, error) {
	switch typed := value.(type) {
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	default:
		return 0, fmt.Errorf("expected number, got %T", value)
	}
}

func asFloat32(value any) (float32, error) {
	switch typed := value.(type) {
	case float64:
		return float32(typed), nil
	case int:
		return float32(typed), nil
	default:
		return 0, fmt.Errorf("expected numeric value, got %T", value)
	}
}
