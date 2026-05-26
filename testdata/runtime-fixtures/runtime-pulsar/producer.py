"""
Pulsar Python producer fixture — testdata/runtime-fixtures/runtime-pulsar/producer.py

Emits PUBLISHES_TO edge to topic:pulsar:persistent://public/default/orders.
"""
import pulsar

client = pulsar.Client('pulsar://localhost:6650')


def send_order(payload: bytes) -> None:
    producer = client.create_producer(topic='persistent://public/default/orders')
    producer.send(payload)
