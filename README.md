# Device Handler

This repository contains a Go-based device handler service that:

- listens for device uploads on `POST /devices?manufacturer=...&model=...&secret=...`
- selects a parser by `manufacturer` + `model`
- resolves the device from a local cache populated from an authenticated REST API
- batches parsed telemetry for downstream publish on `channel.data.device-handler`

The first implemented parser is `comet/UxxxxM`, based on the positional JSON payload documented in [docs/payloads/comet/ULoggerJsonDoc.txt](/home/stefan/git/device-handler/docs/payloads/comet/ULoggerJsonDoc.txt:1).

## Current Shape

- `cmd/device-handler`: service entrypoint
- `internal/api`: JWT-authenticated REST client
- `internal/cache`: in-memory device cache and device update store
- `internal/device`: parser registry and telemetry mapping
- `internal/device/comet/ulogger`: first concrete parser
- `internal/ingest`: HTTP server and orchestration service
- `internal/publish`: buffered publisher abstraction
- `internal/telemetry`: internal model aligned to `docs/protobuf.md`

## Environment

Required:

```text
API_BASE_URL=http://127.0.0.1:8082/api/v1
API_USERNAME=device-handler
API_PASSWORD=secret
SERVICE_NAME=device_handler
```

Optional:

```text
HTTP_ADDRESS=:8080
API_LOGIN_PATH=/auth/token/
API_INTERNAL_DEVICES_PATH=/devices/internal/
API_INTERNAL_UPDATED_PATH=/devices/internal/updated/
API_OUTDATED_PROPERTIES_PATH=/devices/internal/properties/outdated/
NATS_URL=nats://127.0.0.1:4222
API_REQUEST_TIMEOUT=10s
API_JWT_LEEWAY=30s
API_FALLBACK_TOKEN_TTL=10m
SYNC_POLL_INTERVAL=15s
PUBLISH_SUBJECT=channels.data.device-handler
PUBLISH_FLUSH_AFTER=1s
PUBLISH_FLUSH_COUNT=50
PUBLISH_TIMEOUT=5s
LOG_LEVEL=INFO
```

## Run

```bash
go run ./cmd/device-handler
```

## Example Request

```bash
curl -X POST \
  'http://127.0.0.1:8080/devices?manufacturer=comet&model=UxxxxM&secret=xxxxx' \
  -H 'Content-Type: application/json' \
  --data-binary @docs/payloads/comet/comet_UxxxxM.json
```

## Simulator

You can also simulate a Comet upload with the helper script:

```bash
python3 scripts/simulate_comet_uxxxxm.py
```

Useful variants:

```bash
python3 scripts/simulate_comet_uxxxxm.py --dry-run
python3 scripts/simulate_comet_uxxxxm.py --serial 17270001 --temperature 23.4 --humidity 51
python3 scripts/simulate_comet_uxxxxm.py --count 0 --interval 15 --jitter 0.3
```

## Notes

- The publisher now uses NATS JetStream and sends the payload as protobuf using the `telemetry.v1.Batch` schema.
- Channel-to-`channel_id` resolution depends on `channels[].tag` being available from the device metadata API. For `comet/UxxxxM`, the payload `Quantity` such as `Temperature` is normalized to lowercase and matched against the channel `tag`.
- The `secret` query parameter is currently logged for traceability because validation is not implemented yet.
