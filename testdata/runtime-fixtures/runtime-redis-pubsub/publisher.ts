/**
 * Fixture: Redis pub/sub + Streams producer (Node / ioredis).
 * Expected edges:
 *   - PUBLISHES_TO: sendNotification   -> channel:redis-pubsub:notifications
 *   - PUBLISHES_TO: invalidateCache    -> channel:redis-pubsub:cache-invalidation
 *   - PUBLISHES_TO: appendOrderEvent   -> stream:redis:order-events
 */
import Redis from 'ioredis';

const redis = new Redis();

export async function sendNotification(userId: string, message: string): Promise<number> {
  const payload = JSON.stringify({ userId, message });
  return redis.publish('notifications', payload);
}

export async function invalidateCache(key: string): Promise<number> {
  const payload = JSON.stringify({ key, action: 'delete' });
  return redis.publish('cache-invalidation', payload);
}

export async function appendOrderEvent(orderId: string, eventType: string): Promise<string | null> {
  return redis.xadd('order-events', '*', 'event', eventType, 'order_id', orderId);
}
