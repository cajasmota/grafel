"""ETL stage 5 — aggregate.

Subscribes to the Google Cloud Pub/Sub topic `etl-deduped-topic`, rolls the
deduped records up into per-kind aggregates, then publishes each aggregate to
the RabbitMQ queue `etl-load-queue` using the async aio-pika client.

Consumer side: Google Pub/Sub `etl-deduped-topic` (project `polyglot-etl`).
Producer side: RabbitMQ `etl-load-queue` (aio-pika).
"""
import asyncio
import json
from collections import Counter

import aio_pika
from google.cloud import pubsub_v1

subscriber = pubsub_v1.SubscriberClient()
# Subscription bound to the `etl-deduped-topic` topic. The topic resource path
# (inlined at the subscribe call) links this consumer to stage4's producer on
# the same `etl-deduped-topic` Pub/Sub topic.

LOAD_QUEUE = "etl-load-queue"
RABBIT_URL = "amqp://guest:guest@rabbitmq/"

_counts = Counter()


async def publish_aggregate(aggregate):
    """Async-publish one aggregate to the RabbitMQ `etl-load-queue`."""
    connection = await aio_pika.connect_robust(RABBIT_URL)
    async with connection:
        channel = await connection.channel()
        await channel.declare_queue(LOAD_QUEUE, durable=True)
        await channel.default_exchange.publish(
            aio_pika.Message(body=json.dumps(aggregate).encode()),
            routing_key=LOAD_QUEUE,
        )


def handle_message(message):
    """Pub/Sub callback for `etl-deduped-topic`: aggregate then forward."""
    record = json.loads(message.data.decode("utf-8"))
    _counts[record["kind"]] += 1
    aggregate = {"kind": record["kind"], "count": _counts[record["kind"]]}
    asyncio.run(publish_aggregate(aggregate))
    message.ack()


def aggregate():
    """Pull from the Pub/Sub `etl-deduped-topic` subscription."""
    future = subscriber.subscribe(
        "projects/polyglot-etl/topics/etl-deduped-topic",
        callback=handle_message,
    )
    future.result()


if __name__ == "__main__":
    aggregate()
