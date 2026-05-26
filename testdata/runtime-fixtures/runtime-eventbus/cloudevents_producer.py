"""
CloudEvents producer — fixture for #927 CloudEvents acceptance.

Acceptance: CloudEvent(type='com.example.order.shipped', source='/shop/orders')
builder → PUBLISHES_TO synthetic event:cloudevents:/shop/orders:com.example.order.shipped.

Also validates 0 false positives on plain HTTP routes.
"""
from cloudevents.http import CloudEvent, to_structured
import requests


def emit_shipment_event(order_id: str) -> None:
    """Emit a CloudEvent for a shipped order."""
    event = CloudEvent(
        {
            "type": "com.example.order.shipped",
            "source": "/shop/orders",
            "id": order_id,
        }
    )
    headers, body = to_structured(event)
    # In production: POST headers+body to the channel endpoint.
    _ = headers, body


def plain_http_handler(request: dict) -> dict:
    """Plain HTTP handler — must NOT trigger CloudEvent detection."""
    return {"status": "ok", "data": request.get("data")}
