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

## STATUS: GREEN since #6423 — this section is history, not the current grade

**#6423 landed and the fixture now grades 40/40 entities and 13/13
relationships, forbidden hits 0.** Everything below describing the fixture as
red describes the state at the commit that *added* it (#6424), and is kept
because the per-defect cost table is the evidence that the four recall defects
were real and measured rather than read off the regexes. `baseline.json` now
records `entity_found: 40` and `relationship_found: 13`; the floor rose, and
the ratchet holds it there.

One entry was **amended** rather than scored, exactly as the paragraph below
instructs: `MyToken --[EXTENDS]--> ERC20` was written as `to_name` + `"to_kind":
"SCOPE.External"`, which was a *proposal* — MyToken did not exist at all when it
was written, so the shape of its edge could not have been measured. Once MyToken
was extracted the guess turned out to be wrong: `ERC20` does **not** fold to a
`SCOPE.External` node.

The mechanism, established by measurement rather than guessed: ext-synthesis
folds a bare name through `isKnownExternalPackage`
(`internal/external/synth.go`), which **lower-cases** the candidate and looks it
up in `knownExternalPackages` — an allowlist with no language gate on it.
`Context` case-folds to `context`, which sits in that list's *Go stdlib*
section, so it mints `ext:Context` with subtype `package`. `ERC20` folds to
`erc20`, which is not on the list, so nothing is minted and the `EXTENDS` edge
keeps its raw target string. Measured on this fixture: renaming the parent to
`Zebra` produces no external node; renaming it to `Express` (an npm entry) or
to `Fmt` (the Go stdlib `fmt`, which also demonstrates the case-fold) produces
`ext:Express` / `ext:Fmt`, both subtype `package`. The import-path *form* is not
the mechanism — swapping the two files' import styles (relative
`../utils/Context.sol` against `@openzeppelin/...`) changed nothing either way.

So the target legitimately cannot resolve and `to_bare_name` is the correct assertion
for it — see the `to_bare_name` section at the foot of this file, which is the
rule the amendment follows, not an exception to it. The amendment is also the
*stronger* of the two forms here: `to_name` against an entity that does not
exist can never pass, while the raw-`ToID` comparison still fails if the parent
is parsed as `ERC20(` or `)`, which is precisely the defect that entry exists to
catch. `relationship_expected` is unchanged at 13.

A fourth `forbidden_relationships` entry was added in the same review pass:
`Vault.ceiling --[CALLS]--> uint256`. The three original rows guard the
inheritance direction, `new string(32)` and the `emit` keyword — none of them
touches the attribute scan that #6423's defect 4 introduced, so the graded
`forbidden hits: 0` was silent about that defect's precision. The new row is
written as `to_bare_name`, not `to_name` + `to_kind`: a minted type name
resolves to no entity and stays on the edge as a raw ToID string, so an
entity-lookup form would match nothing and be vacuous. Measured with the
paren-skip removed from `modifierUsages`, the row fires and the grade goes to
`forbidden hits: 1`.

The single remaining gap is the `nice_to_have`
`Vault.sweep --[CALLS]--> IERC20` (`relationships 0/1` in the nice-to-have
counters), which is #6425's design question and deliberately not a must-have.

## THIS FIXTURE WAS RED ON PURPOSE

`expected.json` records **what the extractor should produce, not what it does.**
At the commit that added it the grade was:

```
entities:      19 / 40 expected  (recall=47.5%)
relationships:  8 / 13 expected  (recall=61.5%)
forbidden hits: 0
```

measured with `build/grafel quality --json <report> internal/quality/golden/solidity-mini`
against the fixture's own `src/`, and recorded in
`internal/quality/golden/baseline.json`.

### That 47.5% is NOT the Solidity extractor's recall

**It is a red-enriched sample, and it is meant to be.** The must-have set was
chosen to cover the broken shapes named in #6423/#6425, so it deliberately
over-samples them. Reading it as "the extractor finds 47.5% of Solidity" would
be wrong in the extractor's favour on the denominator and against it on the
rate.

The honest figure over the same source is:

```
real declarations in src/:              52
currently extracted:                    31   (recall = 59.6%)
of those 31, asserted as must-haves:    19   (12 working ones are not asserted)
missing:                                21   (all 21 are asserted must-haves)
```

(31 = the 41 entities the run reports minus 2 `Module` carriers, 4 `X.sol` file
carriers, 3 `SCOPE.External` nodes, and the 1 phantom `Vault.helper` from #6425.
That phantom is gone as of the #6423 review; the arithmetic above is the
#6424 measurement and is left as recorded.)

So the fixture's number tracks **the defect classes**, and the ratchet floor
tracks **when they get fixed**; neither is a coverage statistic for Solidity. If
you want one, measure over a corpus, not over a targeted fixture. Quote 59.6%,
never 47.5%, as this source's extractor recall — and quote neither as the
language's.

**The mechanism that makes a red fixture legal is the recorded floor, not a
marker.** `scripts/quality/ratchet.py` gates each fixture against the
`entity_found` / `relationship_found` figures in `baseline.json`; a figure below
`entity_expected` is an ordinary, already-used state — `csharp-aspnet-core-mini`
has sat at 4/5 since long before this fixture existed. The strict gate
(`run.sh` with no `--ratchet`) demands 100% and, per its own header, has never
been green across the full fixture set; it is the aspiration, not the gate.

So: **do not delete a must-have to close the gap.** Fix #6423, and the floor
rises. Deleting one is caught, but by the ratchet rather than by the Go test:
`TestBaselineRecordedCountsAreSane` would still pass, while
`scripts/quality/ratchet.py:310-316` fails the run with
`entity_expected changed 40 -> 39 — the fixture's expectations were edited;
re-record with --update-baseline`. That is a deliberate re-record prompt, not a
wall: it forces the deletion to be stated in a baseline diff instead of
happening silently.

Two entries additionally say so on their own face, and mean it:
`Ownable.constructor` and `Vault.deposit --[CALLS]--> Vault.whenOpen` name a
qualified name and an edge kind that **this fixture is proposing**, not
reporting. If #6423 lands on `Ownable.<constructor>` or on a distinct
`USES`/`DECORATES` kind for a modifier usage, **amend the entry** — do not bend
the extractor to match a fixture's guess.

## What each defect costs, measured

Every line below was produced by running the extractor and reading the emitted
graph, not by reading the regexes. Every row marked #6423 is **fixed** as of
that issue; the costs are kept as the record of what each one was worth.

| Shape | Issue | Measured |
|---|---|---|
| `contract MyToken is ERC20("My Token", "MTK")` | #6423 | **whole file lost**: `MyToken.sol` yields a file carrier and one IMPORTS edge, and nothing else. The contract, its constructor, `mint`, the `Minted` event and `cap` are all absent — 5 entities and 2 edges from one regex. |
| `constructor` | #6423 | 3 misses (`Ownable`, `Vault`, `MyToken`). `Vault.constructor` is the clean one: its contract *is* extracted, so the loss is the denylist alone, not collateral. |
| `receive()` / `fallback()` | #6423 | 2 misses. The ether-receiving entry point of a contract is invisible. |
| file-level free function | #6423 | 1 miss (`computeFee`) — and it costs a second point on the relationship side: the edge `Vault.deposit --[CALLS]--> computeFee` **is** emitted, as an unresolved bare name, because the callee it names is never extracted. The gap costs resolution, not just an entity count. |
| `struct` / `enum` / `error` / `type X is Y` | #6423 | 11 misses across both positions: file-level (`Deposit`, `VaultState`, `VaultLocked`, `ShortString`) and contract-level (`Vault.Receipt`, `Vault.Tier`, `ShortStrings.StringTooLong`, `ShortStrings.InvalidShortString`, `Ownable.OwnableUnauthorizedAccount`, `Ownable.OwnableInvalidOwner`, `Vault.VaultEmpty`). Both positions are asserted on purpose — a file-level-only fix scores four of the eleven and leaves the rest. **Every `error` declaration in `src/` is asserted**, all six of them, not a sample: an earlier revision asserted only `StringTooLong` and `VaultLocked`, which let a partial `error` fix look complete. |
| modifier *usage* | #6423 | 2 edge misses. `Vault.deposit --[CALLS]--> Vault.whenOpen` (modifier declared in the same body) and `Vault.lock --[CALLS]--> Ownable.onlyOwner` (inherited from a base contract in another file). The second exists so a same-body-only fix cannot score the class. |
| Yul `function` inside `assembly {}` | #6425 | confirmed; **fixed and now gated** as of the #6423 review — see below. |

## The precision defects this fixture measured but could not gate

`Vault.roundUp` contains `assembly { function helper(x) -> y { … } }`. The
extractor emitted a `SCOPE.Operation` named `Vault.helper` and a `CONTAINS` edge
to it — an entity with no counterpart in the Solidity surface. Confirmed by
reading the emitted `graph.json`, and confirmed a second way: adding the
matching `forbidden_relationships` entry and re-running the grader turned
`forbidden hits: 0` into `forbidden hits: 1`.

**That first entry is no longer held back — it is in `expected.json` and it
passes.** The #6423 review made every contract member scan brace-depth aware
(`braceDepths`), for a regression of its own: a `fallback();` *statement* at the
start of a line inside another function's body was minting a phantom member. A
Yul `function` sits two braces deep — inside `assembly {}` inside a function
body — so the same guard removes it. Measured: the binary built at `9a13a26b4`
scores that row as `forbidden hits: 1`; the fixed one scores 0, and the graph
loses exactly one entity (`Vault.helper`) and its four edges. The residual
`Vault.roundUp --[CALLS]--> helper` now dangles as a bare name, which is the
honest state — it names nothing in the Solidity surface.

The other two rows still fire and stay in this file. The same run confirms
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

**When the rest of #6425 lands, add these two entries** — they are the
assertion, held here only because the harness has nowhere else to put them yet.
(The third, `Vault --[CONTAINS]--> Vault.helper`, has been moved into
`expected.json`; it passes.)

```json
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

## A note on `to_bare_name` — **read this before writing the next fixture**

`vbnet-mini` pins external targets with `to_bare_name`. An earlier draft of this
file said that would be *vacuous* for Solidity, on the theory that the pipeline
folds every bare `CALLS`/`EXTENDS` target into a `SCOPE.External` entity.
**That was false, and it was inverted guidance.** Counted in the emitted
`graph.json` for this fixture:

* **3** entities are `SCOPE.External` — `Context`, `encode`, `type`.
* **26** relationships still carry a **raw string `ToID`** — `computeFee`,
  `_msgSender`, `abi.encode`, `ShortString.wrap`, `add`, `wrap`,
  `getStringSlot`, `unwrap`, `uint256`, `mstore`, the four `IMPORTS` paths, and
  more. The resolver's own line says it: `unmatched=29`.

So **some** targets fold and most do not. The two targets this fixture actually
asserts with `to_name` + `"to_kind": "SCOPE.External"` — `Context` and `ERC20` —
are ones that *do* fold, so those two entries are correct as written and should
not be changed. But for an **unresolved** target the edge's `ToID` is still a
bare string, no entity exists to match `to_name` against, and `to_bare_name` —
which `resolveExpectedEdge` compares against the raw `ToID` — is **the correct
and only** way to assert the current shape.

Falsified by measurement, not by reading: rewriting the
`Vault.deposit --[CALLS]--> computeFee` entry to use
`"to_bare_name": "computeFee"` moves the fixture from **8/13 to 9/13
relationships** — i.e. it *matches*. That entry is deliberately **left** as
`to_name` + `to_file`, because `computeFee` is a free function the extractor
should have extracted (#6423); asserting the unresolved bare name would record
the defect as the intended shape. Choose per target: assert the *resolved* form
when the target should resolve, and `to_bare_name` when it legitimately cannot.

The original observation that produced the wrong generalisation was real but
narrow: writing `to_bare_name` for `Ownable --[EXTENDS]--> Context` reports a
miss with *both endpoints resolved*, because **that** target had become a node.
One folding target does not make a rule.
