package vbnet

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The real-tree gate (AGENTS.md "Evidence", #6363).
//
// #6363 is the record of what constructed fixtures cost this epic: S3's review
// found three declaration-corrupting defects — `Option Explicit On` swallowing
// the next line, `End With` desynchronising the container stack, and the `32&`
// Long type character eating a declaration — none of which its 156-subtest
// suite could see, because hand-written fixtures are tidy and real files are
// not. This test runs the parser over whatever real VB.NET tree the
// environment points at and fails on a regression in the parse rate.
//
// It skips when no corpus is configured, so it is a gate where a corpus exists
// and never a false green where one does not. Point it at a tree with:
//
//	GRAFEL_VBNET_CORPUS=$HOME/Projects/archigraph-corpora go test ./internal/vbnet/
//
// # Why a count and a denominator, not a rate
//
// The first version asserted a rate, and a rate has two failure modes that
// both make the gate WEAKER the more corpus there is. 299/302 = 0.9901 already
// cleared a 0.99 floor, so it shipped with a whole file of free slack; and
// adding 200 clean files would have bought tolerance for two further failures
// rather than none. A minimum file count plus an absolute maximum failure
// count removes both - growing the corpus can now only make the gate harder.
const corpusMinFiles = 300

// corpusMaxFailures is measured, not chosen: 300 of 302 parse clean, and the
// two that do not are the line-spanning-literal limitation recorded in
// TestMultiLineLiteralIsUnsupported. Lowering it is the point; raising it
// needs a reason in the commit message.
const corpusMaxFailures = 2

func corpusFiles(t *testing.T) []string {
	t.Helper()
	root := os.Getenv("GRAFEL_VBNET_CORPUS")
	if root == "" {
		t.Skip("GRAFEL_VBNET_CORPUS unset: no real VB.NET tree to measure against (#6363)")
	}
	var out []string
	// Walk errors are returned rather than swallowed: a corpus that is half
	// unreadable must fail the gate, not quietly shrink its denominator.
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".vb") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	// Fatal, not Skip. Setting the variable is a statement that a corpus is
	// there; finding too little means the path is wrong, and skipping would
	// turn a misconfigured gate into a silent pass.
	if len(out) < corpusMinFiles {
		t.Fatalf("GRAFEL_VBNET_CORPUS=%s yields %d .vb files, want at least %d: "+
			"a gate is only as strong as its denominator (#6363)",
			root, len(out), corpusMinFiles)
	}
	sort.Strings(out)
	return out
}

// TestCorpusParseRate measures the fraction of real .vb files that parse with
// no diagnostic, and prints every distinct failure so a regression names
// itself rather than only moving a number.
func TestCorpusParseRate(t *testing.T) {
	files := corpusFiles(t)
	clean := 0
	byMessage := map[string]int{}
	var examples []string

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		res := Parse(string(src))
		if len(res.Diagnostics) == 0 {
			clean++
			continue
		}
		if len(examples) < 10 {
			examples = append(examples, fmt.Sprintf("%s: %s", filepath.Base(f), res.Diagnostics[0]))
		}
		for _, d := range res.Diagnostics {
			byMessage[shapeOfDiagnostic(d.Message)]++
		}
	}

	failed := len(files) - clean
	report := fmt.Sprintf("%d/%d files parsed clean; %d failed, at most %d allowed",
		clean, len(files), failed, corpusMaxFailures)
	for msg, n := range byMessage {
		report += fmt.Sprintf("\n  %4d x %s", n, msg)
	}
	for _, e := range examples {
		report += "\n  e.g. " + e
	}
	if failed > corpusMaxFailures {
		t.Fatalf("more failing files than the recorded maximum:\n%s", report)
	}
	t.Log(report)
}

// TestMultiLineLiteralIsUnsupported records the one shape the parser does NOT
// handle, in the direction that makes the gap testable: it asserts the parser
// REPORTS the file rather than silently mis-parsing it, so the day the
// limitation is lifted this test fails and must be updated.
//
// VB 14 allows a string literal to span physical lines, and an interpolation
// hole to do the same. The pre-pass is line-oriented - scanLine resets literal
// state at every newline - so from the opening quote to the next quote on a
// later line, text is scanned as code.
//
// The blast radius is wider than the parse rate suggests, and is stated as
// measured rather than as the two files that happen to be diagnosed:
//
//   - 9 of the 302 corpus files contain a line-spanning literal.
//   - 7 of those 9 parse CLEAN. Their literal interiors are scanned as code
//     with no diagnostic at all; ApplicationSettings.vb survives only because
//     none of its 47 literal lines happens to read as an End.
//   - 2 are diagnosed, and are the entire residual of the 300/302 rate:
//     staxrip's Audio.vb embeds an AviSynth script, Thumbnailer.vb has an
//     interpolation hole broken across five lines.
//   - The damage from the 7 silent ones is small and benign: at most 20 use
//     sites land inside a mis-scanned interior, no phantom node, and no
//     spurious call. (The one IsCall inside the measured window is real code
//     at Thumbnailer.vb:339 - the window over-reaches to the next line
//     carrying a quote, so 20 is an upper bound, not a count.)
func TestMultiLineLiteralIsUnsupported(t *testing.T) {
	// The interior of the literal is scanned as code, so a line inside it that
	// happens to read as an End closes a block that is still open. staxrip's
	// Audio.vb embeds an AviSynth script in exactly this way.
	src := strings.Join([]string{
		"Public Class C",
		"    Shared Function Script() As String",
		"        Return \"# script text",
		"End Function",
		"more text\"",
		"    End Function",
		"End Class",
	}, "\n")
	res := Parse(src)
	if len(res.Diagnostics) == 0 {
		t.Fatalf("multi-line string literals now parse; lift the limitation "+
			"recorded here and in the package doc. Tree:\n%s", res.File.Dump())
	}
}

// shapeOfDiagnostic strips the identifiers out of a diagnostic so failures
// group by cause rather than by name.
func shapeOfDiagnostic(msg string) string {
	if i := strings.IndexByte(msg, '"'); i >= 0 {
		if j := strings.IndexByte(msg[i+1:], '"'); j >= 0 {
			msg = msg[:i] + "<name>" + msg[i+j+2:]
		}
	}
	return msg
}

// Denominators for the agreement invariant, pinned for the same reason
// corpusMinFiles is: an invariant that silently shrinks its own population is
// not an invariant. TestCorpusTreeAgreesWithTable skips files that already
// produce a diagnostic, so a regression that made 200 files fail would
// otherwise make this test easier rather than harder.
const (
	corpusMinTableDecls = 8700
	corpusMinTreeNodes  = 16500
)

// TestCorpusTreeAgreesWithTable pins the two walks against each other, in both
// directions and on kind.
//
// BuildTable (S3) and Parse (S4) are separate walks over the same source with
// separate container-stack logic. If they disagree about scope, the table
// answers a use site's paren classification for a scope the tree never puts it
// in, and every CALLS edge S5 draws from it is silently misattributed. The
// corpus is the only place that divergence shows up: constructed fixtures are
// written to one walk's expectations at a time.
//
// Three things this checks that the first version did not, each of which hid a
// real defect or could have:
//
//   - Tree -> table, not only table -> tree. S5 emits FROM the tree, so a tree
//     that invents a method is the direction that matters, and it was the
//     unchecked one.
//   - Kind, not just presence. A path present under the wrong kind satisfied a
//     bool set.
//   - Every kind at a path, not the last one written. Folded paths collide
//     (a property and a later local can share one), and keeping only the last
//     hid the divergence at CodeEditor.vb:617 where the table recorded a
//     lambda-body local as a type-scope FIELD. That is what led to lambda
//     tracking being shared between the two walks.
func TestCorpusTreeAgreesWithTable(t *testing.T) {
	files := corpusFiles(t)
	tableDecls, treeNodes := 0, 0
	accessors, operators := 0, 0
	var missingInTree, missingInTable, wrongKind []string

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		res := Parse(string(src))
		if len(res.Diagnostics) > 0 {
			continue // a file the parser already reports on proves nothing here
		}
		base := filepath.Base(f)

		// Every kind declared at each folded path, both sides.
		tableAt := map[string]map[SymbolKind]bool{}
		for _, sym := range res.Table.All() {
			path := sym.Name
			if sym.Scope != "" {
				path = sym.Scope + "." + sym.Name
			}
			key := FoldName(path)
			if tableAt[key] == nil {
				tableAt[key] = map[SymbolKind]bool{}
			}
			tableAt[key][sym.Kind] = true
		}
		treeAt := map[string]map[NodeKind]bool{}
		typePaths := map[string]bool{}
		res.File.Walk(func(n *Node) {
			if n.Kind == NodeFile {
				return
			}
			key := FoldName(n.Path())
			if treeAt[key] == nil {
				treeAt[key] = map[NodeKind]bool{}
			}
			treeAt[key][n.Kind] = true
			if n.Kind.IsType() {
				typePaths[key] = true
			}
		})

		// table -> tree, for the kinds a declaration table and a tree both own.
		//
		// KindField and KindConst are in this set for a reason that is not
		// symmetry: a method-body local recorded at TYPE scope is how a
		// container-stack desynchronisation shows itself, and it is invisible
		// to every other direction here. The tree->table direction cannot see
		// it (there is no tree node to start from) and the type/method
		// direction does not cover it. Dropping lambda tracking from
		// BuildTable survived the whole suite until this arm existed.
		for _, sym := range res.Table.All() {
			switch sym.Kind {
			case KindType, KindMethod:
			case KindField, KindConst:
				// Only at TYPE scope. A `Const` declared inside a method body
				// is recorded with KindConst too — the declarator arm tests
				// the modifier before the container — and correctly has no
				// tree node, because it is a local.
				if !typePaths[FoldName(sym.Scope)] {
					continue
				}
			default:
				continue
			}
			path := sym.Name
			if sym.Scope != "" {
				path = sym.Scope + "." + sym.Name
			}
			tableDecls++
			kinds := treeAt[FoldName(path)]
			if len(kinds) == 0 {
				if len(missingInTree) < 12 {
					missingInTree = append(missingInTree,
						fmt.Sprintf("%s:%d table %s %s has no tree node", base, sym.Line, sym.Kind, path))
				}
				continue
			}
			if !anyKindAgrees(sym.Kind, kinds) {
				if len(wrongKind) < 12 {
					wrongKind = append(wrongKind,
						fmt.Sprintf("%s:%d %s: table says %s, tree says %v", base, sym.Line, path, sym.Kind, kinds))
				}
			}
		}

		// tree -> table, the direction S5 emits from.
		var walkErr func(*Node)
		walkErr = func(n *Node) {
			for _, c := range n.Children {
				switch {
				case c.Kind == NodeImport || c.Kind == NodeOption:
					// Directives, not declarations; BuildTable records only
					// aliased Imports, and under the alias rather than a path.
				case c.Kind == NodeAccessor:
					// A Get/Set body is a tree node with no table symbol:
					// BuildTable names accessor containers by keyword, not by
					// path. Counted so the exception cannot grow silently.
					accessors++
				case c.Keyword == "operator":
					// `Operator +` has no identifier for takeIdent to read, so
					// the table holds no symbol for it. Same reasoning.
					operators++
				default:
					treeNodes++
					if len(tableAt[FoldName(c.Path())]) == 0 && len(missingInTable) < 12 {
						missingInTable = append(missingInTable,
							fmt.Sprintf("%s:%d tree %s %s has no table symbol",
								base, c.Span.StartLine, c.Kind, c.Path()))
					}
				}
				walkErr(c)
			}
		}
		walkErr(res.File)
	}

	if tableDecls < corpusMinTableDecls || treeNodes < corpusMinTreeNodes {
		t.Fatalf("population shrank: %d table types/methods (want >= %d), %d tree nodes (want >= %d) — "+
			"this test skips files with diagnostics, so a parse regression shrinks its own invariant",
			tableDecls, corpusMinTableDecls, treeNodes, corpusMinTreeNodes)
	}
	for _, bad := range [][]string{missingInTree, missingInTable, wrongKind} {
		if len(bad) > 0 {
			t.Errorf("tree and table disagree:\n  %s", strings.Join(bad, "\n  "))
		}
	}
	t.Logf("%d table types/methods and %d tree declarations agree on path and kind "+
		"(%d accessors and %d operators are tree-only by construction)",
		tableDecls, treeNodes, accessors, operators)
}

// anyKindAgrees reports whether any tree kind at a path is a legal rendering
// of the table's symbol kind.
//
// The one non-obvious equivalence is delegate: BuildTable records a Delegate
// as KindType with TypeName "delegate", while the tree gives it NodeDelegate.
// 16 corpus paths rely on it, and asserting raw equality would report them as
// divergences they are not.
func anyKindAgrees(k SymbolKind, kinds map[NodeKind]bool) bool {
	for n := range kinds {
		switch k {
		case KindType:
			if n.IsType() || n == NodeDelegate {
				return true
			}
		case KindMethod:
			if n == NodeMethod {
				return true
			}
		case KindField, KindConst:
			if n == NodeField || n == NodeConst {
				return true
			}
		}
	}
	return false
}
