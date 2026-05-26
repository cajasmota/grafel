---
entity_id: <entity_id>
kind: process_flow
disqualified: false
merged_into: ""
rank: <0..1 or omit>
group: <domain-key>
group_label: '<Human-readable domain name>'
summary: '<One sentence describing what this flow achieves>'
gaps:
  - '<Actionable gap or omit this block>'

# ── process_flow-specific fields ─────────────────────────────────────────────
steps:
  - '<Step 1 — brief action description>'
  - '<Step 2>'
  - '<Step 3>'
preconditions: '<What must be true before this flow can execute>'
expected_outcome: '<What the system state is after a successful run>'
examples: '<Prose: happy-path narrative, one or two concrete scenarios>'
caveats: '<Prose: failure modes, retry behaviour, race conditions, side effects>'
---

## `<FlowName>`

<!-- Prose description continues here. Do not alter this section when prepending frontmatter. -->
