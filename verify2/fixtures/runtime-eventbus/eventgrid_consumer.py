"""
Azure EventGrid consumer — fixture for #927 EventGrid trigger detection.

Acceptance: AzureFunction subscribed via EventGridTrigger attribute links
to #940's Azure Function entities (EVENTGRID_TRIGGERS edge).
"""
import azure.functions as func


app = func.FunctionApp()


@app.event_grid_trigger(name="event")
async def handle_inventory_alert(event: func.EventGridEvent) -> None:
    """Azure Function triggered by EventGrid events."""
    data = event.get_json()
    sku = data.get("sku", "unknown")
    _ = sku
