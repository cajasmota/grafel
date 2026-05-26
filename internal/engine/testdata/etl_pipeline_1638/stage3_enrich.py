"""ETL stage 3 — enrich.

Subscribed to the SNS topic `etl-transformed-topic` (SNS -> HTTP/Lambda
delivery). Each transformed record is enriched with geo/account metadata and
appended to the Redis stream `etl-enriched-stream`.

Consumer side: SNS `etl-transformed-topic`.
Producer side: Redis stream `etl-enriched-stream`.
"""
import json

import boto3
import redis

r = redis.Redis(host="redis", port=6379)
sns = boto3.client("sns")

TRANSFORMED_TOPIC_ARN = "arn:aws:sns:us-east-1:123456789012:etl-transformed-topic"
ENRICH_LAMBDA_ARN = "arn:aws:lambda:us-east-1:123456789012:function:etl-enrich"


def register_subscription():
    """Subscribe this enrich Lambda to the SNS `etl-transformed-topic`.

    Consumer registration for the SNS hop, so the topology links
    `etl-transformed-topic` (producer: stage2) to this enrich stage.
    """
    sns.subscribe(
        TopicArn=TRANSFORMED_TOPIC_ARN,
        Protocol="lambda",
        Endpoint=ENRICH_LAMBDA_ARN,
    )


def enrich(record):
    """Attach enrichment metadata to a transformed record."""
    record["geo"] = "us"
    record["enriched"] = True
    return record


def handle_sns_event(event, _context):
    """SNS -> Lambda consumer for `etl-transformed-topic`.

    Records are read from event["Records"][*]["Sns"]["Message"], enriched, then
    pushed to the Redis stream `etl-enriched-stream`.
    """
    for rec in event["Records"]:
        record = enrich(json.loads(rec["Sns"]["Message"]))
        r.xadd("etl-enriched-stream", {"data": json.dumps(record)})
