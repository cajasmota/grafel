package types_test

// producer_entity_kinds_6776_test.go — the Go-side entity-kind guard, and the
// binding ledger of Go-declared entity kinds types.AllEntityKinds() does not
// carry (#6776 arm B3).
//
// # The hole this file exists to close
//
// The guard that lived in producer_kinds_test.go read `Kind:` values only when
// they were written as a string LITERAL (*ast.BasicLit). A producer that writes
//
//	const workflowKind = "SCOPE.Workflow"
//	...
//	types.EntityRecord{Kind: workflowKind}
//
// is an *ast.Ident, not a BasicLit, and was therefore invisible to it. Six
// SCOPE.*-prefixed kinds drifted out of the enum behind that blindness without
// a single test noticing.
//
// This is the same defect class as #6773, where routing a constant through
// types as `const K = string(types.X)` dropped relkinds.SitesFor from 1 site to
// 0 because a *ast.CallExpr is not a source string constant either. The lesson
// generalises and is the reason this file does not hand-roll a third scanner:
//
//	AN AST GUARD THAT RESOLVES ONLY LITERALS HAS A HOLE SHAPED EXACTLY LIKE
//	ITS RESOLVER.
//
// So the resolution is delegated to internal/entkinds.ScanGo, which already
// resolves a literal, a bare identifier naming a package-level string constant
// of the SAME package, a selector qualified by an IMPORTED package, and a
// one-argument conversion around any of those. One resolver, one hole, one
// place to widen it.
//
// # Two fabrication holes in that resolver, found while fixing this one
//
// The maxim above cuts both ways, and reusing a resolver inherits its bugs.
// Review of this change found ScanGo answering CONFIDENTLY WRONG in two
// shapes — worse than the unresolved direction, because a fabricated kind is
// indistinguishable from a real declaration in the output:
//
//   - a selector was resolved by its FINAL IDENTIFIER with no check that the
//     qualifier named a package, so the field read `e.Kind` was reported as a
//     source constant whenever some package declared `const Kind = "..."`;
//   - a bare identifier fell back to a TREE-WIDE constant table, so a
//     function-local `const localName = "SCOPE.ActuallyThisOne"` resolved to an
//     unrelated package's `localName` — a kind appearing nowhere at that site.
//
// Both are fixed in internal/entkinds/scan.go and both are OBSERVED here, in
// TestProducerEntityKinds6776_ResolverDoesNotCollectNonKinds, rather than
// asserted in this comment. The advertised selector capability is observed too,
// in TestProducerEntityKinds6776_ResolverReadsSourceConstantShapes — it had no
// test of any kind before review.
//
// # The resolver's limits, and where they are observed
//
// ScanGo resolves SOURCE constants only. A kind computed at run time — the rule
// layer's `Kind: pattern.EntityType` being the load-bearing example — is
// reported as an UNRESOLVED site rather than guessed at or silently dropped.
// TestProducerEntityKinds6776_UnresolvedSitesAreReportedNotDropped observes
// that limit against the live tree instead of asserting it in prose, and
// TestProducerEntityKinds6776_ResolverDoesNotCollectNonKinds observes the other
// direction: the resolver must not scoop up strings that are not entity kinds.
//
// # The two ledgers
//
// Both are asserted by EXACT SET EQUALITY IN BOTH DIRECTIONS plus a size pin,
// the mechanism #6744 established for ruleDeclaredKindsDeferredMax: an author
// who trips the sweep cannot silence it by appending a line, and one who
// migrates a kind must delete its row and lower the pin in the same change.
//
//   - goPrefixedKindsDeferred — SCOPE.*-prefixed kinds a Go producer writes
//     that are not in the enum. This was #6776 arm B4's worklist and arm B4
//     emptied it: the pin is 0, so any such kind the sweep RESOLVES now fails
//     it. See the ledger's own doc for what the sweep does and does not reach.
//   - goUnprefixedKindsDeferred — Go producers writing an UN-prefixed kind.
//     producer_kinds_test.go used to call these "intentionally outside the
//     validator set". That claim is retracted: #6744's scan of the rule tree
//     settled the namespace question as an ACCIDENT (25 of 27 rule-declared
//     values outside the enum, nothing anywhere treating `Route` differently
//     from `SCOPE.Route`), and this ledger is the Go half of the same accident.
//     It is ledgered rather than skipped precisely so the claim stops being
//     prose and becomes a number that cannot move unnoticed.
//
// # What this guard is NOT
//
// The rule-YAML producer family (internal/engine/rules/**/*.yaml, ~532 sites)
// is out of reach of any Go scan by construction and is ledgered separately in
// internal/entkinds/rule_declared_kinds_sweep_guard_6744_test.go.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/entkinds"
	"github.com/cajasmota/grafel/internal/types"
)

// goPrefixedKindsDeferred are the SCOPE.*-prefixed entity kinds a Go producer
// declares that types.AllEntityKinds() does not carry.
//
// EMPTY, and that is the point: this was arm B4's worklist and arm B4 finished
// it. The six it held — SCOPE.Activity, EventFlow, Process, ScheduledJob,
// StateMachine and Workflow, each written as `Kind: <identifier>` and so
// invisible to the pre-B3 literal-only guard — are now declared in kinds.go and
// listed in AllEntityKinds(). Arm B2 had already removed a seventh,
// SCOPE.EventType, by listing the constant that was already declared.
//
// An EMPTY ledger is not a disabled guard. assertLedgerExact's found→ledger
// direction is what fires, and with nothing ledgered any prefixed kind the
// sweep RESOLVES outside the enum fails it — the steady state this arm was
// working towards, not a hole it leaves behind. It is kept rather than deleted
// so the next drift lands on a mechanism instead of inventing one.
//
// # What "the sweep" actually covers, which is less than every Go producer
//
// ScanGo reads the `Kind:` field of an Entity / EntityRecord COMPOSITE LITERAL.
// A producer that passes the kind as a FUNCTION ARGUMENT is invisible to it:
// internal/engine/iac_cloudformation_edges.go:690 emits SCOPE.ScheduledJob via
// emitEntity(id, cfnScheduledJobKind, ...) and this sweep has never seen that
// site. Nothing is missed today only because that kind is now in the enum.
//
// This is B3's own headline holding one level further out — AN AST GUARD THAT
// RESOLVES ONLY ONE SHAPE HAS A HOLE SHAPED EXACTLY LIKE ITS RESOLVER. B3
// closed the identifier hole (a bare name where a literal was expected) and
// left the call-site hole open. Widening ScanGo to argument positions is its
// own change with its own measurement and is deliberately NOT done here; what
// is done here is to stop the comment claiming coverage the code lacks, which
// is the failure this whole issue exists to correct.
var goPrefixedKindsDeferred = map[string]bool{}

// goPrefixedKindsDeferredMax pins the ledger's exact size.
const goPrefixedKindsDeferredMax = 0

// goUnprefixedKindsDeferred are the un-prefixed entity kinds a Go producer
// declares. See the file header: drift, not a second namespace.
//
// `File` is called out on #6776 as the largest non-enum entity kind (894
// entities) and as an internal commit-coupling artefact rather than a
// first-class kind; it is ledgered here, not promoted.
var goUnprefixedKindsDeferred = map[string]bool{
	"ChannelEvent": true, // engine/websocket_edges.go
	"File":         true, // engine/commit_coupling_edges.go — engine.KindFile, a derived artefact
	"Stream":       true, // engine/sse_edges.go
	"Subscription": true, // engine/graphql_subscriptions.go
	"relationship": true, // extractors/cross/hierarchy/extractor.go — a fallback its own comment calls unreachable
}

// goUnprefixedKindsDeferredMax pins the ledger's exact size.
const goUnprefixedKindsDeferredMax = 5

// goScanAnchors are production sites the scan MUST reach, named as
// (file, kind) pairs. They are the must-scan half of the walk-integrity check
// and exist because a COUNT is only a witness of "read the right files", not
// the property.
//
// Concretely: internal/ holds 2663 _test.go files against 2101 non-test ones,
// so inverting parseGoTree's `.go` / `_test.go` filter parses MORE files than
// the correct walk and clears any sane floor while opening not one production
// source. Measured, not assumed — that mutant leaves both floors below intact
// (#6834 arm 1).
//
// Each pair is a kind the ledgers below already name with its file, so a
// producer that genuinely moves updates both in one change. They sit deep in
// long files on purpose: a walk that reads a PREFIX of the tree's content
// tends to keep the first declarations and lose the last.
var goScanAnchors = []struct{ file, kind string }{
	{"engine/workflow_edges.go", "SCOPE.StateMachine"},
	{"engine/commit_coupling_edges.go", "File"},
	{"engine/sse_edges.go", "Stream"},
}

// scanGoEntityKinds runs the shared resolver over internal/ — the same subtree
// the literal guard walked — and fails loudly if the walk did not do this
// work, since a walk that reaches no files reports no offenders and looks like
// a clean tree.
//
// Three questions, one failure site, because they are one question — "is the
// input the ledgers are about to be compared against real?":
//
//  1. did it read ANYTHING? The FilesParsed floor. 2015 files parse today, so
//     500 is a floor and not a count.
//  2. did it read the RIGHT files? goScanAnchors. The floor alone cannot say
//     so; see that variable's doc for the measurement.
//  3. did it get anything OUT of them? The site floor. 566 sites today.
//
// The message must diagnose the WALK, not the tree. Without these checks the
// mutants above still fail the package — but through the ledger tests, saying
// `ledgered kind "Stream" is no longer declared by any Go producer. Delete its
// row and lower the pin by one.` That is the opposite diagnosis, and following
// it deletes the ledger rows and leaves the broken walk permanently green.
func scanGoEntityKinds(t *testing.T) entkinds.Result {
	t.Helper()
	root := filepath.Join(repoRoot(t), "internal")
	res, err := entkinds.ScanGo(root)
	if err != nil {
		t.Fatalf("ScanGo(%s): %v", root, err)
	}
	broken := ""
	switch {
	case res.FilesParsed < 500:
		broken = fmt.Sprintf("it parsed only %d files, want at least 500", res.FilesParsed)
	case len(res.Sites) < 200:
		broken = fmt.Sprintf("it resolved only %d entity-kind sites, expected hundreds", len(res.Sites))
	default:
		for _, a := range goScanAnchors {
			seen := false
			for _, s := range res.Sites {
				if s.File == a.file && s.Kind == a.kind {
					seen = true
					break
				}
			}
			if !seen {
				broken = fmt.Sprintf("it never resolved %q in %s — it read something, but not the production sources these ledgers grade", a.kind, a.file)
				break
			}
		}
	}
	if broken != "" {
		t.Fatalf("THE SCAN'S WALK IS BROKEN, not the tree: %s (walked %s, %d files, %d sites). "+
			"An empty or shrunken result from a walk that did not read internal/'s production sources "+
			"is not evidence about the ledgers (#6834; cf. 52aaa84f1).", broken, root, res.FilesParsed, len(res.Sites))
	}
	return res
}

// invalidGoKindSites buckets every resolved site whose kind fails
// IsValidEntityKind into prefixed / un-prefixed, keyed by kind.
func invalidGoKindSites(res entkinds.Result) (prefixed, unprefixed map[string][]entkinds.Site) {
	prefixed = map[string][]entkinds.Site{}
	unprefixed = map[string][]entkinds.Site{}
	for _, s := range res.Sites {
		if types.IsValidEntityKind(s.Kind) {
			continue
		}
		if strings.HasPrefix(s.Kind, "SCOPE.") {
			prefixed[s.Kind] = append(prefixed[s.Kind], s)
		} else {
			unprefixed[s.Kind] = append(unprefixed[s.Kind], s)
		}
	}
	return prefixed, unprefixed
}

func sortedKeys(m map[string][]entkinds.Site) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// assertLedgerExact compares found against ledger in BOTH directions and pins
// the ledger's size, so neither an appended row nor a silently-migrated kind
// can leave the ledger stale.
// It takes testing.TB rather than *testing.T so
// TestAssertLedgerExact_ActsOnWhatTheScanReturns can hand it a recorder and
// observe both outcomes. Nothing follows the Fatalf, so the function behaves
// the same whether or not Fatalf is terminal for the TB it was handed.
func assertLedgerExact(t testing.TB, label string, found map[string][]entkinds.Site, ledger map[string]bool, pin int) {
	t.Helper()
	if len(ledger) != pin {
		t.Fatalf("%s: ledger has %d entries but the pin says %d; move both or neither", label, len(ledger), pin)
		return
	}
	for _, kind := range sortedKeys(found) {
		if !ledger[kind] {
			var where []string
			for _, s := range found[kind] {
				where = append(where, s.String())
			}
			t.Errorf("%s: NEW Go-declared entity kind %q outside types.AllEntityKinds():\n    %s\n"+
				"Add it to internal/types/kinds.go (constant AND AllEntityKinds), or ledger it "+
				"here and raise the pin by one.", label, kind, strings.Join(where, "\n    "))
		}
	}
	for kind := range ledger {
		if _, ok := found[kind]; !ok {
			t.Errorf("%s: ledgered kind %q is no longer declared by any Go producer (or has become "+
				"valid). Delete its row and lower the pin by one.", label, kind)
		}
	}
}

// recordingTB is a testing.TB that records failures instead of aborting.
//
// testing.TB embeds a private method, so it is embedded here to satisfy the
// interface; only Helper, Errorf and Fatalf are ever called, and any other
// method would panic loudly rather than silently pass. Fatalf deliberately
// does NOT call runtime.Goexit — recording and returning is what lets one test
// observe both the failing and the passing outcome on its own goroutine.
type recordingTB struct {
	testing.TB
	failed bool
	msgs   []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.failed = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.failed = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

// TestAssertLedgerExact_ActsOnWhatTheScanReturns is the positive control for
// the guard's REACTION.
//
// The walk checks grade the input and the resolver controls grade the matcher.
// Neither observes that anything is DONE with the sites the scan returns:
// wrapping either ledger direction in `false &&`, or returning from
// assertLedgerExact before it compares anything, left this whole package green
// — measured, both mutants ALIVE (#6834 arm 1). The live tree agrees with its
// ledgers today, so the reaction is never exercised by the tree itself and
// nothing but this test can see it.
//
// It grades all three directions AND the clean one, because a reaction that
// fails unconditionally satisfies "a dirty set fails" just as well as a
// correct one does.
func TestAssertLedgerExact_ActsOnWhatTheScanReturns(t *testing.T) {
	site := []entkinds.Site{{File: "engine/x.go", Line: 7, Kind: "SCOPE.Unledgered"}}

	// 1. a kind the scan found that the ledger does not carry.
	var onNew recordingTB
	assertLedgerExact(&onNew, "ctl", map[string][]entkinds.Site{"SCOPE.Unledgered": site}, map[string]bool{}, 0)
	if !onNew.failed {
		t.Fatal("no failure for a found kind that is not ledgered: the ledger comparison is not acting on what the scan returned, so this guard is inert however well the scan works")
	}
	if !strings.Contains(strings.Join(onNew.msgs, "\n"), "SCOPE.Unledgered") {
		t.Fatalf("the failure does not name the offending kind; got %q", onNew.msgs)
	}

	// 2. a ledgered kind the scan no longer finds.
	var onStale recordingTB
	assertLedgerExact(&onStale, "ctl", map[string][]entkinds.Site{}, map[string]bool{"SCOPE.Gone": true}, 1)
	if !onStale.failed {
		t.Fatal("no failure for a ledgered kind absent from the scan: a migrated kind would leave its row behind unnoticed")
	}
	if !strings.Contains(strings.Join(onStale.msgs, "\n"), "SCOPE.Gone") {
		t.Fatalf("the failure does not name the stale kind; got %q", onStale.msgs)
	}

	// 3. ledger size disagreeing with its pin.
	var onPin recordingTB
	assertLedgerExact(&onPin, "ctl", map[string][]entkinds.Site{}, map[string]bool{"SCOPE.Gone": true}, 0)
	if !onPin.failed {
		t.Fatal("no failure when the ledger's size disagrees with its pin: a row could be appended without moving the pin")
	}

	// 4. exact agreement must NOT fail, or every direction above passes for
	//    the wrong reason.
	var onClean recordingTB
	assertLedgerExact(&onClean, "ctl", map[string][]entkinds.Site{"SCOPE.Ledgered": site}, map[string]bool{"SCOPE.Ledgered": true}, 1)
	if onClean.failed {
		t.Fatalf("a ledger in exact agreement with the scan failed: %q", onClean.msgs)
	}
}

// TestProducerEntityKinds6776_PrefixedLedgerIsExact varies the ledger's
// agreement with the live tree; it holds the resolver and the scanned subtree
// constant. It is the guard that would have caught the six SCOPE.*-prefixed
// producers that drifted out of the enum unseen.
func TestProducerEntityKinds6776_PrefixedLedgerIsExact(t *testing.T) {
	prefixed, _ := invalidGoKindSites(scanGoEntityKinds(t))
	assertLedgerExact(t, "prefixed", prefixed, goPrefixedKindsDeferred, goPrefixedKindsDeferredMax)
}

// TestProducerEntityKinds6776_UnprefixedLedgerIsExact does the same for the
// un-prefixed Go producers — the population producer_kinds_test.go used to
// dismiss as intentional. Varies: the ledger. Holds constant: the resolver,
// the subtree, and the prefix classification.
func TestProducerEntityKinds6776_UnprefixedLedgerIsExact(t *testing.T) {
	_, unprefixed := invalidGoKindSites(scanGoEntityKinds(t))
	assertLedgerExact(t, "unprefixed", unprefixed, goUnprefixedKindsDeferred, goUnprefixedKindsDeferredMax)
}

// literalOnlyEntityKinds re-creates the OLD guard's resolver — `Kind:` read
// only when the value is an *ast.BasicLit on an Entity / EntityRecord composite
// literal — over the same subtree. It exists solely so the gap this change
// closes is a measurement rather than a claim in a comment.
func literalOnlyEntityKinds(t *testing.T) map[string]bool {
	t.Helper()
	root := filepath.Join(repoRoot(t), "internal")
	fset := token.NewFileSet()
	out := map[string]bool{}
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules", "vendor", "testdata", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return perr
		}
		files++
		ast.Inspect(f, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			name := ""
			switch tp := cl.Type.(type) {
			case *ast.Ident:
				name = tp.Name
			case *ast.SelectorExpr:
				name = tp.Sel.Name
			}
			if name != "Entity" && name != "EntityRecord" {
				return true
			}
			for _, el := range cl.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || key.Name != "Kind" {
					continue
				}
				lit, ok := kv.Value.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if v, uerr := strconv.Unquote(lit.Value); uerr == nil {
					out[v] = true
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("literal-only walk: %v", err)
	}
	if files < 500 {
		t.Fatalf("literal-only walk parsed %d files; the comparison would be vacuous", files)
	}
	if len(out) == 0 {
		t.Fatal("literal-only walk found no Kind literals at all; it is not a fair stand-in for the old guard")
	}
	return out
}

// identifierDeclaredPrefixedKinds are SCOPE.*-prefixed kinds that Go producers
// write as `Kind: <identifier>` and never as a string literal. It is arm B3's
// ledger, frozen at the moment arm B4 emptied that ledger by promoting all six
// into the enum.
//
// The differential below used to read the ledger itself, which stopped working
// the instant the ledger reached zero — it would have gone vacuous exactly when
// the drift it measures was fixed. These six are still identifier-declared
// producers; only their enum membership changed, so they remain valid exemplars
// and the differential keeps its teeth.
var identifierDeclaredPrefixedKinds = []string{
	"SCOPE.Activity",     // engine/workflow_dag_edges.go, engine/workflow_edges.go
	"SCOPE.EventFlow",    // engine/event_flow.go
	"SCOPE.Process",      // engine/process_flow.go
	"SCOPE.ScheduledJob", // engine/scheduled_jobs_edges.go, engine/serverless_framework_edges.go
	"SCOPE.StateMachine", // engine/workflow_edges.go (5 sites)
	"SCOPE.Workflow",     // engine/workflow_dag_edges.go, engine/workflow_edges.go
}

// allGoKindSites buckets every RESOLVED site by kind, valid or not. The
// ledger tests need the invalid ones; the differential below needs all of
// them, because arm B4 made its six exemplars valid without changing how any
// of them is written.
func allGoKindSites(res entkinds.Result) map[string][]entkinds.Site {
	out := map[string][]entkinds.Site{}
	for _, s := range res.Sites {
		out[s.Kind] = append(out[s.Kind], s)
	}
	return out
}

// TestProducerEntityKinds6776_ResolverSeesWhatLiteralsCannot is the differential
// that justifies arm B3 existing. It varies the RESOLVER (constant-resolving
// vs. literal-only) and holds the scanned subtree, the composite-literal type
// set and the tree state constant.
//
// Every kind in identifierDeclaredPrefixedKinds must be (a) found by the
// resolving scan and (b) absent from the literal-only scan — i.e. each is a
// producer the old guard was structurally incapable of reaching, not one it
// happened to pass. Enum membership is irrelevant to both halves, which is why
// arm B4 promoting all six left this test measuring the same thing.
func TestProducerEntityKinds6776_ResolverSeesWhatLiteralsCannot(t *testing.T) {
	resolved := allGoKindSites(scanGoEntityKinds(t))
	literals := literalOnlyEntityKinds(t)

	if len(identifierDeclaredPrefixedKinds) == 0 {
		t.Fatal("no exemplars to compare; this test would pass vacuously")
	}
	for _, kind := range identifierDeclaredPrefixedKinds {
		sites, ok := resolved[kind]
		if !ok || len(sites) == 0 {
			t.Errorf("%q: the resolving scan found no site, so the differential is untestable", kind)
			continue
		}
		if literals[kind] {
			t.Errorf("%q: the literal-only scan ALSO sees this kind, so it is not evidence of "+
				"identifier-blindness — pick a different exemplar or drop the claim", kind)
		}
	}
}

// TestProducerEntityKinds6776_UnresolvedSitesAreReportedNotDropped observes the
// resolver's documented limit instead of asserting it in prose: a Kind computed
// at run time must surface as an unresolved site, not vanish.
//
// engine/detector.go is the load-bearing example — it copies a rule file's
// entity_type straight into EntityRecord.Kind, which is precisely the producer
// family no Go scan can read and why #6744's separate YAML ledger exists.
//
// Varies: nothing (a live-tree observation). Holds constant: the resolver.
func TestProducerEntityKinds6776_UnresolvedSitesAreReportedNotDropped(t *testing.T) {
	res := scanGoEntityKinds(t)
	if res.Unresolved() == 0 {
		t.Fatal("no unresolved sites at all: either every producer is now a source constant " +
			"(update this test and the file header) or the reporting was lost")
	}
	found := false
	for _, s := range res.UnresolvedSites {
		if s.File == "engine/detector.go" {
			found = true
			break
		}
	}
	if !found {
		var sample []string
		for i, s := range res.UnresolvedSites {
			if i == 5 {
				break
			}
			sample = append(sample, s.File)
		}
		t.Errorf("engine/detector.go's rule-YAML passthrough is not among the %d unresolved sites "+
			"(sample: %v); the Go guard may have silently started claiming coverage of the rule layer",
			res.Unresolved(), sample)
	}
}

// writeFixtureTree writes a multi-package fixture under a fresh temp root and
// returns the root. Split out so the positive and negative resolver tests read
// the SAME tree: a capability test and an over-firing test that disagree about
// the fixture prove nothing about each other.
//
// Package q exists to be imported, and package r to be a stranger that shares a
// constant NAME with a function-local one in package p.
func writeFixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("q/q.go", `package q

const QualifiedKind = "SCOPE.QualifiedOK"
`)
	write("r/r.go", `package r

const shadowedName = "SCOPE.StrangerPackage"
`)
	write("p/a.go", `package p

import "example.com/q"

const usedKind = "SCOPE.Function"
const convertedKind = "SCOPE.Converted"
const orphanKind = "SCOPE.OrphanNeverReferenced"
const wrongTypeKind = "SCOPE.OnAWidget"
const notAKindAtAll = "just a name"
const Kind = "SCOPE.FieldNameCollision"

var runtimeKind = "SCOPE.ComputedAtRunTime"

type Entity struct{ Kind, Name string }
type Widget struct{ Kind string }
type carrier struct{ Kind string }

func f(c carrier) []Entity {
	_ = Widget{Kind: wrongTypeKind}
	const shadowedName = "SCOPE.FunctionLocal"
	return []Entity{
		{Kind: usedKind},
		{Kind: q.QualifiedKind},
		{Kind: string(convertedKind)},
		{Name: notAKindAtAll},
		{Kind: runtimeKind},
		{Kind: c.Kind},
		{Kind: shadowedName},
	}
}
`)
	return root
}

// collectedKinds runs the shared resolver over a fixture tree and returns the
// distinct kinds it reported plus the unresolved count.
func collectedKinds(t *testing.T, root string) (map[string]bool, int) {
	t.Helper()
	res, err := entkinds.ScanGo(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, s := range res.Sites {
		got[s.Kind] = true
	}
	return got, res.Unresolved()
}

// TestProducerEntityKinds6776_ResolverReadsSourceConstantShapes is the POSITIVE
// control for every declaration shape the file header advertises. Before
// review the selector shape was claimed in prose and observed by nothing.
//
// It varies the EXPRESSION SHAPE of the Kind value and holds constant: the
// composite-literal type (Entity), the field (Kind), and the fixture tree.
//
//	{Kind: usedKind}            bare identifier, same package
//	{Kind: q.QualifiedKind}     selector qualified by an imported package
//	{Kind: string(constIdent)}  one-argument conversion
func TestProducerEntityKinds6776_ResolverReadsSourceConstantShapes(t *testing.T) {
	got, _ := collectedKinds(t, writeFixtureTree(t))
	for shape, want := range map[string]string{
		"bare identifier":     "SCOPE.Function",
		"qualified selector":  "SCOPE.QualifiedOK",
		"string() conversion": "SCOPE.Converted",
	} {
		if !got[want] {
			t.Errorf("%s: resolver did not collect %q; collected = %v", shape, want, got)
		}
	}
}

// TestProducerEntityKinds6776_ResolverDoesNotCollectNonKinds is the NEGATIVE
// control: recall cannot detect a resolver that over-fires, so this pins what
// must NOT be collected. Every row is a DISTINCT axis with its own constant —
// no two rows share a declaration, so no row's failure can be masked by
// another's. It varies the shape of the non-declaration and holds constant the
// fixture tree and the value style (a string constant, except where the axis IS
// the value style).
//
//	orphanKind        a string constant referenced nowhere at all
//	notAKindAtAll     a string constant used on a different FIELD (Name)
//	wrongTypeKind     a string constant used on a non-entity TYPE (Widget)
//	runtimeKind       a package-level VAR, not a constant
//	c.Kind            a FIELD READ whose name collides with `const Kind`  (#6776 review)
//	shadowedName      a FUNCTION-LOCAL constant that another package also names (#6776 review)
//
// The last two are the fabrication holes: before the fix the resolver answered
// them with "SCOPE.FieldNameCollision" and "SCOPE.StrangerPackage", neither of
// which is declared at the site. They must now be UNRESOLVED — reported, not
// guessed and not dropped — which is why the unresolved count is asserted
// exactly rather than merely being non-zero.
func TestProducerEntityKinds6776_ResolverDoesNotCollectNonKinds(t *testing.T) {
	got, unresolved := collectedKinds(t, writeFixtureTree(t))

	if !got["SCOPE.Function"] {
		t.Fatalf("positive control missing: SCOPE.Function not collected; collected = %v", got)
	}
	for axis, forbidden := range map[string]string{
		"constant referenced nowhere":                               "SCOPE.OrphanNeverReferenced",
		"constant on another field":                                 "just a name",
		"constant on a non-entity type":                             "SCOPE.OnAWidget",
		"package-level var":                                         "SCOPE.ComputedAtRunTime",
		"field read colliding with a constant name":                 "SCOPE.FieldNameCollision",
		"function-local constant, name shared with another package": "SCOPE.StrangerPackage",
		"function-local constant, its own value":                    "SCOPE.FunctionLocal",
	} {
		if got[forbidden] {
			t.Errorf("resolver over-fired on the %q axis: collected %q, which is not an "+
				"entity-kind declaration at that site", axis, forbidden)
		}
	}
	if len(got) != 3 {
		t.Errorf("collected %d distinct kinds, want exactly 3 (the three source-constant "+
			"shapes): %v", len(got), got)
	}
	if unresolved != 3 {
		t.Errorf("unresolved = %d, want 3 (the package-level var, the field read, and the "+
			"function-local constant): a drop here is a silent blind spot, a rise means a "+
			"shape stopped resolving", unresolved)
	}
}

// TestEntityKindDeclarations6776_MatchAllEntityKindsExactly pins the population
// arm B2 corrected, and pins it as a SET rather than only as a count: 82
// constants of type EntityKind, 81 of them named EntityKind*, and
// AllEntityKinds() listing every one with nothing extra.
//
// The counts were 63/62 before #6776 arm B4 declared the six SCOPE.*-prefixed
// Go producers its ledger held, and 69/68 before arm B5 declared thirteen of
// internal/entkinds' rule-YAML ledger; each arm moved both by exactly its own
// batch size, and the 63rd/69th/82nd
// EntityKind-TYPED-but-not-EntityKind-NAMED constant is still
// HTTPEndpointKindLegacy.
//
// It exists because the count reached review as a number in a comment, and the
// review disagreed with it — one side counting EntityKind-TYPED constants (63)
// and the other EntityKind-NAMED ones (62). Both are now observed, so the
// distinction cannot be re-litigated from prose. Note that the parse counts
// DECLARATIONS: kinds.go also contained a doc comment naming a constant
// (EntityKindStateMachine) that has never existed, and no AST walk sees it.
//
// Varies: nothing (a live-source observation). Holds constant: the source file.
func TestEntityKindDeclarations6776_MatchAllEntityKindsExactly(t *testing.T) {
	path := filepath.Join(repoRoot(t), "internal", "types", "kinds.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	declared := map[string]bool{}
	namedEntityKind := 0
	for _, d := range f.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "EntityKind" {
				continue
			}
			for _, n := range vs.Names {
				declared[n.Name] = true
				if strings.HasPrefix(n.Name, "EntityKind") {
					namedEntityKind++
				}
			}
		}
	}

	listed := map[string]bool{}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "AllEntityKinds" {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, e := range cl.Elts {
				if id, ok := e.(*ast.Ident); ok {
					listed[id.Name] = true
				}
			}
			return true
		})
	}

	if len(declared) != 93 {
		t.Errorf("EntityKind-typed constants = %d, want 93; re-pin this number and the "+
			"comment in AllEntityKinds beside EntityKindEventType", len(declared))
	}
	if namedEntityKind != 92 {
		t.Errorf("constants NAMED EntityKind* = %d, want 92 (the one EntityKind-typed "+
			"constant not so named is HTTPEndpointKindLegacy)", namedEntityKind)
	}
	for name := range declared {
		if !listed[name] {
			t.Errorf("%s is declared as an EntityKind but AllEntityKinds() omits it — "+
				"IsValidEntityKind will reject every entity carrying it", name)
		}
	}
	for name := range listed {
		if !declared[name] {
			t.Errorf("AllEntityKinds() lists %s, which is not an EntityKind constant "+
				"declared in this file", name)
		}
	}
	if len(listed) != len(declared) {
		t.Errorf("AllEntityKinds() lists %d entries, %d constants declared", len(listed), len(declared))
	}
}
