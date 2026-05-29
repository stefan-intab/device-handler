# Telemetry Protobuf Schema Reference

This document describes the `telemetry.v1` protobuf schema used for internal telemetry publishing.

The schema is now centered on one idea:

> Telemetry is written for a device deployment, and values inside that deployment are identified by `channel_id`.

That matches the current service model, where channels belong to a `device_deployment`, not directly to a separate logger/gateway hierarchy.

## Current Schema

```proto
syntax = "proto3";

option go_package = "go-nats-publisher/gen/telemetry/v1;telemetryv1";
package telemetry.v1;

enum BatteryType {
  BATTERY_TYPE_UNSPECIFIED = 0;
  BATTERY_TYPE_VOLTAGE = 1;
  BATTERY_TYPE_PERCENT = 2;
}

enum SignalType {
  SIGNAL_TYPE_UNSPECIFIED = 0;
  SIGNAL_TYPE_WIRELESS_VALUE_2G = 1;
  SIGNAL_TYPE_WIRELESS_VALUE_4G = 2;
  SIGNAL_TYPE_WIRELESS_VALUE_868 = 3;
  SIGNAL_TYPE_COMET_2G = 4;
  SIGNAL_TYPE_COMET_4G = 5;
  SIGNAL_TYPE_NB_IOT = 6;
  SIGNAL_TYPE_CAT_LTE = 7;
}

message BatterySeries {
  BatteryType battery_type = 1;
  repeated float values = 2 [packed=true];
}

message SignalSeries {
  SignalType signal_type = 1;
  repeated float values = 2 [packed=true];
}

message ChannelSeries {
  uint64 channel_id = 1;
  repeated float values = 2 [packed=true];
}

message DeploymentSamples {
  uint64 deployment_id = 1;
  optional uint64 device_id = 2;
  optional uint64 gateway_deployment_id = 3;
  uint64 t0 = 4;
  repeated uint32 dt = 5 [packed=true];

  repeated ChannelSeries channel_series = 6;
  repeated BatterySeries battery_series = 7;
  repeated SignalSeries signal_series = 8;
}

message Batch {
  bytes transmission_id = 1;
  optional uint64 source_deployment_id = 2;
  repeated DeploymentSamples deployment_samples = 3;
}
```

## Why This Structure

The old model separated `Logger` and `Gateway`. That no longer reflects the current domain very well because:

- devices are modeled more uniformly
- channels are attached to `deployment_id`
- a deployment is the real write target for telemetry
- gateway relationships are contextual, not a separate payload type

In the current backend model:

- deployment identity lives on `device_deployments.id`
- a deployment may optionally point to `gateway_deployment_id`
- channels belong to `channels.deployment_id`

So the protobuf is now aligned around:

- `deployment_id` as the primary telemetry owner
- `channel_id` as the primary measurement series key
- optional gateway context only when relevant

## Message Hierarchy

```text
Batch
├── transmission_id
├── source_deployment_id (optional)
└── deployment_samples[]
    └── DeploymentSamples
        ├── deployment_id
        ├── device_id (optional)
        ├── gateway_deployment_id (optional)
        ├── t0
        ├── dt[]
        ├── channel_series[]
        ├── battery_series[]
        └── signal_series[]
```

## Core Semantics

### `Batch`

`Batch` is the top-level telemetry envelope.

It groups one or more deployment payloads into one transmission.

Fields:

| Field | Type | Description |
|---|---|---|
| `transmission_id` | `bytes` | Unique ID for deduplication, tracing, and replay analysis. |
| `source_deployment_id` | `optional uint64` | Deployment that produced or forwarded the batch, if that context matters. |
| `deployment_samples` | `repeated DeploymentSamples` | One or more deployment telemetry payloads. |

Recommended use:

- device-originated packet: `source_deployment_id == deployment_id`
- gateway-forwarded packet: `source_deployment_id` is the gateway deployment and `deployment_samples[]` may contain child deployments

### `DeploymentSamples`

`DeploymentSamples` is the main telemetry payload.

It represents one deployed device context with one shared time axis:

```text
timestamp[index] = t0 + dt[index]
```

All channel, battery, and signal series inside the same deployment align to that time axis.

Fields:

| Field | Type | Description |
|---|---|---|
| `deployment_id` | `uint64` | Primary telemetry owner. This is the most important identity field. |
| `device_id` | `optional uint64` | Optional secondary identity for debugging, validation, or cross-checking. |
| `gateway_deployment_id` | `optional uint64` | Optional parent/uplink deployment when this deployment is attached to a gateway. |
| `t0` | `uint64` | Base Unix timestamp in seconds. |
| `dt` | `repeated uint32` | Sample offsets in seconds from `t0`. |
| `channel_series` | `repeated ChannelSeries` | Main measurement series keyed by `channel_id`. |
| `battery_series` | `repeated BatterySeries` | Optional battery metrics aligned with `dt`. |
| `signal_series` | `repeated SignalSeries` | Optional signal metrics aligned with `dt`. |

### `ChannelSeries`

`ChannelSeries` contains all values for one channel in one deployment payload.

Fields:

| Field | Type | Description |
|---|---|---|
| `channel_id` | `uint64` | Channel identifier. |
| `values` | `repeated float` | Values aligned by index with `DeploymentSamples.dt`. |

Example:

```text
t0 = 1710000000
dt = [0, 60, 120]

channel_id = 9001
values = [21.1, 21.2, 21.3]
```

Interpreted as:

| Timestamp | Channel ID | Value |
|---:|---:|---:|
| `1710000000` | `9001` | `21.1` |
| `1710000060` | `9001` | `21.2` |
| `1710000120` | `9001` | `21.3` |

### `BatterySeries`

Optional battery data for the deployment payload.

Fields:

| Field | Type | Description |
|---|---|---|
| `battery_type` | `BatteryType` | Battery metric type. |
| `values` | `repeated float` | Values aligned with `dt`. |

### `SignalSeries`

Optional signal data for the deployment payload.

Fields:

| Field | Type | Description |
|---|---|---|
| `signal_type` | `SignalType` | Radio/network/signal type. |
| `values` | `repeated float` | Values aligned with `dt`. |

## Why `uint64` IDs

The schema uses `uint64` for `deployment_id`, `device_id`, and `channel_id` because the backend uses `BigInteger` IDs.

Using `uint32` would create avoidable long-term risk if IDs grow.

## Presence and Wire Size

`device_id`, `gateway_deployment_id`, and `source_deployment_id` are marked `optional`.

This gives you explicit presence tracking:

- if omitted, they cost `0 bytes` on the wire
- if present, they are encoded normally
- if absent, the receiver can distinguish that from a field intentionally set by the sender

Typical impact when present:

- tag: usually `1 byte`
- integer payload: usually `1-5 bytes` for normal ID sizes

So an optional ID field that is set usually costs around `2-6 bytes`.

## Validation Rules

The receiver should enforce application rules that protobuf does not.

Recommended validation:

### Batch

```text
transmission_id must be non-empty
deployment_samples should not be empty
```

If `source_deployment_id` is set:

```text
source_deployment_id should reference a known active deployment
```

### DeploymentSamples

```text
deployment_id should be non-zero
t0 should be non-zero for real telemetry
dt should be non-empty when any series are present
dt should be sorted ascending
dt[0] should usually be 0
channel_series should normally be non-empty for measurement payloads
duplicate channel_id values should normally be rejected
```

Alignment rules:

```text
for each ChannelSeries:
    len(values) == len(dt)

for each BatterySeries:
    len(values) == len(dt)

for each SignalSeries:
    len(values) == len(dt)
```

## Example Payload as JSON Mapping

Example JSON-style representation:

```json
{
  "transmissionId": "base64-encoded-16-byte-id",
  "sourceDeploymentId": "410",
  "deploymentSamples": [
    {
      "deploymentId": "410",
      "deviceId": "205",
      "t0": "1710000000",
      "dt": [0, 60, 120],
      "channelSeries": [
        {
          "channelId": "9001",
          "values": [21.1, 21.2, 21.3]
        },
        {
          "channelId": "9002",
          "values": [44.0, 44.1, 44.2]
        }
      ],
      "batterySeries": [
        {
          "batteryType": "BATTERY_TYPE_PERCENT",
          "values": [98.0, 98.0, 97.0]
        }
      ],
      "signalSeries": [
        {
          "signalType": "SIGNAL_TYPE_NB_IOT",
          "values": [-93.0, -92.0, -91.0]
        }
      ]
    }
  ]
}
```

## Practical Guidance

Use this schema when:

- one deployment owns the samples
- channels are the real series identifiers
- gateway relationships are contextual metadata, not a separate payload type

Do not model the payload around old concepts like `logger_batch` and `gateway_batch` unless the runtime domain truly goes back to that split.

## Summary

The most important design choice in this schema is:

> `deployment_id` is the write target, and `channel_id` identifies the measurements inside that deployment.

That keeps the wire model aligned with the current service and database model while still staying compact and efficient for batched telemetry.
