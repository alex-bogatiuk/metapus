from typing import Any, Literal, TypedDict

EventType = Literal[
    "invoice.created", "invoice.paid", "invoice.confirmed", "invoice.completed",
    "invoice.overpaid", "invoice.underpaid", "invoice.expired", "invoice.cancelled", "invoice.reversed",
    "invoice.late_payment",
    "payment.detected", "payment.confirming", "payment.confirmed", "payment.reorged", "payment.replaced",
    "withdrawal.confirmed", "withdrawal.failed", "withdrawal.partially_completed",
    "wallet.risk_changed", "deposit_address.rotated",
]


class EventData(TypedDict):
    object: dict[str, Any]


class MerchantEvent(TypedDict):
    id: str
    object: Literal["event"]
    apiVersion: str
    type: EventType
    createdAt: str
    data: EventData
