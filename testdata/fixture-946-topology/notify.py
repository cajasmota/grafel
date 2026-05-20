"""
fixture-946-topology/notify.py
Synthetic fixture for #946 topology broadening test.

Demonstrates:
  1. Redis pub/sub — redis.publish / pubsub.subscribe
  2. Redis Streams  — r.xadd / r.xreadgroup
  3. Dramatiq async task — @dramatiq.actor + actor.send()
  4. AWS Lambda invoke — boto3 client.invoke(FunctionName=...)
"""
import redis
import dramatiq
import boto3


# ── Redis pub/sub ────────────────────────────────────────────────────────────

def publish_notification(r: redis.Redis, message: str):
    r.publish('notifications', message)


def subscribe_notifications(r: redis.Redis):
    pubsub = r.pubsub()
    pubsub.subscribe('notifications')
    for item in pubsub.listen():
        if item['type'] == 'message':
            handle_notification(item['data'])


# ── Redis Streams ────────────────────────────────────────────────────────────

def publish_event(r: redis.Redis, event: dict):
    r.xadd('events', event)


def consume_events(r: redis.Redis):
    r.xreadgroup('workers', 'w1', {'events': '>'})


# ── Dramatiq async task ──────────────────────────────────────────────────────

@dramatiq.actor
def send_email(to: str, subject: str):
    """Dramatiq actor that sends an email."""
    pass  # implementation omitted in fixture


def enqueue_email(to: str, subject: str):
    send_email.send(to, subject)


# ── AWS Lambda invoke ────────────────────────────────────────────────────────

def invoke_order_processor(order_id: str):
    client = boto3.client('lambda')
    client.invoke(
        FunctionName='OrderProcessor',
        InvocationType='Event',
        Payload=f'{{"order_id": "{order_id}"}}'.encode(),
    )


# ── Lambda handler (consumer side) ───────────────────────────────────────────

def lambda_handler(event, context):
    """AWS Lambda handler for OrderProcessor."""
    order_id = event.get('order_id')
    process_order(order_id)


def handle_notification(data):
    pass


def process_order(order_id):
    pass
