# Provenance and licence — proto-mini fixture

**Both files under `src/` are hand-written for this fixture.** No third-party
proto is committed here.

That is a deliberate departure from `vbnet-mini`'s rule ("a fixture built from
invented source grades the fixture author's model of the language, not the
extractor"), and the reason is specific rather than convenient:

- Proto3 is a small declarative IDL. There is no control flow, no dispatch, no
  member access — the shapes this extractor reads are `message`, `enum`,
  `field`, `service`, `rpc`, `import`. Twenty lines of hand-written proto3
  exercise the same grammar productions as two hundred lines of real proto3;
  the risk vbnet-mini's rule guards against (an author's *model* of a complex
  language diverging from the language) has almost no surface here.
- The central assertion **cannot be sourced from a real project at all**. It
  requires an `rpc` and a `message` that share a name in the same file
  (`message User` + `rpc User`), which is legal proto but something no sane
  API author writes. It is the only shape under which #6422's reparenting
  occurs, so a fixture built from real-world proto would grade green with the
  defect fully present — worse than having no fixture.

Copyright: none asserted; these two files are part of this repository.

## What this fixture grades

| Shape | Defect it watches | Graded by |
|---|---|---|
| `service` → `rpc` CONTAINS, on a colliding and a non-colliding name | #6422 (that the fix did not move the problem into the service arm) | 2 expected rows + forbidden **F2** |
| `rpc` → request/response `message` REFERENCES | #6359 / #6419 | 3 expected rows |
| `message` → field-type REFERENCES, same file | #6419 | 1 expected row |
| `message` → field, `enum` → enum value CONTAINS, **including each enum's first (zero-default) value** | structural containment | 10 expected rows |
| `rpc` type refs are REFERENCES and nothing else — the pre-#6359 `IMPORTS` shape does not come back *alongside* the correct edge | #6359 | forbidden **F3** |
| cross-file `message` type reference emits **no** dangling edge | #6357 | forbidden **F1** |

Every one of those rows was verified load-bearing by mutating the production
code and confirming the fixture goes red. See the issue #6453 thread for the
mutation table.

Two of those rows exist because of a *widening* mutant rather than a narrowing
one, and the distinction is worth keeping in mind when adding rows here:

- **F3** — the three `rpc → message` REFERENCES rows only catch that edge
  *disappearing*. A `buildRPC` that emits a spurious `IMPORTS` edge to each
  request/response type **alongside** the correct REFERENCES — a near-literal
  return of the pre-#6359 shape — took relationships from 53 to 56 and left the
  fixture at 100% / 100% / 0 forbidden. F3 is what makes that mutant die.
- **`Role.ROLE_UNKNOWN` / `Status.STATUS_UNKNOWN`** — the enum rows were
  one-value-deep. Dropping the *first* `enum_field` of every enum (the natural
  off-by-one over `enum_body`'s children) took entities 29 → 27 with the fixture
  still green. In proto3 the first value is the enum's mandatory zero default,
  so that is the one value silently losing which matters most.

## One shape that is present, unasserted, and deliberately NOT pinned here

`internal/extractors/cross/endpoint` emits two dangling `SERVES` edges into this
fixture's graph:

    UNARY /UserService/User       --SERVES--> RAW[scope:operation:user.proto#UserService.User]
    UNARY /UserService/GetProfile --SERVES--> RAW[scope:operation:user.proto#UserService.GetProfile]

They are the same #6298/#6357 dangling class F1 polices, and they really are the
only forbidden-looking shape in this graph that no row covers. They are still not
pinned, for one reason: **the edges exist today.** A forbidden row spelling them
would hit on the very first run and leave the fixture permanently red — it would
be a bug report written as a test, not a regression guard, and this fixture's
whole discipline is that no row is added that has not been *seen* to fail on a
mutant and pass on clean code. Nor can they be pinned as *expected* rows: the
`to` side resolves to nothing, so an expected row would bless the dangling
target as the correct answer, which is exactly what #6441 warns against.

The right home for them is a fix in the gRPC endpoint cross-extractor (the
handler qname `UserService.User` is built in the operation address space while
the proto rpc entity is addressed as bare `User` — the same collision this
fixture exists for). When that lands, add the two `SERVES` rows as **expected**,
with `to_name`/`to_kind` naming the rpc entities.

## The one shape this fixture CANNOT grade — read this before adding rows for it

**The `file → message` / `file → enum` CONTAINS edge — the literal subject of
#6422 — is invisible to the golden harness, and no row here asserts it.**

`fileContainsRel` (internal/extractors/proto/proto.go) sets `FromID` to the
raw file path on purpose, and the proto extractor emits **no per-file carrier
entity**. Unlike Java, where the file itself is a `SCOPE.Component` that
`expected.json` can name as `from_name`, nothing in a proto graph is named
`user.proto`, so the resolver leaves those edges dangling on the FROM side
(the known #6298 offender, `internal/extractors/file_anchored_rels_guard_test.go`).
`internal/quality/diff.go`'s `resolveExpectedEdge` needs at least one resolvable
`from` candidate before it can match anything — there is no `from_bare_name` —
so any row spelling this edge would be **unsatisfiable**, i.e. red forever and
telling you nothing. Measured: reverting `fileContainsSchemaRel` to
`BuildOperationStructuralRef` (the exact pre-#6422 defect) leaves this fixture
at **18/18 entities, 16/16 relationships, 0 forbidden hits** — completely green.

So the load-bearing guard for that half remains
`TestFileContains_MessageIsNotReparentedOntoTheRPC_6422` and its siblings in
`internal/extractors/proto/file_contains_6422_test.go`, which run at the
extractor level and *do* fail on that mutant (verified). This fixture covers
the surrounding shapes and, crucially, keeps the collision in `user.proto` so
that F2 and the `UserService → User (SCOPE.Operation)` row have something to
bite on.

**If a future change gives proto a per-file carrier entity** (or #6298 is
otherwise closed), add the two rows this fixture is missing:
`user.proto --[CONTAINS]--> User (SCOPE.Schema)` and a forbidden
`user.proto --[CONTAINS]--> User (SCOPE.Operation)`. The source already
contains everything they need.

## #6459 (`SCOPE.Service` reachable from the operation address space) is NOT flipped here

**This fixture cannot observe #6459, before or after.** The `file → service`
CONTAINS in `user.proto` reaches its service through the kind-agnostic
`byLocation` fallback, and that fallback succeeds here because no other entity
in `user.proto` is named `UserService`. The bug #6459 fixes is what happens when
that name DOES collide — so a fixture without a collision on the service name
returns the same answer either way.

Confirmed by measurement rather than assumed: grading this fixture immediately
before and after the resolver change produced byte-identical output — 18/18
entities, 16/16 relationships, 0 forbidden hits, and the same
`resolver: rewrote=27 ambiguous=0 unmatched=7` line. The fixture served as the
regression net for that change, not as its demonstration; the demonstration is
the constructed collision in
`internal/resolve/proto_service_family_6459_test.go`.

**Where the admission lives (#6492).** `SCOPE.Service` is NOT in
`internal/resolve/refs.go`'s shared `operationKindFamily`. That slice also feeds
`hintKinds` and the `familyMaskByKind` leaf-name filter, and `SCOPE.Service` is
emitted by ~60 non-proto sites — several of which name the entity after a
function or class in the same file (celery/dramatiq task markers, Spring
stereotypes). Because a family match must be UNIQUE, admitting it there
destroys those bindings rather than adding any. The kind is admitted only by
`protoOperationKindFamily`, reached from
`structuralKindFamilies("operation", "proto")` — the proto language segment of
the structural ref. No row here asserts anything about it, and neither #6459 nor
#6492 should expect this fixture's numbers to move.

The fixture that DOES observe the difference is `python-dramatiq-mini`:
`src/workers/billing.py` declares `@dramatiq.actor def charge_card`, and
`internal/custom/python/dramatiq.go` mints a `SCOPE.Service` marker at the same
(file, name) as the function's `SCOPE.Operation`. Graded with the shared-slice
admission its resolver line degrades from
`resolver: rewrote=11 ambiguous=0 unmatched=4` to
`resolver: rewrote=9 ambiguous=2 unmatched=4` — two actor CALLS edges lost to a
family collision. With the proto-scoped admission the line is unchanged from
baseline.
