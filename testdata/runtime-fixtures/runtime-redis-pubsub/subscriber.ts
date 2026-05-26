/**
 * Fixture: Redis pub/sub + Streams consumer (Node / ioredis).
 * Expected edges:
 *   - SUBSCRIBES_TO: startNotificationListener -> channel:redis-pubsub:notifications
 *   - SUBSCRIBES_TO: consumeOrderStream         -> stream:redis:order-events
 */
import Redis from 'ioredis';

const subscriber = new Redis();
const client = new Redis();

export async function startNotificationListener(): Promise<void> {
  await subscriber.subscribe('notifications', (err) => {
    if (err) throw err;
  });
  subscriber.on('message', (channel, message) => {
    handleNotification(channel, message);
  });
}

export async function consumeOrderStream(consumerName: string = 'worker-1'): Promise<void> {
  const results = await client.xreadgroup(
    'GROUP', 'order-processors', consumerName,
    'COUNT', '10',
    'STREAMS', 'order-events', '>'
  ) as Array<[string, Array<[string, string[]]>]> | null;

  if (!results) return;
  for (const [, entries] of results) {
    for (const [id, fields] of entries) {
      await processOrderEvent(id, fields);
    }
  }
}

async function handleNotification(_channel: string, _message: string): Promise<void> {}
async function processOrderEvent(_id: string, _fields: string[]): Promise<void> {}
