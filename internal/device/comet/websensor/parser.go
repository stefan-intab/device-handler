package websensor

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"device-handler/internal/device"
)

type Parser struct {
	model string
}

func New(model string) *Parser {
	return &Parser{model: model}
}

func SupportedModels() []string {
	return []string{
		"TA3610",
		"TA3611",
		"TA7610",
		"TA4611",
		"TA0610",
		"TA7611",
		"TA5640",
		"TA4621",
		"TA3621",
		"TA3645",
		"TA7640",
		"PA8652",
		"PA8611",
		"PA8641",
		"PA8610",
	}
}

func (p *Parser) Manufacturer() string { return "comet" }
func (p *Parser) Model() string        { return p.model }

func (p *Parser) Parse(_ context.Context, req device.Request) (device.ParseResult, error) {
	body := bytes.TrimPrefix(req.Body, []byte("\xef\xbb\xbf"))

	var payload message
	if err := json.Unmarshal(body, &payload); err != nil {
		return device.ParseResult{}, fmt.Errorf("decode comet websensor payload: %w", err)
	}

	if payload.JsonType != 5 {
		return device.ParseResult{}, fmt.Errorf("unexpected JsonType %d", payload.JsonType)
	}
	if strings.TrimSpace(payload.Sn) == "" {
		return device.ParseResult{}, fmt.Errorf("missing serial number")
	}

	occurredAt, err := parseTimestamp(payload.Time.Sample)
	if err != nil {
		return device.ParseResult{}, fmt.Errorf("read sample time: %w", err)
	}

	channels := make([]device.ChannelMeasurement, 0, len(payload.Channels))
	for _, channel := range payload.Channels {
		measurement, ok, err := parseChannel(channel)
		if err != nil {
			return device.ParseResult{}, err
		}
		if ok {
			channels = append(channels, measurement)
		}
	}

	return device.ParseResult{
		Serial:              payload.Sn,
		OccurredAt:          occurredAt,
		ChannelMeasurements: channels,
		Response:            device.DefaultSuccessResponse(),
		Metadata: map[string]any{
			"secret":       req.Secret,
			"content_type": req.ContentType,
			"json_type":    payload.JsonType,
			"json_version": payload.JsonVersion,
			"kind":         payload.Kind,
			"msg_type":     payload.MsgType,
			"msg_cache":    payload.MsgCache,
			"conf_id":      payload.ConfID,
		},
	}, nil
}

type message struct {
	JsonType    int              `json:"JsonType"`
	JsonVersion int              `json:"JsonVersion"`
	OrderID     int              `json:"OrderId"`
	MsgType     int              `json:"MsgType"`
	MsgCache    int              `json:"MsgCache"`
	Sn          string           `json:"Sn"`
	Desc        string           `json:"Desc"`
	Kind        int              `json:"Kind"`
	NConf       int              `json:"NConf"`
	ConfID      string           `json:"ConfID"`
	Interval    int              `json:"Interval"`
	Time        messageTime      `json:"Time"`
	Channels    []messageChannel `json:"Channels"`
}

type messageTime struct {
	Now     string `json:"Now"`
	Sample  string `json:"Sample"`
	IsValid int    `json:"IsValid"`
}

type messageChannel struct {
	Number    int      `json:"Nr"`
	Enabled   *int     `json:"En,omitempty"`
	Quantity  string   `json:"Quant"`
	Value     string   `json:"Val"`
	ValueText string   `json:"ValStr"`
	Unit      string   `json:"Unit"`
	Decimals  int      `json:"Dec"`
	Type      []int    `json:"Type"`
	Alarm     []int    `json:"Alarm"`
	AlarmMode []int    `json:"AlarmMode"`
	BinDesc   []string `json:"BinDesc,omitempty"`
}

func parseChannel(channel messageChannel) (device.ChannelMeasurement, bool, error) {
	quantity := strings.TrimSpace(channel.Quantity)
	if quantity == "" {
		return device.ChannelMeasurement{}, false, nil
	}
	if channel.Enabled != nil && *channel.Enabled == 0 {
		return device.ChannelMeasurement{}, false, nil
	}

	value, err := parseHexFloat(channel.Value)
	if err != nil {
		if isMeasurementError(channel.Value) {
			return device.ChannelMeasurement{}, false, nil
		}
		return device.ChannelMeasurement{}, false, fmt.Errorf("read value for channel %d (%s): %w", channel.Number, quantity, err)
	}

	return device.ChannelMeasurement{
		Number: channel.Number,
		Name:   quantity,
		Unit:   strings.TrimSpace(channel.Unit),
		Value:  value,
	}, true, nil
}

func parseTimestamp(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func parseHexFloat(value string) (float32, error) {
	raw := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
	if isMeasurementError(raw) {
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

func isMeasurementError(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "ff8100")
}
