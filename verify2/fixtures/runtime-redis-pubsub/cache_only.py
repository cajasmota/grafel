"""
Fixture: Redis cache-only operations (Python).
PURPOSE: false-positive guard — none of these calls should generate
         pub/sub or stream edges.
"""
import redis

r = redis.Redis(host='localhost', port=6379)


def set_user_session(user_id: str, data: dict, ttl: int = 3600) -> bool:
    return r.setex(f'session:{user_id}', ttl, str(data))


def get_user_session(user_id: str):
    return r.get(f'session:{user_id}')


def delete_user_session(user_id: str) -> int:
    return r.delete(f'session:{user_id}')


def increment_rate_limit(key: str) -> int:
    pipe = r.pipeline()
    pipe.incr(key)
    pipe.expire(key, 60)
    results = pipe.execute()
    return results[0]


def cache_product(product_id: str, data: dict) -> bool:
    return r.hset(f'product:{product_id}', mapping=data)
