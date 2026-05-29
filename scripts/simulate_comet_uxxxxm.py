#!/usr/bin/env python3
"""Simulate a Comet UxxxxM device upload against the local device handler."""

from __future__ import annotations

import argparse
import json
import random
import struct
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone


SECONDS_BETWEEN_1970_AND_2000 = 946684800


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Send simulated Comet UxxxxM payloads to the device-handler."
    )
    parser.add_argument(
        "--endpoint",
        default="http://127.0.0.1:8083/devices",
        help="Device-handler endpoint without query parameters.",
    )
    parser.add_argument("--manufacturer", default="comet")
    parser.add_argument("--model", default="UxxxxM")
    parser.add_argument("--secret", default="xxxxx")
    parser.add_argument("--serial", type=int, default=17270001)
    parser.add_argument("--description", default="Simulated Datalogger")
    parser.add_argument("--customer-uid", default="simulator")
    parser.add_argument("--temperature", type=float, default=21.5)
    parser.add_argument("--humidity", type=float, default=48.0)
    parser.add_argument("--binary-state", type=int, choices=(0, 1), default=0)
    parser.add_argument("--battery", type=int, default=99)
    parser.add_argument("--rssi", type=int, default=31)
    parser.add_argument("--interval-power", type=int, default=120)
    parser.add_argument("--interval-battery", type=int, default=300)
    parser.add_argument(
        "--count",
        type=int,
        default=1,
        help="Number of payloads to send. Use 0 for an endless loop.",
    )
    parser.add_argument(
        "--interval",
        type=float,
        default=5.0,
        help="Seconds between sends when count is not 1.",
    )
    parser.add_argument(
        "--jitter",
        type=float,
        default=0.0,
        help="Random +/- adjustment added to temperature and humidity each send.",
    )
    parser.add_argument("--timeout", type=float, default=10.0)
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print the generated JSON payload instead of posting it.",
    )
    return parser.parse_args()


def float_to_hex(value: float) -> str:
    # The Comet payload uses IEEE-754 float32 values encoded as big-endian hex.
    return struct.pack(">f", value).hex()


def comet_time_hex(now: datetime) -> str:
    seconds_since_2000 = int(now.timestamp()) - SECONDS_BETWEEN_1970_AND_2000
    return f"{seconds_since_2000:08X}"


def build_payload(args: argparse.Namespace, order_id: int) -> list[object]:
    now = datetime.now(timezone.utc)
    time_hex = comet_time_hex(now)

    temperature = args.temperature
    humidity = args.humidity
    if args.jitter:
        temperature += random.uniform(-args.jitter, args.jitter)
        humidity += random.uniform(-args.jitter, args.jitter)

    # The payload is positional, so we keep the structure aligned to the Comet
    # documentation rather than converting it into a friendlier object layout.
    return [
        1,  # JsonType
        6,  # JsonVersion
        args.rssi,
        time_hex,
        order_id % 256,
        0,  # IsAsync
        args.serial,
        args.description,
        1,  # Kind
        time_hex,
        0,  # Timezone
        args.interval_power,
        args.interval_battery,
        0,  # RstOk
        1,  # BufferedModeMaxMessagesCnt
        65535,  # AStateMask
        64,  # AState, external power present
        1,  # NConf
        0,  # AlarmsEvalOffDueTimeLimit
        args.customer_uid,
        args.battery,
        [
            [
                1,
                0,
                "Temperature",
                float_to_hex(temperature),
                "\u00b0C",
                1,
                [0, 0],
                [float_to_hex(0.0), float_to_hex(40.0)],
                [1, 2],
            ],
            [
                2,
                0,
                "Rel. humidity",
                float_to_hex(humidity),
                "%",
                0,
                [0, 0],
                [float_to_hex(30.0), float_to_hex(60.0)],
                [1, 2],
            ],
            [
                3,
                1,
                "Input 1",
                ["off", "on"],
                args.binary_state,
                [0, 0],
                [0, 0],
            ],
        ],
    ]


def build_url(args: argparse.Namespace) -> str:
    query = urllib.parse.urlencode(
        {
            "manufacturer": args.manufacturer,
            "model": args.model,
            "secret": args.secret,
        }
    )
    return f"{args.endpoint}?{query}"


def send_payload(url: str, payload: list[object], timeout: float) -> None:
    body = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(
        url,
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:
        response_body = response.read().decode("utf-8", errors="replace")
        print(f"[{datetime.now().isoformat(timespec='seconds')}] status={response.status}")
        print(response_body)


def main() -> int:
    args = parse_args()
    url = build_url(args)

    iteration = 0
    while True:
        payload = build_payload(args, order_id=iteration + 1)

        if args.dry_run:
            print(json.dumps(payload, indent=2))
        else:
            try:
                send_payload(url, payload, timeout=args.timeout)
            except urllib.error.HTTPError as exc:
                body = exc.read().decode("utf-8", errors="replace")
                print(f"HTTP {exc.code}: {body}", file=sys.stderr)
                return 1
            except urllib.error.URLError as exc:
                print(f"request failed: {exc}", file=sys.stderr)
                return 1

        iteration += 1
        if args.count == 1 or (args.count > 0 and iteration >= args.count):
            return 0
        time.sleep(args.interval)


if __name__ == "__main__":
    raise SystemExit(main())
