"""
Azure EventGrid producer — fixture for #927 EventGrid acceptance.

Acceptance: EventGridPublisherClient.send(events) → PUBLISHES_TO
synthetic event:eventgrid:<subject>:<event-type>.
"""
from azure.core.credentials import AzureKeyCredential
from azure.eventgrid import EventGridPublisherClient, EventGridEvent


def publish_inventory_alert(topic_endpoint: str, topic_key: str, sku: str) -> None:
    """Publish an Inventory.LowStock event to EventGrid."""
    client = EventGridPublisherClient(
        topic_endpoint, AzureKeyCredential(topic_key)
    )
    events = [
        EventGridEvent(
            subject="/inventory/low",
            event_type="Inventory.LowStock",
            data={"sku": sku, "quantity": 0},
            data_version="1.0",
        )
    ]
    client.send(events)
