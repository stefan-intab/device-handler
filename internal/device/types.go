package device

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"device-handler/internal/cache"
	"device-handler/internal/telemetry"
)

var ErrUnsupportedDevice = errors.New("unsupported manufacturer/model")
var ErrDeviceNotFound = errors.New("device not found in cache")

type Parser interface {
	Manufacturer() string
	Model() string
	Parse(context.Context, Request) (ParseResult, error)
}

type Request struct {
	Manufacturer string
	Model        string
	Secret       string
	Body         []byte
	ContentType  string
	ReceivedAt   time.Time
}

type ParseResult struct {
	Serial              string
	OccurredAt          time.Time
	ChannelMeasurements []ChannelMeasurement
	BatteryPercent      *float32
	SignalStrength      *float32
	Response            Response
	Metadata            map[string]any
}

type ChannelMeasurement struct {
	Number int
	Name   string
	Unit   string
	Value  float32
}

type Response struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

func DefaultSuccessResponse() Response {
	return Response{
		StatusCode:  200,
		ContentType: "text/plain; charset=utf-8",
		Body:        []byte("OK"),
	}
}

func BuildTelemetry(record cache.DeviceRecord, parsed ParseResult) (telemetry.DeploymentSamples, error) {
	timestamp := parsed.OccurredAt.UTC().Unix()
	sample := telemetry.DeploymentSamples{
		DeploymentID:        record.DeploymentID,
		DeviceID:            &record.DeviceID,
		GatewayDeploymentID: record.GatewayDeploymentID,
		T0:                  uint64(timestamp),
		DT:                  []uint32{0},
	}

	for _, measurement := range parsed.ChannelMeasurements {
		channelID, err := cache.ResolveChannelIDByTag(record, measurement.Name)
		if err != nil {
			return telemetry.DeploymentSamples{}, fmt.Errorf("resolve channel %d (%s): %w", measurement.Number, measurement.Name, err)
		}
		sample.ChannelSeries = append(sample.ChannelSeries, telemetry.ChannelSeries{
			ChannelID: channelID,
			Values:    []float32{measurement.Value},
		})
	}

	if parsed.BatteryPercent != nil {
		sample.BatterySeries = append(sample.BatterySeries, telemetry.BatterySeries{
			BatteryType: telemetry.BatteryTypePercent,
			Values:      []float32{*parsed.BatteryPercent},
		})
	}

	if parsed.SignalStrength != nil {
		sample.SignalSeries = append(sample.SignalSeries, telemetry.SignalSeries{
			SignalType: telemetry.SignalTypeComet4G,
			Values:     []float32{*parsed.SignalStrength},
		})
	}

	return sample, nil
}

func Key(manufacturer, model string) string {
	return strings.ToLower(strings.TrimSpace(manufacturer)) + "|" + strings.ToLower(strings.TrimSpace(model))
}
