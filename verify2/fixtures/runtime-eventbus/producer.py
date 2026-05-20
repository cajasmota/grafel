"""
EventBridge producer — fixture for #927 acceptance criteria.

Acceptance: Python publisher `events.put_events(Entries=[{'Source':'orders','DetailType':'OrderPlaced',...}])`
→ PUBLISHES_TO synthetic `event:eventbridge:orders:OrderPlaced`.
"""
import boto3


def place_order(order_id: str, amount: float) -> None:
    """Publish an OrderPlaced event to EventBridge."""
    client = boto3.client("events", region_name="us-east-1")
    client.put_events(
        Entries=[
            {
                "Source": "orders",
                "DetailType": "OrderPlaced",
                "Detail": f'{{"orderId":"{order_id}","amount":{amount}}}',
                "EventBusName": "default",
            }
        ]
    )
