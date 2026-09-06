from __future__ import annotations

import math
import re
from datetime import timedelta
from numbers import Real
from typing import TypeAlias

Duration: TypeAlias = str | float | int | timedelta

_DURATION_PART = re.compile(r"(\d+(?:\.\d+)?)(ms|s|m|h)")
_UNIT_SECONDS = {
    "ms": 0.001,
    "s": 1.0,
    "m": 60.0,
    "h": 3600.0,
}


def normalize_duration(duration: Duration) -> tuple[str, float]:
    if isinstance(duration, timedelta):
        seconds = duration.total_seconds()
        return _format_seconds(seconds), _validate_seconds(seconds, duration)

    if isinstance(duration, Real) and not isinstance(duration, bool):
        seconds = float(duration)
        return _format_seconds(seconds), _validate_seconds(seconds, duration)

    if not isinstance(duration, str) or not duration:
        raise ValueError(f"invalid duration: {duration!r}")

    seconds = 0.0
    position = 0
    while position < len(duration):
        match = _DURATION_PART.match(duration, position)
        if match is None:
            raise ValueError(f"invalid duration: {duration!r}")
        seconds += float(match.group(1)) * _UNIT_SECONDS[match.group(2)]
        position = match.end()

    return duration, _validate_seconds(seconds, duration)


def format_duration(duration: Duration) -> str:
    return normalize_duration(duration)[0]


def duration_seconds(duration: Duration) -> float:
    return normalize_duration(duration)[1]


def _validate_seconds(seconds: float, original: object) -> float:
    if not math.isfinite(seconds) or seconds <= 0:
        raise ValueError(f"duration must be positive and finite: {original!r}")
    return seconds


def _format_seconds(seconds: float) -> str:
    _validate_seconds(seconds, seconds)
    text = f"{seconds:.9f}".rstrip("0").rstrip(".")
    if not text or text == "0":
        raise ValueError(f"duration is too small: {seconds!r}")
    return f"{text}s"
