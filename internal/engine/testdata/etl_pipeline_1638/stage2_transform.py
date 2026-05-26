"""ETL stage 2 — transform.

Consumes raw records from the SQS `etl-ingest-queue`, normalises them, then
fans the transformed records out to the SNS topic `etl-transformed-topic`.

Consumer side: SQS `etl-ingest-queue`.
Producer side: SNS `etl-transformed-topic`.
"""
import json

import boto3

sqs = boto3.client("sqs")
sns = boto3.client("sns")

TRANSFORMED_TOPIC_ARN = "arn:aws:sns:us-east-1:123456789012:etl-transformed-topic"


def normalise(record):
    """Apply field normalisation to a raw record."""
    record["kind"] = record["kind"].upper()
    record["normalised"] = True
    return record


def transform():
    """Consume from `etl-ingest-queue`, publish to `etl-transformed-topic`."""
    resp = sqs.receive_message(
        QueueUrl="https://sqs.us-east-1.amazonaws.com/123456789012/etl-ingest-queue",
        MaxNumberOfMessages=10,
    )
    for msg in resp.get("Messages", []):
        record = normalise(json.loads(msg["Body"]))
        sns.publish(
            TopicArn=TRANSFORMED_TOPIC_ARN,
            Message=json.dumps(record),
        )


if __name__ == "__main__":
    transform()
