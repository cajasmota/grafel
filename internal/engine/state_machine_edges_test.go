package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// fsmRun runs the FSM-topology pass and returns entities + relationships.
func fsmRun(lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	res := applyStateMachineEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

// hasState reports whether a SCOPE.State entity for (lib, machine, state) exists.
func hasState(ents []types.EntityRecord, lib, machine, state string) bool {
	want := fsmStateID(lib, machine, state)
	for _, e := range ents {
		if e.Kind == fsmStateKind && e.Name == want {
			return true
		}
	}
	return false
}

// hasTransition reports whether a TRANSITIONS_TO edge source→target exists with
// the given triggering event. If event is "" the event property must be absent.
func hasTransition(rels []types.RelationshipRecord, lib, machine, source, target, event string) bool {
	wantFrom := fsmStateKind + ":" + fsmStateID(lib, machine, source)
	wantTo := fsmStateKind + ":" + fsmStateID(lib, machine, target)
	for _, r := range rels {
		if r.Kind != transitionsToEdgeKind || r.FromID != wantFrom || r.ToID != wantTo {
			continue
		}
		if r.Properties["event"] == event {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// XState (JS/TS)
// ---------------------------------------------------------------------------

func TestXState_StringTargets(t *testing.T) {
	// idle --FETCH--> loading, loading --SUCCESS--> done.
	src := `
import { createMachine } from 'xstate';
const fetchMachine = createMachine({
  id: 'fetch',
  initial: 'idle',
  states: {
    idle: { on: { FETCH: 'loading' } },
    loading: { on: { SUCCESS: 'done' } },
    done: { type: 'final' },
  },
});
`
	ents, rels := fsmRun("typescript", "src/fetchMachine.ts", src)

	for _, s := range []string{"idle", "loading", "done"} {
		if !hasState(ents, "xstate", "fetch", s) {
			t.Errorf("missing SCOPE.State for %q", s)
		}
	}
	if !hasTransition(rels, "xstate", "fetch", "idle", "loading", "FETCH") {
		t.Error("expected idle --FETCH--> loading")
	}
	if !hasTransition(rels, "xstate", "fetch", "loading", "done", "SUCCESS") {
		t.Error("expected loading --SUCCESS--> done")
	}
	// `done` is final — it must be a state with no outgoing transition.
	for _, r := range rels {
		if r.FromID == fsmStateKind+":"+fsmStateID("xstate", "fetch", "done") {
			t.Errorf("final state 'done' should have no outgoing transition, got %+v", r)
		}
	}
}

func TestXState_ObjectTargets(t *testing.T) {
	// loading --FAILURE--> idle, via object-form { target: 'idle' }.
	src := `
const m = createMachine({
  id: 'jobs',
  states: {
    idle: { on: { START: 'loading' } },
    loading: { on: { FAILURE: { target: 'idle', actions: 'logError' } } },
  },
});
`
	_, rels := fsmRun("javascript", "machine.js", src)
	if !hasTransition(rels, "xstate", "jobs", "idle", "loading", "START") {
		t.Error("expected idle --START--> loading")
	}
	if !hasTransition(rels, "xstate", "jobs", "loading", "idle", "FAILURE") {
		t.Error("expected loading --FAILURE--> idle (object target)")
	}
}

func TestXState_InitialStateMarked(t *testing.T) {
	src := `createMachine({ id: 'x', initial: 'boot', states: { boot: { on: { GO: 'run' } }, run: {} } });`
	ents, _ := fsmRun("typescript", "x.ts", src)
	for _, e := range ents {
		if e.Name == fsmStateID("xstate", "x", "boot") {
			if e.Properties["initial"] != "true" {
				t.Errorf("expected boot marked initial, props=%v", e.Properties)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Ruby AASM
// ---------------------------------------------------------------------------

func TestAASM_StatesAndTransitions(t *testing.T) {
	// pending --activate--> active, active --suspend--> suspended.
	src := `
class Account
  include AASM
  aasm do
    state :pending, initial: true
    state :active
    state :suspended

    event :activate do
      transitions from: :pending, to: :active
    end

    event :suspend do
      transitions from: [:active], to: :suspended
    end
  end
end
`
	ents, rels := fsmRun("ruby", "app/models/account.rb", src)

	for _, s := range []string{"pending", "active", "suspended"} {
		if !hasState(ents, "aasm", "aasm", s) {
			t.Errorf("missing SCOPE.State for %q", s)
		}
	}
	if !hasTransition(rels, "aasm", "aasm", "pending", "active", "activate") {
		t.Error("expected pending --activate--> active")
	}
	if !hasTransition(rels, "aasm", "aasm", "active", "suspended", "suspend") {
		t.Error("expected active --suspend--> suspended (array from:)")
	}
	// `initial: true` must be stamped on pending.
	for _, e := range ents {
		if e.Name == fsmStateID("aasm", "aasm", "pending") && e.Properties["initial"] != "true" {
			t.Errorf("expected pending marked initial, props=%v", e.Properties)
		}
	}
}

func TestAASM_MultipleFromSymbols(t *testing.T) {
	// from: [:a, :b], to: :c on event reset → two edges a→c, b→c.
	src := `
aasm do
  state :a
  state :b
  state :c
  event :reset do
    transitions from: [:a, :b], to: :c
  end
end
`
	_, rels := fsmRun("ruby", "m.rb", src)
	if !hasTransition(rels, "aasm", "aasm", "a", "c", "reset") {
		t.Error("expected a --reset--> c")
	}
	if !hasTransition(rels, "aasm", "aasm", "b", "c", "reset") {
		t.Error("expected b --reset--> c")
	}
}

// ---------------------------------------------------------------------------
// Spring StateMachine (Java)
// ---------------------------------------------------------------------------

func TestSpringStateMachine_Transitions(t *testing.T) {
	// IDLE --START--> RUNNING, RUNNING --FINISH--> DONE.
	src := `
@Configuration
@EnableStateMachine
public class Config extends StateMachineConfigurerAdapter<States, Events> {
  public void configure(StateMachineStateConfigurer<States, Events> states) throws Exception {
    states
      .withStates()
        .initial(States.IDLE)
        .state(States.RUNNING)
        .end(States.DONE);
  }
  public void configure(StateMachineTransitionConfigurer<States, Events> transitions) throws Exception {
    transitions
      .withExternal().source(States.IDLE).target(States.RUNNING).event(Events.START)
      .and()
      .withExternal().source(States.RUNNING).target(States.DONE).event(Events.FINISH);
  }
}
`
	ents, rels := fsmRun("java", "Config.java", src)

	for _, s := range []string{"IDLE", "RUNNING", "DONE"} {
		if !hasState(ents, "spring-statemachine", "spring", s) {
			t.Errorf("missing SCOPE.State for %q", s)
		}
	}
	if !hasTransition(rels, "spring-statemachine", "spring", "IDLE", "RUNNING", "START") {
		t.Error("expected IDLE --START--> RUNNING")
	}
	if !hasTransition(rels, "spring-statemachine", "spring", "RUNNING", "DONE", "FINISH") {
		t.Error("expected RUNNING --FINISH--> DONE")
	}
}

// ---------------------------------------------------------------------------
// Python `transitions`
// ---------------------------------------------------------------------------

func TestPythonTransitions_DictForm(t *testing.T) {
	// A --go--> B, B --back--> A.
	src := `
from transitions import Machine

machine = Machine(
    model=self,
    states=['A', 'B'],
    transitions=[
        {'trigger': 'go', 'source': 'A', 'dest': 'B'},
        {'trigger': 'back', 'source': 'B', 'dest': 'A'},
    ],
    initial='A',
)
`
	ents, rels := fsmRun("python", "fsm.py", src)

	for _, s := range []string{"A", "B"} {
		if !hasState(ents, "python-transitions", "transitions", s) {
			t.Errorf("missing SCOPE.State for %q", s)
		}
	}
	if !hasTransition(rels, "python-transitions", "transitions", "A", "B", "go") {
		t.Error("expected A --go--> B")
	}
	if !hasTransition(rels, "python-transitions", "transitions", "B", "A", "back") {
		t.Error("expected B --back--> A")
	}
}

func TestPythonTransitions_ListSourceFanOut(t *testing.T) {
	// source list ['solid','liquid'] dest 'gas' on trigger heat → two edges.
	src := `
machine = Machine(states=['solid', 'liquid', 'gas'], transitions=[
    {'trigger': 'heat', 'source': ['solid', 'liquid'], 'dest': 'gas'},
])
`
	_, rels := fsmRun("python", "phase.py", src)
	if !hasTransition(rels, "python-transitions", "transitions", "solid", "gas", "heat") {
		t.Error("expected solid --heat--> gas")
	}
	if !hasTransition(rels, "python-transitions", "transitions", "liquid", "gas", "heat") {
		t.Error("expected liquid --heat--> gas")
	}
}

// ---------------------------------------------------------------------------
// Negative cases
// ---------------------------------------------------------------------------

func TestFSM_StateWithNoTransition_EmitsEntityNoEdge(t *testing.T) {
	// `done` is a final state with no `on:` — it must produce a State entity
	// but participate in NO transition edge.
	src := `createMachine({ id: 'q', states: { active: { on: { FINISH: 'done' } }, done: { type: 'final' } } });`
	ents, rels := fsmRun("typescript", "q.ts", src)

	if !hasState(ents, "xstate", "q", "done") {
		t.Fatal("expected SCOPE.State entity for 'done'")
	}
	for _, r := range rels {
		from := fsmStateKind + ":" + fsmStateID("xstate", "q", "done")
		to := fsmStateKind + ":" + fsmStateID("xstate", "q", "done")
		if r.FromID == from || r.ToID == to {
			// 'done' may be a transition TARGET (active->done) but never a SOURCE.
			if r.FromID == from {
				t.Errorf("'done' should have no outgoing transition, got %+v", r)
			}
		}
	}
	// Exactly one edge: active --FINISH--> done.
	count := 0
	for _, r := range rels {
		if r.Kind == transitionsToEdgeKind {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 transition edge, got %d", count)
	}
}

func TestFSM_NonFSMFile_NoEmission(t *testing.T) {
	// A plain JS file with no createMachine must emit nothing.
	src := `export function add(a, b) { return a + b; }`
	ents, rels := fsmRun("javascript", "math.js", src)
	for _, e := range ents {
		if e.Kind == fsmStateKind {
			t.Errorf("unexpected SCOPE.State entity: %s", e.Name)
		}
	}
	for _, r := range rels {
		if r.Kind == transitionsToEdgeKind {
			t.Errorf("unexpected TRANSITIONS_TO edge: %+v", r)
		}
	}
}

func TestFSM_WrongLanguageGate(t *testing.T) {
	// XState source fed as Python must not be parsed by the JS branch.
	src := `createMachine({ id: 'x', states: { a: { on: { GO: 'b' } }, b: {} } });`
	ents, rels := fsmRun("python", "x.py", src)
	for _, e := range ents {
		if e.Kind == fsmStateKind && e.Properties["library"] == "xstate" {
			t.Errorf("xstate parsed under python gate: %s", e.Name)
		}
	}
	_ = rels
}
