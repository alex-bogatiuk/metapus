import hashlib
import hmac
import json
import time
from typing import Any


def verify_webhook(raw_body: bytes, signature_header: str, secret: str, *, now: float | None = None, tolerance_seconds: int = 300) -> dict[str, Any]:
    """Verify Metapus-Signature against the unmodified request body."""
    timestamp: str | None = None
    signatures: list[str] = []
    for field in signature_header.split(","):
        key, separator, value = field.strip().partition("=")
        if not separator:
            continue
        if key == "t":
            timestamp = value
        elif key == "v1":
            signatures.append(value)
    if timestamp is None or not timestamp.isdigit() or not signatures:
        raise ValueError("invalid Metapus-Signature")
    current = time.time() if now is None else now
    if abs(current - int(timestamp)) > tolerance_seconds:
        raise ValueError("webhook timestamp is outside tolerance")
    signed = timestamp.encode("ascii") + b"." + raw_body
    expected = hmac.new(secret.encode("utf-8"), signed, hashlib.sha256).hexdigest()
    if not any(hmac.compare_digest(expected, candidate) for candidate in signatures):
        raise ValueError("invalid webhook signature")
    return json.loads(raw_body)
