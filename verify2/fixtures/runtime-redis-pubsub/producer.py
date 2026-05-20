"""
Fixture: Redis pub/sub + Streams producer (Python / redis-py).
Expected edges:
  - PUBLISHES_TO: emit_notification -> channel:redis-pubsub:notifications
  - PUBLISHES_TO: emit_cache_invalidation -> channel:redis-pubsub:cache-invalidation
  - PUBLISHES_TO: push_order_event -> stream:redis:order-events
"""
import redis
import json

r = redis.Redis(host='localhost', port=6379, decode_responses=True)


def emit_notification(user_id: str, message: str) -> int:
    payload = json.dumps({"user_id": user_id, "message": message})
    return r.publish('notifications', payload)


def emit_cache_invalidation(key: str) -> int:
    payload = json.dumps({"key": key, "action": "delete"})
    return r.publish('cache-invalidation', payload)


def push_order_event(order_id: str, event_type: str) -> str:
    return r.xadd('order-events', {
        'event': event_type,
        'order_id': order_id,
    })
