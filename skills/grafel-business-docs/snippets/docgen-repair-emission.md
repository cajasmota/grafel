# Docgen Repair Emission — shared writer contract

Every writer pass (Passes 3, 4, 5, 6, 12, and 3a) MUST run this emission step
after its main doc output is produced. Do not skip, even when no candidates are
found — the zero-candidate case is the common case and costs nothing.

## When to collect a candidate

As you reason over source code and graph data to write the doc, note any of the
following observations in a running candidate list:

| Observation | Repair type |
|-------------|-------------|
| An `UNRESOLVED` stub that matches a real entity you just read (import, class definition, function) | `resolve_ref` |
| A dynamic call site whose callee you can identify from context (string registry, event bus dispatch, factory pattern) | `add_edge` |
| An entity whose `kind` is clearly wrong (e.g. a class catalogued as `Function`, a topic catalogued as `Class`) | `fix_kind` |
| A stub that resolves to a well-known external library / SaaS API (e.g. `stripe`, `aws-sdk`, `sendgrid`) | `label_external` |
| Two flow entities that represent the same business workflow with different entry points | `merge_flow` |

## Confidence rules

Use these levels — never fabricate certainty:

| Confidence | Meaning |
|------------|---------|
| `0.9–1.0` | Agent READ the source file AND saw the literal target (e.g. import statement, function definition, explicit assignment). Only use this band when the evidence is unambiguous. |
| `0.7–0.8` | Agent has strong contextual evidence (type signature, docstring, or call convention strongly implies the target) but did not see the literal binding. |
| `0.5–0.7` | Pattern-matched or inferred from naming convention and structure. Plausible but not confirmed. |
| Below 0.5 | Do not emit. Let the static extractor own it. |

## Emission procedure

After the doc is written (AFTER — do not interrupt prose generation):

1. For each collected observation, produce one JSON object per line and append
   to the JSONL file:

   ```
   ~/.grafel/groups/<group>/docgen-repairs.jsonl
   ```

   To obtain the exact path, call `grafel_docgen_apply (kind=repairs)` with no
   parameters and read the `state_dir` field, or use the path recorded in
   `docgen-state.json`.

2. Record shape (all fields are described in SKILL.md § "Docgen Repair Feedback
   Contract"; reproduce the exact schema below for reference):

   ```json
   {
     "type": "resolve_ref | add_edge | fix_kind | label_external | merge_flow",
     "source_entity_id": "<hex entity id>",
     "target": "<target id, qualified name, or ext:<module>>",
     "edge_kind": "CALLS",
     "new_kind": "Service",
     "confidence": 0.9,
     "evidence": "<file.go>@line N: <one-line observation>",
     "source": "generate-docs/pass-N",
     "emitted_at": "<ISO 8601 timestamp>"
   }
   ```

   Field rules:
   - `type` — always required; one of the five types above.
   - `source_entity_id` — always required; the entity the repair targets.
   - `target` — required for all types except `fix_kind`; a resolved entity id,
     qualified name, or `ext:<module>` for externals.
   - `edge_kind` — required only for `add_edge`; e.g. `CALLS`, `IMPORTS`,
     `SUBSCRIBES_TO`.
   - `new_kind` — required only for `fix_kind`; the replacement Kind string.
   - `confidence` — always required; float in [0.5, 1.0] (below 0.5 → don't
     emit at all).
   - `evidence` — always required; format: `"<file>@line N: <observation>"`.
     One line only — no multi-line strings. Must be citable.
   - `source` — optional but strongly recommended; set to the pass name so the
     audit trail is readable (e.g. `"generate-docs/pass-3"`,
     `"generate-docs/pass-4"`, `"generate-docs/pass-3a"`).
   - `emitted_at` — ISO 8601 UTC timestamp of when the observation was made.

3. If zero candidates were collected during this pass, skip the file append
   entirely (do not write an empty line).

4. At the end of the pass report, append one line:

   > `repair-emission: <N> candidates emitted to docgen-repairs.jsonl (<M> resolve_ref, <K> add_edge, <J> fix_kind, <L> label_external, <O> merge_flow).`

   If zero: `repair-emission: 0 candidates — no new discoveries this pass.`

## Worked examples

### Example A — resolve_ref (confidence 0.95)

Writer is documenting `OrderService` in Pass 4. It calls
`grafel_get_source(node_id=<OrderService id>)` and reads:

```go
// order_service.go@line 8
import "github.com/example/app/internal/billing"
```

The graph shows `OrderService` has an `UNRESOLVED` outbound edge to the stub
`billing`. The writer recognises this is `BillingService` in the `billing`
package (`entity_id = a3f9...`). Emission:

```json
{
  "type": "resolve_ref",
  "source_entity_id": "8b2c4d...",
  "target": "a3f9...",
  "confidence": 0.95,
  "evidence": "order_service.go@line 8: import \"github.com/example/app/internal/billing\" — stub resolves to BillingService",
  "source": "generate-docs/pass-4",
  "emitted_at": "2026-05-23T14:00:00Z"
}
```

### Example B — label_external (confidence 0.92)

Writer is documenting `EmailNotifier` in Pass 6. Source shows:

```python
# notifier.py@line 14
import sendgrid
```

The graph has an `UNRESOLVED` stub for `sendgrid`. Emission:

```json
{
  "type": "label_external",
  "source_entity_id": "c7d1e2...",
  "target": "ext:sendgrid",
  "confidence": 0.92,
  "evidence": "notifier.py@line 14: import sendgrid — well-known external SaaS email API",
  "source": "generate-docs/pass-6",
  "emitted_at": "2026-05-23T14:00:00Z"
}
```

### Example C — add_edge (confidence 0.75)

Writer is documenting `TaskDispatcher` in Pass 4. Source shows a string-keyed
registry dispatch:

```python
# dispatcher.py@line 42
handler = self._registry.get(event.type)   # "payment.completed" → PaymentHandler
```

The graph has no edge from `TaskDispatcher` to `PaymentHandler`. The writer
infers the mapping from a comment but did not read the registry initialiser.
Confidence is 0.75 (contextual, not literal). Emission:

```json
{
  "type": "add_edge",
  "source_entity_id": "d4a9c0...",
  "target": "PaymentHandler",
  "edge_kind": "CALLS",
  "confidence": 0.75,
  "evidence": "dispatcher.py@line 42: string registry dispatch — comment names PaymentHandler as handler for payment.completed",
  "source": "generate-docs/pass-4",
  "emitted_at": "2026-05-23T14:01:00Z"
}
```

### Example D — fix_kind (confidence 0.90)

Writer is documenting `UserEventTopic` in Pass 12. The entity is catalogued as
kind `Class` but the source file shows it is a Kafka topic config struct, not a
class. Emission:

```json
{
  "type": "fix_kind",
  "source_entity_id": "f1b3a7...",
  "new_kind": "MessageTopic",
  "confidence": 0.90,
  "evidence": "events/user_event_topic.go@line 3: var UserEventTopic = kafka.Topic{Name: \"user.events\"} — this is a Kafka topic definition, not a class",
  "source": "generate-docs/pass-12",
  "emitted_at": "2026-05-23T14:02:00Z"
}
```

### Example E — merge_flow (confidence 0.80)

Writer is documenting flows in Pass 4 and notices two process-flow entities
`checkout_flow` and `checkout_legacy_flow` that both describe the same checkout
business workflow, distinguished only by an A/B flag at entry. Emission:

```json
{
  "type": "merge_flow",
  "source_entity_id": "e9c2f5...",
  "target": "a1b4d8...",
  "confidence": 0.80,
  "evidence": "checkout_handler.go@line 71: ab_flag routes to checkout_flow or checkout_legacy_flow — same business workflow, same terminal state",
  "source": "generate-docs/pass-4",
  "emitted_at": "2026-05-23T14:03:00Z"
}
```

## Invariants

- Only one candidate per distinct observation — do not emit the same
  `(source_entity_id, type, target)` triple twice in one pass.
- Never emit below confidence 0.5. If you are not sure, use the Pass 3a
  "Runtime-resolved edge" callout in prose instead.
- Emission is additive: if the same repair was already submitted by Pass 1a or
  Pass 3a via `grafel_docgen_apply(kind="repairs", action=submit)`, the daemon deduplicates; you
  may still emit it here — the duplicate will be silently dropped.
- The JSONL file may already contain entries from a prior pass or a prior run;
  always append, never overwrite.
- Evidence must be a single line. Multi-line evidence will fail
  `DocgenRepairCandidate.Validate()`.
