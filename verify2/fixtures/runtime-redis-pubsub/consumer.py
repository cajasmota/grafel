"""
Fixture: Redis pub/sub + Streams consumer (Python / redis-py).
Expected edges:
  - SUBSCRIBES_TO: listen_notifications -> channel:redis-pubsub:notifications
  - SUBSCRIBES_TO: watch_all_events -> channel:redis-pubsub:events.* (wildcard)
  - SUBSCRIBES_TO: consume_order_events -> stream:redis:order-events
"""
import redis

r = redis.Redis(host='localhost', port=6379, decode_responses=True)


def listen_notifications():
    pubsub = r.pubsub()
    pubsub.subscribe('notifications')
    for message in pubsub.listen():
        if message['type'] == 'message':
            handle_notification(message['data'])


def watch_all_events():
    """Pattern subscribe — wildcard, best-effort cross-repo matching."""
    pubsub = r.pubsub()
    pubsub.psubscribe('events.*')
    for message in pubsub.listen():
        if message['type'] == 'pmessage':
            handle_event(message['data'])


def consume_order_events(consumer_name: str = 'worker-1'):
    """Consumer group reads from the order-events stream."""
    while True:
        messages = r.xreadgroup(
            'order-processors',
            consumer_name,
            {'order-events': '>'},
            count=10,
        )
        for stream, entries in (messages or []):
            for msg_id, fields in entries:
                process_order(msg_id, fields)
                r.xack('order-events', 'order-processors', msg_id)


def handle_notification(data):
    pass


def handle_event(data):
    pass


def process_order(msg_id, fields):
    pass
