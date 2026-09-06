from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True, slots=True)
class Register:
    value: Any
    metadata: dict[str, Any] = field(default_factory=dict)


@dataclass(frozen=True, slots=True)
class RegisterUpdate:
    name: str
    value: Any = None
    metadata: dict[str, Any] = field(default_factory=dict)
    removed: bool = False
