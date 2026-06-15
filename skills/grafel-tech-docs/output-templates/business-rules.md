---
tier: business
doc_type: business-rules
anchors: []   # derive from headings per snippets/anchor-contract.md after writing
---

# Business rules

> The constraints, requirements, and policies the product enforces, reverse-
> engineered from the implementation and stated as PRODUCT requirements — the
> kind a team lead would lift into a spec. Each rule is plain language and
> testable in business terms. Group by business area, one `##` per area.
>
> A rule reads like a requirement, not like code:
>   GOOD: "An inspection cannot be submitted until every device on the building
>          checklist has an outcome recorded."
>   BAD:  "`InspectionSerializer.validate()` raises if `devices` is empty."

## <Business area, e.g. Inspections>

### <Rule title in business language>

- **Rule:** <the requirement, one sentence>.
- **Why:** <the business reason / what it protects>.
- **Applies when:** <the situation that triggers it>.
- **If violated:** <what the product does — rejects, warns, blocks — in
  business terms>.

### <Next rule>

> …

## <Next business area, e.g. Access & permissions>

> Rules about who can do what, in business terms ("only office staff may close
> an inspection"; "field inspectors cannot edit a submitted report").

<details>
<summary>Where this came from (for engineers)</summary>

Rules were inferred from validation logic, conditional branches, permission
checks, and required fields surfaced via `grafel_find` and the technical
tier. Each rule should be verifiable against that logic. Last regenerated:
<date>.
</details>
