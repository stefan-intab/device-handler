package soaptx

import (
	"context"
	"os"
	"testing"
	"time"

	"device-handler/internal/device"
)

func TestParsePressureSchema(t *testing.T) {
	result := parseFixture(t, "../../../../docs/payloads/comet/Tx6xx_SOAP/soapTx5xx_v2.xml", "T7511")

	if result.Serial != "17965562" {
		t.Fatalf("serial = %q, want %q", result.Serial, "17965562")
	}
	if len(result.ChannelMeasurements) != 4 {
		t.Fatalf("channel count = %d, want 4", len(result.ChannelMeasurements))
	}
	if result.ChannelMeasurements[2].Name != "Dew point" {
		t.Fatalf("computed channel name = %q, want %q", result.ChannelMeasurements[2].Name, "Dew point")
	}
	if result.ChannelMeasurements[3].Name != "Pressure" {
		t.Fatalf("fourth channel name = %q, want %q", result.ChannelMeasurements[3].Name, "Pressure")
	}
}

func TestParseCO2Schema(t *testing.T) {
	result := parseFixture(t, "../../../../docs/payloads/comet/Tx6xx_SOAP/soapTx5xxCO2.xml", "T6540")

	if result.Serial != "16963997" {
		t.Fatalf("serial = %q, want %q", result.Serial, "16963997")
	}
	if len(result.ChannelMeasurements) != 4 {
		t.Fatalf("channel count = %d, want 4", len(result.ChannelMeasurements))
	}
	if result.ChannelMeasurements[3].Name != "CO2" {
		t.Fatalf("fourth channel name = %q, want %q", result.ChannelMeasurements[3].Name, "CO2")
	}
}

func parseFixture(t *testing.T, path, model string) device.ParseResult {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	receivedAt := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	result, err := New(model).Parse(context.Background(), device.Request{
		Manufacturer: "comet",
		Model:        model,
		Secret:       "test-secret",
		Body:         body,
		ContentType:  "text/xml; charset=utf-8",
		ReceivedAt:   receivedAt,
	})
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}

	if !result.OccurredAt.Equal(receivedAt) {
		t.Fatalf("occurred_at = %s, want %s", result.OccurredAt, receivedAt)
	}

	return result
}
