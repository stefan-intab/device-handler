package ulogger

import (
	"context"
	"os"
	"testing"
	"time"

	"device-handler/internal/device"
)

func TestParse(t *testing.T) {
	body, err := os.ReadFile("../../../../docs/payloads/comet/comet_UxxxxM.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	result, err := New().Parse(context.Background(), device.Request{
		Manufacturer: "comet",
		Model:        "UxxxxM",
		Secret:       "test-secret",
		Body:         body,
		ContentType:  "application/json",
		ReceivedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}

	if result.Serial != "17270001" {
		t.Fatalf("serial = %q, want %q", result.Serial, "17270001")
	}
	if len(result.ChannelMeasurements) != 3 {
		t.Fatalf("channel count = %d, want 3", len(result.ChannelMeasurements))
	}
	if result.ChannelMeasurements[0].Number != 1 {
		t.Fatalf("first channel number = %d, want 1", result.ChannelMeasurements[0].Number)
	}
	if result.Response.StatusCode != 200 {
		t.Fatalf("status code = %d, want 200", result.Response.StatusCode)
	}
}
