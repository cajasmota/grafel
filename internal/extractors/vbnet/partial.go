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
// The listing is filtered to entries os.ReadFile can actually open — see
// openableAsFile — and the pure choice among the surviving names is
// pickSibling, so the tie-break is testable without a filesystem that can
// represent the tie.
func siblingPath(repoRoot, base string) (string, bool) {
	dir, file := path.Split(base)
	absDir := filepath.Join(repoRoot, filepath.FromSlash(dir))
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return "", false
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !openableAsFile(absDir, e) {
			continue
		}
		names = append(names, e.Name())
	}
	name, ok := pickSibling(names, file+".vb")
	if !ok {
		return "", false
	}
	return dir + name, true
}

// openableAsFile reports whether os.ReadFile on this entry will RETURN — with
// content or with an error — rather than block.
//
// This is a liveness guard, not a tidiness one. The anchor is read by
// typeDecls, and a FIFO named `Widget.vb` is a legitimate directory entry whose
// open(2) blocks until a writer appears; nothing bounds that wait, so the
// indexing worker that picked up `Widget.Designer.vb` never finishes. A
// directory or a device node fails the read harmlessly, but a FIFO does not
// fail — it hangs — so "it will just fail the read" is not a filter substitute.
// This is also the only vetting the path gets: partial.go reads a path the file
// walker never handed it.
//
// A SYMLINK to a regular file is deliberately ACCEPTED. The walker reports one
// as a file and indexes it (walker.go branches only on d.IsDir()), so refusing
// it here would deny an anchor the walker does claim a file entity for, and the
// merge would be lost for no gain. A symlink to a directory or to a FIFO is
// refused by the same stat, which is the shape that actually matters.
func openableAsFile(dir string, e os.DirEntry) bool {
	mode := e.Type()
	if mode.IsRegular() {
		return true
	}
	if mode&os.ModeSymlink == 0 {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, e.Name()))
	return err == nil && fi.Mode().IsRegular()
}

// pickSibling chooses the entry that spells `want` out of a directory listing.
//
// An exact match wins outright. Otherwise a single case-insensitive match is
// taken, and an ambiguous set — possible only on a case-sensitive filesystem —
// is refused, because picking one of them would be arbitrary.
//
// It takes names rather than reading the directory itself so the refusal arm is
// reachable in a test on every platform. Built against a real filesystem the
// case is unrepresentable on macOS and Windows, where the two entries collapse
// into one, which left the arm covered on Linux only.
func pickSibling(names []string, want string) (string, bool) {
	var folded string
	n := 0
	for _, name := range names {
		if name == want {
			return want, true
		}
		if strings.EqualFold(name, want) {
			folded = name
			n++
		}
	}
	if n != 1 {
		return "", false
	}
	return folded, true
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
// Reading the sibling is no more a filesystem access than stat'ing it, so it
// costs the design nothing it was not already paying — though it is not free:
// only the read-and-parse is memoised here. siblingPath's os.ReadDir runs
// BEFORE this lookup (partialAnchor:285 precedes :289), so a multi-type
// designer file parses its sibling once but lists the directory once per
// partial type. Memoising the listing too would mean giving this cache a second
// map and a constructor, which is not worth it for a listing the OS caches
// anyway.
//
// The NAMESPACE compares case-insensitively and the NAME compares exactly, and
// the asymmetry is deliberate.
//
// VB.NET identifiers are case-insensitive, so `Namespace ALPHA` and
// `Namespace Alpha` are the SAME namespace and a partial type split across the
// two is one type. The namespace is not part of the identity triple at all —
// graph.EntityID hashes (repo, kind, name, source_file), graph.go:259-269 —
// so it never keeps such a pair apart on its own; comparing it exactly only
// refuses a legitimate merge.
//
// The name is different precisely because it IS in that triple. `Widget` and
// `WIDGET` derive two ids whatever file they claim, so re-anchoring one onto
// the other's file merges nothing and costs the moved half its span — the
// wrong-file outcome above.
func (c siblingCache) declares(repoRoot, anchor, ns, name string) bool {
	set, ok := c[anchor]
	if !ok {
		set = typeDecls(repoRoot, anchor)
		c[anchor] = set
	}
	return set[declKey(ns, name)]
}

// declKey is the lookup key typeDecls builds and declares queries. Both sides
// must fold the namespace identically, which is why it is one function.
func declKey(ns, name string) string {
	return strings.ToLower(ns) + "\x00" + name
}

// typeDecls returns the (namespace, name) pairs of every type declared in the
// file, or an empty set when it cannot be read or parsed. siblingPath has
// already vetted that this path can be opened without blocking; anything else
// that is not VB source fails the read here and declares nothing.
//
// The walk recurses through Namespace blocks JOINING each segment onto the
// enclosing path, and through types, because emit does both: a type nested in
// `Namespace A / Namespace B` is emitted under `A.B`, and a type declared
// inside a Class or Module is emitted under its enclosing namespace like any
// other. Either recursion dropped and the sibling's set is keyed differently
// from emit's, so nothing matches and every such merge is silently lost.
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
				set[declKey(ns, child.Name)] = true
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
