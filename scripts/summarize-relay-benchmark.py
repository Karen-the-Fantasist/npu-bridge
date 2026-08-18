#!/usr/bin/env python3
"""Summarize raw timing files emitted by benchmark-cross-boundary.sh."""

from __future__ import annotations

import argparse
import json
import math
import statistics
from pathlib import Path


def summarize(path: Path, payload_bytes: int) -> dict[str, int | float]:
    seconds = [float(line) for line in path.read_text().splitlines() if line.strip()]
    if len(seconds) < 2:
        raise ValueError(f"{path} must contain one cold and at least one warm request")
    warm = seconds[1:]
    ordered = sorted(warm)
    p95 = ordered[math.ceil(0.95 * len(ordered)) - 1]
    median = statistics.median(warm)
    return {
        "requests": len(seconds),
        "payload_bytes_each_way": payload_bytes,
        "first_request_ms": round(seconds[0] * 1000, 3),
        "warm_median_ms": round(median * 1000, 3),
        "warm_p95_ms": round(p95 * 1000, 3),
        "warm_bidirectional_mib_per_second": round(
            (2 * payload_bytes) / median / (1024 * 1024), 3
        ),
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("bench_dir", type=Path)
    args = parser.parse_args()

    measurements = {
        "direct_small": summarize(args.bench_dir / "direct-small.txt", 1),
        "bridge_small": summarize(args.bench_dir / "bridge-small.txt", 1),
        "direct_large": summarize(args.bench_dir / "direct-large.txt", 1024 * 1024),
        "bridge_large": summarize(args.bench_dir / "bridge-large.txt", 1024 * 1024),
    }
    result = {
        "schema": "npu-bridge/relay-benchmark/v1",
        "measurements": measurements,
        "derived": {
            "first_small_added_ms": round(
                measurements["bridge_small"]["first_request_ms"]
                - measurements["direct_small"]["first_request_ms"],
                3,
            ),
            "warm_small_added_median_ms": round(
                measurements["bridge_small"]["warm_median_ms"]
                - measurements["direct_small"]["warm_median_ms"],
                3,
            ),
        },
    }
    print(json.dumps(result, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
