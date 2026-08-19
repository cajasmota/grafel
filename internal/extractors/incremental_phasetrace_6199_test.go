package extractors_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
//
// It also covers every DISABLE token end-to-end. "Unset" is not the only way an
// operator turns this off: GRAFEL_PHASE_TRACE=0 used to mean "enabled, and
// append JSONL to a file named 0 in the daemon's cwd".
func TestPhaseTrace_SilentByDefault(t *testing.T) {
	for _, raw := range []string{"", " ", "0", "false", "FALSE", "off", "OFF", "no", " no "} {
		t.Run("value="+raw, func(t *testing.T) {
			t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "1")
			t.Setenv("GRAFEL_PHASE_TRACE", raw)

			var buf bytes.Buffer
			logger := log.New(&buf, "", 0)
			extractors.TryIncremental(context.Background(), t.TempDir(), t.TempDir(), logger, nil)

			if strings.Contains(buf.String(), "incremental: phases") {
				t.Fatalf("GRAFEL_PHASE_TRACE=%q means DISABLED but the phases summary line "+
					"was logged; landing #6199 must not change default daemon output. Log was:\n%s",
					raw, buf.String())
			}
			assertNoStrayTraceFile(t, raw)
		})
	}
}

// assertNoStrayTraceFile fails if the env value was taken as a filename in the
// process's working directory (which, for the daemon, is wherever launchd
// started it).
func assertNoStrayTraceFile(t *testing.T, raw string) {
	t.Helper()
	for _, candidate := range []string{raw, strings.TrimSpace(raw)} {
		if candidate == "" || strings.ContainsAny(candidate, `/\`) {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			_ = os.Remove(candidate)
			t.Fatalf("GRAFEL_PHASE_TRACE=%q wrote a JSONL file literally named %q in the "+
				"working directory; boolean/falsey tokens must never be treated as paths",
				raw, candidate)
		}
	}
}

// TestPhaseTrace_BooleanEnableLogsWithoutFile covers the middle setting: the
// greppable summary line on a live daemon without inventing a JSONL sink.
func TestPhaseTrace_BooleanEnableLogsWithoutFile(t *testing.T) {
	for _, raw := range []string{"1", "true", "TRUE", "yes", "on", " on "} {
		t.Run("value="+raw, func(t *testing.T) {
			t.Setenv("GRAFEL_INCREMENTAL_REINDEX", "1")
			t.Setenv("GRAFEL_PHASE_TRACE", raw)

			var buf bytes.Buffer
			logger := log.New(&buf, "", 0)
			extractors.TryIncremental(context.Background(), t.TempDir(), t.TempDir(), logger, nil)

			if !strings.Contains(buf.String(), "incremental: phases outcome=") {
				t.Fatalf("GRAFEL_PHASE_TRACE=%q should log the summary line; log was:\n%s",
					raw, buf.String())
			}
			assertNoStrayTraceFile(t, raw)
		})
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

// TestPhaseTrace_EverySpanClosesOnEveryPath is the second structural proof, and
// it is the one that matters for the NUMBERS rather than for the record's
// existence.
//
// A span is `end := tr.span("name")` … `end()`. If a return sits between those
// two points, the span never closes: the phase is missing from the trace
// entirely and `accounted` silently underreports `total` by however long that
// phase ran. That is worse than a missing record, because the record still
// shows up and still looks plausible.
//
// It was real: `extract-changed-files` opens the single most expensive span in
// the pass, and two function-level fallback returns sat inside it —
// `extract-error file=…` and `no-tree-no-records file=…`. Those are precisely
// the fallbacks that pay the FULL extraction and then throw it away, so the two
// passes whose extract cost most needed measuring were the two that reported no
// extract phase at all. incremental_phasetrace.go promises the opposite: the
// counters "are reported even when the pass falls back part-way, which is the
// point".
//
// So this is checked structurally rather than by fixing the one instance: every
// return lexically inside an open span must be preceded, in its own block or an
// enclosing one up to the span's block, by that span's end() call.
func TestPhaseTrace_EverySpanClosesOnEveryPath(t *testing.T) {
	files := spanAuditFiles(t)
	if len(files) == 0 {
		t.Fatalf("no non-test .go files found to audit")
	}

	spans := 0
	var problems []string
	for _, name := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		n, probs := auditSpans(fset, file)
		spans += n
		problems = append(problems, probs...)
	}

	// Vacuity guard: if the spans stop being found this test passes trivially.
	if spans < 25 {
		t.Fatalf("found only %d tr.span(...) openings across %d files in this package; "+
			"this test has stopped describing the code — revisit it rather than letting "+
			"it pass", spans, len(files))
	}
	t.Logf("checked %d spans for closure on every path across %d files", spans, len(files))
	if len(problems) > 0 {
		t.Fatalf("%d span problem(s):\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}

// spanAuditFiles lists every non-test .go file in this package.
//
// The audit used to parse incremental.go and nothing else, while findCallSites
// (the other half of the proof) walked the whole module. That asymmetry was a
// silent coverage cliff: the day a phase moved to a helper file — which is a
// plausible refactor for a 117KB file — its spans would stop being audited and
// NOT ONE test would fail. Widening to the package is the stronger of the two
// options considered (the other being "assert no tr.span( exists outside
// incremental.go"): it does not merely detect the move, it keeps auditing the
// spans after it. A helper file with no spans contributes nothing and costs a
// parse.
func spanAuditFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// auditSpans is the checker itself, factored out of the test so the mutants it
// must catch can be run against synthetic sources in
// TestPhaseTrace_SpanAuditCatchesMutants instead of only against the real file.
// It returns the number of well-formed span openings it recognised and one
// problem string per defect found.
func auditSpans(fset *token.FileSet, file *ast.File) (spans int, problems []string) {
	// Positions of the tr.span(...) calls that were recognised as a proper
	// `end := tr.span("phase")` opening, used below to flag the ones that were
	// not (see the "discarded" check at the end of this function).
	recognised := map[token.Pos]bool{}

	ast.Inspect(file, func(n ast.Node) bool {
		// Every statement list in Go source hangs off exactly one of these three
		// nodes, so matching only them visits each list once. Going through
		// stmtLists here instead would double-count (a FuncDecl and its own
		// BlockStmt yield the same list).
		var lists [][]ast.Stmt
		switch s := n.(type) {
		case *ast.BlockStmt:
			lists = [][]ast.Stmt{s.List}
		case *ast.CaseClause:
			lists = [][]ast.Stmt{s.Body}
		case *ast.CommClause:
			lists = [][]ast.Stmt{s.Body}
		default:
			return true
		}
		for _, list := range lists {
			for i, st := range list {
				endName, phase, ok := spanOpen(st)
				if !ok {
					continue
				}
				spans++
				recognised[st.(*ast.AssignStmt).Rhs[0].Pos()] = true
				close := -1
				for j := i + 1; j < len(list); j++ {
					if isCallToIdent(list[j], endName) {
						close = j
						break
					}
				}
				if close < 0 {
					problems = append(problems, fmt.Sprintf(
						"span %q opened at %s as %s() is never closed in its own block",
						phase, fset.Position(st.Pos()), endName))
					continue
				}
				var bad []ast.Stmt
				scanForUnclosedReturns(list[i+1:close], endName, &bad)
				for _, r := range bad {
					problems = append(problems, fmt.Sprintf(
						"span %q (%s) is still open at the return on %s: that path reports no "+
							"%q phase at all, and accounted_ms underreports total_ms by it",
						phase, endName, fset.Position(r.Pos()), phase))
				}
			}
		}
		return true
	})

	return spans, problems
}

// spanOpen matches `endX := tr.span("phase")` and returns endX and the phase.
func spanOpen(st ast.Stmt) (endName, phase string, ok bool) {
	as, isAssign := st.(*ast.AssignStmt)
	if !isAssign || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return "", "", false
	}
	id, isIdent := as.Lhs[0].(*ast.Ident)
	if !isIdent {
		return "", "", false
	}
	call, isCall := as.Rhs[0].(*ast.CallExpr)
	if !isCall {
		return "", "", false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != "span" || len(call.Args) != 1 {
		return "", "", false
	}
	lit, isLit := call.Args[0].(*ast.BasicLit)
	if !isLit || lit.Kind != token.STRING {
		return "", "", false
	}
	return id.Name, strings.Trim(lit.Value, `"`), true
}

// isCallToIdent matches a bare `name()` expression statement.
func isCallToIdent(st ast.Stmt, name string) bool {
	es, ok := st.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == name
}

// scanForUnclosedReturns collects every ReturnStmt reachable from stmts while
// the span named endName is still open. Reaching `endName()` closes the span
// for the remainder of THAT statement list (and only that list — statements
// after a close inside a nested block are outside the span, while the enclosing
// list is unaffected).
//
// Function literals are not descended into: their returns exit the closure, not
// the pass, and they cannot escape a span.
func scanForUnclosedReturns(stmts []ast.Stmt, endName string, bad *[]ast.Stmt) {
	for _, st := range stmts {
		if isCallToIdent(st, endName) {
			return
		}
		if r, ok := st.(*ast.ReturnStmt); ok {
			*bad = append(*bad, r)
			continue
		}
		for _, sub := range stmtLists(st) {
			scanForUnclosedReturns(sub, endName, bad)
		}
	}
}

// stmtLists returns the statement lists nested directly in n, skipping function
// literals (a FuncLit is reached through an expression, never through one of
// these fields, so it is excluded by construction).
func stmtLists(n ast.Node) [][]ast.Stmt {
	switch s := n.(type) {
	case *ast.BlockStmt:
		return [][]ast.Stmt{s.List}
	case *ast.IfStmt:
		var out [][]ast.Stmt
		if s.Body != nil {
			out = append(out, s.Body.List)
		}
		if s.Else != nil {
			out = append(out, stmtLists(s.Else)...)
		}
		return out
	case *ast.ForStmt:
		if s.Body != nil {
			return [][]ast.Stmt{s.Body.List}
		}
	case *ast.RangeStmt:
		if s.Body != nil {
			return [][]ast.Stmt{s.Body.List}
		}
	case *ast.SwitchStmt:
		if s.Body != nil {
			return [][]ast.Stmt{s.Body.List}
		}
	case *ast.TypeSwitchStmt:
		if s.Body != nil {
			return [][]ast.Stmt{s.Body.List}
		}
	case *ast.SelectStmt:
		if s.Body != nil {
			return [][]ast.Stmt{s.Body.List}
		}
	case *ast.CaseClause:
		return [][]ast.Stmt{s.Body}
	case *ast.CommClause:
		return [][]ast.Stmt{s.Body}
	case *ast.LabeledStmt:
		return stmtLists(s.Stmt)
	case *ast.FuncDecl:
		if s.Body != nil {
			return [][]ast.Stmt{s.Body.List}
		}
	}
	return nil
}
