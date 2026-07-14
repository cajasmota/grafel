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
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(m tea.Model, msg tea.Msg) tea.Model {
	nm, _ := m.Update(msg)
	return nm
}

func newTestModel(d Driver, idx IndexFunc) Model {
	m := New(d, idx, true, true, nil, nil)
	return m.update(tea.WindowSizeMsg{Width: 100, Height: 40})
}

// newTestModelMCP builds a model WITH detected MCP tools so the
// "Configure MCP for which tools?" screen is exercised (#5344).
func newTestModelMCP(d Driver, idx IndexFunc, mcp []MCPToolOption) Model {
	m := New(d, idx, true, true, mcp, nil)
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

// TestMCPScreenShownWhenMultipleTools: with >1 detected tool, the docs step
// advances to the MCP picker (not straight to index), and confirming the
// picker carries the selection into Result.MCPTools (#5344).
func TestMCPScreenShownWhenMultipleTools(t *testing.T) {
	d := fakeDriver{suggested: ActionSingle, cands: []Candidate{
		{Label: "/repo", Value: "/repo", Selected: true},
	}}
	mcp := []MCPToolOption{
		{ID: "claude", DisplayName: "Claude Code", DefaultSelected: true},
		{ID: "cursor", DisplayName: "Cursor", DefaultSelected: false},
	}
	m := newTestModelMCP(d, nilIndex, mcp)
	m = m.update(key("enter")) // action → select
	m = m.update(key("enter")) // select → name
	m = m.update(key("enter")) // name → docs
	m = m.update(key("enter")) // docs → MCP (not index, because 2 tools)
	if m.scr != scrMCP {
		t.Fatalf("after docs enter, scr = %v, want scrMCP", m.scr)
	}
	// claude is default-checked; confirm as-is.
	m = m.update(key("enter")) // confirm MCP → index
	if m.scr != scrIndex {
		t.Fatalf("after MCP enter, scr = %v, want scrIndex", m.scr)
	}
	if m.res.MCPTools == nil {
		t.Fatal("MCPTools not set after the picker")
	}
	if got := *m.res.MCPTools; len(got) != 1 || got[0] != "claude" {
		t.Errorf("MCPTools = %v, want [claude]", got)
	}
}

// TestMCPScreenSkippedWhenSingleTool: with exactly 1 detected tool, the picker
// is skipped and that tool is auto-selected (#5344).
func TestMCPScreenSkippedWhenSingleTool(t *testing.T) {
	d := fakeDriver{suggested: ActionSingle, cands: []Candidate{
		{Label: "/repo", Value: "/repo", Selected: true},
	}}
	mcp := []MCPToolOption{{ID: "claude", DisplayName: "Claude Code", DefaultSelected: true}}
	m := newTestModelMCP(d, nilIndex, mcp)
	m = m.update(key("enter")) // action → select
	m = m.update(key("enter")) // select → name
	m = m.update(key("enter")) // name → docs
	m = m.update(key("enter")) // docs → index (MCP skipped: only 1 tool)
	if m.scr != scrIndex {
		t.Fatalf("after docs enter, scr = %v, want scrIndex (single tool auto-used)", m.scr)
	}
	if m.res.MCPTools == nil || len(*m.res.MCPTools) != 1 || (*m.res.MCPTools)[0] != "claude" {
		t.Errorf("MCPTools = %v, want [claude] auto-selected", m.res.MCPTools)
	}
}

// TestNameInputAcceptsKeystrokes: the Name screen's text input must actually
// be focused so runes and backspace edit the field (regression for the
// value-receiver focusCmd bug where m.ti.Focus() mutated a throwaway copy of
// the stored inputModel, leaving m.focus permanently false and every
// tea.KeyMsg silently dropped by textinput.Model.Update).
func TestNameInputAcceptsKeystrokes(t *testing.T) {
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
	before := m.nameInput.value()
	m = m.update(key("x"))
	if m.nameInput.value() != before+"x" {
		t.Fatalf("nameInput.value() = %q after typing 'x', want %q (input is not focused/editable)", m.nameInput.value(), before+"x")
	}
	m = m.update(key("backspace"))
	if m.nameInput.value() != before {
		t.Fatalf("nameInput.value() = %q after backspace, want %q", m.nameInput.value(), before)
	}
}

// TestDocsInputAcceptsKeystrokes: same regression, for the Docs screen input.
func TestDocsInputAcceptsKeystrokes(t *testing.T) {
	d := fakeDriver{suggested: ActionGroup, cands: []Candidate{
		{Label: "/a", Value: "/a", Selected: true},
		{Label: "/b", Value: "/b", Selected: true},
	}}
	m := newTestModel(d, nilIndex)
	m = m.update(key("enter")) // action → select
	m = m.update(key("enter")) // select → name
	m = m.update(key("enter")) // accept default name → docs
	if m.scr != scrDocs {
		t.Fatalf("scr = %v, want scrDocs", m.scr)
	}
	m = m.update(key("y"))
	if m.docsInput.value() != "y" {
		t.Fatalf("docsInput.value() = %q after typing 'y', want %q (input is not focused/editable)", m.docsInput.value(), "y")
	}
	m = m.update(key("backspace"))
	if m.docsInput.value() != "" {
		t.Fatalf("docsInput.value() = %q after backspace, want empty", m.docsInput.value())
	}
}

// driveToIndexScreen walks a fresh model through action→select→name→docs to
// land on scrIndex, for tests that only care about outcome/queryable handling.
func driveToIndexScreen(t *testing.T, idx IndexFunc) Model {
	t.Helper()
	d := fakeDriver{suggested: ActionGroup, cands: []Candidate{
		{Label: "/a", Value: "/a", Selected: true},
	}}
	m := newTestModel(d, idx)
	m = m.update(key("enter")) // action → select
	m = m.update(key("enter")) // select → name
	m = m.update(key("enter")) // name → docs
	m = m.update(key("enter")) // docs → index
	if m.scr != scrIndex {
		t.Fatalf("scr = %v, want scrIndex", m.scr)
	}
	return m
}

// TestModel_InterimOutcome_EntersQueryableAndKeepsWaiting: an Interim outcome
// must NOT transition to scrDone — it enters the queryable sub-state, captures
// the interim stats, and re-arms waitOutcome so the (still-pending) final
// outcome is not missed.
func TestModel_InterimOutcome_EntersQueryableAndKeepsWaiting(t *testing.T) {
	m := driveToIndexScreen(t, nilIndex)

	m = m.update(outcomeMsg(IndexOutcome{
		Interim:  true,
		Entities: 4200,
		Rels:     100,
		Install:  InstallSummary{Applied: true, Hooks: 1},
	}))

	if m.scr != scrIndex {
		t.Fatalf("scr = %v, want scrIndex (interim must not finish the wizard)", m.scr)
	}
	if !m.idx.queryable {
		t.Error("idx.queryable not set after an interim outcome")
	}
	if m.idx.terminal {
		t.Error("idx.terminal set by an interim outcome")
	}
	if m.idx.summaryEntities != 4200 || m.idx.summaryRels != 100 {
		t.Errorf("interim stats not captured: entities=%d rels=%d", m.idx.summaryEntities, m.idx.summaryRels)
	}
	if !m.idx.install.Applied {
		t.Error("interim outcome's Install summary not captured")
	}
}

// TestModel_EnterInQueryableState_FinishesWithInterimStats: pressing enter
// while queryable (but not yet terminal) finishes the wizard immediately as
// SUCCESS, using the already-captured interim stats.
func TestModel_EnterInQueryableState_FinishesWithInterimStats(t *testing.T) {
	m := driveToIndexScreen(t, nilIndex)
	m = m.update(outcomeMsg(IndexOutcome{Interim: true, Entities: 777, Rels: 33}))
	if m.scr != scrIndex {
		t.Fatalf("scr = %v, want scrIndex after interim", m.scr)
	}

	m = m.update(key("enter"))

	if m.scr != scrDone {
		t.Fatalf("scr = %v, want scrDone after enter in queryable state", m.scr)
	}
	if !m.idx.terminal {
		t.Error("idx.terminal not set after finishing early from queryable")
	}
	if m.idx.summaryEntities != 777 || m.idx.summaryRels != 33 {
		t.Errorf("Done summary lost the interim stats: entities=%d rels=%d", m.idx.summaryEntities, m.idx.summaryRels)
	}
}

// TestModel_EnterEarlyAppliesInterimRepoStats: when the user finishes early
// from the queryable state, the interim outcome's per-repo classify stats must
// be overlaid onto the rows — otherwise a repo that emitted zero progress
// events would show "Done · 0 entities" on early finish. Regression for the
// review's "enter-early drops classify stats" finding.
func TestModel_EnterEarlyAppliesInterimRepoStats(t *testing.T) {
	m := driveToIndexScreen(t, nilIndex)
	// Seed a queued row for a repo that will NEVER report a progress event.
	m = m.update(progressMsg(progress.Event{RepoSlug: "silent-repo", Phase: PhaseQueued}))

	// Interim (queryable) outcome carries the classify's per-repo stats.
	m = m.update(outcomeMsg(IndexOutcome{
		Interim:   true,
		Entities:  9000,
		RepoStats: []RepoStat{{Slug: "silent-repo", Entities: 9000}},
	}))
	if m.scr != scrIndex {
		t.Fatalf("scr = %v, want scrIndex after interim", m.scr)
	}
	if got := m.idx.rows["silent-repo"].EntitiesSoFar; got != 0 {
		t.Fatalf("precondition: silent repo should still be 0 before finishing early, got %d", got)
	}

	// User presses enter to finish early.
	m = m.update(key("enter"))

	if m.scr != scrDone {
		t.Fatalf("scr = %v, want scrDone", m.scr)
	}
	row, ok := m.idx.rows["silent-repo"]
	if !ok {
		t.Fatal("silent repo's row missing after early finish")
	}
	if row.Phase != progress.PhaseDone {
		t.Errorf("Phase = %q, want Done", row.Phase)
	}
	if row.EntitiesSoFar != 9000 {
		t.Errorf("EntitiesSoFar = %d, want 9000 (interim classify stats must be applied on early finish, not 0)", row.EntitiesSoFar)
	}
}

// TestModel_EnterBeforeQueryable_NoOp: pressing enter on the index screen
// BEFORE any interim/terminal outcome has landed is a no-op (matches the old
// ctrl-c-only behavior; a bare enter must not skip the wait).
func TestModel_EnterBeforeQueryable_NoOp(t *testing.T) {
	m := driveToIndexScreen(t, nilIndex)
	m = m.update(key("enter"))
	if m.scr != scrIndex {
		t.Errorf("scr = %v, want scrIndex (enter with no queryable/terminal state must be a no-op)", m.scr)
	}
}

// TestModel_FinalOutcomeAfterInterim_ReachesDoneWithFinalStats: the sequence
// interim → final (the normal background-completes-on-its-own path) lands on
// scrDone carrying the FINAL stats (which may differ from the interim ones).
func TestModel_FinalOutcomeAfterInterim_ReachesDoneWithFinalStats(t *testing.T) {
	m := driveToIndexScreen(t, nilIndex)
	m = m.update(outcomeMsg(IndexOutcome{Interim: true, Entities: 100, Rels: 10}))
	if m.scr != scrIndex {
		t.Fatalf("scr = %v, want scrIndex after interim", m.scr)
	}

	m = m.update(outcomeMsg(IndexOutcome{Entities: 500, Rels: 90, Elapsed: "5m00s"}))

	if m.scr != scrDone {
		t.Fatalf("scr = %v, want scrDone after the final outcome", m.scr)
	}
	if !m.idx.terminal {
		t.Error("idx.terminal not set by the final outcome")
	}
	if m.idx.summaryEntities != 500 || m.idx.summaryRels != 90 {
		t.Errorf("final stats not applied: entities=%d rels=%d", m.idx.summaryEntities, m.idx.summaryRels)
	}
	if m.idx.elapsed != "5m00s" {
		t.Errorf("final elapsed not applied: %q", m.idx.elapsed)
	}
}

// TestModel_SilentRepoStillRendersRowAndCompletes is the model-level
// regression for the live dropped-row bug: a 3-repo group where the third
// repo's progress events never arrive must still render a row for it
// throughout indexing, and the completion screen must show its REAL entity
// count and Done state (sourced from IndexOutcome.RepoStats), never
// missing/blank — with the aggregate matching the sum of the rows.
func TestModel_SilentRepoStillRendersRowAndCompletes(t *testing.T) {
	slugs := []string{"core-mobile", "upvate_core", "upvate_core_frontend"}
	idx := func(Result) (<-chan progress.Event, <-chan IndexOutcome) {
		ev := make(chan progress.Event, 8)
		out := make(chan IndexOutcome, 1)
		// Seed a row for every selected repo up front (mirrors what the cli
		// layer's makeIndexFunc does before the real index starts).
		for _, s := range slugs {
			ev <- progress.Event{RepoSlug: s, Phase: PhaseQueued, TS: 0}
		}
		// Only two of the three ever report real progress — the third (the
		// exact live symptom) emits nothing.
		ev <- progress.Event{RepoSlug: "core-mobile", Phase: progress.PhaseDone, EntitiesSoFar: 8383, TS: 1}
		ev <- progress.Event{RepoSlug: "upvate_core", Phase: progress.PhaseDone, EntitiesSoFar: 6039, TS: 1}
		close(ev)
		out <- IndexOutcome{
			Entities: 31692,
			RepoStats: []RepoStat{
				{Slug: "core-mobile", Entities: 8383},
				{Slug: "upvate_core", Entities: 6039},
				{Slug: "upvate_core_frontend", Entities: 17270},
			},
		}
		close(out)
		return ev, out
	}

	d := fakeDriver{suggested: ActionGroup, cands: []Candidate{
		{Label: "/core-mobile", Value: "/core-mobile", Selected: true},
		{Label: "/upvate_core", Value: "/upvate_core", Selected: true},
		{Label: "/upvate_core_frontend", Value: "/upvate_core_frontend", Selected: true},
	}}
	m := newTestModel(d, idx)
	m = m.update(key("enter")) // action → select (all pre-selected)
	m = m.update(key("enter")) // select → name
	m = m.update(key("enter")) // name → docs
	m = m.update(key("enter")) // docs → index
	if m.scr != scrIndex {
		t.Fatalf("scr = %v, want scrIndex", m.scr)
	}

	evc, outc := m.index(m.res)
	m = m.update(indexStartedMsg{events: evc, outcome: outc})
	// Drain every buffered progress event synchronously (the test channels are
	// pre-filled and closed, so waitEvent resolves immediately each time).
	for i := 0; i < 5; i++ {
		e, ok := <-evc
		if !ok {
			break
		}
		m = m.update(progressMsg(e))
	}
	if len(m.idx.rows) != 3 {
		t.Fatalf("mid-index: len(rows) = %d, want 3 (a row for every selected repo, even one with zero events)", len(m.idx.rows))
	}
	o := <-outc
	m = m.update(outcomeMsg(o))

	if m.scr != scrDone {
		t.Fatalf("scr = %v, want scrDone", m.scr)
	}
	if len(m.idx.rows) != 3 {
		t.Fatalf("final: len(rows) = %d, want 3 (the silent repo's row must never disappear)", len(m.idx.rows))
	}
	row, ok := m.idx.rows["upvate_core_frontend"]
	if !ok {
		t.Fatal("silent repo's row is missing from the completion screen")
	}
	if row.Phase != progress.PhaseDone {
		t.Errorf("silent repo's Phase = %q, want Done", row.Phase)
	}
	if row.EntitiesSoFar != 17270 {
		t.Errorf("silent repo's EntitiesSoFar = %d, want 17270 (its real count from the classify)", row.EntitiesSoFar)
	}

	var sum int64
	for _, r := range m.idx.rows {
		sum += int64(r.EntitiesSoFar)
	}
	if sum != m.idx.summaryEntities {
		t.Errorf("sum of per-repo rows = %d, but Done summary reports %d entities — silent shortfall", sum, m.idx.summaryEntities)
	}
	if m.idx.summaryEntities != 31692 {
		t.Errorf("summaryEntities = %d, want 31692", m.idx.summaryEntities)
	}
}

// TestModel_MonolithMode_StillRendersAndCompletes: a terminal IndexOutcome
// with NO RepoStats (the monolith path, which has no per-repo classify) must
// still complete cleanly via the finalizeRows fallback — no regression.
func TestModel_MonolithMode_StillRendersAndCompletes(t *testing.T) {
	idx := func(Result) (<-chan progress.Event, <-chan IndexOutcome) {
		ev := make(chan progress.Event, 2)
		out := make(chan IndexOutcome, 1)
		ev <- progress.Event{RepoSlug: "monolith-repo", Phase: progress.PhaseExtractAST, FilesDone: 3, FilesTotal: 10, TS: 1}
		close(ev)
		out <- IndexOutcome{Entities: 999, Rels: 50, Elapsed: "10s"}
		close(out)
		return ev, out
	}
	d := fakeDriver{suggested: ActionSingle, cands: []Candidate{
		{Label: "/monolith-repo", Value: "/monolith-repo", Selected: true},
	}}
	m := newTestModel(d, idx)
	m = m.update(key("enter")) // action → select
	m = m.update(key("enter")) // select → name
	m = m.update(key("enter")) // name → docs
	m = m.update(key("enter")) // docs → index

	evc, outc := m.index(m.res)
	m = m.update(indexStartedMsg{events: evc, outcome: outc})
	e := <-evc
	m = m.update(progressMsg(e))
	o := <-outc
	m = m.update(outcomeMsg(o))

	if m.scr != scrDone {
		t.Fatalf("scr = %v, want scrDone", m.scr)
	}
	row, ok := m.idx.rows["monolith-repo"]
	if !ok {
		t.Fatal("monolith repo's row missing")
	}
	if row.Phase != progress.PhaseDone {
		t.Errorf("Phase = %q, want Done (finalizeRows fallback with no RepoStats)", row.Phase)
	}
	if m.idx.summaryEntities != 999 {
		t.Errorf("summaryEntities = %d, want 999", m.idx.summaryEntities)
	}
}

func nilIndex(Result) (<-chan progress.Event, <-chan IndexOutcome) {
	ev := make(chan progress.Event)
	out := make(chan IndexOutcome)
	close(ev)
	close(out)
	return ev, out
}
