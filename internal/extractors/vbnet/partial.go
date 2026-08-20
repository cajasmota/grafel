package vbnet

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/vbnet"
)

// S7a of #6327: merge a VB.NET partial type that is split across a WinForms
// designer pair into ONE SCOPE.Component.
//
// # Why the merge is an identity rewrite and not a post-hoc join
//
// graph.EntityID hashes (repo, kind, name, source_file) and
// entityRecordToGraphEntity DERIVES it unconditionally — EntityRecord.ID is
// deliberately ignored (#6150, incremental.go:1780). So the only lever a
// producer has over entity identity is the (Kind, Name, SourceFile) triple.
// Kind and Name already agree between the two halves of a partial class; the
// source file is the sole reason `Form1.vb` and `Form1.Designer.vb` mint two
// ids. Making them agree therefore means making the two records claim the SAME
// source file, at which point the fold that ALREADY exists on both assembly
// paths — buildDocument's corpus-wide dedup branch and
// convertExtractedRecords' entityPos fold + mergeEntitiesDeduped — collapses
// them with no change outside this package.
//
// # Why the anchor is derived per-file and never from the file SET
//
// The obvious design is a cross-file pass: group every partial declaration by
// (project, namespace, name), pick a canonical member, re-anchor the rest. It
// is rejected because the incremental path re-extracts ONLY changed files
// (incremental.go:1035). Editing `Form1.Designer.vb` alone would present that
// pass with a one-member group and a different canonical anchor than a full
// index computed, so the two paths would disagree about an entity's IDENTITY —
// the exact defect class #6150 documents, and strictly worse than the
// duplication it removes. An anchor that is a pure function of ONE file's path
// is identical under both paths by construction.
//
// # Why the existence guard is not optional
//
// MEASURED on the 302-file corpus: 88 files are `*.Designer.vb`, and only 33
// of them have a `.vb` sibling. The other 55 are `My Project/Settings.
// Designer.vb`, `Resources.Designer.vb`, `Application.Designer.vb` and the
// localized `Strings.<culture>.Designer.vb` family, which are generated
// STANDALONE — there is no `Settings.vb` to merge with. Stripping the infix
// unconditionally would re-anchor 55 files' Components onto paths that do not
// exist, breaking source retrieval for every one of them in exchange for
// merging nothing. So the rewrite requires the sibling to be on disk.
//
// # Why the rewritten record's span is zeroed
//
// Records reach the fold in sort order, and "Form1.Designer.vb" sorts BEFORE
// "Form1.vb" ('D' < 'v'), so the designer half usually wins the survivor slot.
// foldDuplicateEntity is gap-fill-never-override and fills a span only when it
// is ZERO (incremental.go:2058) — it explicitly refuses to union spans, citing
// custom_dispatch.go:507's "third span covering neither declaration". Keeping
// the designer half's lines while claiming `Form1.vb` as the source file would
// point at arbitrary lines of the wrong file. Zeroing the REWRITTEN half makes
// the outcome order-independent: whichever record survives, the merged
// Component carries the anchor file's true span, because that is the only
// non-zero span in the pair.
//
// # What this deliberately does NOT merge
//
// A partial type split across arbitrarily-named files — measured residual:
// `MainForm.vb` + `MainForm_Assistant.vb` + `MainForm_ShowOptions.vb` +
// `MainForm_ShowSettings.vb` (staxrip), `SetupAPI.vb` + `SetupAPI_Inf.vb`
// (display-drivers-uninstaller), and `ApplicationEvents.vb` + `My Project/
// Application.Designer.vb` (WakeOnLAN) — 3 groups, 5 duplicate rows. No
// per-file rule can recover those: the pairing lives in the file SET, and for
// the reason above the file set is not available to an identity decision that
// must match between a full and an incremental index. Merging them needs a
// compilation-unit model (the `.vbproj`) that grafel does not have, and it is
// NOT attempted here.
//
// Name-only merging is not a cheaper substitute for that model, it is a worse
// bug: `MySettings` is declared by SEVEN distinct `My Project/Settings.
// Designer.vb` files in the corpus, in the same `My` namespace, one per
// project. They are seven different types. A path-derived anchor keeps them
// apart for free, because their paths differ.

// designerSuffix is the infix Visual Studio gives a generated designer half.
// The comparison is case-insensitive because both ".Designer.vb" and
// ".designer.vb" occur; the ANCHOR keeps the sibling's real spelling, which is
// why the base is sliced off the original path rather than lower-cased.
const designerSuffix = ".designer.vb"

// designerAnchorPath maps `Foo.Designer.vb` to its `Foo.vb` sibling.
//
// The returned path is repo-relative in the same form as the input, so it is
// directly usable as EntityRecord.SourceFile. ok is false when the path is not
// a designer half at all.
func designerAnchorPath(filePath string) (string, bool) {
	if len(filePath) <= len(designerSuffix) {
		return "", false
	}
	tail := filePath[len(filePath)-len(designerSuffix):]
	if !strings.EqualFold(tail, designerSuffix) {
		return "", false
	}
	base := filePath[:len(filePath)-len(designerSuffix)]
	if base == "" || strings.HasSuffix(base, "/") {
		return "", false
	}
	// ".vb" rather than the sibling's own spelling: the extension the
	// classifier keys on is case-folded (classifier.go:368) and every anchor in
	// the corpus is lower-case.
	return base + ".vb", true
}

// isPartialType reports whether n is a type declaration carrying the `partial`
// modifier.
//
// The modifier is the gate, not the filename. A `.Designer.vb` file that
// declares a NON-partial type declares a type of its own, and re-anchoring it
// would move an entity that has no other half. vbnet.Parse folds modifiers to
// lower case (parse.go declModifiers), so the compare is exact.
func isPartialType(n *vbnet.Node) bool {
	if n == nil || !n.Kind.IsType() {
		return false
	}
	for _, m := range n.Modifiers {
		if m == "partial" {
			return true
		}
	}
	return false
}

// partialAnchor returns the source file a type declaration should be emitted
// under, and whether that differs from the file it was actually declared in.
//
// repoRoot may be empty — every extractor entry point stamps it
// (index.go:3579/3913, incremental.go:974) but the testable core is callable
// without it. With no repo root the sibling cannot be verified, so no rewrite
// happens: an unverified rewrite is the failure mode the existence guard
// exists to prevent.
func partialAnchor(filePath, repoRoot string, n *vbnet.Node) (string, bool) {
	if repoRoot == "" || !isPartialType(n) {
		return filePath, false
	}
	anchor, ok := designerAnchorPath(filePath)
	if !ok {
		return filePath, false
	}
	st, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(anchor)))
	if err != nil || st.IsDir() {
		return filePath, false
	}
	return anchor, true
}
