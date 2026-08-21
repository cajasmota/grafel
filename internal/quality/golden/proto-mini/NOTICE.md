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
| `message` → field, `enum` → enum value CONTAINS | structural containment | 8 expected rows |
| cross-file `message` type reference emits **no** dangling edge | #6357 | forbidden **F1** |

Every one of those rows was verified load-bearing by mutating the production
code and confirming the fixture goes red. See the issue #6453 thread for the
mutation table.

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
at **16/16 entities, 14/14 relationships, 0 forbidden hits** — completely green.

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

## #6459 (`SCOPE.Service` missing from `operationKindFamily`) is NOT flipped here

`internal/resolve/refs.go`'s `operationKindFamily` omits `SCOPE.Service`, so
the `file → service` CONTAINS reaches its service only via the kind-agnostic
`byLocation` fallback. In this fixture that fallback happens to succeed, because
no other entity in `user.proto` is named `UserService`. It is the same
file-anchored edge described above, so this fixture would not observe the fix
either way — deliberately, no row here asserts anything about it, and #6459
should not expect this fixture's numbers to move.
