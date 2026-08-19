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
	"runtime"
	"strings"
	"testing"
	"time"

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
//	    at the top level of the function body — as a bare call, or inside a
//	    deferred FuncLit whose body calls it unconditionally — and no statement
//	    before it can return (no ReturnStmt at any nesting depth precedes it).
//
// (a) and (b) together mean: whichever of tryIncremental's returns fires — the
// ten that exist today, and every one added tomorrow — emit runs. No return can
// escape it, because there is no other way into or out of tryIncremental. That
// is the whole invariant, discharged without naming a single return.
//
// A NORMAL return is only half the exits, though, and the AST cannot see the
// other half: a panic unwinds past a bare trailing call. So the emit is
// deferred, and TestPhaseTrace_PanicStillEmitsAndStopsSampler drives a real
// panic through the pass to prove the trace — and the heap sampler's shutdown —
// survive it.
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
		t.Fatalf("TryIncremental has no UNCONDITIONAL top-level tr.emit(...) call " +
			"(bare, or in a deferred FuncLit that calls it unconditionally). " +
			"If emit moved inside an if/switch/loop, some return paths no longer " +
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

// isEmitCall reports whether stmt guarantees `X.emit(...)` runs on exit. Two
// shapes qualify, and only two:
//
//	tr.emit(...)                        // bare top-level call
//	defer func() { ...; tr.emit(...) }() // deferred, unconditional in its body
//
// The deferred shape is REQUIRED for panic-safety and was originally rejected
// by this helper, which is how the two requirements ended up mutually
// exclusive: emit takes the pass result, so it cannot be a bare
// `defer tr.emit(...)` (the arguments would be evaluated before the pass runs),
// and a `defer func(){...}()` is a DeferStmt wrapping a FuncLit, not an
// ExprStmt. Accepting it is not a loosening: the FuncLit's body is checked to
// call emit at ITS top level with no return before it, so the deferred form
// carries the same "unconditional" guarantee as the bare form, plus coverage of
// the unwinding path the bare form silently loses.
//
// A conditional emit (wrapped in if/for/select, at either level) is still NOT
// matched: that is the mutant this test must kill.
func isEmitCall(stmt ast.Stmt) bool {
	if isBareEmitCall(stmt) {
		return true
	}
	def, ok := stmt.(*ast.DeferStmt)
	if !ok {
		return false
	}
	lit, ok := def.Call.Fun.(*ast.FuncLit)
	if !ok || lit.Body == nil {
		return false
	}
	for _, inner := range lit.Body.List {
		if isBareEmitCall(inner) {
			return true
		}
		// A return inside the deferred body before the emit would skip it.
		if _, isRet := inner.(*ast.ReturnStmt); isRet {
			return false
		}
	}
	return false
}

// isBareEmitCall reports whether stmt is a plain `X.emit(...)` expression
// statement.
func isBareEmitCall(stmt ast.Stmt) bool {
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
		// Inspect the WHOLE file, not just its FuncDecls. A call site can sit
		// in a package-level var/const initialiser (a *ast.GenDecl), which an
		// f.Decls loop that only descends into *ast.FuncDecl never sees — and
		// such a call site would run the pass with tr == nil, silently swallowed
		// by the nil guards in span/add/emit. That was a real hole in this
		// proof: the mutant
		//
		//	var x = func(...) Result { return tryIncremental(..., nil) }
		//
		// compiled and left this test passing.
		enclosing := func(pos token.Pos) string {
			for _, decl := range f.Decls {
				if pos < decl.Pos() || pos > decl.End() {
					continue
				}
				if fn, ok := decl.(*ast.FuncDecl); ok {
					return fn.Name.Name
				}
				return "<package-level declaration>"
			}
			return "<unknown>"
		}
		ast.Inspect(f, func(x ast.Node) bool {
			call, ok := x.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != name {
				return true
			}
			sites = append(sites, callSite{
				enclosing: enclosing(call.Pos()),
				pos:       fset.Position(call.Pos()).String(),
			})
			return true
		})
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

// panicLogWriter panics the first time the pass logs anything that is not the
// phase-trace summary line, and writes normally after that.
//
// This is the seam the panic test needs and it needs no product-code hook:
// tryIncremental logs through the *log.Logger the caller hands it, and
// log.Logger.output writes to l.out with the mutex released via defer, so a
// panicking Writer unwinds cleanly out of tryIncremental exactly as a panic in
// any other part of the pass would. Letting the phases line through afterwards
// is what makes "did emit still run?" observable.
type panicLogWriter struct {
	buf   bytes.Buffer
	armed bool
}

func (w *panicLogWriter) Write(p []byte) (int, error) {
	if w.armed && !bytes.Contains(p, []byte("incremental: phases")) {
		w.armed = false
		panic("6199: injected panic from the pass's logger")
	}
	return w.buf.Write(p)
}

// TestPhaseTrace_PanicStillEmitsAndStopsSampler pins the half of the invariant
// the AST proof cannot see: control reaching the statement after the call is
// only guaranteed for a NORMAL return. A panic unwinds straight past a
// non-deferred emit — and past the stopHeapSampler that only emit calls.
//
// This is not hypothetical on this codebase. internal/daemon/sched/scheduler.go
// wraps the incremental call in a recover() precisely because "an index can
// panic for reasons the fbwriter fail-soft doesn't catch". So the daemon
// survives the panic, and with a non-deferred emit the 5 ms heap sampler
// survives with it — permanently. Each recovered panic while GRAFEL_PHASE_TRACE
// is set strands one goroutine calling runtime.ReadMemStats, which is
// stop-the-world, in a harness whose entire purpose is trustworthy timings.
func TestPhaseTrace_PanicStillEmitsAndStopsSampler(t *testing.T) {
	t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "1")
	t.Setenv("GRAFEL_INCREMENTAL_MAX_FILES", "1")
	tracePath := filepath.Join(t.TempDir(), "phases.jsonl")
	t.Setenv("GRAFEL_PHASE_TRACE", tracePath)

	repo := t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(repo, name),
			[]byte("package p\n\nfunc F() {}\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	runtime.GC()
	baseline := runtime.NumGoroutine()

	w := &panicLogWriter{armed: true}
	logger := log.New(w, "", 0)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		extractors.TryIncremental(context.Background(), repo, t.TempDir(), logger, nil)
	}()
	if recovered == nil {
		t.Fatalf("the injected logger panic did not fire; this test proves nothing. log was:\n%s", w.buf.String())
	}

	// (i) emit must still have run on the panic path.
	recs := readTrace(t, tracePath)
	if len(recs) != 1 {
		t.Fatalf("a panicking pass wrote %d trace records, want 1: the panic unwound past "+
			"tr.emit, so the pass is untraced AND the heap sampler was never stopped", len(recs))
	}
	if !strings.Contains(w.buf.String(), "incremental: phases outcome=") {
		t.Fatalf("no summary line on the panic path; log was:\n%s", w.buf.String())
	}

	// (ii) and the heap sampler goroutine must be gone.
	deadline := time.Now().Add(3 * time.Second)
	for {
		runtime.GC()
		n := runtime.NumGoroutine()
		if n <= baseline {
			break
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			buf = buf[:runtime.Stack(buf, true)]
			t.Fatalf("goroutine count did not return to baseline after a recovered panic "+
				"(baseline=%d now=%d): the GRAFEL_PHASE_TRACE heap sampler leaked, and it "+
				"calls the stop-the-world runtime.ReadMemStats every 5 ms forever.\n%s",
				baseline, n, buf)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
