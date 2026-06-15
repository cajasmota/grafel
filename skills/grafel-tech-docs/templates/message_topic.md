---
entity_id: <entity_id>
kind: message_topic
disqualified: false
merged_into: ""
rank: <0..1 or omit>
group: <domain-key>
group_label: '<Human-readable domain name>'
summary: '<One sentence overview for list views>'
gaps:
  - '<Actionable gap or omit this block>'

# ── message_topic-specific fields ────────────────────────────────────────────
purpose: '<Prose: business reason this topic exists — consumed by Topology detail panel>'
schema: '<{ field: type, field: type }>'
typical_payload_size_bytes: <integer or omit>
volume_estimate: <low|medium|high|very-high>
expected_consumers: [<service-a>, <service-b>]
examples: '<Prose: example publish event, key field values>'
caveats: '<Prose: schema version history, consumer compatibility notes, ordering guarantees>'
---

## `<TopicName>`

<!-- Prose description continues here. Do not alter this section when prepending frontmatter. -->
