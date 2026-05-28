package vue_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #2890 — Vue Internals / pinia_store: promote from `partial` to `full`
// by entitizing each defineStore() as a dedicated store entity plus its
// state/getters/actions members (store → member CONTAINS), replacing the thin
// single state_store op that #2876 left at `partial`.

func storeContains(s *types.EntityRecord, toID string) bool {
	for _, r := range s.Relationships {
		if r.Kind == "CONTAINS" && r.ToID == toID {
			return true
		}
	}
	return false
}

func TestIssue2890_PiniaDedicatedStore(t *testing.T) {
	src := loadVueFixture(t, "CounterStore.vue")
	ents := extract(t, "src/stores/CounterStore.vue", src)

	// --- options-syntax store: defineStore('counter', { state, getters, actions })
	counter := findByName(ents, "store:counter")
	if counter == nil {
		t.Fatalf("pinia_store: missing dedicated store entity store:counter; %s", dump(ents))
	}
	if counter.Subtype != "pinia_store" {
		t.Errorf("store:counter subtype = %q, want pinia_store", counter.Subtype)
	}
	if counter.Properties["store_id"] != "counter" || counter.Properties["state_lib"] != "pinia" {
		t.Errorf("store:counter props = %v, want store_id=counter state_lib=pinia", counter.Properties)
	}

	// state fields, getters and actions are each their own member entity with a
	// distinct subtype, and the store CONTAINS every one of them.
	wantMembers := []struct {
		name, subtype string
	}{
		{"counter.state.count", "pinia_state"},
		{"counter.state.label", "pinia_state"},
		{"counter.getters.doubled", "pinia_getter"},
		{"counter.getters.isPositive", "pinia_getter"},
		{"counter.actions.increment", "pinia_action"},
		{"counter.actions.load", "pinia_action"},
	}
	for _, w := range wantMembers {
		if !hasEntity(ents, w.subtype, w.name) {
			t.Errorf("pinia_store: missing member entity %s (%s); %s", w.name, w.subtype, dump(ents))
		}
		if !storeContains(counter, w.name) {
			t.Errorf("pinia_store: store:counter missing CONTAINS -> %s", w.name)
		}
	}

	// --- single-line state object: `state: () => ({ items: [], total: 0 })`.
	// Real apps frequently write the state object on one line; the depth-aware
	// key scanner must still break out each field.
	cart := findByName(ents, "store:cart")
	if cart == nil {
		t.Fatalf("pinia_store: missing single-line store store:cart; %s", dump(ents))
	}
	for _, m := range []string{"cart.state.items", "cart.state.total"} {
		if !hasEntity(ents, "pinia_state", m) {
			t.Errorf("pinia_store: single-line state missing %s; %s", m, dump(ents))
		}
		if !storeContains(cart, m) {
			t.Errorf("pinia_store: store:cart missing CONTAINS -> %s", m)
		}
	}
	if !hasEntity(ents, "pinia_action", "cart.actions.add") {
		t.Errorf("pinia_store: missing cart.actions.add")
	}

	// --- setup-syntax store: defineStore('session', () => { … return {…} })
	session := findByName(ents, "store:session")
	if session == nil {
		t.Fatalf("pinia_store: missing setup-syntax store entity store:session; %s", dump(ents))
	}
	setupMembers := []struct {
		name, subtype string
	}{
		{"session.state.token", "pinia_state"},       // ref()
		{"session.getters.isAuthed", "pinia_getter"}, // computed()
		{"session.actions.setToken", "pinia_action"}, // function
		{"session.actions.clear", "pinia_action"},    // arrow const
	}
	for _, w := range setupMembers {
		if !hasEntity(ents, w.subtype, w.name) {
			t.Errorf("pinia_store (setup): missing member %s (%s); %s", w.name, w.subtype, dump(ents))
		}
		if !storeContains(session, w.name) {
			t.Errorf("pinia_store (setup): store:session missing CONTAINS -> %s", w.name)
		}
	}

	// The component (file-derived) CONTAINS each store entity.
	comp := findByName(ents, "CounterStore")
	if comp == nil {
		t.Fatalf("component CounterStore not extracted; %s", dump(ents))
	}
	if !relTarget(comp, "CONTAINS", "store:counter") {
		t.Errorf("component missing CONTAINS -> store:counter; %s", dump(ents))
	}
	if !relTarget(comp, "CONTAINS", "store:session") {
		t.Errorf("component missing CONTAINS -> store:session")
	}
}
