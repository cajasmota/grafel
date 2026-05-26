"""ETL stage 1 — ingest.

Reads raw clickstream records and pushes each one onto the AWS SQS queue
`etl-ingest-queue`. This is the head of the multi-broker ETL pipeline:

    ingest (SQS) -> transform (SNS) -> enrich (Redis stream)
        -> dedup (Google Pub/Sub) -> load (RabbitMQ / aio-pika) -> sink

Producer side: SQS `etl-ingest-queue`.
"""
import json

import boto3

sqs = boto3.client("sqs")


def fetch_raw_records():
    """Pretend source: yields raw clickstream events."""
    for i in range(100):
        yield {"event_id": i, "kind": "click", "ts": i * 1000}


def ingest():
    """Read raw records and produce them to the SQS `etl-ingest-queue`."""
    for record in fetch_raw_records():
        sqs.send_message(
            QueueUrl="https://sqs.us-east-1.amazonaws.com/123456789012/etl-ingest-queue",
            MessageBody=json.dumps(record),
        )


if __name__ == "__main__":
    ingest()
