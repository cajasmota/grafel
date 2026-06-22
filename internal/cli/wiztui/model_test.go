package wiztui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cajasmota/grafel/internal/progress"
)

// fakeDriver is a static Driver for headless model tests.
type fakeDriver struct {
	suggested Action
	cands     []Candidate
	groups    []Candidate
}

func (d fakeDriver) ContextLine() string              { return "Detected: test fixture" }
func (d fakeDriver) SuggestedAction() Action          { return d.suggested }
func (d fakeDriver) Groups() []Candidate              { return d.groups }
func (d fakeDriver) DefaultGroupName([]string) string { return "mygroup" }
func (d fakeDriver) Candidates(Action) (string, []Candidate) {
	return "2 repos found", append([]Candidate(nil), d.cands...)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "ctrl-c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(m tea.Model, msg tea.Msg) tea.Model {
	nm, _ := m.Update(msg)
	return nm
}

func newTestModel(d Driver, idx IndexFunc) Model {
	m := New(d, idx, true, true)
	return m.update(tea.WindowSizeMsg{Width: 100, Height: 40})
}

// update is a tiny test helper to apply a msg and return the concrete Model.
func (m Model) update(msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

// TestCtrlCBeforeConfirmRegistersNothing: ctrl-c on the first screen cancels and
// never invokes the IndexFunc (no registration).
func TestCtrlCBeforeConfirmRegistersNothing(t *testing.T) {
	indexed := false
	idx := func(Result) (<-chan progress.Event, <-chan IndexOutcome) {
		indexed = true
		ev := make(chan progress.Event)
		out := make(chan IndexOutcome)
		close(ev)
		close(out)
		return ev, out
	}
	d := fakeDriver{suggested: ActionGroup, cands: []Candidate{
		{Label: "/a", Value: "/a", Selected: true},
	}}
	m := newTestModel(d, idx)
	m = m.update(key("ctrl-c"))
	if !m.Result().Cancelled {
		t.Error("ctrl-c did not set Cancelled")
	}
	if indexed {
		t.Error("IndexFunc was invoked despite cancel — registration leaked")
	}
}

// TestEscOnActionCancels: esc on the action screen cancels cleanly.
func TestEscOnActionCancels(t *testing.T) {
	d := fakeDriver{suggested: ActionGroup}
	m := newTestModel(d, nilIndex)
	m = m.update(key("esc"))
	if !m.Result().Cancelled {
		t.Error("esc on action screen did not cancel")
	}
}

// TestSingleFlowReachesIndex: action(single) → select(confirm) → index.
func TestSingleFlowReachesIndex(t *testing.T) {
	started := false
	idx := func(r Result) (<-chan progress.Event, <-chan IndexOutcome) {
		started = true
		if r.Action != ActionSingle {
			t.Errorf("action = %q, want single", r.Action)
		}
		ev := make(chan progress.Event)
		out := make(chan IndexOutcome, 1)
		close(ev)
		out <- IndexOutcome{Entities: 5}
		close(out)
		return ev, out
	}
	d := fakeDriver{suggested: ActionSingle, cands: []Candidate{
		{Label: "/repo", Value: "/repo", Selected: true},
	}}
	m := newTestModel(d, idx)
	// Action screen: cursor pre-placed on single (suggested). Confirm.
	m = m.update(key("enter")) // choose action → select screen
	if m.scr != scrSelect {
		t.Fatalf("after action enter, scr = %v, want scrSelect", m.scr)
	}
	// Single's one candidate is pre-selected; confirm → name screen (a single
	// repo still becomes its own group, so a name is prompted).
	m = m.update(key("enter"))
	if m.scr != scrName {
		t.Fatalf("after select enter, scr = %v, want scrName", m.scr)
	}
	m = m.update(key("enter")) // accept default group name → docs
	if m.scr != scrDocs {
		t.Fatalf("after name enter, scr = %v, want scrDocs", m.scr)
	}
	m = m.update(key("enter")) // skip docs → index
	if m.scr != scrIndex {
		t.Fatalf("after docs enter, scr = %v, want scrIndex", m.scr)
	}
	if m.index == nil {
		t.Fatal("index func not wired")
	}
	// Simulate the indexStartedMsg → progress → outcome path.
	evc, outc := m.index(m.res)
	if !started {
		t.Error("IndexFunc not started")
	}
	m = m.update(indexStartedMsg{events: evc, outcome: outc})
	// Pump the outcome.
	o := <-outc
	m = m.update(outcomeMsg(o))
	if m.scr != scrDone {
		t.Errorf("after outcome, scr = %v, want scrDone", m.scr)
	}
}

// TestGroupMultiSelectRequiresSelection: confirming with nothing selected errors
// and stays on the select screen.
func TestGroupMultiSelectRequiresSelection(t *testing.T) {
	d := fakeDriver{suggested: ActionGroup, cands: []Candidate{
		{Label: "/a", Value: "/a", Selected: false},
		{Label: "/b", Value: "/b", Selected: false},
	}}
	m := newTestModel(d, nilIndex)
	m = m.update(key("enter")) // action → select
	m = m.update(key("enter")) // confirm with none selected
	if m.scr != scrSelect {
		t.Errorf("advanced past select with 0 selected; scr=%v", m.scr)
	}
	if m.err == "" {
		t.Error("expected an error message for empty selection")
	}
}

// TestGroupFlowToName: selecting a repo advances to the name screen.
func TestGroupFlowToName(t *testing.T) {
	d := fakeDriver{suggested: ActionGroup, cands: []Candidate{
		{Label: "/a", Value: "/a", Selected: true},
		{Label: "/b", Value: "/b", Selected: true},
	}}
	m := newTestModel(d, nilIndex)
	m = m.update(key("enter")) // action → select (both pre-selected)
	m = m.update(key("enter")) // confirm selection → name
	if m.scr != scrName {
		t.Fatalf("scr = %v, want scrName", m.scr)
	}
	if m.nameInput.value() != "mygroup" {
		t.Errorf("name prefilled %q, want mygroup", m.nameInput.value())
	}
}

// TestAddGroupFlow: add-to-group picks a group then repos, then indexes.
func TestAddGroupFlow(t *testing.T) {
	d := fakeDriver{
		suggested: ActionGroup,
		cands:     []Candidate{{Label: "/x", Value: "/x", Selected: true}},
		groups:    []Candidate{{Label: "existing", Value: "existing"}},
	}
	m := newTestModel(d, nilIndex)
	// Move cursor to add-group (4th action) and confirm.
	m = m.update(key("down"))
	m = m.update(key("down"))
	m = m.update(key("down"))
	m = m.update(key("enter"))
	if m.scr != scrGroupPick {
		t.Fatalf("scr = %v, want scrGroupPick", m.scr)
	}
	m = m.update(key("enter")) // pick group
	if m.scr != scrSelect {
		t.Fatalf("scr = %v, want scrSelect after group pick", m.scr)
	}
	if m.res.AddToGroup != "existing" {
		t.Errorf("AddToGroup = %q, want existing", m.res.AddToGroup)
	}
}

func nilIndex(Result) (<-chan progress.Event, <-chan IndexOutcome) {
	ev := make(chan progress.Event)
	out := make(chan IndexOutcome)
	close(ev)
	close(out)
	return ev, out
}
