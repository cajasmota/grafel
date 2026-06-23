// data_dispatch_test.go — unit tests for issue #1709: data-structure-driven
// dispatch resolution (STEPS = [(steps.f, ...), ...] + for f in STEPS: f(ctx)).
package python_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/python" // trigger init()
)

// ---------------------------------------------------------------------------
// Helper: extract entities from a Python source string.
// ---------------------------------------------------------------------------

func ddExtract(t *testing.T, filePath, src string) []ddRel {
	t.Helper()
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     filePath,
		Content:  []byte(src),
		Language: "python",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Collect all CALLS relationships from all entities.
	var out []ddRel
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" {
				out = append(out, ddRel{
					from:         e.Name,
					toID:         r.ToID,
					importAlias:  r.Properties["import_alias"],
					callLeaf:     r.Properties["call_leaf"],
					dataDispatch: r.Properties["data_dispatch"] == "1",
				})
			}
		}
	}
	return out
}

type ddRel struct {
	from         string
	toID         string
	importAlias  string
	callLeaf     string
	dataDispatch bool
}

func ddFindCallsEdge(rels []ddRel, from, alias, leaf string) *ddRel {
	for i := range rels {
		r := &rels[i]
		if r.from == from && r.importAlias == alias && r.callLeaf == leaf {
			return r
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// TestDataDispatch_BasicSTEPSTuple — canonical PlaceOrderSaga pattern.
// STEPS is a module-level list of (callable, str) tuples. The saga's run
// method iterates and calls the loop variable directly.
// ---------------------------------------------------------------------------

func TestDataDispatch_BasicSTEPSTuple(t *testing.T) {
	src := `from . import steps

STEPS = [
    (steps.create_order,   "order_created"),
    (steps.reserve_stock,  "stock_reserved"),
    (steps.charge_payment, "payment_charged"),
    (steps.notify_user,    "notified"),
]

class PlaceOrderSaga:
    def run(self, ctx):
        for f, _ in STEPS:
            f(ctx)
`
	rels := ddExtract(t, "sagas/place_order.py", src)

	// Expect 4 data-dispatch CALLS edges from PlaceOrderSaga.run.
	stepFns := []string{"create_order", "reserve_stock", "charge_payment", "notify_user"}
	for _, fn := range stepFns {
		r := ddFindCallsEdge(rels, "PlaceOrderSaga.run", "steps", fn)
		if r == nil {
			t.Errorf("missing CALLS edge from PlaceOrderSaga.run via steps.%s; got: %+v", fn, rels)
			continue
		}
		if !r.dataDispatch {
			t.Errorf("CALLS edge steps.%s missing data_dispatch=1 marker", fn)
		}
		if r.toID != fn {
			t.Errorf("CALLS edge steps.%s: want ToID=%q, got %q", fn, fn, r.toID)
		}
	}
}

// TestDataDispatch_FlatList — STEPS is a flat list of attribute callables
// (no inner tuple).
func TestDataDispatch_FlatList(t *testing.T) {
	src := `from . import validators

PIPELINE = [
    validators.check_schema,
    validators.check_auth,
    validators.check_rate_limit,
]

def run_pipeline(req):
    for fn in PIPELINE:
        fn(req)
`
	rels := ddExtract(t, "api/pipeline.py", src)

	expected := []string{"check_schema", "check_auth", "check_rate_limit"}
	for _, fn := range expected {
		r := ddFindCallsEdge(rels, "run_pipeline", "validators", fn)
		if r == nil {
			t.Errorf("missing CALLS edge from run_pipeline via validators.%s; got: %+v", fn, rels)
		}
	}
}

// TestDataDispatch_UpperCaseName — constant with UPPER_CASE name (canonical
// Python convention). Verifies the UPPER_CASE pre-filter doesn't block it.
func TestDataDispatch_UpperCaseName(t *testing.T) {
	src := `import handlers

HANDLERS = [
    handlers.on_created,
    handlers.on_updated,
]

class EventDispatcher:
    def dispatch(self, event):
        for h in HANDLERS:
            h(event)
`
	rels := ddExtract(t, "events/dispatcher.py", src)

	for _, fn := range []string{"on_created", "on_updated"} {
		r := ddFindCallsEdge(rels, "EventDispatcher.dispatch", "handlers", fn)
		if r == nil {
			t.Errorf("missing CALLS edge from EventDispatcher.dispatch via handlers.%s; all rels: %+v", fn, rels)
		}
	}
}

// TestDataDispatch_TupleFirstPosition — element is a tuple where position 0
// is the callable and position 1 is a string tag. Verifies that tuple-wrapped
// callables are found at position 0.
func TestDataDispatch_TupleFirstPosition(t *testing.T) {
	src := `from . import steps

STEPS = [
    (steps.create, "create"),
    (steps.validate, "validate"),
]

def execute(ctx):
    for step_fn, label in STEPS:
        step_fn(ctx)
`
	rels := ddExtract(t, "workflow/exec.py", src)

	for _, fn := range []string{"create", "validate"} {
		r := ddFindCallsEdge(rels, "execute", "steps", fn)
		if r == nil {
			t.Errorf("missing CALLS edge from execute via steps.%s; all rels: %+v", fn, rels)
		}
	}
}

// TestDataDispatch_NoFire_DirectCalls — when the saga also calls steps
// directly (the #1706 path), the data-dispatch pass must not double-emit
// the same edge. Verifies de-duplication.
func TestDataDispatch_NoFire_DirectCalls(t *testing.T) {
	src := `from . import steps

STEPS = [
    (steps.create_order, "created"),
]

class Saga:
    def run(self, ctx):
        steps.create_order(ctx)  # direct call from #1706
        for f, _ in STEPS:
            f(ctx)              # data-dispatch call from #1709
`
	rels := ddExtract(t, "sagas/saga.py", src)

	// Count how many CALLS edges have alias=steps + leaf=create_order from Saga.run.
	count := 0
	for _, r := range rels {
		if r.from == "Saga.run" && r.importAlias == "steps" && r.callLeaf == "create_order" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 CALLS edge for steps.create_order from Saga.run (de-dup), got %d; rels: %+v", count, rels)
	}
}

// TestDataDispatch_NoFire_MutatedConst — if the constant name is reassigned
// anywhere in the file, the pass must NOT fire (mutation guard).
func TestDataDispatch_NoFire_MutatedConst(t *testing.T) {
	src := `from . import steps

STEPS = [
    (steps.create_order, "ok"),
]

# Reassignment — mutation guard must detect this.
STEPS = STEPS + [(steps.extra, "extra")]

def run(ctx):
    for f, _ in STEPS:
        f(ctx)
`
	rels := ddExtract(t, "sagas/mut.py", src)

	for _, r := range rels {
		if r.dataDispatch && r.from == "run" {
			t.Errorf("data-dispatch must not fire when constant is reassigned; got: %+v", r)
		}
	}
}

// TestDataDispatch_NoFire_NotIterable — the constant is not a list/tuple, so
// the scanner must not register it as a callable registry.
func TestDataDispatch_NoFire_NotIterable(t *testing.T) {
	src := `from . import steps

# Not a list/tuple — dict.
STEPS = {
    "create": steps.create_order,
}

def run(ctx):
    for key in STEPS:
        STEPS[key](ctx)
`
	rels := ddExtract(t, "sagas/dict.py", src)
	for _, r := range rels {
		if r.dataDispatch {
			t.Errorf("data-dispatch must not fire for dict constant; got: %+v", r)
		}
	}
}

// TestDataDispatch_NoFire_SubscriptCall — loop variable is called via
// subscript `f[0](...)` not direct `f(...)`. Must not fire.
func TestDataDispatch_NoFire_SubscriptCall(t *testing.T) {
	src := `from . import steps

STEPS = [
    (steps.create_order, "ok"),
]

def run(ctx):
    for item in STEPS:
        item[0](ctx)  # subscript call — not the simple loop-var pattern
`
	rels := ddExtract(t, "sagas/subscript.py", src)
	for _, r := range rels {
		if r.dataDispatch && r.from == "run" {
			t.Errorf("data-dispatch must not fire for subscript call item[0](...); got: %+v", r)
		}
	}
}

// TestDataDispatch_NoFire_UnknownReceiver — attribute reference whose
// receiver is not an import alias or PascalCase class name. Must not fire.
func TestDataDispatch_NoFire_UnknownReceiver(t *testing.T) {
	src := `# No imports — 'steps' is not a known alias.

STEPS = [
    (steps.create, "ok"),
]

def run(ctx):
    for f, _ in STEPS:
        f(ctx)
`
	rels := ddExtract(t, "sagas/unknown.py", src)
	for _, r := range rels {
		if r.dataDispatch && r.callLeaf == "create" {
			t.Errorf("data-dispatch must not fire when receiver is not a known import; got: %+v", r)
		}
	}
}

// TestDataDispatch_NoFire_BodyReassigns — the loop body assigns to the loop
// variable (mutation in body). Must not fire.
func TestDataDispatch_NoFire_BodyReassigns(t *testing.T) {
	src := `from . import steps

STEPS = [
    (steps.create, "ok"),
]

def run(ctx):
    for f, _ in STEPS:
        f = steps.create  # reassign loop var — conservative: skip
        f(ctx)
`
	rels := ddExtract(t, "sagas/body_mut.py", src)
	for _, r := range rels {
		if r.dataDispatch && r.from == "run" {
			t.Errorf("data-dispatch must not fire when loop var is reassigned in body; got: %+v", r)
		}
	}
}

// TestDataDispatch_DispatchSourceProperty — verifies that the dispatch_source
// property on the emitted edge names the constant.
func TestDataDispatch_DispatchSourceProperty(t *testing.T) {
	src := `from . import steps

MY_STEPS = [
    (steps.step_one, "one"),
]

def run(ctx):
    for f, _ in MY_STEPS:
        f(ctx)
`
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "sagas/dispatch_source.py",
		Content:  []byte(src),
		Language: "python",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	for _, e := range ents {
		if e.Name != "run" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && r.Properties["data_dispatch"] == "1" {
				if src := r.Properties["dispatch_source"]; src != "MY_STEPS" {
					t.Errorf("expected dispatch_source=MY_STEPS, got %q", src)
				}
				return
			}
		}
	}
	t.Error("no data-dispatch CALLS edge found on 'run'")
}

// TestDataDispatch_MultipleConstants — two constants, two different dispatch
// methods. Each method should only see edges from its own constant.
func TestDataDispatch_MultipleConstants(t *testing.T) {
	src := `from . import a_steps
from . import b_steps

A_STEPS = [
    (a_steps.step1, "s1"),
    (a_steps.step2, "s2"),
]

B_STEPS = [
    (b_steps.process, "p1"),
]

class A:
    def run(self, ctx):
        for f, _ in A_STEPS:
            f(ctx)

class B:
    def run(self, ctx):
        for f, _ in B_STEPS:
            f(ctx)
`
	rels := ddExtract(t, "wf/multi.py", src)

	// A.run should have a_steps.step1 and a_steps.step2, NOT b_steps.process.
	if r := ddFindCallsEdge(rels, "A.run", "a_steps", "step1"); r == nil {
		t.Errorf("A.run missing a_steps.step1; rels: %+v", rels)
	}
	if r := ddFindCallsEdge(rels, "A.run", "a_steps", "step2"); r == nil {
		t.Errorf("A.run missing a_steps.step2; rels: %+v", rels)
	}
	if r := ddFindCallsEdge(rels, "A.run", "b_steps", "process"); r != nil {
		t.Errorf("A.run must NOT have b_steps.process edge; got: %+v", r)
	}

	// B.run should have b_steps.process only.
	if r := ddFindCallsEdge(rels, "B.run", "b_steps", "process"); r == nil {
		t.Errorf("B.run missing b_steps.process; rels: %+v", rels)
	}
	if r := ddFindCallsEdge(rels, "B.run", "a_steps", "step1"); r != nil {
		t.Errorf("B.run must NOT have a_steps.step1 edge; got: %+v", r)
	}
}

// TestDataDispatch_NoFire_ListComprehension — STEPS is built with a list
// comprehension. The scanner should not register it (RHS is not a bare list
// literal).
func TestDataDispatch_NoFire_ListComprehension(t *testing.T) {
	src := `from . import steps

ALL = [steps.create, steps.validate]
STEPS = [fn for fn in ALL]  # comprehension — skip

def run(ctx):
    for f in STEPS:
        f(ctx)
`
	rels := ddExtract(t, "sagas/comp.py", src)
	for _, r := range rels {
		if r.dataDispatch && r.from == "run" {
			t.Errorf("data-dispatch must not fire for comprehension-built constant; got: %+v", r)
		}
	}
}

// TestDataDispatch_Regression_DirectCallsUnaffected — verifies that direct
// attribute calls (the #1706 path) still work correctly when data-dispatch is
// also active in the same file.
func TestDataDispatch_Regression_DirectCallsUnaffected(t *testing.T) {
	src := `from . import steps

STEPS = [
    (steps.create, "create"),
]

class Saga:
    def run(self, ctx):
        steps.validate(ctx)    # direct cross-module call (#1706)
        for f, _ in STEPS:
            f(ctx)             # data-dispatch (#1709)
`
	rels := ddExtract(t, "sagas/regression.py", src)

	// Direct call must be present and NOT tagged as data_dispatch.
	foundDirect := false
	for _, r := range rels {
		if r.from == "Saga.run" && r.importAlias == "steps" && r.callLeaf == "validate" {
			foundDirect = true
			if r.dataDispatch {
				t.Errorf("direct call steps.validate must not be tagged data_dispatch=1")
			}
		}
	}
	if !foundDirect {
		t.Errorf("missing direct CALLS edge for steps.validate from Saga.run; rels: %+v", rels)
	}

	// Data-dispatch call must be present and tagged.
	if r := ddFindCallsEdge(rels, "Saga.run", "steps", "create"); r == nil {
		t.Errorf("missing data-dispatch CALLS edge for steps.create from Saga.run; rels: %+v", rels)
	} else if !r.dataDispatch {
		t.Errorf("steps.create via data-dispatch must be tagged data_dispatch=1")
	}
}

// TestDataDispatch_PascalCaseReceiver — STEPS contains callable references
// whose receiver is a PascalCase class name (class-level registry pattern).
func TestDataDispatch_PascalCaseReceiver(t *testing.T) {
	src := `# No import needed — class is declared locally.

class Handler:
    @staticmethod
    def on_created(event):
        pass

    @staticmethod
    def on_deleted(event):
        pass

HANDLERS = [
    Handler.on_created,
    Handler.on_deleted,
]

def dispatch(event):
    for h in HANDLERS:
        h(event)
`
	rels := ddExtract(t, "events/pascal.py", src)

	for _, fn := range []string{"on_created", "on_deleted"} {
		r := ddFindCallsEdge(rels, "dispatch", "Handler", fn)
		if r == nil {
			// PascalCase receivers without an import binding are accepted.
			// Verify the edge exists at all (with correct leaf, any alias).
			found := false
			for _, rx := range rels {
				if rx.from == "dispatch" && rx.callLeaf == fn && rx.dataDispatch {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("missing data-dispatch CALLS edge for Handler.%s; rels: %+v", fn, rels)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// White-box: buildModuleConstRegistry
// ---------------------------------------------------------------------------

// Note: buildModuleConstRegistry is an internal function tested indirectly
// through the full Extract pipeline above. The tests below probe the
// data_dispatch pass exclusively via the public Extract API to avoid
// coupling test internals to unexported types.

// TestDataDispatch_NoFire_EmptyList — STEPS is an empty list. No edges.
func TestDataDispatch_NoFire_EmptyList(t *testing.T) {
	src := `from . import steps

STEPS = []

def run(ctx):
    for f in STEPS:
        f(ctx)
`
	rels := ddExtract(t, "sagas/empty.py", src)
	for _, r := range rels {
		if r.dataDispatch {
			t.Errorf("data-dispatch must not fire for empty constant list; got: %+v", r)
		}
	}
}

// TestDataDispatch_NoFire_NoCallInBody — the for-loop iterates the constant
// but doesn't call the loop variable directly (e.g. passes it as an
// argument). Must not fire.
func TestDataDispatch_NoFire_NoCallInBody(t *testing.T) {
	src := `from . import steps

STEPS = [
    (steps.create, "ok"),
]

def run(ctx, registry):
    for f, label in STEPS:
        registry.add(f, label)  # 'f' is an argument, not called directly
`
	rels := ddExtract(t, "sagas/nocall.py", src)
	for _, r := range rels {
		if r.dataDispatch && r.callLeaf == "create" {
			t.Errorf("data-dispatch must not fire when loop var is not directly called; got: %+v", r)
		}
	}
}

// TestDataDispatch_NoFire_IterateExpression — the for-loop iterates a
// non-identifier expression (e.g. a function call). Must not fire.
func TestDataDispatch_NoFire_IterateExpression(t *testing.T) {
	src := `from . import steps
from . import registry

STEPS = [
    (steps.create, "ok"),
]

def run(ctx):
    for f, _ in registry.get_steps():  # function call as iterable
        f(ctx)
`
	rels := ddExtract(t, "sagas/iter_expr.py", src)
	for _, r := range rels {
		if r.dataDispatch && r.callLeaf == "create" {
			t.Errorf("data-dispatch must not fire when iterating a call expression; got: %+v", r)
		}
	}
}

// TestDataDispatch_DispatchSourceInProperties — the emitted edge's
// dispatch_source property must name the constant used to generate it
// (verifies property attribution in multi-constant files).
func TestDataDispatch_DispatchSourceInProperties(t *testing.T) {
	src := `from . import steps

ALPHA = [
    (steps.alpha_fn, "a"),
]
BETA = [
    (steps.beta_fn, "b"),
]

def run_alpha(ctx):
    for f, _ in ALPHA:
        f(ctx)

def run_beta(ctx):
    for f, _ in BETA:
        f(ctx)
`
	tree := parse(t, []byte(src))
	ext, ok := extractor.Get("python")
	if !ok {
		t.Fatal("python extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "wf/sources.py",
		Content:  []byte(src),
		Language: "python",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	check := func(fnName, expectedSource string) {
		t.Helper()
		for _, e := range ents {
			if !strings.HasSuffix(e.Name, fnName) {
				continue
			}
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" && r.Properties["data_dispatch"] == "1" {
					if got := r.Properties["dispatch_source"]; got != expectedSource {
						t.Errorf("%s: want dispatch_source=%q, got %q", fnName, expectedSource, got)
					}
					return
				}
			}
		}
		t.Errorf("no data-dispatch CALLS edge found for %s", fnName)
	}

	check("run_alpha", "ALPHA")
	check("run_beta", "BETA")
}
