// Package javascript — issue #2884 Angular state_management proving tests.
//
// Re-greens the Data Flow/state_management cell that the independent audit
// (#2847) reverted full->partial: the pre-#2884 extractor only recognised ngrx
// Redux Store (select/dispatch), so on the gothinkster angular-realworld app —
// where all 11 stateful files use Angular signals + RxJS BehaviorSubject and 0
// use ngrx — the cell extracted no state. These tests prove the dominant modern
// idioms (signals + BehaviorSubject) now emit state_store containers and
// state_setter operations, while ngrx detection is unchanged (no regression).
package javascript_test

import (
	"os"
	"testing"
)

// TestIssue2884_AngularStateManagement runs the proving fixture and asserts the
// three state families are detected: signals, RxJS BehaviorSubject, and ngrx
// (both the signal store and the legacy Redux Store path).
func TestIssue2884_AngularStateManagement(t *testing.T) {
	src, err := os.ReadFile("../../../testdata/fixtures/real-world/typescript/angular_state_management.ts")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ents := extractReact(t, "angular_state_management.ts", src)

	hasContainer := func(name, lib, prim string) bool {
		for i := range ents {
			e := &ents[i]
			if e.Subtype == "state_store" && e.Name == name &&
				e.Properties["state_lib"] == lib && e.Properties["primitive"] == prim {
				return true
			}
		}
		return false
	}
	hasSetter := func(name, kind string) bool {
		for i := range ents {
			e := &ents[i]
			if e.Subtype == "state_setter" && e.Name == name && e.Properties["setter_kind"] == kind {
				return true
			}
		}
		return false
	}
	hasWritesTo := func(setter, state string) bool {
		for i := range ents {
			e := &ents[i]
			if e.Subtype != "state_setter" || e.Name != setter {
				continue
			}
			for _, r := range e.Relationships {
				if r.Kind == "WRITES_TO" && r.ToID == "state:"+state {
					return true
				}
			}
		}
		return false
	}

	// 1. Angular signals: signal()/computed() containers (the modern default).
	if !hasContainer("isSubmitting", "angular-signals", "signal") {
		t.Errorf("missing signal state_store isSubmitting; %s", dumpKinds(ents))
	}
	if !hasContainer("errors", "angular-signals", "signal") {
		t.Errorf("missing signal state_store errors")
	}
	if !hasContainer("hasErrors", "angular-signals", "computed") {
		t.Errorf("missing computed state_store hasErrors")
	}
	// signal setters: .set / .update both fire and WRITE_TO their signal.
	if !hasSetter("isSubmitting.set", "signal") || !hasWritesTo("isSubmitting.set", "isSubmitting") {
		t.Errorf("missing signal setter isSubmitting.set (+WRITES_TO); %s", dumpKinds(ents))
	}
	if !hasSetter("isSubmitting.update", "signal") {
		t.Errorf("missing signal setter isSubmitting.update")
	}

	// 2. RxJS BehaviorSubject service state: container + .next() setter.
	if !hasContainer("currentUserSubject", "rxjs-subject", "BehaviorSubject") {
		t.Errorf("missing BehaviorSubject state_store currentUserSubject; %s", dumpKinds(ents))
	}
	if !hasContainer("authStateSubject", "rxjs-subject", "BehaviorSubject") {
		t.Errorf("missing BehaviorSubject state_store authStateSubject")
	}
	if !hasSetter("currentUserSubject.next", "rxjs_subject") ||
		!hasWritesTo("currentUserSubject.next", "currentUserSubject") {
		t.Errorf("missing BehaviorSubject setter currentUserSubject.next (+WRITES_TO); %s", dumpKinds(ents))
	}
	if !hasSetter("authStateSubject.next", "rxjs_subject") {
		t.Errorf("missing BehaviorSubject setter authStateSubject.next")
	}

	// 3a. ngrx signal store: signalStore(withState(...)).
	if !hasContainer("store", "angular-signals", "ngrx_signal_store") {
		t.Errorf("missing ngrx signal store state_store; %s", dumpKinds(ents))
	}

	// 3b. ngrx Redux Store: select/dispatch CALLS edges (unchanged — no regress).
	comp := findByName(ents, "CounterComponent")
	if comp == nil {
		t.Fatalf("CounterComponent not extracted; %s", dumpKinds(ents))
	}
	if !hasRel(comp.Relationships, "CALLS", "Store.select") {
		t.Errorf("ngrx regression: missing CALLS Store.select; rels=%v", comp.Relationships)
	}
	if !hasRel(comp.Relationships, "CALLS", "Store.dispatch") {
		t.Errorf("ngrx regression: missing CALLS Store.dispatch")
	}
	if !hasSetter("dispatch:increment", "ngrx_dispatch") {
		t.Errorf("ngrx regression: missing dispatch:increment state_setter; %s", dumpKinds(ents))
	}
}
