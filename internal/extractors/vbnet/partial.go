package vbnet

import (
	"os"
	"path"
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
// duplication it removes.
//
// # KNOWN DIVERGENCE: the anchor is not a pure function of one file's path
//
// The anchor is derived from one file's path AND from the state of the
// filesystem at that path, because the sibling must be read to be verified. A
// full index and an incremental index therefore do NOT always agree, and this
// slice does not claim they do. Incremental re-extracts only changed and
// deleted files (incremental.go:700-720) and evicts on
// `changedSet[e.SourceFile]` (:800-810), so a change to one half never
// re-extracts the other. Two concrete scenarios, both reachable today:
//
//   - DELETE `Form1.vb` from a merged pair. Incremental evicts the merged
//     Component (it is anchored at the deleted path) and never re-extracts the
//     unchanged `Form1.Designer.vb`, so `Form1` VANISHES from the graph. A full
//     index of the same tree yields `Form1` anchored at `Form1.Designer.vb`,
//     because the sibling is gone and no rewrite happens.
//
//   - ADD `Form1.vb` next to an existing lone `Form1.Designer.vb`. Incremental
//     extracts only the new file and leaves the designer half at its old
//     anchor, giving TWO Components. A full index gives ONE.
//
// The mitigation is a full re-index, which reconciles both: every file is
// re-extracted against the tree as it then stands. Making the incremental path
// invalidate the sibling of a changed designer pair is the real fix and is a
// separate, larger change — it means teaching the changed-set computation about
// a producer-defined file relationship, which nothing in the pipeline models
// today.
//
// # Why the sibling guard is not optional
//
// MEASURED on the 302-file corpus: 88 files are `*.Designer.vb`, and only 33
// of them have a `.vb` sibling. The other 55 are `My Project/Settings.
// Designer.vb`, `Resources.Designer.vb`, `Application.Designer.vb` and the
// localized `Strings.<culture>.Designer.vb` family, which are generated
// STANDALONE — there is no `Settings.vb` to merge with. Stripping the infix
// unconditionally would re-anchor 55 files' Components onto paths that do not
// exist, breaking source retrieval for every one of them in exchange for
// merging nothing. So the rewrite requires the sibling to be on disk — and,
// because a file merely BEING there proves nothing about what it declares, to
// declare the same type in the same namespace. See siblingCache.declares for
// the three shapes an existence-only check gets wrong.
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

// designerBase strips the designer infix, mapping `Foo.Designer.vb` to `Foo`.
//
// The returned base is repo-relative in the same slash form as the input. ok is
// false when the path is not a designer half at all.
func designerBase(filePath string) (string, bool) {
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
	return base, true
}

// siblingPath resolves `<base>.vb` against the REAL directory listing under
// repoRoot and returns the path spelled as the file is spelled on disk.
//
// A hardcoded `base + ".vb"` is not portable: a sibling named `Widget.VB` makes
// os.Stat succeed on case-insensitive macOS and Windows and fail on Linux, and
// the macOS outcome is the worst of the three — the ids still differ by path
// spelling so nothing merges, and the Component is left pointing at a path no
// file entity claims, with its span zeroed. Reading the directory costs one
// syscall more than stat'ing it and gives the same answer everywhere.
//
// An exact match wins outright. Otherwise a single case-insensitive match is
// taken, and an ambiguous set — possible only on a case-sensitive filesystem —
// is refused, because picking one of them would be arbitrary.
//
// Directory entries are deliberately NOT filtered out here. A directory named
// `Widget.vb` is caught one step later: typeDecls cannot read it, so it
// declares no type and no rewrite happens. A filter would be a second, weaker
// spelling of the same guard with no behaviour of its own to test.
func siblingPath(repoRoot, base string) (string, bool) {
	dir, file := path.Split(base)
	want := file + ".vb"
	entries, err := os.ReadDir(filepath.Join(repoRoot, filepath.FromSlash(dir)))
	if err != nil {
		return "", false
	}
	var folded string
	n := 0
	for _, e := range entries {
		if e.Name() == want {
			return dir + want, true
		}
		if strings.EqualFold(e.Name(), want) {
			folded = e.Name()
			n++
		}
	}
	if n != 1 {
		return "", false
	}
	return dir + folded, true
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

// siblingCache memoises the type declarations of an anchor candidate for the
// duration of ONE file's extraction. A designer file declaring several partial
// types would otherwise re-read and re-parse the same sibling once per type.
type siblingCache map[string]map[string]bool

// declares reports whether the file at anchor declares a type named name inside
// namespace ns.
//
// Existence is not enough. Three real shapes break a rewrite that only stats
// the path, and all three are the same missing check:
//
//   - `Widget.Designer.vb` declaring `Partial Class Widget` beside a
//     `Widget.vb` that declares `Gadget`. Nothing merges, and Widget is moved
//     onto a file that does not declare it, with its span dropped.
//   - a designer file declaring an EXTRA type (`Widget` AND `WidgetHelper`).
//     Only the type with a real other half may move. Multi-type designer files
//     are common.
//   - `Namespace Alpha / Class Widget` beside `Namespace Beta / Partial Class
//     Widget`. `vbnet_namespace` is a PROPERTY, not part of the identity triple
//     (extractor.go:157), so the path was the only thing keeping the two apart
//     and the rewrite is what destroys it. They are different types and must
//     stay two Components.
//
// Reading the sibling is no less a filesystem access than stat'ing it, so it
// costs the design nothing it was not already paying. The name and namespace
// compare EXACTLY rather than case-insensitively: the identity triple is
// case-sensitive, so a case-differing declaration would not fold into one id
// anyway, and re-anchoring it would only produce the wrong-file outcome above.
func (c siblingCache) declares(repoRoot, anchor, ns, name string) bool {
	set, ok := c[anchor]
	if !ok {
		set = typeDecls(repoRoot, anchor)
		c[anchor] = set
	}
	return set[ns+"\x00"+name]
}

// typeDecls returns the (namespace, name) pairs of every type declared in the
// file, or an empty set when it cannot be read or parsed. A directory at the
// anchor path lands here too: it fails the read and declares nothing.
func typeDecls(repoRoot, anchor string) map[string]bool {
	set := map[string]bool{}
	src, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(anchor)))
	if err != nil {
		return set
	}
	var walk func(n *vbnet.Node, ns string)
	walk = func(n *vbnet.Node, ns string) {
		for _, child := range n.Children {
			if child.Kind == vbnet.NodeNamespace {
				inner := child.Name
				if ns != "" && inner != "" {
					inner = ns + "." + inner
				}
				walk(child, inner)
				continue
			}
			if child.Kind.IsType() && child.Name != "" {
				set[ns+"\x00"+child.Name] = true
			}
			walk(child, ns)
		}
	}
	walk(vbnet.Parse(string(src)).File, "")
	return set
}

// partialAnchor returns the source file a type declaration should be emitted
// under, and whether that differs from the file it was actually declared in.
//
// ns is the namespace the declaration sits in, which is part of what makes two
// same-named halves the SAME type rather than two types that collide.
//
// repoRoot may be empty — every extractor entry point stamps it
// (index.go:3579/3913, incremental.go:974) but the testable core is callable
// without it. With no repo root there is nothing to resolve the sibling
// against, so no rewrite happens: an unverified rewrite is the failure mode the
// sibling checks exist to prevent.
func partialAnchor(filePath, repoRoot, ns string, n *vbnet.Node, cache siblingCache) (string, bool) {
	if repoRoot == "" || !isPartialType(n) {
		return filePath, false
	}
	base, ok := designerBase(filePath)
	if !ok {
		return filePath, false
	}
	anchor, ok := siblingPath(repoRoot, base)
	if !ok {
		return filePath, false
	}
	if !cache.declares(repoRoot, anchor, ns, n.Name) {
		return filePath, false
	}
	return anchor, true
}
