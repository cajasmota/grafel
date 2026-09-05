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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/repowalk"
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
		// A return inside the deferred body before the emit would skip it — at
		// ANY nesting depth, which is why this is countReturns and not a
		// type-assertion to *ast.ReturnStmt. Asserting on the top level only was
		// the bug: `if !returned { return }` is a *ast.IfStmt, so the emit-less
		// panic path sailed straight past this check and
		// TestPhaseTrace_EveryReturnIsTraced passed on a mutant that emitted
		// nothing when the pass panicked. countReturns is also what the BARE
		// branch has always used, so the two shapes now really do carry the same
		// "unconditional" guarantee this comment claims for them.
		if countReturns(inner) > 0 {
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
//
// The wrapper's only job is the ROOT. Splitting the walk out (findCallSitesIn)
// is what lets TestFindCallSitesIn_DoesNotDescendIntoAgentWorktrees drive it
// over a synthetic tree; the risk of that split — a parameterised decision the
// caller is then free to skip — is covered because
// TestPhaseTrace_EveryReturnIsTraced consumes THIS function and requires
// exactly one site, so a wrapper that lost the module root would find zero and
// fail.
func findCallSites(t *testing.T, name string) []callSite {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("abs module root: %v", err)
	}
	return findCallSitesIn(t, root, name)
}

// findCallSitesIn is the walk, parameterised on its root.
//
// The exclusion list is repowalk.SkippedDir (#6846). It used to be a
// hand-written `.git, node_modules, testdata, vendor` switch with NO .claude
// case, and this walk is rooted at the MODULE root — so in any development
// checkout every agent worktree contributed its own copy of every call site.
// That made the "exactly one call site" assertion above fail outright, with no
// parse error needed: worse than the atomicfile instance #6846 was filed for,
// and firing on every local run rather than only on a mid-edit branch.
func findCallSitesIn(t *testing.T, root, name string) []callSite {
	t.Helper()
	var sites []callSite
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if repowalk.SkippedDir(d.Name()) {
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
				var esc []spanEscape
				var shadows []ast.Stmt
				scanForSpanEscapes(list[i+1:close], endName, spanScope{}, &esc, &shadows)
				for _, e := range esc {
					problems = append(problems, fmt.Sprintf(
						"span %q (%s) is still open at the %s on %s: that path reports no "+
							"%q phase at all, and accounted_ms underreports total_ms by it",
						phase, endName, e.kind, fset.Position(e.node.Pos()), phase))
				}
				for _, s := range shadows {
					problems = append(problems, fmt.Sprintf(
						"span %q (%s) is re-declared at %s while the outer span is still open: "+
							"every %s() after that point closes the INNER span, so this audit "+
							"cannot tell whether the outer one is ever closed — rename it",
						phase, endName, fset.Position(s.Pos()), endName))
				}
			}
		}
		return true
	})

	// A span opening the audit did NOT recognise is not a span the audit can
	// vouch for, and silently skipping it is how MUTANT-A survived: a bare
	// `tr.span("x")` expression statement compiles (unlike `end := tr.span("x")`,
	// which "declared and not used" rejects), drops the end func on the floor so
	// the phase never closes, and was not even counted here. The PR body claimed
	// the compiler covered the never-closed case; it covers exactly one spelling
	// of it. So: every X.span(...) call in the file must be the RHS of a
	// single-identifier assignment, and anything else is reported rather than
	// ignored.
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "span" || recognised[call.Pos()] {
			return true
		}
		problems = append(problems, fmt.Sprintf(
			"the %s(...) call at %s is not bound to a single identifier "+
				"(`end := tr.span(\"phase\")`), so this audit cannot check that it is ever "+
				"closed; if the end func is discarded the phase never closes at all",
			exprString(sel), fset.Position(call.Pos())))
		return true
	})

	return spans, problems
}

// exprString renders `tr.span` for the message above without pulling in
// go/printer for a two-node selector.
func exprString(sel *ast.SelectorExpr) string {
	if id, ok := sel.X.(*ast.Ident); ok {
		return id.Name + "." + sel.Sel.Name
	}
	return sel.Sel.Name
}

// spanOpen matches `endX := tr.span("phase")` and returns endX and the phase.
func spanOpen(st ast.Stmt) (endName, phase string, ok bool) {
	as, isAssign := st.(*ast.AssignStmt)
	if !isAssign || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return "", "", false
	}
	id, isIdent := as.Lhs[0].(*ast.Ident)
	if !isIdent || id.Name == "_" {
		// `_ = tr.span("x")` throws the end func away. It is not an opening this
		// audit can follow, so it falls through to the unrecognised-call check
		// in auditSpans and is reported there.
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

// spanEscape is one statement that leaves the span's region while the span is
// still open, and the kind of jump it is.
type spanEscape struct {
	node ast.Stmt
	kind string // "return", "break", "continue" or "goto"
}

// spanScope records what the scan has descended THROUGH since the span was
// opened, which is what decides whether a break/continue escapes the span at
// all. A `continue` inside a loop that is itself inside the span region jumps to
// that loop's next iteration and eventually falls out to the close, so it is
// harmless; a `continue` in a span opened INSIDE the loop body jumps straight
// past the close. The difference is exactly "has a loop been crossed", so that
// is what is tracked.
type spanScope struct {
	inLoop   bool            // a for/range inside the span region encloses us
	inSwitch bool            // a switch/select inside the span region encloses us
	labels   map[string]bool // labels of loop/switch statements inside the region
}

func (sc spanScope) withLabel(name string) spanScope {
	labels := make(map[string]bool, len(sc.labels)+1)
	for k := range sc.labels {
		labels[k] = true
	}
	labels[name] = true
	sc.labels = labels
	return sc
}

// scanForSpanEscapes collects every statement reachable from stmts that leaves
// the region while the span named endName is still open — a return, and also a
// break/continue/goto that jumps past the close. Reaching `endName()` closes the
// span for the remainder of THAT statement list (and only that list — statements
// after a close inside a nested block are outside the span, while the enclosing
// list is unaffected).
//
// It also reports SHADOWING. Matching on the name alone was not enough: a
// `endGraphLoad := tr.span(...)` re-declared in a nested block rebinds the name,
// so the `endGraphLoad()` that follows closes the inner span and the outer one
// is still open at the return — and the scan, which stopped at the first
// matching name it met, called that closed. Rather than model the two bindings,
// the shadowing itself is reported: it defeats the audit, and no phase needs it.
//
// Function literals are not descended into: their returns exit the closure, not
// the pass, and they cannot escape a span.
func scanForSpanEscapes(stmts []ast.Stmt, endName string, sc spanScope, esc *[]spanEscape, shadows *[]ast.Stmt) {
	for _, st := range stmts {
		if isCallToIdent(st, endName) {
			return
		}
		if redeclaresIdent(st, endName) {
			*shadows = append(*shadows, st)
			return
		}
		switch s := st.(type) {
		case *ast.ReturnStmt:
			*esc = append(*esc, spanEscape{node: s, kind: "return"})
			continue
		case *ast.BranchStmt:
			if kind, escapes := branchEscapes(s, sc); escapes {
				*esc = append(*esc, spanEscape{node: s, kind: kind})
			}
			continue
		}
		for _, sub := range childScopes(st, sc) {
			scanForSpanEscapes(sub.stmts, endName, sub.scope, esc, shadows)
		}
	}
}

// branchEscapes reports whether a break/continue/goto jumps out of the span's
// region, given what the scan has descended through to reach it.
func branchEscapes(b *ast.BranchStmt, sc spanScope) (kind string, escapes bool) {
	switch b.Tok {
	case token.GOTO:
		// A goto can land anywhere, including past the close. There are none in
		// this package; if one appears, it is worth a human look.
		return "goto", true
	case token.FALLTHROUGH:
		return "", false
	}
	kind = strings.ToLower(b.Tok.String())
	if b.Label != nil {
		// A labelled jump targets an outer construct unless that construct is
		// itself inside the span's region.
		return kind, !sc.labels[b.Label.Name]
	}
	if b.Tok == token.CONTINUE {
		return kind, !sc.inLoop
	}
	return kind, !(sc.inLoop || sc.inSwitch)
}

// redeclaresIdent reports whether st re-declares name — `name := ...` or
// `var name = ...` — which shadows an outer span's end func.
func redeclaresIdent(st ast.Stmt, name string) bool {
	switch s := st.(type) {
	case *ast.AssignStmt:
		if s.Tok != token.DEFINE {
			return false
		}
		for _, lhs := range s.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
				return true
			}
		}
	case *ast.DeclStmt:
		gd, ok := s.Decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			return false
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, id := range vs.Names {
				if id.Name == name {
					return true
				}
			}
		}
	}
	return false
}

// scopedStmts is a nested statement list plus the scope it is reached through.
type scopedStmts struct {
	stmts []ast.Stmt
	scope spanScope
}

// childScopes returns the statement lists nested directly in st, each tagged
// with the scope it is reached through, skipping function literals (a FuncLit is
// reached through an expression, never through one of these fields, so it is
// excluded by construction).
func childScopes(n ast.Node, sc spanScope) []scopedStmts {
	loop := sc
	loop.inLoop = true
	sw := sc
	sw.inSwitch = true

	switch s := n.(type) {
	case *ast.BlockStmt:
		return []scopedStmts{{s.List, sc}}
	case *ast.IfStmt:
		var out []scopedStmts
		if s.Body != nil {
			out = append(out, scopedStmts{s.Body.List, sc})
		}
		if s.Else != nil {
			out = append(out, childScopes(s.Else, sc)...)
		}
		return out
	case *ast.ForStmt:
		if s.Body != nil {
			return []scopedStmts{{s.Body.List, loop}}
		}
	case *ast.RangeStmt:
		if s.Body != nil {
			return []scopedStmts{{s.Body.List, loop}}
		}
	case *ast.SwitchStmt:
		if s.Body != nil {
			return []scopedStmts{{s.Body.List, sw}}
		}
	case *ast.TypeSwitchStmt:
		if s.Body != nil {
			return []scopedStmts{{s.Body.List, sw}}
		}
	case *ast.SelectStmt:
		if s.Body != nil {
			return []scopedStmts{{s.Body.List, sw}}
		}
	case *ast.CaseClause:
		return []scopedStmts{{s.Body, sc}}
	case *ast.CommClause:
		return []scopedStmts{{s.Body, sc}}
	case *ast.LabeledStmt:
		return childScopes(s.Stmt, sc.withLabel(s.Label.Name))
	case *ast.FuncDecl:
		if s.Body != nil {
			return []scopedStmts{{s.Body.List, sc}}
		}
	}
	return nil
}

// --- the audit's own mutants -----------------------------------------------
//
// The two structural proofs above are checkers, and a checker that cannot be
// shown to REJECT anything is worth as little as a test that cannot fail. Each
// case below is a mutant that compiled against the real incremental.go and left
// the audit PASSING before the fix that accompanies it; they are kept here,
// against synthetic sources, so the checkers stay able to reject them without
// anybody having to hand-mutate a 117KB production file again.

// parseSnippet parses a self-contained source string for the audit helpers.
func parseSnippet(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "mutant.go", src, 0)
	if err != nil {
		t.Fatalf("snippet does not parse (the mutant must be valid Go, or it "+
			"proves nothing the compiler would not have caught): %v\n%s", err, src)
	}
	return fset, f
}

func TestPhaseTrace_SpanAuditCatchesMutants(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // substring of the expected problem; "" means "must be clean"
	}{{
		// FINDING 2. `end := tr.span(...)` is protected by "declared and not
		// used", and `_ = tr.span(...)` was already caught here — but a bare
		// call statement escapes BOTH. It compiles, the end func is dropped on
		// the floor, and the audit did not even count the span.
		name: "MUTANT-A: span opened as a discarded expression statement",
		src: `package p
func f() {
	tr.span("MUTANT-A-discarded")
	work()
}`,
		want: "not bound to a single identifier",
	}, {
		name: "MUTANT-A': span assigned to the blank identifier",
		src: `package p
func f() {
	_ = tr.span("MUTANT-A-blank")
	work()
}`,
		want: "not bound to a single identifier",
	}, {
		name: "MUTANT-A'': a span the audit cannot verify is rejected, not skipped",
		src: `package p
func f() {
	defer tr.span("MUTANT-A-nested")()
	work()
}`,
		want: "not bound to a single identifier",
	}, {
		// FINDING 3. The scan matched on the NAME only and stopped at the first
		// end() it met, so an inner span shadowing the outer one laundered the
		// escape: the close belongs to the inner span and the outer one is
		// still open at the return.
		name: "MUTANT-B: an inner span shadows the outer one and closes it instead",
		src: `package p
func f() {
	endGraphLoad := tr.span("graph-materialise")
	doc, loadErr := load()
	if loadErr != nil {
		endGraphLoad := tr.span("MUTANT-B-shadow")
		endGraphLoad()
		return fallback(doc)
	}
	endGraphLoad()
}`,
		want: "re-declared",
	}, {
		name: "MUTANT-B': var re-declaration shadows the outer span",
		src: `package p
func f() {
	endGraphLoad := tr.span("graph-materialise")
	if bad() {
		var endGraphLoad = tr.span("MUTANT-B-var-shadow")
		endGraphLoad()
		return
	}
	endGraphLoad()
}`,
		want: "re-declared",
	}, {
		// FINDING 4. Only ReturnStmt was collected, so a BranchStmt jumping
		// past the close was invisible. Latent today — the one loop-adjacent
		// span is opened outside its loop — but a per-file span is exactly the
		// span someone would add next.
		name: "MUTANT-D: continue skips the close of a span opened in the loop body",
		src: `package p
func f() {
	for _, mrel := range reallyChanged {
		endPerFile := tr.span("MUTANT-D-perfile")
		if mrel == "" {
			continue
		}
		endPerFile()
	}
}`,
		want: "continue",
	}, {
		name: "MUTANT-D': break skips the close of a span opened in the loop body",
		src: `package p
func f() {
	for _, mrel := range reallyChanged {
		endPerFile := tr.span("MUTANT-D-break")
		if mrel == "" {
			break
		}
		endPerFile()
	}
}`,
		want: "break",
	}, {
		name: "MUTANT-D'': labelled continue jumps out of the loop the span lives in",
		src: `package p
func f() {
outer:
	for range a {
		end := tr.span("MUTANT-D-labelled")
		for range b {
			continue outer
		}
		end()
	}
}`,
		want: "continue",
	}, {
		name: "MUTANT-D''': goto jumps past the close",
		src: `package p
func f() {
	end := tr.span("MUTANT-D-goto")
	if bad() {
		goto done
	}
	end()
done:
	return
}`,
		want: "goto",
	}, {
		// The original defect, kept as a regression case.
		name: "return inside an open span",
		src: `package p
func f() {
	endExtract := tr.span("extract-changed-files")
	if err != nil {
		return fallback(err)
	}
	endExtract()
}`,
		want: "return",
	}, {
		name: "a span never closed at all",
		src: `package p
func f() {
	endExtract := tr.span("extract-changed-files")
	work()
}`,
		want: "never closed",
	},
		// --- negative controls: the shapes the real code actually uses -----
		// A checker that flags these would be unusable, so they are pinned too.
		{
			name: "clean: open, work, close",
			src: `package p
func f() {
	end := tr.span("clean")
	work()
	end()
}`,
		}, {
			name: "clean: continue inside a loop that is itself inside the span",
			src: `package p
func f() {
	end := tr.span("extract-changed-files")
	for _, x := range files {
		if x == "" {
			continue
		}
		work(x)
	}
	end()
}`,
		}, {
			name: "clean: break inside a switch inside the span",
			src: `package p
func f() {
	end := tr.span("clean-switch")
	switch kind {
	case 1:
		break
	default:
		work()
	}
	end()
}`,
		}, {
			name: "clean: the span is closed before the early return",
			src: `package p
func f() {
	end := tr.span("clean-early-return")
	if bad() {
		end()
		return
	}
	end()
}`,
		}, {
			name: "clean: labelled continue to a loop that is itself inside the span",
			src: `package p
func f() {
	end := tr.span("clean-labelled")
outer:
	for range a {
		for range b {
			continue outer
		}
	}
	end()
}`,
		}, {
			name: "clean: a return inside a func literal does not exit the pass",
			src: `package p
func f() {
	end := tr.span("clean-funclit")
	g(func() error {
		return nil
	})
	end()
}`,
		}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset, file := parseSnippet(t, tc.src)
			_, problems := auditSpans(fset, file)
			joined := strings.Join(problems, "\n  ")
			if tc.want == "" {
				if len(problems) > 0 {
					t.Fatalf("this is a shape the real code uses and the audit must accept it, "+
						"but it reported %d problem(s):\n  %s", len(problems), joined)
				}
				return
			}
			if len(problems) == 0 {
				t.Fatalf("the audit found NOTHING wrong with this mutant; it compiles, it "+
					"breaks the phase accounting, and the audit passes — which is the whole "+
					"defect. Want a problem mentioning %q. Source:\n%s", tc.want, tc.src)
			}
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("the audit complained, but not about the right thing: want a problem "+
					"mentioning %q, got:\n  %s", tc.want, joined)
			}
		})
	}
}

// TestPhaseTrace_EmitAuditCatchesMutants pins FINDING 1: the deferred branch of
// isEmitCall rejected only a TOP-LEVEL ReturnStmt before the emit, so a return
// nested one level down — `if !returned { return }` — was invisible to it and
// TestPhaseTrace_EveryReturnIsTraced passed with a panic path that emitted
// nothing. The bare branch has always used countReturns, which does descend, so
// this was also an asymmetry between the two shapes the doc comment claims are
// equivalent.
func TestPhaseTrace_EmitAuditCatchesMutants(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool // whether the single statement in f's body must count as the emit
	}{{
		name: "MUTANT-C: a nested return in the deferred body skips the emit",
		src: `package p
func f() {
	defer func() {
		if !returned {
			return
		}
		tr.emit(res)
	}()
}`,
		want: false,
	}, {
		name: "a top-level return in the deferred body skips the emit",
		src: `package p
func f() {
	defer func() {
		return
		tr.emit(res)
	}()
}`,
		want: false,
	}, {
		name: "a return nested two levels down still skips the emit",
		src: `package p
func f() {
	defer func() {
		for _, x := range xs {
			if x == nil {
				return
			}
		}
		tr.emit(res)
	}()
}`,
		want: false,
	}, {
		name: "the shipped shape: deferred and unconditional",
		src: `package p
func f() {
	defer func() {
		changed := res.ChangedFiles
		tr.emit(changed)
	}()
}`,
		want: true,
	}, {
		name: "the bare shape",
		src: `package p
func f() {
	tr.emit(res)
}`,
		want: true,
	}, {
		name: "a conditional emit is not an emit",
		src: `package p
func f() {
	if traced {
		tr.emit(res)
	}
}`,
		want: false,
	}, {
		name: "a return in a func literal INSIDE the deferred body is not an escape",
		src: `package p
func f() {
	defer func() {
		g(func() error { return nil })
		tr.emit(res)
	}()
}`,
		want: true,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, file := parseSnippet(t, tc.src)
			var fn *ast.FuncDecl
			for _, d := range file.Decls {
				if d, ok := d.(*ast.FuncDecl); ok && d.Name.Name == "f" {
					fn = d
				}
			}
			if fn == nil || len(fn.Body.List) != 1 {
				t.Fatalf("snippet must contain exactly one statement in f")
			}
			if got := isEmitCall(fn.Body.List[0]); got != tc.want {
				t.Fatalf("isEmitCall = %v, want %v — a %q that the checker gets wrong means "+
					"TestPhaseTrace_EveryReturnIsTraced is not proving what it says. Source:\n%s",
					got, tc.want, tc.name, tc.src)
			}
		})
	}
}

// TestFindCallSitesIn_DoesNotDescendIntoAgentWorktrees pins the #6846 fix on
// this walk, which is rooted at the MODULE root and had no `.claude` case.
//
// This instance was strictly worse than the internal/atomicfile one #6846 was
// filed for. That one needed a mid-edit parse error in a worktree to fire; this
// one needs nothing but a worktree, because every checkout of this repository
// contains internal/extractors/incremental.go and therefore its call to
// tryIncremental. Every extra worktree adds a site, so
// TestPhaseTrace_EveryReturnIsTraced's "exactly ONE call site" assertion failed
// on every local run in a checkout with agent worktrees. Measured before the
// fix, with one planted worktree:
//
//	incremental_phasetrace_6199_test.go:103: tryIncremental must have exactly
//	ONE call site … found 2: [{shadowCaller …/.claude/worktrees/fake-agent/
//	internal/extractors/shadow.go:3:27} {TryIncremental …/internal/extractors/
//	incremental.go:427:8}]
//
// The tree is built here rather than taken from the ambient checkout: CI has no
// worktrees, so an ambient-tree test would pass exactly where the bug does not
// reproduce and stay silent where it does.
func TestFindCallSitesIn_DoesNotDescendIntoAgentWorktrees(t *testing.T) {
	root := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	const caller = `package p

func Real() { tryIncremental() }
`
	// Ordinary source: MUST be found, or the test passes on a walk that reads
	// nothing at all.
	write("internal/extractors/incremental.go", caller)
	// An agent worktree's copy: must NOT be found.
	write(".claude/worktrees/agent-x/internal/extractors/incremental.go", caller)
	// A directory whose name merely CONTAINS "claude" is ordinary source and
	// must still be walked — the exclusion is an exact base-name match.
	write(".claude-backup/internal/extractors/incremental.go", caller)

	sites := findCallSitesIn(t, root, "tryIncremental")
	var got []string
	for _, s := range sites {
		p := s.pos
		if i := strings.Index(p, root); i == 0 {
			p = strings.TrimPrefix(p[len(root):], string(filepath.Separator))
		}
		got = append(got, filepath.ToSlash(p))
	}
	sort.Strings(got)

	want := []string{
		".claude-backup/internal/extractors/incremental.go:3:15",
		"internal/extractors/incremental.go:3:15",
	}
	if len(got) != len(want) {
		t.Fatalf("findCallSitesIn reported %v; want exactly %v.\n"+
			"An extra entry under .claude/worktrees/ means the walk descended into an agent "+
			"worktree (#6846); a missing entry means it stopped reading real source.", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("findCallSitesIn reported %v; want exactly %v", got, want)
		}
	}
	for _, s := range sites {
		if s.enclosing != "Real" {
			t.Errorf("call site %s reported enclosing func %q, want \"Real\" — the walk is "+
				"reading the file but not resolving what encloses the call", s.pos, s.enclosing)
		}
	}
}
