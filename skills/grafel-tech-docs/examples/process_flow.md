---
entity_id: flow-checkout
kind: process_flow
disqualified: false
merged_into: ""
rank: 0.95
group: orders
group_label: 'Order management'
summary: 'Handles the full checkout sequence: stock validation, payment, order persistence, and post-purchase event emission.'
gaps:
  - 'No rollback path documented if the order.created event fails to publish'
  - 'Stock check is advisory — race condition possible under concurrent checkouts'

# ── process_flow-specific fields ─────────────────────────────────────────────
steps:
  - Authenticate user and validate session
  - Validate cart contents — check each SKU exists and quantity is available
  - Reserve stock (advisory lock, not transactional)
  - Charge payment method via payment service
  - Persist order record and order_items rows to database
  - Emit order.created event to broker
  - Return order confirmation to client
preconditions: 'User is authenticated, cart is non-empty, and at least one payment method is on file'
expected_outcome: 'Order row persisted with status=confirmed, payment captured, order.created event published, inventory decremented (async via fulfillment service)'
examples: 'Happy path: user checks out 3 items (SKUs A, B, C), payment succeeds on first attempt — order row created with id=uuid, order.created published within 50 ms. Retry path: payment fails on first attempt, user retries with a different card — first charge is voided, second succeeds.'
caveats: 'Stock reservation is advisory: two concurrent checkouts for the last unit of a SKU may both succeed. The fulfillment service is responsible for detecting and handling oversell. The flow does not send the confirmation email directly — the notifications service consumes order.created for that.'
---

## `checkout` — Checkout flow

Orchestrates the full purchase sequence from cart validation through to order confirmation. This is the highest-traffic flow in the orders domain and the primary source of `order.created` events.

### Entry point

`POST /api/checkout` handled by `CheckoutHandler.handle` in `apps/api/handlers/checkout.py`.

### Error handling

Payment failures return a `402 Payment Required` with a structured error body. Stock shortfalls after reservation return `409 Conflict`. All other failures return `500` and trigger a Sentry alert.
