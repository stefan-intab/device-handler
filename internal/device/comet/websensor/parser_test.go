package websensor

import (
	"context"
	"os"
	"testing"
	"time"

	"device-handler/internal/device"
)

func TestParseTA3610(t *testing.T) {
	result := parseFixture(t, "../../../../docs/payloads/comet/TAx6xx_PAx6xx/TA3610.json", "TA3610")

	if result.Serial != "26680011" {
		t.Fatalf("serial = %q, want %q", result.Serial, "26680011")
	}
	if len(result.ChannelMeasurements) != 3 {
		t.Fatalf("channel count = %d, want 3", len(result.ChannelMeasurements))
	}
	if result.ChannelMeasurements[0].Name != "Temperature" {
		t.Fatalf("first channel name = %q, want %q", result.ChannelMeasurements[0].Name, "Temperature")
	}
}

func TestParseTA0610(t *testing.T) {
	result := parseFixture(t, "../../../../docs/payloads/comet/TAx6xx_PAx6xx/TA0610.json", "TA0610")

	if len(result.ChannelMeasurements) != 1 {
		t.Fatalf("channel count = %d, want 1", len(result.ChannelMeasurements))
	}
	if result.ChannelMeasurements[0].Name != "Temperature" {
		t.Fatalf("channel name = %q, want %q", result.ChannelMeasurements[0].Name, "Temperature")
	}
}

func TestParsePA8652(t *testing.T) {
	result := parseFixture(t, "../../../../docs/payloads/comet/TAx6xx_PAx6xx/PA8652.json", "PA8652")

	if len(result.ChannelMeasurements) != 5 {
		t.Fatalf("channel count = %d, want 5", len(result.ChannelMeasurements))
	}
	if result.ChannelMeasurements[3].Name != "Binary input 1" {
		t.Fatalf("fourth parsed channel name = %q, want %q", result.ChannelMeasurements[3].Name, "Binary input 1")
	}
	if result.ChannelMeasurements[3].Value != 0 {
		t.Fatalf("binary channel value = %v, want 0", result.ChannelMeasurements[3].Value)
	}
}

func parseFixture(t *testing.T, path, model string) device.ParseResult {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	result, err := New(model).Parse(context.Background(), device.Request{
		Manufacturer: "comet",
		Model:        model,
		Secret:       "test-secret",
		Body:         body,
		ContentType:  "application/json",
		ReceivedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}

	return result
}
