---
entity_id: topic-order-created
kind: message_topic
disqualified: false
merged_into: ""
rank: 0.92
group: orders
group_label: 'Order management'
summary: 'Published when a new order is confirmed; triggers fulfillment, analytics ingest, and customer notification.'
gaps:
  - 'schema.discount_code field present in v2 payloads but absent from v1 — consumers must handle both'
  - 'No dead-letter queue documented for failed consumer deliveries'

# ── message_topic-specific fields ────────────────────────────────────────────
purpose: 'Primary integration point for the orders domain. Any service that needs to react to a new confirmed order subscribes here rather than polling the orders database directly.'
schema: '{ order_id: string (UUID), user_id: string (UUID), total_cents: int, currency: string (ISO-4217), items: [{ sku: string, quantity: int, unit_price_cents: int }], discount_code: string|null (v2 only), created_at: string (ISO-8601) }'
typical_payload_size_bytes: 640
volume_estimate: high
expected_consumers: [order-fulfillment, analytics, notifications]
examples: 'order.created published after a checkout with order_id="ord-abc123", user_id="usr-xyz", total_cents=4999, items=[{sku:"WIDGET-1", quantity:2, unit_price_cents:2000}]'
caveats: 'Schema v1 (pre-2025-Q3) payloads lack discount_code — consumers must treat it as optional. Message ordering within the topic is not guaranteed. Retry semantics: the broker delivers at-least-once; consumers must be idempotent on order_id.'
---

## `order.created` — Order created topic

Published by the checkout flow immediately after an order is successfully persisted. This is the authoritative signal that a purchase has been confirmed.

### Broker

RabbitMQ, exchange `orders.events`, routing key `order.created`.

### Schema versions

| Version | Added | Changes |
|---------|-------|---------|
| v1 | 2024-Q1 | Initial schema |
| v2 | 2025-Q3 | Added `discount_code` field (nullable) |

Consumers must handle both versions. Use `discount_code` only when non-null.

### Consumers

| Service | Action |
|---------|--------|
| `order-fulfillment` | Begins pick-and-pack sequence |
| `analytics` | Ingests order into the data warehouse |
| `notifications` | Sends order confirmation email to the user |
