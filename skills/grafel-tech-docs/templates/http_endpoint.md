---
entity_id: <entity_id>
kind: http_endpoint
disqualified: false
merged_into: ""
rank: <0..1 or omit>
group: <domain-key>
group_label: '<Human-readable domain name>'
summary: '<One sentence from the user perspective>'
gaps:
  - '<Actionable gap or omit this block>'

# ── http_endpoint-specific fields ────────────────────────────────────────────
method: <GET|POST|PUT|PATCH|DELETE>
path: /api/<path>
parameters:
  - name: <param>
    in: <query|path|header|body>
    type: <string|int|bool|...>
    required: <true|false>
    default: <value or omit>
    description: '<Short description>'
responses:
  '200':
    description: '<Success description>'
    shape: '<{ field: type, ... }>'
  '400':
    description: '<Validation error>'
  '401':
    description: '<Unauthenticated>'
  '404':
    description: '<Not found>'
auth: '<Bearer token required (JWT) | No auth required | ...>'
tables_touched: [<table1>, <table2>]
parameters_explained: '<Prose: clarify non-obvious parameter semantics, defaults, limits>'
response_shapes_explained: '<Prose: describe nested object shapes, enum values>'
examples: '<Prose: one or two representative call examples with expected outcome>'
caveats: '<Prose: edge cases, rate limits, soft-delete behaviour, deprecations>'
---

## `<EntityName>`

<!-- Prose description continues here. Do not alter this section when prepending frontmatter. -->
