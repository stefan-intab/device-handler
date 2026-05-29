package telemetry

type BatteryType int32

const (
	BatteryTypeUnspecified BatteryType = 0
	BatteryTypeVoltage     BatteryType = 1
	BatteryTypePercent     BatteryType = 2
)

type SignalType int32

const (
	SignalTypeUnspecified      SignalType = 0
	SignalTypeWirelessValue2G  SignalType = 1
	SignalTypeWirelessValue4G  SignalType = 2
	SignalTypeWirelessValue868 SignalType = 3
	SignalTypeComet2G          SignalType = 4
	SignalTypeComet4G          SignalType = 5
	SignalTypeNBIoT            SignalType = 6
	SignalTypeCatLTE           SignalType = 7
)

type BatterySeries struct {
	BatteryType BatteryType `json:"battery_type"`
	Values      []float32   `json:"values"`
}

type SignalSeries struct {
	SignalType SignalType `json:"signal_type"`
	Values     []float32  `json:"values"`
}

type ChannelSeries struct {
	ChannelID uint64    `json:"channel_id"`
	Values    []float32 `json:"values"`
}

type DeploymentSamples struct {
	DeploymentID        uint64          `json:"deployment_id"`
	DeviceID            *uint64         `json:"device_id,omitempty"`
	GatewayDeploymentID *uint64         `json:"gateway_deployment_id,omitempty"`
	T0                  uint64          `json:"t0"`
	DT                  []uint32        `json:"dt"`
	ChannelSeries       []ChannelSeries `json:"channel_series,omitempty"`
	BatterySeries       []BatterySeries `json:"battery_series,omitempty"`
	SignalSeries        []SignalSeries  `json:"signal_series,omitempty"`
}

type Batch struct {
	TransmissionID     []byte              `json:"transmission_id"`
	SourceDeploymentID *uint64             `json:"source_deployment_id,omitempty"`
	DeploymentSamples  []DeploymentSamples `json:"deployment_samples"`
}
