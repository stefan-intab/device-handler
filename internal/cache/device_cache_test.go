package cache

import (
	"encoding/json"
	"testing"
)

func TestDeviceRecordUnmarshalAndResolveChannelByTag(t *testing.T) {
	input := []byte(`{
		"id": 2000,
		"device_id": 1000,
		"serial_number": "GW-01000",
		"manufacturer": "Intab",
		"model": "Nordic Gateway 4G",
		"sync_updated_at": "2026-05-28T06:55:38.434685Z",
		"channels": [
			{
				"id": 3037,
				"tag": "temperature",
				"name": "Temperature"
			}
		]
	}`)

	var record DeviceRecord
	if err := json.Unmarshal(input, &record); err != nil {
		t.Fatalf("unmarshal device record: %v", err)
	}

	if record.DeploymentID != 2000 {
		t.Fatalf("deployment_id = %d, want 2000", record.DeploymentID)
	}
	if record.Serial != "GW-01000" {
		t.Fatalf("serial = %q, want %q", record.Serial, "GW-01000")
	}
	if len(record.ChannelMappings) != 1 {
		t.Fatalf("channel mapping count = %d, want 1", len(record.ChannelMappings))
	}

	channelID, err := ResolveChannelIDByTag(record, "Temperature")
	if err != nil {
		t.Fatalf("resolve channel by tag: %v", err)
	}
	if channelID != 3037 {
		t.Fatalf("channel id = %d, want 3037", channelID)
	}
}
