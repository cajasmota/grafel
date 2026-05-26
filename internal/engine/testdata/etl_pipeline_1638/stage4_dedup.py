"""ETL stage 4 — dedup.

Consumes enriched records from the Redis stream `etl-enriched-stream` using a
consumer group, drops duplicates, then publishes the unique records to the
Google Cloud Pub/Sub topic `etl-deduped-topic`.

Consumer side: Redis stream `etl-enriched-stream`.
Producer side: Google Pub/Sub `etl-deduped-topic` (project `polyglot-etl`).
"""
import json

import redis
from google.cloud import pubsub_v1

r = redis.Redis(host="redis", port=6379)
publisher = pubsub_v1.PublisherClient()

DEDUP_GROUP = "etl-dedup-group"
# Full Pub/Sub topic resource path (inlined at the publish call) so the
# topology resolves the `etl-deduped-topic` topic statically.

_seen = set()


def dedup():
    """Read from `etl-enriched-stream`, publish uniques to `etl-deduped-topic`."""
    resp = r.xreadgroup(
        DEDUP_GROUP,
        "dedup-consumer-1",
        {"etl-enriched-stream": ">"},
        count=10,
    )
    for _stream, entries in resp:
        for _msg_id, fields in entries:
            record = json.loads(fields[b"data"])
            if record["event_id"] in _seen:
                continue
            _seen.add(record["event_id"])
            publisher.publish(
                "projects/polyglot-etl/topics/etl-deduped-topic",
                json.dumps(record).encode("utf-8"),
            )


if __name__ == "__main__":
    dedup()
