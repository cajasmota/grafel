// file_carrier.go — the conditional form of FileEntity (#6815).
//
// FileEntity (extractor.go) is the per-file SCOPE.Component(subtype="file")
// that #577 introduced so a path-anchored FromID has something to resolve to.
// Most extractors call it unconditionally at the top of Extract. Three did not
// call it at all AS MEASURED BY #6815 — erlang, nim and groovy each emit
// IMPORTS edges whose FromID
// is the source path, with no record carrying that path, so
// ReferencesEmbeddedWithAllowlist had nothing to rewrite the FromID onto and
// the edge reached the graph with a raw path at its FROM end.
//
// The fix is deliberately NOT "call FileEntity unconditionally". These three
// languages have no other file-anchored edge, so an unconditional carrier would
// mint one bare orphan node per source file across an entire repo — invisible
// to every recall-shaped assertion, which only ever asks whether the carrier
// EXISTS. proto took the same decision in #6518: the carrier is emitted when
// the file has something for it to carry, and not otherwise.
//
// THREE WAS NEVER THE POPULATION. #6847 measured the class at runtime and found
// twelve more offenders over one corpus, with three further ones behind
// languages that corpus never reaches — so #6815 fixed three of at least
// fifteen, and the total is unbounded above until every registered language is
// driven. #6852 tracks the rest, one language at a time. The caller list is
// read from source by carrier_caller_set_6861_test.go; no count of callers is
// written anywhere in this file.

package extractor

import "github.com/cajasmota/grafel/internal/types"

// FileCarrierFor returns the FileEntity that gives records' path-anchored
// relationships a real FROM end, and ok=false when no such entity should be
// emitted.
//
// The carrier is emitted when ALL THREE clauses hold. Each rejects on its own,
// and each is graded by a mutant that only that clause's fixture kills:
//
//  1. path is non-empty. A nameless carrier is not a carrier — it would resolve
//     nothing and add a blank node. That clause alone rejects records that DO
//     carry an anchoring relationship (an empty FromID trivially equals an empty
//     path), which is why it is tested separately and not folded into clause 2.
//     Pinned by TestFileCarrierFor_EmptyPathNeverCarries_6815.
//  2. Some relationship in records has FromID == path. This is the exact
//     resolution requirement — refs.go has no path→entity index, so a
//     path-valued FromID resolves if and only if some emitted node carries that
//     exact string as its Name. A file with entities but no path-anchored edge
//     needs no carrier and gets none; that clause alone rejects, e.g., an
//     erlang module that declares functions and imports nothing. Pinned by
//     TestFileCarrierFor_NoCarrierWhenNoEdgeIsPathAnchored_6815 and, for the
//     narrower "some OTHER file's path is not this file's anchor" reading, by
//     TestFileCarrierFor_AnotherFilesAnchorDoesNotCount_6815.
//  3. No record in records is ALREADY named path. That clause alone rejects a
//     file which does have a path-anchored edge but whose extractor already
//     minted a path-named container for it — emitting a second one would put
//     two nodes under one id and make the rewrite target ambiguous. hcl (#6852)
//     is a caller for which this clause fires in production rather than only
//     under a unit fixture: its file-level SCOPE.Component is named
//     BASENAME(path), which at a ROOT path ("main.tf") already IS the path, so
//     a root .tf that reaches clause 3 takes this rejection while a nested one
//     never does. Note "reaches": a root .tf with no top-level blocks emits no
//     file component at all, so clause 2 rejects it first and clause 3 is not
//     consulted — this is the depth split among files that DO anchor, not a
//     property of every root .tf. Pinned end-to-end by
//     TestTerraform_RootPathGetsNoSecondCarrier_6852, with the block-less case
//     pinned separately by TestTerraform_EmptyFileGetsNoCarrier_6852.
//     fsharp (#6852) is another such caller, reached by a different route.
//     (No ordinal, and no roster of which callers those are: this paragraph is
//     inside the file whose doctrine below says the caller set is deliberately
//     not written down here, having been stated as measured fact three times
//     and falsified twice — #6861.) The route: nothing fsharp emits is named
//     after the file as a rule, but moduleRE captures
//     a dotted `[\w.]+`, so a ROOT file Core.fs declaring `module Core.fs`
//     emits a SCOPE.Component named exactly the path — and since graph.EntityID
//     does not hash Subtype, a carrier there would land a second node under the
//     module record's id (the #6369/#6480 hazard). Pinned by
//     TestFSharp_ModuleNamedLikeThePathGetsNoSecondCarrier_6852, whose nested
//     subtest is the contrast that stops it passing on a carrier that is never
//     emitted at all.
//     shell (#6852) reaches it by BOTH of those routes and by a third that is
//     new: it emits hcl's BASENAME(path) file component (root depth only, and
//     only for a script that declares a function), AND its `source`/`.` import
//     stub is named after the SOURCED path VERBATIM — so a script that sources
//     itself by the spelling of its own path is a path-named record at ANY
//     depth. That is the case fsharp's import marker provably cannot produce,
//     and it means clause 3 is not a root-depth phenomenon. Pinned by
//     TestShell_ScriptComponentNamedLikeThePathGetsNoSecondCarrier_6852 and
//     TestShell_SelfSourcingImportStubGetsNoSecondCarrier_6852.
//     cobol (#6852) reaches it by a route that is neither a file component nor
//     an import stub: an OPERAND. Nothing cobol emits is named after the file
//     under any condition, but cicsQueueRe/cicsMapRe capture their operand from
//     the class [A-Za-z0-9$#@][A-Za-z0-9$#@_.-]* — which admits '.' — and
//     extractCICSQueues stores it VERBATIM (only the dedup key is upper-cased).
//     So `EXEC CICS READQ TS QUEUE('PAYROLL.cbl')` in a ROOT file PAYROLL.cbl
//     emits a SCOPE.Datastore named exactly the path. Pinned by
//     TestCobol_PathNamedCICSQueueGetsNoSecondCarrier_6852, whose nested
//     subtest is the contrast that stops it passing on a carrier that is never
//     emitted at all.
//     cobol reaches clause 3 at NESTED depth too, by a SECOND route, and that
//     is worth stating here because the arm first shipped the opposite claim.
//     It asserted that no cobol Name can contain '/' and that clause 3 was
//     therefore root-depth-only. False: buildDLISegmentEntity names its record
//     `op + " " + segment`, and segmentFromSSA upper-cases, caps at 8 chars and
//     demands a letter-led first byte without rejecting '/', while the operand
//     it slices comes from dliUsingArgRe's quoted-literal branch or a traced
//     WORKING-STORAGE VALUE (wsValueRe group 3 = `[^'"]*`). The claim that
//     SURVIVES is weaker: such a Name carries a mandatory "<OP> " prefix, so it
//     equals a path only when the path is spelled with it — `EXEC A/B.CBL`,
//     which is a legal on-disk path and is DRIVEN by
//     TestCobol_PathNamedDLISegmentGetsNoSecondCarrier_6852 rather than argued.
//     The general lesson for the arms still queued: a character-class closure
//     over "every Name this package emits" is the claim most likely to be
//     wrong, because one builder concatenating a prefix onto a literal-derived
//     operand defeats it — clojure's `.clj-kondo` correction, one package over.
//     zig (#6852) is the LAST arm, and it answers that lesson with the strongest
//     form the series found. Its one candidate name site, importTopSegment,
//     trims exactly ONE trailing extension, so a ROOT main.zig importing
//     "main.zig.zig" names an import stub exactly the path and clause 3
//     declines — driven, not argued. What settles the NESTED question is not a
//     character-class closure at all: the arm brute-forces the whole input
//     space and establishes that importTopSegment's result is always a
//     SUBSTRING of its input. That is the anti-concatenation property itself,
//     asserted over the function rather than over a paragraph, so the edit that
//     defeated cobol's closure — concatenating a prefix onto a literal-derived
//     operand — fails a test here instead of quietly invalidating prose. The
//     nested cell that remains (a path ending in '/') is DRIVEN too, and
//     reported as alive-but-not-production-reachable by naming the input.
//     Pinned by TestZig_ImportStubNamedLikeThePath_6852 and, for the
//     enumeration, TestZig_ImportTopSegmentIsAlwaysASubstring_6852.
//
// WITH THE ZIG ARM, #6852'S LEDGER REACHED EMPTY over #6847's corpus:
// knownMissingCarrier6847 has no entries left. That is a statement about a
// CORPUS, not about the registered language set — reasonml and rescript are
// confirmed offenders that corpus never reaches, and the population stays
// unbounded above until every registered language is driven. Read
// len(knownMissingCarrier6847) and the coverage sets beside it, never this
// sentence, for the current state.
//
// Clause 3 is checked for EVERY record, before the loop may short-circuit on
// clause 2 being satisfied — deliberately, and the order is load-bearing.
// Hoisting the `if anchored { continue }` above the Name test would stop the
// scan as soon as anchoring is established, so a path-named record emitted
// AFTER the anchoring one would go unseen and a second carrier would be minted.
// That reordering is a mutant the fixture set kills:
// TestFileCarrierFor_NoSecondCarrierWhenThePathNamedRecordComesLast_6815 exists
// for it specifically, because the ordinary clause-3 case places the path-named
// record first and cannot see it. The property is pinned rather than relied on
// precisely because whether some caller emits in that order is not a fact this
// file can hold.
//
// lang is stamped explicitly rather than taken from FileInput.Language so the
// carrier cannot become the one record in an extraction that disagrees with the
// classifier token every other record carries (proto's #6356 trap).
//
// The grading status of that parameter is INVARIANT, and is stated here as an
// invariant rather than as a count of callers (#6861). A WRONG token is caught
// for every caller (mutating erlang's "erlang" to "beam" fails
// TestErlang_CarrierIsLanguageTagged_6815). An EMPTY one is caught only for a
// caller that does NOT run extractor.TagEntitiesLanguage: that helper fills an
// empty Language with the extractor's own token, so for a caller that tags,
// passing "" is equivalent ON THE Language FIELD ALONE — unless some test in
// that package distinguishes the two some other way, as shell's does (#6852):
// TagEntitiesLanguage only touches a record whose Language is empty, and stamps
// Properties["language"] when it does, so a package whose every other record
// sets Language explicitly can observe the fill as provenance rather than as a
// token.
//
// THERE IS A SECOND WAY A TAGGING CALLER ESCAPES THE EQUIVALENCE, and it is not
// a test-side trick at all — it is CALL ORDER. TagEntitiesLanguage fills only
// the records it is handed, so a caller that runs it and THEN prepends the
// carrier never offers the carrier to it: an empty token stays empty and is
// caught on the Language field directly. dockerfile (#6852) is such a caller —
// dockerfile.go tags at :145 and prepends at :183 — and
// TestDockerfile_CarrierShape_6852 asserts both halves: the token on Language,
// and the absence of Properties["language"] that is the premise for reading
// Language as evidence at all. Named because the sentence above would otherwise
// imply something a measurement in this repo contradicts; the mechanism, not
// the roster, is the durable part, and a caller can leave this category by
// having its carrier line moved above its tagging call.
//
// A caller that does not tag keeps whatever token this parameter is
// given, and that second shape is the one the parameter exists for. The
// asymmetry to keep in view is the direction of the error: this paragraph is
// safe when it UNDER-claims grading for a tagging caller (a mutant dies that it
// did not promise would), and unsafe the other way, so the clause above is a
// softening and not a second measured fact about which callers do it.
//
// WHICH callers are which is deliberately NOT written down here. This paragraph
// stated it as a MEASURED fact three times — "all three current callers", then
// "THREE of the FOUR", then "THREE of the FIVE" — and #6852's language arms
// falsified it twice in two consecutive PRs, with ten arms still queued. The
// roster now lives in a test that reads it out of the source tree on every run:
// carrier_caller_set_6861_test.go enumerates the non-test callers of
// PrependFileCarrier / FileCarrierFor, classifies each by whether its package
// calls TagEntitiesLanguage, and FAILS on a caller the invariant above does not
// cover — a non-tagging caller with no test grading its token. Adding a caller
// therefore fails a test rather than silently ageing a comment.
//
// The token need not be a literal. hcl passes a VARIABLE: one
// HCLExtractor.Extract serves both the "hcl" and "terraform" registrations, so
// the two produce carriers that differ only in this field — which is what
// TestHCLToken_ImportsFromEndResolves_6852 asserts.
//
// The returned record owns no relationships, and the rule for a caller is
// stated as a rule rather than as a fact about which callers exist. A caller
// that has file-scoped edges to re-home (proto's file-level CONTAINS) assigns
// them to the carrier afterwards. A caller whose path-anchored records already
// carry the edges themselves — the per-import records, bicep's per-module ones,
// the file-level SCOPE.Component hcl's emitFileLevelRelationships emits — must
// not, or the edges double. Which callers fall on which side is not written
// down here for the same reason the caller count is not: see #6861.
func FileCarrierFor(path, lang string, records []types.EntityRecord) (types.EntityRecord, bool) {
	if path == "" {
		return types.EntityRecord{}, false
	}
	anchored := false
	for i := range records {
		if records[i].Name == path {
			return types.EntityRecord{}, false
		}
		if anchored {
			continue
		}
		for j := range records[i].Relationships {
			if records[i].Relationships[j].FromID == path {
				anchored = true
				break
			}
		}
	}
	if !anchored {
		return types.EntityRecord{}, false
	}
	return FileEntity(FileInput{Path: path, Language: lang}), true
}

// PrependFileCarrier returns records with FileCarrierFor's carrier at index 0,
// or records unchanged when no carrier is due. Index 0 matches the #577
// convention that the file entity is the first record an extractor appends,
// which python/re_exports.go and python/prune_import_placeholders.go both rely
// on.
func PrependFileCarrier(path, lang string, records []types.EntityRecord) []types.EntityRecord {
	carrier, ok := FileCarrierFor(path, lang, records)
	if !ok {
		return records
	}
	return append([]types.EntityRecord{carrier}, records...)
}
