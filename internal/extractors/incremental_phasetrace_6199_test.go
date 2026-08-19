package extractors_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
)

// --- #6199: the phase trace must cover EVERY return path -------------------
//
// The design claim this file pins is the one the commit message makes:
// "TryIncremental is now a thin wrapper whose only job is to own the per-phase
// trace across EVERY return path (there are 10+ fallback returns in
// tryIncremental, and a trace that misses the fallbacks would miss precisely
// the passes that pay the fixed tail and then throw it away)."
//
// A test that LISTS the fallback returns and drives them one by one is exactly
// the shape that rotted on #6345: the list is written once, a new return is
// added later, and the list silently stops being a coverage claim. So the
// coverage half of this file is proved STRUCTURALLY from the AST instead, and
// it is a proof rather than a sample:
//
//	(a) tryIncremental has EXACTLY ONE call site in the entire module, namely
//	    the one inside TryIncremental. Enumerated from the AST of every .go
//	    file, not from a list.
//	(b) inside TryIncremental, tr.emit(...) is reached UNCONDITIONALLY: it sits
//	    at the top level of the function body, and no statement before it can
//	    return (no ReturnStmt at any nesting depth precedes it).
//
// (a) and (b) together mean: whichever of tryIncremental's returns fires — the
// ten that exist today, and every one added tomorrow — control lands on the
// single statement after the call, which is emit. No return can escape it,
// because there is no other way into or out of tryIncremental. That is the
// whole invariant, discharged without naming a single return.
//
// TestPhaseTrace_EmitsOnFallbackAndSuccess then keeps the structural proof from
// being vacuous by checking the plumbing actually produces a record on a real
// fallback pass and a real success pass.

const tryIncrementalSrc = "incremental.go"

// TestPhaseTrace_EveryReturnIsTraced is the structural coverage proof. See the
// file comment for why it is formulated over the AST rather than as a list of
// driven fallbacks.
func TestPhaseTrace_EveryReturnIsTraced(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, tryIncrementalSrc, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", tryIncrementalSrc, err)
	}

	var wrapper, inner *ast.FuncDecl
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		switch fn.Name.Name {
		case "TryIncremental":
			wrapper = fn
		case "tryIncremental":
			inner = fn
		}
	}
	if wrapper == nil || inner == nil {
		t.Fatalf("expected both TryIncremental and tryIncremental in %s (got wrapper=%v inner=%v)",
			tryIncrementalSrc, wrapper != nil, inner != nil)
	}

	// Sanity: the invariant is only interesting because the inner function has
	// many returns. If it ever collapses to one, this test has stopped guarding
	// anything and should be revisited rather than silently passing.
	returns := countReturns(inner.Body)
	if returns < 5 {
		t.Fatalf("tryIncremental has only %d return statements; the #6199 "+
			"multi-return invariant no longer describes this code — revisit this test", returns)
	}
	t.Logf("tryIncremental has %d return statements, all covered by the proof below", returns)

	// --- (a) exactly one call site of tryIncremental in the whole module ----
	sites := findCallSites(t, "tryIncremental")
	if len(sites) != 1 {
		t.Fatalf("tryIncremental must have exactly ONE call site (inside TryIncremental) "+
			"or the wrapper stops owning the trace for every return path; found %d: %v",
			len(sites), sites)
	}
	if !strings.HasSuffix(sites[0].enclosing, "TryIncremental") {
		t.Fatalf("the sole tryIncremental call site is inside %q, not TryIncremental (%s)",
			sites[0].enclosing, sites[0].pos)
	}

	// --- (b) emit is unconditional and no return precedes it ---------------
	emitIdx := -1
	for i, stmt := range wrapper.Body.List {
		if isEmitCall(stmt) {
			if emitIdx >= 0 {
				t.Fatalf("TryIncremental calls tr.emit more than once at top level "+
					"(indices %d and %d); a pass would emit two traces", emitIdx, i)
			}
			emitIdx = i
		}
	}
	if emitIdx < 0 {
		t.Fatalf("TryIncremental has no UNCONDITIONAL top-level tr.emit(...) call. " +
			"If emit moved inside an if/switch/defer, some return paths no longer " +
			"emit a trace — that is precisely the #6199 defect this test exists for.")
	}
	for i := 0; i < emitIdx; i++ {
		if n := countReturns(wrapper.Body.List[i]); n > 0 {
			t.Fatalf("TryIncremental can return at statement %d, BEFORE tr.emit at "+
				"statement %d (%s): that exit path emits no trace",
				i, emitIdx, fset.Position(wrapper.Body.List[i].Pos()))
		}
	}
	// And emit must actually dominate the exit: everything after it is just the
	// return of the already-computed result.
	if n := countReturns(wrapper.Body.List[emitIdx]); n > 0 {
		t.Fatalf("the tr.emit statement itself contains a return; emit must be a " +
			"plain unconditional call")
	}
}

// isEmitCall reports whether stmt is a bare top-level `X.emit(...)` call.
// A conditional emit (wrapped in if/for/select) is deliberately NOT matched:
// that is the mutant this test must kill.
func isEmitCall(stmt ast.Stmt) bool {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "emit"
}

func countReturns(n ast.Node) int {
	count := 0
	ast.Inspect(n, func(x ast.Node) bool {
		// Do not descend into function literals: their returns exit the
		// closure, not the enclosing function.
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		if _, ok := x.(*ast.ReturnStmt); ok {
			count++
		}
		return true
	})
	return count
}

type callSite struct {
	enclosing string
	pos       string
}

// findCallSites enumerates every call to name across the whole module, so the
// "exactly one caller" half of the proof cannot rot when a second caller is
// added in another package or another file.
func findCallSites(t *testing.T, name string) []callSite {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("abs module root: %v", err)
	}
	var sites []callSite
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // unparseable fixture: not a call site
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(fn, func(x ast.Node) bool {
				call, ok := x.(*ast.CallExpr)
				if !ok {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok || id.Name != name {
					return true
				}
				sites = append(sites, callSite{
					enclosing: fn.Name.Name,
					pos:       fset.Position(call.Pos()).String(),
				})
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	return sites
}

// TestPhaseTrace_EmitsOnFallbackAndSuccess keeps the structural proof honest:
// it drives a real fallback return and a real non-fallback return through the
// public entry point and asserts a trace record materialised for each.
//
// It is NOT the coverage claim (that is the AST test above) — it is the
// vacuity guard, so "every return reaches emit" cannot be satisfied by an emit
// that does nothing.
func TestPhaseTrace_EmitsOnFallbackAndSuccess(t *testing.T) {
	t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "1")
	tracePath := filepath.Join(t.TempDir(), "phases.jsonl")
	t.Setenv("GRAFEL_PHASE_TRACE", tracePath)
	t.Setenv("GRAFEL_PHASE_TRACE_LABEL", "6199-invariant")

	// A two-file repo with no manifest and the trigger limit pinned to 1
	// drives the too-many-changed fallback: the cheapest fallback return to
	// reach deterministically. Any of the others would do equally well — the
	// point of this test is that a fallback emits at all, not which one.
	t.Setenv("GRAFEL_INCREMENTAL_MAX_FILES", "1")
	fallbackRepo := t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(fallbackRepo, name),
			[]byte("package p\n\nfunc F() {}\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	res := extractors.TryIncremental(context.Background(), fallbackRepo, t.TempDir(), logger, nil)
	if res.Done {
		t.Fatalf("expected the too-many-changed fallback, got Done=true")
	}

	recs := readTrace(t, tracePath)
	if len(recs) != 1 {
		t.Fatalf("fallback pass wrote %d trace records, want exactly 1 "+
			"(a fallback that emits no trace is the #6199 defect)", len(recs))
	}
	if recs[0].Done {
		t.Fatalf("trace record reports done=true for a fallback pass: %+v", recs[0])
	}
	if recs[0].FallbackReason == "" {
		t.Fatalf("trace record carries no fallback_reason: %+v", recs[0])
	}
	if !strings.Contains(buf.String(), "incremental: phases outcome=fallback:") {
		t.Fatalf("no summary phases line for the fallback pass; log was:\n%s", buf.String())
	}

	// Now a pass that does NOT fall back, to show the emit is not
	// fallback-only. An empty repo with an up-to-date manifest no-ops.
	t.Setenv("GRAFEL_INCREMENTAL_MAX_FILES", "10000")
	repo := t.TempDir()
	stateDir := t.TempDir()
	buf.Reset()
	res2 := extractors.TryIncremental(context.Background(), repo, stateDir, logger, nil)
	recs = readTrace(t, tracePath)
	if len(recs) != 2 {
		t.Fatalf("second pass did not append a trace record (have %d, want 2); outcome Done=%v reason=%q",
			len(recs), res2.Done, res2.FallbackReason)
	}
	if !strings.Contains(buf.String(), "incremental: phases outcome=") {
		t.Fatalf("no summary phases line for the second pass; log was:\n%s", buf.String())
	}
}

type traceRec struct {
	Label          string             `json:"label"`
	Done           bool               `json:"done"`
	FallbackReason string             `json:"fallback_reason"`
	TotalMS        float64            `json:"total_ms"`
	Phases         map[string]float64 `json:"phases"`
}

func readTrace(t *testing.T, path string) []traceRec {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trace %s: %v (no record was written at all)", path, err)
	}
	defer f.Close()
	var out []traceRec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r traceRec
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("trace line is not JSON: %q: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

// TestPhaseTrace_SilentByDefault pins the #6199 decision that the measurement
// harness changes NOTHING about default daemon output.
//
// The summary `incremental: phases ...` line was unconditional in the first
// draft. The zero-change no-op — the most frequent incremental pass on a live
// daemon — emits no log output at all today, so an unconditional line would
// have introduced a brand-new per-poll, per-repo log line for every user as a
// side effect of landing measurement tooling. Gating it was the deliberate
// choice; this test is what makes the choice hold.
func TestPhaseTrace_SilentByDefault(t *testing.T) {
	t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "1")
	t.Setenv("GRAFEL_PHASE_TRACE", "")

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	extractors.TryIncremental(context.Background(), t.TempDir(), t.TempDir(), logger, nil)

	if strings.Contains(buf.String(), "incremental: phases") {
		t.Fatalf("GRAFEL_PHASE_TRACE is unset but the phases summary line was logged; "+
			"landing #6199 must not change default daemon output. Log was:\n%s", buf.String())
	}
}

// TestPhaseTrace_BooleanEnableLogsWithoutFile covers the middle setting: the
// greppable summary line on a live daemon without inventing a JSONL sink.
func TestPhaseTrace_BooleanEnableLogsWithoutFile(t *testing.T) {
	t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "1")
	t.Setenv("GRAFEL_PHASE_TRACE", "1")

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	extractors.TryIncremental(context.Background(), t.TempDir(), t.TempDir(), logger, nil)

	if !strings.Contains(buf.String(), "incremental: phases outcome=") {
		t.Fatalf("GRAFEL_PHASE_TRACE=1 should log the summary line; log was:\n%s", buf.String())
	}
	if _, err := os.Stat("1"); err == nil {
		_ = os.Remove("1")
		t.Fatalf(`GRAFEL_PHASE_TRACE=1 wrote a JSONL file literally named "1"; ` +
			"boolean tokens must not be treated as paths")
	}
}
