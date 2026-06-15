---
entity_id: ep-orders-list
kind: http_endpoint
disqualified: false
merged_into: ""
rank: 0.82
group: orders
group_label: 'Order management'
summary: 'Returns a paginated list of orders for the authenticated user, sorted by creation date descending.'
gaps:
  - 'No 429 rate-limit response documented'
  - 'limit parameter has no server-side cap documented — actual max is 200'

# ── http_endpoint-specific fields ────────────────────────────────────────────
method: GET
path: /api/orders
parameters:
  - name: page
    in: query
    type: int
    required: false
    default: 1
    description: Page number (1-indexed)
  - name: limit
    in: query
    type: int
    required: false
    default: 50
    description: Items per page (server-side cap at 200)
  - name: status
    in: query
    type: string
    required: false
    description: Filter by order status (pending, confirmed, shipped, cancelled)
responses:
  '200':
    description: Paginated order list
    shape: '{ orders: Order[], total: int, page: int, pages: int }'
  '400':
    description: Invalid query parameters (non-integer page, unknown status value)
  '401':
    description: Missing or expired JWT
auth: 'Bearer token required (JWT) — user identity derived from token sub claim'
tables_touched: [orders, order_items]
parameters_explained: 'page is 1-indexed; page=0 returns a 400. limit is capped at 200 server-side regardless of the requested value — callers should not assume they received fewer items than limit means there are no more pages.'
response_shapes_explained: 'orders contains Order objects with fields: id (UUID), status (enum), total_cents (int), created_at (ISO-8601), items (OrderItem[]). OrderItem has sku, quantity, unit_price_cents.'
examples: 'GET /api/orders?page=2&limit=20&status=shipped — returns the second page of 20 shipped orders for the authenticated user'
caveats: 'Soft-deleted orders are excluded unless the caller has the admin role and passes ?include_deleted=true. The response total reflects the filtered count, not the global order count.'
---

## `GET /api/orders` — List orders

Returns a paginated list of orders belonging to the authenticated user. Orders are sorted by `created_at` descending (newest first).

### Authentication

Requires a valid Bearer JWT in the `Authorization` header. The endpoint derives the user identity from the token `sub` claim.

### Pagination

Use `page` and `limit` to paginate. The response `pages` field tells you the total page count. When `page` exceeds `pages`, an empty `orders` array is returned (not a 404).
