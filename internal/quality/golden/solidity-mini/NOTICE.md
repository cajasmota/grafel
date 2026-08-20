# Provenance, licence, and the red-on-purpose contract — solidity-mini fixture

This is the first graded Solidity fixture (#6424). Before it there was no `.sol`
fixture anywhere under `testdata/fixtures/` or `internal/quality/golden/`, and no
measured Solidity precision or recall in the tree at all — only an *indexer*
count on the OpenZeppelin contracts package (`docs/coverage/registry.json`),
which says nothing about whether the extractor found the right things.

## Licence and provenance

| `src/` file | Upstream | Licence | Extent |
|---|---|---|---|
| `Ownable.sol` | [OpenZeppelin/openzeppelin-contracts](https://github.com/OpenZeppelin/openzeppelin-contracts) `v5.0.2` — `contracts/access/Ownable.sol` | MIT | verbatim, whole file |
| `ShortStrings.sol` | OpenZeppelin/openzeppelin-contracts `v5.0.2` — `contracts/utils/ShortStrings.sol` | MIT | verbatim, whole file |
| `MyToken.sol` | hand-written for this fixture | — | — |
| `Vault.sol` | hand-written for this fixture | — | — |

OpenZeppelin is MIT, so committing excerpts is redistribution the licence
permits. It is the reference corpus the registry note already names. No other
Solidity corpus is excerpted here: some are LGPL-3 and committing them into this
repo would not be permitted, and cloning one to run the parser over locally is a
different act from vendoring it.

`vbnet-mini/NOTICE.md` argues, correctly, that a fixture built from invented
source grades the fixture author's model of the language rather than the
extractor. Two files here are hand-written anyway, and the reason is specific:
**no OpenZeppelin file in the library carries the two shapes those files exist
for.** OZ never writes base-constructor arguments into its own `is` clauses
(that form appears in its *documentation*, as the way a user names a token), and
no OZ v5 contract declares a Yul `function` inside `assembly`. Both are ordinary
production Solidity; neither could be sourced verbatim. Both hand-written files
are deliberately small and shaped on the documented OZ usage, and each is
confined to the shapes it exists to test, so a modelling error in one cannot
quietly move the figure for the other. The two verbatim OZ files carry the bulk
of the expectations, including every expectation that currently passes.

## THIS FIXTURE IS RED ON PURPOSE

`expected.json` records **what the extractor should produce, not what it does.**
At the commit that added it the grade was:

```
entities:      19 / 36 expected  (recall=52.8%)
relationships:  8 / 13 expected  (recall=61.5%)
forbidden hits: 0
```

measured with `build/grafel quality --json <report> internal/quality/golden/solidity-mini`
at `2872b201a`, and recorded in `internal/quality/golden/baseline.json`.

**The mechanism that makes a red fixture legal is the recorded floor, not a
marker.** `scripts/quality/ratchet.py` gates each fixture against the
`entity_found` / `relationship_found` figures in `baseline.json`; a figure below
`entity_expected` is an ordinary, already-used state — `csharp-aspnet-core-mini`
has sat at 4/5 since long before this fixture existed. The strict gate
(`run.sh` with no `--ratchet`) demands 100% and, per its own header, has never
been green across the full fixture set; it is the aspiration, not the gate.

So: **do not delete a must-have to close the gap.** `TestBaselineRecordedCountsAreSane`
would still pass and the defect would simply stop being visible. Fix #6423, and
the floor rises.

Two entries additionally say so on their own face, and mean it:
`Ownable.constructor` and `Vault.deposit --[CALLS]--> Vault.whenOpen` name a
qualified name and an edge kind that **this fixture is proposing**, not
reporting. If #6423 lands on `Ownable.<constructor>` or on a distinct
`USES`/`DECORATES` kind for a modifier usage, **amend the entry** — do not bend
the extractor to match a fixture's guess.

## What each defect costs, measured

Every line below was produced by running the extractor and reading the emitted
graph, not by reading the regexes.

| Shape | Issue | Measured |
|---|---|---|
| `contract MyToken is ERC20("My Token", "MTK")` | #6423 | **whole file lost**: `MyToken.sol` yields a file carrier and one IMPORTS edge, and nothing else. The contract, its constructor, `mint`, the `Minted` event and `cap` are all absent — 5 entities and 2 edges from one regex. |
| `constructor` | #6423 | 3 misses (`Ownable`, `Vault`, `MyToken`). `Vault.constructor` is the clean one: its contract *is* extracted, so the loss is the denylist alone, not collateral. |
| `receive()` / `fallback()` | #6423 | 2 misses. The ether-receiving entry point of a contract is invisible. |
| file-level free function | #6423 | 1 miss (`computeFee`) — and it costs a second point on the relationship side: the edge `Vault.deposit --[CALLS]--> computeFee` **is** emitted, as an unresolved bare name, because the callee it names is never extracted. The gap costs resolution, not just an entity count. |
| `struct` / `enum` / `error` / `type X is Y` | #6423 | 7 misses across both positions: file-level (`Deposit`, `VaultState`, `VaultLocked`, `ShortString`) and contract-level (`Vault.Receipt`, `Vault.Tier`, `ShortStrings.StringTooLong`). Both positions are asserted on purpose — a file-level-only fix scores four of the seven and leaves the rest. |
| modifier *usage* | #6423 | 2 edge misses. `Vault.deposit --[CALLS]--> Vault.whenOpen` (modifier declared in the same body) and `Vault.lock --[CALLS]--> Ownable.onlyOwner` (inherited from a base contract in another file). The second exists so a same-body-only fix cannot score the class. |
| Yul `function` inside `assembly {}` | #6425 | **confirmed, not gated** — see below. |

## The one thing this fixture measures but cannot gate

`Vault.roundUp` contains `assembly { function helper(x) -> y { … } }`. The
extractor emits a `SCOPE.Operation` named `Vault.helper` and a `CONTAINS` edge
to it — an entity with no counterpart in the Solidity surface. Confirmed by
reading the emitted `graph.json`, and confirmed a second way: adding the
matching `forbidden_relationships` entry and re-running the grader turned
`forbidden hits: 0` into `forbidden hits: 1`. The same run confirms
`Vault.ceiling --[CALLS]--> type` and `Vault.fingerprint --[CALLS]--> encode`,
which appear in the graph as `SCOPE.External` nodes literally named `type` and
`encode`.

**Those entries are deliberately absent from `expected.json`**, because
`scripts/quality/ratchet.py:293-295` treats any forbidden hit as *always fatal*,
and `Evaluate` in `internal/quality/diff.go` counts them unconditionally —
`nice_to_have` does not apply to the forbidden list. There is no known-gaps or
expected-failure channel for precision the way the recorded floor is one for
recall. Committing a currently-violated forbidden entry would therefore **break
the gate rather than record a gap**, which is the one outcome #6424 ruled out.

**When #6425 lands, add these three entries** — they are the assertion, held
here only because the harness has nowhere else to put them yet:

```json
{
  "from_name": "Vault", "from_kind": "SCOPE.Component", "from_file": "Vault.sol",
  "kind": "CONTAINS",
  "to_name": "Vault.helper", "to_kind": "SCOPE.Operation", "to_file": "Vault.sol",
  "must_exist": false,
  "note": "#6425: `helper` is a Yul function inside assembly{}, not a contract member. Member scanning is depth-blind — functionRE looks anywhere in the contract body rather than only at brace depth 1."
},
{
  "from_name": "Vault.ceiling", "from_kind": "SCOPE.Operation", "from_file": "Vault.sol",
  "kind": "CALLS", "to_name": "type", "to_kind": "SCOPE.External",
  "must_exist": false,
  "note": "#6425: `type(uint256).max` is a language builtin, not an invocation."
},
{
  "from_name": "Vault.fingerprint", "from_kind": "SCOPE.Operation", "from_file": "Vault.sol",
  "kind": "CALLS", "to_name": "encode", "to_kind": "SCOPE.External",
  "must_exist": false,
  "note": "#6425: from `abi.encode(...)`. The dotted target folds to a bare `encode` external node."
}
```

The opposite error in the same scan — `callBareRE` requiring a lowercase first
character, so `IERC20(token).transfer(...)` yields no edge to `IERC20` — is
present in `Vault.sweep` and asserted as **`nice_to_have`**, not as a must-have.
Whether an explicit type conversion is a call is a genuine design question, and
a fixture is not entitled to settle it; #6425 should. `nice_to_have` makes the
decision visible in the report's own counters (`relationships 0/1`) instead of
invisible.

## A note on `to_bare_name`

`vbnet-mini` pins external targets with `to_bare_name`. **That would be vacuous
here.** By the time the graph is written, the Solidity pipeline has folded every
bare `CALLS`/`EXTENDS` target into a `SCOPE.External` entity, and
`resolveExpectedEdge` compares `to_bare_name` against the raw `ToID` string
only. Every external target in this fixture is therefore written as
`to_name` + `"to_kind": "SCOPE.External"`. This was found by writing
`to_bare_name` first and watching `Ownable --[EXTENDS]--> Context` report as a
miss with *both endpoints resolved* — the tell that the target had become a
node, not a string.
