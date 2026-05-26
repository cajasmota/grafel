"""
Pulsar Python consumer fixture — testdata/runtime-fixtures/runtime-pulsar/consumer.py

Emits SUBSCRIBES_TO edge to topic:pulsar:persistent://public/default/orders.
"""
import pulsar

client = pulsar.Client('pulsar://localhost:6650')


def consume_orders() -> None:
    consumer = client.subscribe(
        'persistent://public/default/orders',
        subscription_name='order-consumer',
    )
    while True:
        msg = consumer.receive()
        consumer.acknowledge(msg)
