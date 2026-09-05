// file_carrier.go — the conditional form of FileEntity (#6815).
//
// FileEntity (extractor.go) is the per-file SCOPE.Component(subtype="file")
// that #577 introduced so a path-anchored FromID has something to resolve to.
// Most extractors call it unconditionally at the top of Extract. Three did not
// call it at all — erlang, nim and groovy each emit IMPORTS edges whose FromID
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
//     two nodes under one id and make the rewrite target ambiguous.
//
// Clause 3 is checked for EVERY record, before the loop may short-circuit on
// clause 2 being satisfied — deliberately, and the order is load-bearing.
// Hoisting the `if anchored { continue }` above the Name test would stop the
// scan as soon as anchoring is established, so a path-named record emitted
// AFTER the anchoring one would go unseen and a second carrier would be minted.
// That reordering is a mutant the fixture set kills:
// TestFileCarrierFor_NoSecondCarrierWhenThePathNamedRecordComesLast_6815 exists
// for it specifically, because the ordinary clause-3 case places the path-named
// record first and cannot see it. No current caller emits in that order — which
// is the reason to pin the property rather than to rely on it.
//
// lang is stamped explicitly rather than taken from FileInput.Language so the
// carrier cannot become the one record in an extraction that disagrees with the
// classifier token every other record carries (proto's #6356 trap).
//
// MEASURED grading status of that parameter, stated rather than implied: a
// WRONG token is caught (mutating erlang's "erlang" to "beam" fails
// TestErlang_CarrierIsLanguageTagged_6815), but an EMPTY one is not — all three
// current callers run extractor.TagEntitiesLanguage afterwards, and that helper
// fills an empty Language with the extractor's own token, so passing "" is
// equivalent under the suite. It is not equivalent for a caller that does not
// tag, which is why the parameter exists and is passed.
//
// The returned record owns no relationships. Callers that DO have file-scoped
// edges to re-home (proto's file-level CONTAINS) assign them afterwards; for
// erlang, nim and groovy the per-import stub records still carry the IMPORTS
// edges themselves, so hanging them off the carrier as well would double them.
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
