# ADR-0021: Engine — rescue / remove / extend the YAML-rules `custom_extractor` mechanism

- **Status:** Accepted
- **Date:** 2026-06-02 (accepted 2026-06-03)
- **Deciders:** archigraph maintainers
- **Related:** ADR-0001 (Go-native single binary), ADR-0018 (agent-learned patterns), issue #3585/#3586 (dead Java pattern-extractor layer cited as coverage)
- **Scope:** architecture evaluation. The REMOVE half (#3636 P1) is implemented: the dead
  `custom_extractors:` YAML blocks and the `CustomExtractor` struct were deleted, a
  cite-validity guard added, and useful description prose migrated into the corresponding
  Go extractor doc-comments. EXTEND (P2–P4) is deferred.

---

## Context

archigraph began as a Python project whose framework-extraction engine read **YAML
rule files**, one per framework. Those YAML files have four block types. Three are
declarative regex/glob rules; the fourth, `custom_extractors:`, was an **escape
hatch**: a pluggable pointer to a Python callable (`module:function`) for "patterns
YAML rules cannot capture" (cross-file dataflow, AST-level logic, computed
properties).

When the engine was ported to Go (schema comment dated 2026-05-27,
`internal/engine/schema.go`), the **declarative** rules were kept and re-implemented,
but the **pluggable Python-callable layer was dropped**. The schema field survives
only for YAML-parse compatibility:

```go
// internal/engine/schema.go:79-86
type CustomExtractor struct {
    // Module is a legacy module path retained for YAML compatibility, unused in Go.
    Module      string `yaml:"module"`
    Function    string `yaml:"function"`
    Description string `yaml:"description"`
}
```

**222** YAML rule files still carry `custom_extractors:` blocks. The field
`FrameworkRule.CustomExtractors` is **read nowhere in the Go codebase** — verified:
`grep -rn '\.CustomExtractors' internal cmd` returns zero hits outside the struct
definition. The blocks are parsed by `yaml.Unmarshal` in
`internal/engine/loader.go:87` and silently discarded.

Meanwhile the rich extraction those blocks once described now lives in a **second,
imperative Go pipeline**. The question this ADR settles: **rescue** the pluggable
hook, **remove** the dead blocks, or **extend** the declarative engine so future
coverage is authored declaratively?

---

## The two pipelines (with evidence)

Both pipelines run per-file inside one extraction subprocess
(`internal/daemon/extract/subproc.go`) and **emit into the same envelope stream that
builds the graph**. They are not isolated — they are layered passes feeding one
graph, deduped downstream.

`subproc.go` runs, in order, per file:

| Pass | What | Mechanism | Evidence |
|---|---|---|---|
| Pass 1 | Base language entities (tree-sitter) | Go | `subproc.go:~330` |
| Pass 2 | Custom/framework extractors | **Go registry** | `subproc.go:359` → `extractors.RunCustomExtractors` |
| Pass 2.5 | YAML framework rules + ~50 edge passes | **declarative engine + Go passes** | `subproc.go:406` → `detector.Detect` |
| Pass 3 | Cross-language extractors | Go | `subproc.go:421` |

### Pipeline A — the declarative rules engine (`internal/engine/`)

- **Loader** (`loader.go`): `//go:embed all:rules`; walks `rules/<lang>/{frameworks,orms,queues}/*.yaml` into `map[lang][]FrameworkRule`.
- **Schema** (`schema.go`): four blocks — `file_conventions`, `source_patterns`, `relationship_rules`, `custom_extractors` (dead).
- **Detector** (`detector.go`): compiles regexes once (`compile()`), then `Detect()` applies them.

**What the declarative schema can express today:**

| Capability | Block | Reach |
|---|---|---|
| Tag a file by glob → entity | `file_conventions` (`name_from: filename / parent_dir / class_name`) | file-granular only; cannot emit multi-field property bags (`detector.go:319` comment says so explicitly) |
| Regex → entity, name from a capture group | `source_patterns` (`scope: file/line`, `name_group`) | one entity per match; single name group; no computed props beyond `framework`/`pattern_type` |
| Regex → directed edge between two captured names | `relationship_rules` (`source_group`/`target_group`) | flat 2-name capture; both endpoints must appear in **one regex match on one file** |

**Hard limits, from the code itself:**
- No cross-file reasoning (each `Detect` call sees one file; `Pass1Entities`/`CrossFileFields` are injected by Go, not expressible in YAML — `subproc.go:377-392`).
- No lexical scope / prefix composition. `gin.yaml` emits bare `Route:/path`; resolving the group prefix and binding the receiver to a qualified handler is done by the **Go pass** `applyGoRouteComposition` (`detector.go:483`). Same story for Spring (`applySpringRouteComposition`), Django (`applyDjangoRouteComposition`).
- No conditionals, no computed properties, no AST access.

**Liveness:** of 767 rule files, **163 carry live `source_patterns`**, 160 `relationship_rules`, 165 `file_conventions`. So the declarative path is **genuinely live and broadly used** (all major languages), not vestigial — but it only ever produces *coarse* entities. Everything semantically rich is bolted on in Go.

### Pipeline B — the Go-registered extractor registry (`internal/custom/`, `internal/extractors/`)

- **Dispatch** (`internal/extractors/custom_dispatch.go`): `RunCustomExtractors` looks up extractors by registry-key prefix (`customPrefixForLanguage`: `python_`, `custom_go_`, `custom_js_`, …) and fans out, each in a panic-recovery wrapper.
- **Registry:** **267** registered `Register("custom_<lang>_…")` keys; **359** Go extractor source files across 15 language trees (`internal/custom/{golang,python,java,…}`).
- **Engine edge passes:** **29** `*_edges.go` files (~**19,850** lines) wired as ~50 sequential `applyPass(...)` calls in `detector.go` (Kafka, gRPC, K8s, CDK, Pulsar, EventBridge, …).

This is where **all recent grind has gone** (e.g. `ef5c56fa6` "wire 20 orphaned Ruby+PHP framework extractors", `2939e7c1a` "add Lighthouse + API Platform extractors", `df98cbd4d` "rdkafka + lapin + sea-query records").

### Are the two pipelines redundant, complementary, or both?

**Both — and that is the core finding.**

- They are **complementary in the live system**: Pass 2.5 declarative rules emit coarse anchor entities (`Route:/users`), and the Go passes/extractors emit the rich version and the edges (`gin.go` resolves bind/validate/group; `applyGoRouteComposition` rewrites the edge target). Neither is a strict superset of the other *today*.
- They are **redundant in intent** at the `custom_extractors` boundary: the **dead** `custom_extractors` block in `gin.yaml` literally describes what `internal/custom/golang/gin.go` (165 LoC) + `applyGoRouteComposition` now do:

  > `gin.yaml` custom_extractors description: *"Extract Gin routes … route groups … with full path resolution, middleware chains, request binding (c.ShouldBindJSON), engine creation, custom validators (RegisterValidation), … Emits OWNS, DEPENDS_ON, CALLS relationships."*

  That paragraph is a **spec for `gin.go`**. The escape-hatch was re-implemented imperatively in Go and the YAML pointer was orphaned.

So: the **declarative `source_patterns` path is live and complementary**; the **`custom_extractors` path is dead and was functionally replaced by Pipeline B**.

---

## What the escape-hatch provided

Reading the surviving `custom_extractors` descriptions (gin, echo, GORM, …), the
hook bought exactly what the declarative schema still cannot:

- **Full path / prefix resolution** (route groups, nested routers) — needs lexical scope.
- **Request/response binding & validation detection** (`c.ShouldBindJSON`, `RegisterValidation`) — needs call-graph awareness.
- **Multi-field property bags** (GORM model → associations, hooks, scopes, table names, connections) — the schema can only attach `framework`/`pattern_type`.
- **Computed / cross-file edges** (OWNS, DEPENDS_ON, CALLS spanning declarations).

All four are now delivered by Pipeline B (Go extractors + edge passes). **Nothing
live is lost by deleting the dead blocks** — they are pointers to Python modules
that do not exist in this repo and are never dereferenced.

---

## Quantifying "could the recent Go grind have been declarative?"

A representative read of the Go extractors and edge passes:

- **The 80% regular case** — "regex on one file → entity / 2-endpoint edge" — *is* already what `source_patterns`/`relationship_rules` do. A meaningful slice of simple framework records (e.g. flat route-decorator detection, simple ORM-model class tagging) **could have been YAML rules**. Rough estimate from sampling `internal/custom/*` framework files: **~30–40%** of the *simpler* framework extractors are regex-shaped and within the current declarative schema's reach.
- **The 20% hard case** — the ~50 `applyPass` edge synthesizers (Kafka/gRPC/K8s/CDK/route-composition) — are **fundamentally beyond** the current schema: cross-file rendezvous, ID canonicalization, scope composition, JSON/manifest parsing. These **must** be Go.

So the team is **partially reinventing imperatively what a slightly-richer engine
could express declaratively** — but a large, irreducible core genuinely needs Go.

---

## Options

### Option 1 — RESCUE (rebuild the pluggable hook in Go)

Build a Go-native dispatch: YAML `custom_extractors: {function: "gin_extract"}` → a
registered Go callback, reconnecting the 222 files.

- **Effort:** Medium-High. New registry keyed by `function` name + a dispatch in `Detect`; then **port/route the 222 blocks** to existing Go extractors and **re-validate 222 stale specs** against current code.
- **Payoff:** Low/negative. The callbacks the blocks point to **already run** via Pipeline B's prefix dispatch. Rescuing the YAML pointer adds a **second, parallel dispatch path to the same Go functions** — pure indirection.
- **Risk:** High. Two mechanisms reach the same extractors; coverage-citation ambiguity (which path "counts"?) — the exact failure mode of #3585/#3586. **Reject.**

### Option 2 — REMOVE (delete the dead blocks + schema field)

Strip the 222 `custom_extractors:` blocks and the `CustomExtractors` schema field;
keep `source_patterns`/`relationship_rules`/`file_conventions`; standardize rich
extraction on the Go registry.

- **Effort:** Low. Mechanical: drop the field (`schema.go`), drop 222 YAML blocks, add a **cite-validity guard** so coverage tooling can never again cite a `custom_extractors` description as evidence (mirrors the #3586 fix for the dead Java layer).
- **Cost:** None live is lost — the blocks are already dead (`grep` proves zero readers). The *documentation value* of the descriptions (they're decent specs) should be preserved by migrating the useful prose into the corresponding Go extractor's doc-comment before deletion.
- **Risk:** Low. The only real risk is throwing away the spec prose; mitigated by the migrate-then-delete step.

### Option 3 — EXTEND (invest in the declarative engine)

Grow `source_patterns`/`relationship_rules` + a small set of **thin Go primitives**
so the regular 80% of *new* framework coverage is authored as YAML, shrinking the
manual-Go grind; keep Go for the hard 20%.

- **Missing primitives** (none exist in today's schema): `prefix_compose` (group/router prefix resolution), `multi_prop` (capture N named props into one entity), `cross_file_ref` (resolve a captured name against a Pass-1 entity index — the plumbing already exists in `subproc.go`, just not exposed to YAML), and a `rendezvous_id` template for cross-repo edge keys.
- **Effort:** High up-front (design + build ~4 primitives + a richer rule schema + validation), **but amortized**: the grind goal is 100+ more extraction tickets.
- **Payoff:** Each new *regular* framework becomes a reviewed YAML diff, not 150 LoC of Go — cheaper, more uniform, lower bug-rate, and self-documenting.
- **Risk:** Medium. Schema churn; risk of a half-built DSL that's neither expressive enough nor simple. Must be scoped to the **80% case only**, with Go as the explicit escape hatch.

---

## Recommendation

**Adopt a hybrid: REMOVE now, then EXTEND deliberately. Do NOT rescue.**

Reasoning, grounded in evidence:

1. **Rescue is strictly dominated.** The Go functions the 222 blocks point to already
   execute via Pipeline B's prefix dispatch (`custom_dispatch.go`). A second dispatch
   path adds indirection and re-creates the coverage-citation hazard that #3585/#3586
   just spent effort cleaning up. Reject.

2. **Remove is unambiguously correct and cheap.** `FrameworkRule.CustomExtractors`
   has **zero readers** (`grep` proven). 222 dead blocks (92 files have *nothing but*
   the dead block) are dead weight and an active mis-citation magnet — the same class
   of liability as the dead Java pattern layer that produced 270 false coverage cells.
   Deleting them loses **nothing live**.

3. **Extend is the right long-term direction** because the live declarative path is
   already broad (163 files, all major languages) but only emits coarse entities,
   forcing every rich case into ~20k lines of Go. With 100+ extraction tickets ahead,
   pushing the **regular 80%** into a slightly richer declarative schema (4 new
   primitives) makes the grind cheaper, more uniform, lower-bug, and self-documenting,
   while Go remains the honest escape hatch for the irreducible 20% (cross-file
   rendezvous, manifest parsing, scope composition).

The hybrid — **declarative-first for the regular case, Go escape-hatch for the
hard case** — is what the system is *already* groping toward; this ADR makes it
intentional and removes the dead middle layer that confuses the picture.

---

## Phased plan

**Phase 0 — Decide & guard (small).**
- Accept this ADR; renumber into `docs/adrs/`.
- Add a coverage cite-validity guard: a `custom_extractors` description may never be cited as coverage evidence (extend the #3586 orphan-cite check).

**Phase 1 — REMOVE (mechanical, reversible).**
- For each of the 222 blocks, migrate any useful description prose into the doc-comment of the corresponding Go extractor (e.g. `gin.yaml` desc → `internal/custom/golang/gin.go` header).
- Delete the 222 `custom_extractors:` blocks and the `CustomExtractor` struct + `FrameworkRule.CustomExtractors` field (`schema.go`).
- Confirm `engine` tests + `loader_test.go` still pass; re-run coverage `validate`/`fmt --check`.

**Phase 2 — EXTEND design spike (1 framework, end-to-end).**
- Pick one *regular* framework currently done in Go (e.g. a simple route-decorator framework).
- Implement two primitives first: `multi_prop` (named-group → property bag) and `cross_file_ref` (resolve a captured name against `Pass1Entities`, plumbing already in `subproc.go`).
- Re-author that framework as YAML rules + the new primitives; A/B the graph output against the Go version. Acceptance = parity on entities/edges with fewer LoC.

**Phase 3 — EXTEND rollout (gated).**
- Add `prefix_compose` and `rendezvous_id` primitives.
- Establish a rule-of-thumb: new framework coverage is authored as YAML **unless** it needs cross-file rendezvous / manifest parsing / scope composition, in which case Go. Document the decision tree next to the rule schema.
- Migrate the *simplest* existing Go extractors opportunistically (no big-bang rewrite).

**Phase 4 — Maintainability guardrails.**
- Coverage tooling distinguishes "declarative rule" vs "Go extractor" as the cite source, so the two pipelines never blur again.
- Track the share of *new* framework tickets landed declaratively vs imperatively as a health metric.

---

## Consequences

- **Positive:** dead weight removed; coverage citations honest; future grind cheaper and more uniform; the declarative/imperative boundary becomes intentional and documented.
- **Negative:** Phase 2–3 is real engineering investment; risk of an under-powered DSL if scope creeps past the 80% case (mitigated by the explicit Go escape hatch and parity-gated rollout).
- **Neutral:** Pipeline B (Go registry) remains the backbone for hard cases regardless.
