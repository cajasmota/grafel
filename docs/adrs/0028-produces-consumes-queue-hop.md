# ADR-0028: PRODUCES / CONSUMES model the queue hop, and supplement CALLS

- **Status**: Accepted
- **Date**: 2026-09-02
- **Issue**: #6741 (Arm 1 — blocking; Arms 2-5 are written against this decision)
- **Deciders**: Jorge Cajas
- **Amends**: ADR-0003 (see its 2026-09-02 amendment)

## Context

Five golden fixtures — `csharp-hangfire-mini`, `csharp-quartz-net-mini`, `java-quartz-mini`, `python-dramatiq-mini`, `python-rq-mini` — carried the identical sentence at `expected.json:4`: *"Used by the extraction-quality benchmark to verify PRODUCES/CONSUMES edge emission for …"*, and `docs/coverage/detail/msg.hangfire-recurring.md` marked Producer extraction **full**, asserting "All PRODUCES carrying `task:hangfire:<Type>.<Method>`". (Arm 5 rewrote all six; see the amendment at the end of this ADR.)

None of it was true. Grounding for #6741 established:

- **No `PRODUCES` or `CONSUMES` relationship kind existed.** `internal/types/kinds.go` declared neither; the nearest name, `CONSUMES_API`, is a different semantic (same-file HTTP client → endpoint).
- The C# and Python passes (`custom/csharp/hangfire.go`, `custom/csharp/quartz_net.go`, `custom/csharp/handle_messages.go`, `custom/python/dramatiq.go`, `custom/python/rq.go`) set `edge_kind: "PRODUCES"` as an **entity property**. They contain zero `addRel` / `Relationship{` calls. It is inert metadata, not an edge.
- Exactly one site emits a real edge of this kind: `internal/custom/java/quartz.go:274`. It is same-file-only, and in `java-quartz-mini` the producers and consumers live in different files, so it never fires there either.
- Four consumers already traverse both strings: `internal/mcp/flow_tools.go`, `internal/mcp/dead_code.go`, `internal/links/reachability.go`, `internal/dashboard/topology_compound.go`.
- Nothing rejected the undeclared string, because `IsValidRelationshipKind` has zero non-test callers (#6757 wires it, and must land *after* this ADR or it would drop `java/quartz.go`'s edge).

So the vocabulary had to be declared. The question that blocked everything else is what the edges **mean**, because nothing in the repo recorded it and each later arm implements a different language against the answer.

The concrete case: `BackgroundJob.Enqueue(() => EmailService.SendConfirmation(orderId))` already yields a `CALLS` edge from the enclosing method to `SendConfirmation`, emitted by the generic C# extractor. Does `PRODUCES` **supplement** that edge, or **replace** it with one modelling the queue hop?

## Decision

### 1. Endpoints and direction

`PRODUCES` and `CONSUMES` are two hops of one path, joined on a shared **work-unit** node — the job/task/message the queue carries, addressed `task:<framework>:<id>` (e.g. `task:hangfire:EmailService.SendConfirmation`, the address the inert `edge_kind` properties already imply):

```
producing site --PRODUCES--> work unit --CONSUMES--> handler
```

- **`PRODUCES`**: the enclosing operation of the dispatch call — or the framework producer entity, such as a Quartz `job_builder` — → the work unit.
- **`CONSUMES`**: the work unit → the handler that executes it.

`CONSUMES` points *away* from the queue, not back at it. This is deliberate and matches `TRIGGERS` (`ScheduledJob` → handler) and `DELIVERS_TO` (topic → handler). The alternative reading — handler `CONSUMES` queue — is the more natural English, and it is wrong for this graph: it makes the path un-walkable, so a reachability BFS from a producer would never reach the handler and every queue-only handler would be reported as dead code by `internal/links/reachability.go` and `internal/mcp/dead_code.go`. Both already list `PRODUCES` and `CONSUMES` as forward reachability edges; the direction above is what makes that listing correct.

Where a pass mints no separate work-unit node and the job class *is* the work unit, the one-hop form (producer → job class) that `java/quartz.go:274` already emits is a valid degenerate case. Whether Arm 2 normalises it is Arm 2's call; this ADR does not require churn on a shipped edge.

### 2. `PRODUCES` supplements `CALLS`. It never replaces it.

A framework pass emits `PRODUCES` in addition to whatever `CALLS` the generic extractor derived. It does not suppress, rewrite, or delete that edge.

Reasoning, in the order that decided it:

**The user's question settles it.** "What happens when `PlaceOrder` runs?" has two correct answers that must both be visible: *the code names `SendConfirmation`* (a static fact about this file, which `CALLS` records) and *that invocation is deferred through a queue* — it happens later, in another process, possibly retried, possibly never. `CALLS` cannot express the second; that is exactly what `PRODUCES` adds. Replacing `CALLS` would trade one of those facts for the other, and would answer "what happens when X runs?" by deleting the part the source code literally says.

**It is not merely an annotation on `CALLS`.** The two do not always coincide: `JobBuilder.newJob(SendEmailJob.class)` and string- or type-keyed dispatch produce a `PRODUCES` hop where there is no `CALLS` edge at all, because no method is named at the call site. So `PRODUCES` must be emitted independently. Where it does coincide with `CALLS`, the endpoints are the same but the claims are not, and duplicating the *path* is the price of not losing the *distinction*.

**Replacement is not a pass's to make.** `CALLS` is emitted by the generic language extractor; a framework pass deleting another producer's edge has no precedent here, and the passes are append-only by construction. `CONSUMES_API` states the same stance about its own overlap with `CALLS`→`ExternalEndpoint`: *"complementary to, not a duplicate of"*.

**Supplement is recoverable; replacement is not.** With both edges, a consumer wanting only synchronous flow filters to `CALLS`, and one wanting "reaches at all" walks both — every existing `CALLS` consumer keeps working unchanged. With replacement, every consumer that does not yet know about `PRODUCES` silently loses a call it used to see: `find_callers` on `SendConfirmation`, impact radius, the call graph. A supplement can be collapsed later by any consumer that wants to; a replacement cannot be un-deleted.

**The losing option, stated plainly.** *Replace: the enqueue site does not really call the job method, it hands a description of the call to a queue; a `CALLS` edge there is a lie about control flow, and keeping both duplicates a path the graph can already walk.* This is a real argument, and it loses on cost: the duplication it objects to is one extra edge per dispatch site, while the precision it buys is paid for by breaking every unqualified `CALLS` consumer in the tree and discarding a fact the source text states outright. The deferral is better carried *on* the graph as an extra, differently-typed edge than *by removing* an existing one.

### 3. Not a second `ENQUEUES`

`ENQUEUES` (caller → `SCOPE.ScheduledJob`) with `TRIGGERS` (`ScheduledJob` → handler) is the same shape and is already complete for the Sidekiq/Resque/Que topology. **A pass that emits `ENQUEUES` for a hop must not also emit `PRODUCES` for that hop.** One hop, one pair. Where a pass currently emits neither pair consistently — Arm 4 records `dramatiq` yielding `REFERENCES` where `rq` yields `ENQUEUES` for the same producer shape — the fix is to pick one pair for that hop, not to add a third edge alongside them.

### 4. `CONSUMES_QUEUE` stays invalid

ADR-0003's "closed enum" listed `CONSUMES_QUEUE`, `TRIGGERS_LAMBDA`, `READS_TABLE`, `WRITES_TABLE`; `internal/types/kinds_test.go` asserted all four must **not** be valid. The test is right and the ADR was wrong; ADR-0003's amendment records why and what shipped in their place. Declaring the sketch names to make the prose true would re-create the exact defect #6741 is about: a documented edge kind no producer emits.

## Consequences for Arms 3-4 (the C#/Python emitters)

These are the constraints those arms are written against:

1. **Emit real relationships.** `addRel(&result, seenRels, Relationship{...})`, as `java/quartz.go:274` does. Do **not** keep satisfying the requirement with an `edge_kind` entity property — that property is what made two fixtures pass for months without the edge they claim to verify. Arm 5 decides whether the now-redundant property is removed.
2. **Emit both halves, on the address the pair joins on.** `PRODUCES` → work unit and `CONSUMES` → handler, sharing one `task:<framework>:<id>` address. A producer half alone leaves the consumer an island, which is the state #6741 found.
3. **Leave the `CALLS` edge alone.** Do not suppress the lambda-body call. Expect the duplicate path; it is intended.
4. **Do not emit `PRODUCES` where the pass already emits `ENQUEUES`** for the same hop (§3).
5. **Every new edge kind must be in `AllRelationshipKinds()`.** #6757 makes `IsValidRelationshipKind` load-bearing; a constant declared but not registered will start dropping edges once it lands.
6. **The mutant test is the acceptance bar**, not a green fixture: removing the emission must fail a test. The fixtures' `nice_to_have` PRODUCES/CONSUMES rows, written from the source by #6740, are the specification, and are promoted to `must_exist` as each arm lands.

## Alternatives considered

- **Replace `CALLS` with `PRODUCES`** — rejected; see §2 for the full argument and its cost.
- **Keep `PRODUCES` as an entity property and teach consumers to read it** — rejected: four consumers already traverse it as an *edge kind*, edges are what reachability, `find_callers` and the topology dashboard walk, and a property on one endpoint cannot express a two-node relationship at all.
- **Reuse `ENQUEUES`/`TRIGGERS` for every queue framework and never declare `PRODUCES`** — genuinely tempting, and it would have been the smaller change. Rejected because one emitter already ships `PRODUCES` edges, four consumers hard-code both strings, five fixtures and a coverage doc promise them, and `ENQUEUES` is documented as targeting `SCOPE.ScheduledJob` specifically, which is not the entity the message-broker and Hangfire-style passes mint. Renaming the shipped edge is a migration with no payoff over declaring the kind that already exists in the tree.
- **Add `CONSUMES_QUEUE`, `TRIGGERS_LAMBDA`, `READS_TABLE`, `WRITES_TABLE` so ADR-0003 reads true** — rejected; §4.

## What actually shipped (Arms 2-5, 2026-09-02) — the language split, and the CONSUMES gap

This section is the amendment Arm 5 owes a reader who sees `PRODUCES` in Java and `ENQUEUES` in Python
and wants to know whether that is deliberate. It is. Measured against the tree at Arm 5, not asserted
from the plan above.

### The split

| Dispatch shape | Target | Edge pair | Emitters |
|---|---|---|---|
| **Scheduler-style** — a builder/dispatch site names a job **class** | the job class | `PRODUCES` (one-hop degenerate form, §1) | `internal/custom/java/quartz.go` (Quartz Java), `internal/custom/csharp/hangfire.go` + `quartz_net.go` via `csJobProducesEdge` (Hangfire, Quartz.NET) |
| **Queue-style** — an enqueue site names a **function** | the function | `ENQUEUES` (+ `TRIGGERS` where a `SCOPE.ScheduledJob` exists) | `internal/engine/scheduled_jobs_edges.go` (Python RQ and dramatiq, Sidekiq, Resque, Go asynq) |

The line is the **target**, not the language. §3 is what forces the choice: one hop gets one pair, so a
hop already covered by `ENQUEUES` may not also carry `PRODUCES`. Arm 4 found that RQ's producer regexes
scan the *same call sites* `synthesizeRQEnqueueEdges` already walks, hop for hop, and that a dramatiq
actor is a plain function — so both Python frameworks settle on `ENQUEUES`, and **no `PRODUCES` edge is
emitted for Python at all**. Arms 2 and 3 checked the same question for Java and C# and found no
`ENQUEUES` on those hops, so `PRODUCES` is free there.

A new framework pass picks its side by asking what its dispatch site names — a class, or a function —
and by checking `scheduled_jobs_edges.go` for an existing `ENQUEUES` on that hop before adding anything.

### `CONSUMES` is declared and emitted by nothing. That is a known gap, not an oversight.

`RelationshipKindConsumes` is declared (`internal/types/kinds.go:751`), registered in
`AllRelationshipKinds()` (`:1642`), and read by four consumers (`internal/mcp/dead_code.go`,
`internal/mcp/flow_tools.go`, `internal/dashboard/topology_compound.go`, `internal/links/reachability.go`).
**No producer emits it**, in any language. Verified at Arm 5 by grepping the string across `internal/` and `cmd/`: outside `kinds.go`, those four
consumers and test files, every occurrence was an entity *property*, and Arm 5 deleted the ones in the
five passes behind #6741's fixtures.

It was left unbuilt deliberately. The one-hop degenerate form Arms 2-3 emit has no work-unit node for a
`CONSUMES` edge to originate from, so emitting it needs the two-hop form of §1 — minting work-unit
entities that do not exist today, across two language families, plus re-baselining both fixture sets.
That is a modelling change, and it should be motivated by wanting a queue-hop node in the graph, not by
wanting a sentence to read true. Retiring the kind instead was considered and rejected: it forecloses the
two-hop model, and `CONSUMES` is the more useful half for *"what handles this queue?"*.

Consequences a reader should expect while the gap stands:

- A queue-only handler is reachable from its producer **only** via `PRODUCES`/`ENQUEUES`/`TRIGGERS`. The
  `CONSUMES` entry in the reachability and dead-code edge sets is inert.
- `internal/quality/golden/python-rq-mini` keeps its `Worker … CONSUMES … send_email` row as
  `nice_to_have`, deliberately **unmet**, so the gap stays visible in the benchmark rather than
  disappearing from it. Deleting that row would re-create the defect #6741 is about.

### The inert `edge_kind` entity property (Arm 5)

Arms 2-4's passes stamped `edge_kind: "PRODUCES"` / `"CONSUMES"` as an entity property. Nothing ever read
it, and it is what let two fixtures claim for months to verify an edge kind that did not exist. Arm 5
removed it from the five passes behind #6741's five fixtures (`java/quartz.go`, `csharp/hangfire.go`,
`csharp/quartz_net.go`, `python/dramatiq.go`, `python/rq.go`): false in Python and on unresolved C#
producers, duplicative at best elsewhere. **A framework pass must not record an intended edge as an entity
property.** Emit the edge, or record the gap in prose where a reader will find it.

### Completing the sweep (#6767)

#6741 listed six more carriers as out of scope: `csharp/coravel.go`, `csharp/masstransit.go`,
`csharp/mediatr.go`, `csharp/wolverine.go`, `csharp/handle_messages.go` (`PRODUCES`/`CONSUMES`) and
`python/dramatiq.go`'s routing entities (`ROUTES_TO`). #6767 swept them. The grounded population was
**26 sites** — 24 across the five C# files and 2 in dramatiq — and the five files named in the issue were
in fact the whole C# set. `internal/custom/csharp/produces_edges_6767_test.go` now walks all of
`internal/custom` and fails on any re-introduction, in any language.

**PRODUCES became real for every C# dispatch site that names its target.** The split #6741 settled is by
target SHAPE, not by framework: `PRODUCES` for dispatch onto a **class**, `ENQUEUES` for dispatch onto a
**function**. Every site in these five passes names a class — a Coravel `IInvocable`, or a message
contract constructed inline (`Publish(new OrderSubmitted{…})`) — so all five take `PRODUCES`, emitted via
`csJobProducesEdge` (job-class target, `job_class` property) or its new sibling `csMessageProducesEdge`
(message-contract target, `message_type` property).

**§3's double came from the passes themselves, not from the engine.** No engine synthesis mentions
MassTransit / MediatR / Wolverine / NServiceBus / Coravel (zero hits), and `csharp/redis.go`'s
`PUBLISHES_TO` needs a string-literal channel argument, so `.Publish(new T())` cannot reach it. But
MassTransit, MediatR and NServiceBus/Rebus spell `.Publish(new T())` and `.Send(new T())` identically —
`mtSendRe` and `mxSendRe` are the same regex, byte for byte — MediatR had **no signal gate at all**, and
Coravel's mailer matches the same bytes for a `Mailable`-suffixed type. The first cut of #6767 therefore
turned duplicate inert entities into duplicate edges: two `PRODUCES` for one hop, on an ordinary
NServiceBus handler. Per-pass signal gates are necessary but **not sufficient** — a MassTransit consumer
that dispatches through `IMediator` satisfies both gates truthfully. `csSharedDispatchVerbOwner` names one
owner per file (NServiceBus > MassTransit > MediatR > Coravel's mailer, most specific marker first), and
it deliberately under-reports the MediatR hop in a mixed file: an absent edge is a gap a reader can see,
while a doubled edge silently corrupts any traversal that counts hops. Graded by
`TestSharedDispatchVerbsEmitOnePRODUCESPerHop`, which runs **every** producing C# pass over one source —
the axis the per-pass tests cannot see, since each of those drives a single pass.

**Two Coravel forms emit nothing, deliberately.** `Schedule(() => …)` and `QueueAsyncTask(…)` name no
invocable. They keep their honest unresolved producer entity and take no edge — the same guard
`hangfire.go` applies to `enqueue_dynamic` / `recurring_dynamic`. Recall is structurally blind to an
over-broad emitter, so the exclusion is pinned negatively, by
`TestCoravelAnonymousDispatchEmitsNoProduces` — whose fixture deliberately DECLARES an `IInvocable`
alongside the anonymous dispatch, so an emitter widened to "reach for the nearest invocable" is caught
rather than merely finding nothing to reach for.

**dramatiq's `ROUTES_TO` property is removed and no edge replaces it.** Section 7's call site
(`actor.send_with_options(…)`) is already paired by a real `ENQUEUES` edge from
`synthesizeDramatiqSendEdges`, so a second edge would be exactly the §3 double edge Arm 4 refused for this
pass; section 6's decorator names no second node at all — the queue is a string, not an entity, and the
routing entity is already named after the actor. The routing fact survives on `queue_name` + `actor`.

**CONSUMES is still emitted by nothing**, so every consumer half of these five passes is now edgeless and
says so, in the pass and in `docs/coverage/registry.json`. The property was not harmless redundancy on
these carriers: of the 26 sites, **14 were outright false** — the 10 `CONSUMES`
stamps, dramatiq's 2 `ROUTES_TO` stamps and the 2 anonymous-Coravel `PRODUCES` stamps all named an edge
that existed nowhere in the tree. The other 12 were merely duplicative, and only became so once #6767
made their edge real.
