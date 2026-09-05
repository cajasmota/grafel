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
| an `enum` is **not** stamped as a `message` — the two are the same `Kind` from the same file and differ only in `Subtype` | #6422 (message/enum confusion), #6488 arm B | forbidden entity **FE1** |
| the `internal/custom/cpp/protobuf.go` near-duplicate family does **not** swallow enums into its message half | #6488 arm B (entity over-emission) | forbidden entity **FE2** |

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
- **FE1 / FE2** — the first two `forbidden_entities` rows in the corpus
  (#6488 arm B), and both exist for the same reason F3 does: recall counts
  what was FOUND, so it is blind to the graph growing something it should not
  have. FE1's mutant (`buildEnum`'s `Subtype` `"enum"` → `"message"`) left the
  fixture at **19/19 entities, 17/17 relationships, 0 forbidden edges, exit
  0** — `Subtype` is not hashed into `graph.EntityID`, so the flip moves no
  id, breaks no edge and drops no row. FE2's (widening `reProtoMessage` to
  `(?:message|enum)`) took extracted entities 31 → 33 and was likewise fully
  green. Each fires its row and nothing else does.
  - The two forms are deliberately different: **FE1 states a `subtype`** and so
    forbids only that stamp (the legitimate `Role`/`enum` does not trip it);
    **FE2 states none** and so forbids `proto_message:Role` whatever subtype it
    wears. An omitted `subtype` is "any", the same "empty means don't care"
    rule an expected row has carried since #6488 arm A.
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

## The shape this fixture could NOT grade until #6518 — now graded

**The `file → message` / `file → enum` CONTAINS edge — the literal subject of
#6422 — used to be invisible to the golden harness. It is graded now, and this
section is kept because the reason it was invisible is worth knowing.**

WAS: `fileContainsRel` (internal/extractors/proto/proto.go) set `FromID` to the
raw file path, and the proto extractor emitted **no per-file carrier entity**.
Unlike Java, where the file itself is a `SCOPE.Component` that `expected.json`
can name as `from_name`, nothing in a proto graph was named `user.proto`, so
the resolver left those edges dangling on the FROM side (the #6298 offender in
`internal/extractors/file_anchored_rels_guard_test.go`).
`internal/quality/diff.go`'s `resolveExpectedEdge` needed at least one resolvable
`from` candidate before it could match anything — there was no `from_bare_name`
until #6488 arm C added one — so any row spelling this edge was **unsatisfiable**: red forever, telling you
nothing. Measured at the time: reverting `fileContainsSchemaRel` to
`BuildOperationStructuralRef` (the exact pre-#6422 defect) left this fixture at
**18/18 entities, 16/16 relationships, 0 forbidden hits** — completely green.

IS: #6518 re-anchored those edges onto `extractor.FileEntity`, the per-file
`SCOPE.Component` ~25 other extractors already emit under #577, so `user.proto`
and `common.proto` are now named entities in the graph and the FROM side
resolves. The two rows this section used to ask for are in `expected.json`:
the expected `user.proto --[CONTAINS]--> User (SCOPE.Schema)` and the forbidden
twin **F4**, `user.proto --[CONTAINS]--> User (SCOPE.Operation)`. The carrier
itself is an expected entity row, which is why the counts moved to **19
entities / 17 relationships**.

Re-measured on the SAME mutant that used to leave the fixture green — revert
`fileContainsSchemaRel` to `BuildOperationStructuralRef` — the fixture now
reports **16/17 relationships and 1 forbidden hit (F4)**. The golden grade is
no longer blind to #6422's defect, in either direction: the expected row dies
if the edge disappears, F4 fires if it reparents onto the rpc.

The extractor-level guards remain the FIRST place a regression is reported —
`TestFileContains_MessageIsNotReparentedOntoTheRPC_6422` and its siblings in
`internal/extractors/proto/file_contains_6422_test.go`, plus
`internal/extractors/proto/issue6518_anchoring_test.go` for the anchoring
itself — because they name the line that caused it rather than a count. The
fixture is the end-to-end confirmation, not the replacement.

**Still not gradeable here:** the file-anchored `user.proto → common.proto`
IMPORTS row. #6518 made it writable (the FROM side now resolves onto the
carrier, which is #566/#577 working as designed), but it grades the cross-repo
FromID rewrite, not this fixture's subject. See F3's note in `expected.json`.

## #6459 (`SCOPE.Service` reachable from the operation address space) is NOT flipped here

**This fixture cannot observe #6459, before or after.** The `file → service`
CONTAINS in `user.proto` reaches its service through the kind-agnostic
`byLocation` fallback, and that fallback succeeds here because no other entity
in `user.proto` is named `UserService`. The bug #6459 fixes is what happens when
that name DOES collide — so a fixture without a collision on the service name
returns the same answer either way.

Confirmed by measurement rather than assumed: grading this fixture immediately
before and after the resolver change produced byte-identical output — 18/18
entities, 16/16 relationships, 0 forbidden hits (the fixture's size AT THAT
TIME; #6518 has since taken it to 19/17, see the section above), and the same
`resolver: rewrote=27 ambiguous=0 unmatched=7` line. The fixture served as the
regression net for that change, not as its demonstration; the demonstration is
the constructed collision in
`internal/resolve/proto_service_family_6459_test.go`.

**Where the admission lives (#6492).** `SCOPE.Service` is in NO kind family at
all — not the shared `operationKindFamily`, and not a proto-only variant of it.
The shared slice also feeds `hintKinds` and the `familyMaskByKind` leaf-name
filter, and `SCOPE.Service` is emitted by ~60 non-proto sites — several of which
name the entity after a function or class in the same file (celery/dramatiq task
markers, Spring stereotypes). Because a family match must be UNIQUE, admitting
it there destroys those bindings rather than adding any.

A proto-only family widening fails too, for a reason internal to proto:
`buildService` addresses each `rpc` child with the same
`BuildOperationStructuralRef` the `file → service` edge uses, so rpcs and
services share one address space. With `SCOPE.Service` in the filtering family,

    service User  { rpc Get(Foo)  returns (Foo); }
    service Admin { rpc User(Foo) returns (Foo); }

— ordinary proto — makes `rpc User` match two family members and dangles the
`service Admin → rpc User` CONTAINS edge that resolved before. This fixture
cannot see that either: no `rpc` in `user.proto` shares a *service* name.

The mechanism is instead an ordered tier, `lookupProtoServiceTier`: the
unmodified operation family is tried first, and `SCOPE.Service` is consulted
only when that family matched nothing at all, and only for a proto language
segment. No row here asserts anything about it, and neither #6459 nor #6492
should expect this fixture's numbers to move.

**No golden fixture observes the difference — the guards are unit tests.**
An earlier revision of this note credited `python-dramatiq-mini` with observing
it. That was wrong, and worth recording as a trap: `baseline.json` records
`relationship_expected: 0` for that fixture, so
`TestJobFixturesAbsoluteRecall_6260/python-dramatiq-mini` passes with the
regression fully present. The only thing that moves there is an unasserted
`resolver: rewrote=…` line on stderr, which no assertion reads. A fixture whose
numbers cannot change is not a guard.

The real guards, all of which were seen red on the corresponding mutant:

| Direction | Guard |
|---|---|
| Widening into the shared family destroys a non-proto binding | `TestCeleryTaskCallStillBindsToTheFunction6492` (`internal/resolve/`) |
| Widening the proto family destroys an *rpc* binding | `TestProtoRpcNamedAfterASiblingServiceStillBinds6492` |
| The tier binds the #6459 `message Foo` + `service Foo` collision | `TestProtoServiceRefResolvesUnderNameCollision6459` |
| The tier's language boundary admits proto and nothing else | `TestProtoServiceTierIsPinnedToProto6492` |
| The tier's precondition scans the whole family against `.base` | `TestProtoServiceTierPreconditionScansTheWholeFamilyAndBase6492` |
| The residual below stays visible | `TestSelfNamedRpcLeavesTheServiceOrphaned6459Residual` (`internal/extractors/proto/`) |

### What #6459 does NOT close

The scope above is exact, not shorthand. #6459 is closed for the shape where the
service's name collides with a **non-operation** entity — `message Foo` beside
`service Foo`. It is **not** closed when a service collides with an **operation**
entity it owns:

    service Foo { rpc Foo(Bar) returns (Bar); }

`fileContainsOperationRel` (the `file → service` edge) and `buildService` (the
`service → rpc` edge) mint the **byte-identical** ref
`scope:operation:method:proto:<file>:Foo` for two different entities. The ordered
tier cannot separate them and must not try — its precondition sees the rpc in the
operation family and bails, because the alternative is a service outranking a real
rpc. Measured end-to-end through the real extractor at the head that added the
tier: **the service ends with 0 inbound CONTAINS and the rpc carries 2** (its own
parent edge plus the `file → service` edge mis-bound onto it). That is #6459's
title symptom surviving in this one shape.

It is a mis-binding, not a dangle, so no ref-integrity check reports it. Closing
it needs the proto extractor to stop addressing a service and its rpc identically
— a ref *format* change — and belongs in its own issue. This fixture cannot
observe the residual either: no service in `user.proto` shares a name with its own
rpc.
