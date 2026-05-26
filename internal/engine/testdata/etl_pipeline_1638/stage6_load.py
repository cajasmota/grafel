"""ETL stage 6 — load / sink.

Final stage. Consumes aggregates from the RabbitMQ queue `etl-load-queue` using
the async aio-pika client and writes them to the warehouse sink. This is the
tail of the multi-broker ETL pipeline.

Consumer side: RabbitMQ `etl-load-queue` (aio-pika).
"""
import asyncio
import json

import aio_pika

LOAD_QUEUE = "etl-load-queue"
RABBIT_URL = "amqp://guest:guest@rabbitmq/"


def write_to_warehouse(aggregate):
    """Sink: persist the aggregate to the analytics warehouse."""
    print(f"LOADED {aggregate['kind']}={aggregate['count']}")


async def consume():
    """Async-consume the RabbitMQ `etl-load-queue` and load each aggregate."""
    connection = await aio_pika.connect_robust(RABBIT_URL)
    channel = await connection.channel()
    queue = await channel.declare_queue(LOAD_QUEUE, durable=True)

    async def on_message(message):
        async with message.process():
            write_to_warehouse(json.loads(message.body.decode()))

    await queue.consume(on_message)


if __name__ == "__main__":
    asyncio.run(consume())
