package publish

import (
	"device-handler/internal/telemetry"
	"device-handler/internal/telemetrypb"
)

func toProtoBatch(batch telemetry.Batch) *telemetrypb.Batch {
	protoBatch := &telemetrypb.Batch{
		TransmissionId:    append([]byte(nil), batch.TransmissionID...),
		DeploymentSamples: make([]*telemetrypb.DeploymentSamples, 0, len(batch.DeploymentSamples)),
	}
	if batch.SourceDeploymentID != nil {
		value := *batch.SourceDeploymentID
		protoBatch.SourceDeploymentId = &value
	}

	for _, sample := range batch.DeploymentSamples {
		protoBatch.DeploymentSamples = append(protoBatch.DeploymentSamples, toProtoDeploymentSamples(sample))
	}
	return protoBatch
}

func toProtoDeploymentSamples(sample telemetry.DeploymentSamples) *telemetrypb.DeploymentSamples {
	protoSample := &telemetrypb.DeploymentSamples{
		DeploymentId:  sample.DeploymentID,
		T0:            sample.T0,
		Dt:            append([]uint32(nil), sample.DT...),
		ChannelSeries: make([]*telemetrypb.ChannelSeries, 0, len(sample.ChannelSeries)),
		BatterySeries: make([]*telemetrypb.BatterySeries, 0, len(sample.BatterySeries)),
		SignalSeries:  make([]*telemetrypb.SignalSeries, 0, len(sample.SignalSeries)),
	}
	if sample.DeviceID != nil {
		value := *sample.DeviceID
		protoSample.DeviceId = &value
	}
	if sample.GatewayDeploymentID != nil {
		value := *sample.GatewayDeploymentID
		protoSample.GatewayDeploymentId = &value
	}

	for _, series := range sample.ChannelSeries {
		protoSample.ChannelSeries = append(protoSample.ChannelSeries, &telemetrypb.ChannelSeries{
			ChannelId: series.ChannelID,
			Values:    append([]float32(nil), series.Values...),
		})
	}
	for _, series := range sample.BatterySeries {
		protoSample.BatterySeries = append(protoSample.BatterySeries, &telemetrypb.BatterySeries{
			BatteryType: telemetrypb.BatteryType(series.BatteryType),
			Values:      append([]float32(nil), series.Values...),
		})
	}
	for _, series := range sample.SignalSeries {
		protoSample.SignalSeries = append(protoSample.SignalSeries, &telemetrypb.SignalSeries{
			SignalType: telemetrypb.SignalType(series.SignalType),
			Values:     append([]float32(nil), series.Values...),
		})
	}

	return protoSample
}
