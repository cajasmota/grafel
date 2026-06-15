# Business-voice rules

The single style contract for every page in the BUSINESS tier (`business/`).
A writer subagent in any business pass (15–19) MUST read this first and obey it
without exception. The audience is a Product Manager, a business analyst, or a
non-engineer team lead reading on Confluence — **not** an engineer.

The 2026-05-23 docgen audit found `user-journeys.md` was *labeled* a narrative
but was a 60-step mermaid `sequenceDiagram` naming `useAuthStore`,
`syncEngine.ts`, `/api/v1/mobile/*`. That is a technical-tier artifact. It must
never appear in the business tier.

## Hard rules (a violation is a hard stop — rewrite, do not ship)

1. **Zero internal symbol names.** No class names, function names, file names,
   module slugs, env vars, table names, API paths, or package names. If you
   would write `OrderViewSet`, `syncEngine.ts`, `/api/v1/mobile`, or
   `useAuthStore`, instead name the *business thing* it implements ("the order
   submission step", "the offline sync that runs when the device reconnects").
2. **No code mermaid.** No `sequenceDiagram`, no `classDiagram`, no
   call-graph/flow-of-functions. A simple business-narrative `flowchart`
   diagram of *business steps* (boxes a PM would recognise: "Inspection
   created" → "Deficiency recorded" → "Report issued") is allowed, max ~8 nodes,
   and must duplicate nothing the prose already says clearly.
3. **No code blocks of source.** No quoted source. A small illustrative
   payload-in-business-terms table is fine ("an inspection record carries: who,
   where, when, outcome"); a JSON/Python/TS block is not.
4. **Plain language.** Short sentences. Define any unavoidable jargon inline or
   link to the domain glossary. Prefer "the system" / "the field app" /
   "office staff" over component names.
5. **Business framing, always.** Every capability/rule/journey answers *what the
   business gets* and *why*, then *what the user does*. Never *how the code
   does it*.

## What to write instead of symbols

| Tempting (technical) | Write this (business) |
|----------------------|-----------------------|
| `InspectionViewSet.create()` | "An inspector starts a new inspection" |
| `permission_classes = [...]` | "Only authorised office staff can do this" |
| `order.created` topic | "An order-placed notification fans out to fulfilment and billing" |
| `if status == 'OVERDUE'` | "When an inspection passes its due date it becomes overdue" |
| `core-mobile` repo | "the field inspection app" |

## Provenance, kept out of the reader's way

A business page is reverse-engineered from code, so it can be wrong if the code
changes. Each business page carries a hidden-by-default provenance note at the
bottom so an engineer can audit it without polluting the PM reading:

```markdown
<details>
<summary>Where this came from (for engineers)</summary>

Derived from the technical docs and graph evidence:
- capability backed by the endpoints documented in `~/.grafel/docs/<group>/<repo-slug>/reference/api.md`
- rule observed in validation logic surfaced by `grafel_find`
- last regenerated: <date>

</details>
```

The `<summary>` block is the **only** place a file path or symbol may appear in
a business page, and it is collapsed by default so a PM never has to read it.

## Translating evidence → business language

You will be handed graph evidence and technical-tier docs full of symbols.
Your job is translation, not transcription:

1. Read the evidence (endpoints, flows, entities, validation logic, the
   already-written technical docs).
2. Group it by *business meaning*, not by code structure.
3. Write the business meaning in the reader's language.
4. Drop the provenance trail into the collapsed `<details>` block.
