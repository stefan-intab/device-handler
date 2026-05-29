# JetStream Streams and Consumers — Documentation Review and Recommendations

## Purpose

This document provides recommended wording and structure for documenting NATS JetStream streams, consumers, durable consumers, client subscriptions, and retention modes.

The main clarification is the difference between:

- **Stream**: the server-side log of stored messages.
- **JetStream consumer**: the server-side delivery state and acknowledgement tracker.
- **Client subscription / service instance**: the running application process that connects to NATS and consumes messages.

This distinction is important because the word **consumer** is often used casually to mean both the JetStream server-side object and the application that reads messages. In the documentation, these should be separated clearly.

---

# Recommended Terminology

## Stream

A stream is the server-side log of messages.

It stores messages that match one or more subjects and applies configuration such as:

- retention policy
- storage type
- max age
- max bytes
- max messages
- replicas

Example:

```text
Stream: channel_data_stream
Subjects:
- channel.data.mqtt
- channel.data.legacy-bridge
- channel.data.device-handler
- channel.data.sdg-bridge
```

A stream does **not** have a durable name. Durable state belongs to consumers, not streams.

Think:

```text
Stream = stored messages
Consumer = delivery state and ACK tracking
Client subscription = running service connected to a consumer
```

---

## JetStream Consumer

A JetStream consumer is a server-side object that defines how messages are delivered from a stream.

It controls:

- which messages are visible, using filter subjects
- where delivery starts, using deliver policy
- how acknowledgements work
- how long the server waits for an ACK
- how many times a message may be redelivered
- whether delivery is push or pull
- backpressure, using `max_ack_pending`

A consumer also tracks delivery state: which messages have been delivered and which messages have been acknowledged.

Example durable consumer:

```json
{
  "durable_name": "data-writer",
  "ack_policy": "explicit",
  "max_deliver": 10,
  "backoff": [
    10000000000,
    30000000000,
    300000000000,
    1800000000000,
    7200000000000
  ],
  "filter_subject": "channel.data.*",
  "max_ack_pending": 2000,
  "replay_policy": "instant",
  "deliver_policy": "all"
}
```

Example ephemeral consumer:

```json
{
  "ack_policy": "explicit",
  "max_deliver": 10,
  "backoff": [
    10000000000,
    30000000000,
    300000000000,
    1800000000000,
    7200000000000
  ],
  "filter_subject": "channel.data.*",
  "max_ack_pending": 2000,
  "replay_policy": "instant",
  "deliver_policy": "all"
}
```

The important difference is that the durable consumer has a `durable_name`.

---

## Client Subscription / Service Instance

A client subscription is the running application code that connects to NATS and consumes messages.

For example:

```text
Data Writer service process
        |
        | binds to
        v
JetStream durable consumer: data-writer
        |
        | reads from
        v
Stream: channel_data_stream
```

The client process may stop, restart, or be redeployed. If it binds to the same durable consumer, JetStream remembers the consumer’s delivery position.

This means:

- the stream stores the messages
- the durable consumer stores the progress
- the service instance does the actual processing

Recommended wording:

> A JetStream consumer is the server-side delivery object. A client subscription is the connection from a running service to that consumer.  
> For example, `data-writer` may be a durable consumer, while the Data Writer service is the application process that binds to it and fetches messages.

---

## Durable vs Ephemeral Consumers

“Durable” refers to whether the consumer’s delivery state is persisted on the server.

A durable consumer keeps its position even if the client disconnects or restarts. This is what production services should normally use.

An ephemeral consumer is temporary. It does not have a durable name and is normally deleted automatically when no longer in use.

| Property | Durable consumer | Ephemeral consumer |
|---|---:|---:|
| Has `durable_name` | Yes | No |
| Delivery state persisted on server | Yes | Temporary |
| Survives client restart | Yes | No / not normally |
| Good for production microservices | Yes | No |
| Good for debugging / temporary reads | Sometimes | Yes |

Use durable consumers for services such as:

- Data Writer
- Alarm Evaluator
- Webhook Sender

Use ephemeral consumers for:

- temporary debugging
- ad-hoc inspection
- short-lived replay tests

---

### Ephemeral consumers for local caches

Some services maintain a local in-memory cache of rarely-changing state, such as device configuration or device metadata.

For these services, an ephemeral consumer may be used if the service can rebuild its full state from an authoritative HTTP endpoint.

Startup flow:

1. Start consuming update events, buffering them temporarily.
2. Fetch the current full state from the HTTP API.
3. Apply buffered events that are newer than the fetched snapshot.
4. Continue applying live update events.

This pattern is suitable when the JetStream events are used only to keep a disposable cache fresh.

Ephemeral consumers should not be used for workflows where every event must be processed reliably, such as database writes, alarms, webhooks, notifications, or audit logs. Those services should use durable consumers with explicit ACK.

To reduce the risk of missed updates, cached state should include a version, revision, or updated timestamp, and the service should periodically reconcile with the HTTP source of truth.

```json
{
  "type": "logger_config_changed",
  "logger_id": 12367,
  "revision": 43
}
```

Notify other handlers that config has been applied. 

----

# Retention Modes

## Limits Retention

Limits retention keeps messages until configured limits are reached, such as:

- max age
- max bytes
- max messages

Multiple independent consumers can read the same messages and maintain independent ACK state.

This is usually the best option when several services need to process the same telemetry messages independently.

Example:

```text
channel.data.mqtt message
        |
        v
channel_data_stream
        |
        +--> data-writer consumer
        +--> alarm-evaluator consumer
        +--> webhook-sender consumer
```

Each consumer has its own progress and ACK state.

---

## Interest Retention

Interest retention keeps messages while there is consumer interest.

Messages can be removed once all interested consumers have acknowledged them.

This can reduce storage usage, but it is less suitable if you want a predictable replay or backfill window.

Use Interest retention when:

- you only care about current consumers
- you do not need to keep a fixed history
- you want messages removed once all active consumers have processed them

---

## WorkQueue Retention

WorkQueue retention makes a stream behave more like a job queue.

A message is retained until it has been successfully acknowledged by a consumer. After it is acknowledged, it is eligible for removal and should not be treated as replayable history.

Use WorkQueue when each message should be processed once by one worker group.

Do **not** use WorkQueue when several independent services need to consume the same messages, for example:

- one service writes to the database
- one service evaluates alarms
- one service sends webhooks

For that kind of fan-out, use Limits retention instead.

---

# Quarantine Stream

## `quarantine.channel.data`

For consistency with the `channel.data.*` ingest pipeline, the recommended quarantine subject is:

```text
quarantine.channel.data
```

This should be configured as a separate JetStream stream used for operator inspection.

Startup expectation:

- the intended stream for this subject is `channel_data_quarantine_stream`
- `data-writer` should validate that the quarantine subject is backed by some JetStream stream
- `data-writer` does not require any consumer on the quarantine stream

Current implementation note:

- the bootstrap now provisions `channel_data_quarantine_stream`
- add producer-side startup validation when quarantine publishing is implemented

Recommended stream:

```text
Stream: channel_data_quarantine_stream
Subjects:
- quarantine.channel.data
Retention:
- Limits
```

Why `Limits` retention:

- quarantine events may need later inspection
- multiple tools may want to read the same bad-message record
- this is not a worker-queue use case

Recommended configuration direction:

- keep the stream dedicated to quarantine events instead of mixing it into `channel_data_stream`
- set `max_age`, `max_bytes`, or `max_messages` according to how long you want bad payload evidence available
- use file storage if you want quarantine records to survive broker restart
- add a durable consumer only if you have an auditing, alerting, or support workflow that reads these events

Quarantine event payload contents:

- original NATS subject
- first-value copy of the original message headers
- error string describing why the message was rejected
- SHA-256 hash of the original payload
- `occurred_at` timestamp in UTC

Important implication:

> If you later want to re-read the last 10 days of telemetry data for debugging, replay, or backfills, WorkQueue retention is usually not the right choice.

---

# Recommended Setup for Intabcloud Telemetry

For `channel_data_stream`, use:

- Retention: **Limits**
- Storage: **File**
- Replicas: **1 initially, 3 later for production HA**
- Max age: **10 days**
- ACK policy: **explicit**
- Consumer type: **durable pull consumers** for production services

Recommended durable consumers:

| Service | Durable consumer | Filter subject |
|---|---|---|
| Data Writer | `data-writer` | `channel.data.*` |
| Alarm Evaluator | `alarm-evaluator` | `channel.data.*` |
| Webhook Sender | `webhook-sender` | `channel.data.*` |

Each service gets its own durable consumer because each service needs independent delivery progress.

This means the same telemetry message can be processed by all three services, and each service tracks its own ACK state.

---

# Recommended Stream Definitions

## `channel_data_stream`

Subjects:

- `channel.data.mqtt`
- `channel.data.legacy-bridge`
- `channel.data.device-handler`
- `channel.data.sdg-bridge`

Description:

This stream stores telemetry messages from different ingestion sources.

Recommended configuration:

```text
Retention: Limits
Storage: File
Replicas: 1 now, 3 later for production HA
Max age: 10 days
Max size: TBD
```

Reason:

`channel_data_stream` is expected to have multiple independent consumers, for example:

- Data Writer
- Alarm Evaluator
- Webhook Sender

Each consumer may need to process the same message independently. Therefore WorkQueue retention is not appropriate for this stream.

With Limits retention, messages are retained until configured limits are reached. This allows replay and backfill within the configured retention window.

---

## `audit_stream`

Subjects:

- `audit.auth.login`
- `audit.auth.logout`
- `audit.auth.refresh`
- `audit.logger.channel.new`
- `audit.logger.channel.update`

Recommended configuration:

```text
Retention: Limits
Storage: File
Replicas: 1 now, 3 later for production HA
Max age: 10 days
Max size: 5 GB
```

Notes:

Audit data may be useful for troubleshooting and security review, so Limits retention with a clear max age and max size is a good default.

---

## `notification_stream`

Subjects:

- `notification.sms`
- `notification.email`
- `notification.report`

Recommended configuration:

```text
Retention: Limits
Storage: File
Replicas: 1 now, 3 later for production HA
Max age: 10 days
Max size: 5 GB
```

---

# Replicas

Replicas define the stream’s replication factor across JetStream cluster nodes.

## Replicas = 1

One copy of the stream data exists on one JetStream node.

This is acceptable for:

- development
- testing
- non-critical data
- early production where replay/availability is not mission-critical

If the node or disk fails, stream data and availability are at risk.

## Replicas = 3

Three copies are maintained on three different JetStream nodes.

This is recommended for production high availability.

To get real HA from `replicas = 3`, you need at least three independent JetStream servers, ideally running on separate machines or availability zones.

Replicas do **not** mean three disks on the same server. They mean three independent JetStream nodes that can fail independently.

Practical wording:

> `replicas = 1` is fine for development or non-critical deployments.  
> `replicas = 3` is recommended for production HA and allows the stream to tolerate one node failure.

---

# ACK and Delivery Notes

## `ack_wait`

`ack_wait` is specified in nanoseconds in the JSON API.

Example:

```json
"ack_wait": 60000000000
```

This means 60 seconds.

If a message is delivered and not acknowledged within `ack_wait`, it may be redelivered depending on the consumer configuration.

If a consumer defines `backoff`, JetStream derives the ACK wait from that retry schedule instead of a standalone `ack_wait` value.

## `backoff`

The current `js-init` consumer configs use stepped retry delays instead of immediate redelivery:

```json
"backoff": [
  10000000000,
  30000000000,
  300000000000,
  1800000000000,
  7200000000000
]
```

This means:

- 10 seconds
- 30 seconds
- 5 minutes
- 30 minutes
- 2 hours

With `max_deliver = 10`, JetStream keeps using the last delay for the remaining delivery attempts.

---

## `ack_policy`

Recommended for production services:

```json
"ack_policy": "explicit"
```

With explicit ACK, the service must acknowledge a message after it has processed it successfully.

This gives at-least-once delivery semantics.

Important implication:

> Services must be idempotent, because the same message may be delivered more than once if a service crashes, times out, or fails to ACK.

---

## `deliver_policy`

Example:

```json
"deliver_policy": "all"
```

This means the consumer starts from the beginning of available messages.

For production rollout, consider whether you want:

```text
all = start from the beginning of retained messages
new = only receive messages published after the consumer is created
```

Recommended guidance:

- Use `all` when creating a new service that must process existing retained data.
- Use `new` when deploying a service that should only process new messages from that point forward.
- Be explicit in deployment documentation to avoid accidental replay of old data.
