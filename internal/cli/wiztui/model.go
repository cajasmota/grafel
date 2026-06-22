package wiztui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	prog "github.com/cajasmota/grafel/internal/progress"
)

// Action mirrors the four top-level wizard actions. The wiztui package keeps
// its own enum so it has no dependency on the cli package (which imports it).
type Action string

const (
	ActionSingle   Action = "single"
	ActionGroup    Action = "group"
	ActionMonorepo Action = "monorepo"
	ActionAddGroup Action = "add-group"
)

// Candidate is one selectable repo/package/group option.
type Candidate struct {
	Label    string // shown to the user
	Value    string // returned (abs path, package root, or group name)
	Selected bool   // initial selection state (for multiselect)
}

// rescanSentinel is the special Candidate value for "scan a different folder…".
const RescanSentinel = "\x00rescan"

// Result is the outcome of the interactive selection portion of the wizard,
// handed back to the cli package which owns all the side effects (classify,
// applyGroupConfig, index). The TUI itself performs no registration.
type Result struct {
	Action     Action
	Repos      []string // chosen absolute repo paths (or package roots)
	AddToGroup string   // non-empty when Action == ActionAddGroup
	GroupName  string
	GroupDocs  string
	Watchers   bool
	GitHooks   bool
	// MCPTools is the user's choice of which AI tools get the grafel MCP
	// server (#5344). nil = no explicit choice (caller falls back to its
	// default behaviour); non-nil (incl. empty) = register exactly these tool
	// IDs. Set by the "Configure MCP for which tools?" screen.
	MCPTools  *[]string
	Cancelled bool // user pressed ctrl-c / esc out of the first screen

	indexErr error // set if indexing failed (read via IndexErr)
}

// IndexErr returns the indexing error, if any (nil on success / daemon-down).
func (r Result) IndexErr() error { return r.indexErr }

// Driver supplies the dynamic data the model needs at each step and is
// implemented by the cli package. This keeps all classification / registry /
// filesystem logic in cli (preserved verbatim) while the model owns only
// presentation + interaction.
type Driver interface {
	// ContextLine is the "Detected: …" line for the action screen.
	ContextLine() string
	// SuggestedAction is the pre-placed cursor action on the action screen.
	SuggestedAction() Action
	// Candidates returns the selectable repos/packages for an action, plus a
	// title. needsPath is true when no candidates could be derived and the
	// caller should fall through to a path prompt (handled out-of-band).
	Candidates(a Action) (title string, cands []Candidate)
	// Groups returns the existing group names (for add-to-group).
	Groups() []Candidate
	// DefaultGroupName suggests a group name from chosen repo paths.
	DefaultGroupName(repos []string) string
}

// IndexFunc starts the index for the assembled result and returns a channel of
// progress events plus a channel that delivers the final outcome. It is invoked
// by the model once the user confirms; the model only renders what flows back.
type IndexFunc func(r Result) (<-chan prog.Event, <-chan IndexOutcome)

// IndexOutcome is the terminal result of indexing.
type IndexOutcome struct {
	Entities int64
	Rels     int64
	Elapsed  string
	Err      error
	// DaemonDown indicates the group was registered but not indexed (daemon
	// not running) — a soft, non-error completion.
	DaemonDown bool
	// Install summarizes what applyGroupConfig wrote (hooks/watchers/MCP) and
	// any non-fatal watcher warnings, captured so the TUI can render them inside
	// the Done screen instead of letting raw stdout scatter over the alt-screen.
	Install InstallSummary
}

// InstallSummary is the captured, structured outcome of applyGroupConfig's
// install transaction. The cli package fills it from install.Result so the TUI
// owns all post-completion output (fix C, #5340).
type InstallSummary struct {
	Applied         bool     // an install transaction ran (RunInstall was true)
	Hooks           int      // git hooks installed
	Watchers        int      // watcher units written
	MCP             int      // MCP settings entries touched
	WatcherWarnings []string // non-fatal watcher-activation warnings
	ConfigPath      string   // saved per-group config path (e.g. "saved …")
}

// screen identifies the active screen in the state machine.
type screen int

const (
	scrAction screen = iota
	scrSelect
	scrGroupPick // add-to-group: pick target group
	scrName
	scrDocs
	scrMCP // choose which AI tools get the grafel MCP server (#5344)
	scrIndex
	scrDone
)

// MCPToolOption is one selectable AI tool in the "Configure MCP for which
// tools?" screen (#5344). The cli package builds these from the tool detector;
// wiztui stays decoupled from the install package.
type MCPToolOption struct {
	ID              string // adapter ID persisted + passed to install
	DisplayName     string // human-facing name
	HasGrafel       bool   // already has a grafel entry (shown as "configured")
	DefaultSelected bool   // B+C computed default checkbox state
}

// progressMsg / outcomeMsg are tea messages carrying indexing data.
type progressMsg prog.Event
type outcomeMsg IndexOutcome
type indexStartedMsg struct {
	events  <-chan prog.Event
	outcome <-chan IndexOutcome
}

// Model is the full-screen Bubble Tea wizard model.
type Model struct {
	drv   Driver
	index IndexFunc

	width, height int

	scr  screen
	step Step

	// screens
	actionList listModel
	selectList multiListModel
	groupPick  listModel
	nameInput  inputModel
	docsInput  inputModel
	mcpList    multiListModel
	idx        indexView

	// mcpTools are the detected MCP-capable tools offered on the scrMCP
	// screen, in display order. Empty (or len<=1) skips the screen (#5344).
	mcpTools []MCPToolOption

	// accumulated result
	res Result

	// channels for live indexing
	evCh  <-chan prog.Event
	outCh <-chan IndexOutcome

	err  string
	quit bool
}

// New builds the wizard model. watchers/gitHooks seed the result defaults
// (features are taken from flags, matching the current behavior — the TUI does
// not re-prompt for them). mcpTools are the detected MCP-capable tools offered
// on the "Configure MCP for which tools?" screen (#5344); pass nil/empty to
// skip that screen entirely (e.g. a flag preset the selection, or ≤1 detected).
func New(drv Driver, index IndexFunc, watchers, gitHooks bool, mcpTools []MCPToolOption) Model {
	m := Model{
		drv:      drv,
		index:    index,
		scr:      scrAction,
		step:     StepAction,
		mcpTools: mcpTools,
	}
	m.res.Watchers = watchers
	m.res.GitHooks = gitHooks

	m.actionList = newListModel("What do you want to index?", []Candidate{
		{Label: "Index a single repository", Value: string(ActionSingle)},
		{Label: "Index a group of related repositories", Value: string(ActionGroup)},
		{Label: "Index a monorepo", Value: string(ActionMonorepo)},
		{Label: "Add a repository to an existing group", Value: string(ActionAddGroup)},
	})
	// Pre-place cursor on the suggested action, with a one-line explainer of
	// what the four indexing modes mean under the detected-context line.
	m.actionList.context = drv.ContextLine() + "\n" +
		"A single repo, a group of related repos, a monorepo's packages, or add to an existing group."
	m.actionList.setCursorByValue(string(drv.SuggestedAction()))
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Result returns the final accumulated result (read after the program exits).
func (m Model) Result() Result { return m.res }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.idx.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		// Global quit. ctrl-c always cancels cleanly (nothing registered).
		if msg.Type == tea.KeyCtrlC {
			m.res.Cancelled = true
			m.quit = true
			return m, tea.Quit
		}
		return m.updateKey(msg)

	case indexStartedMsg:
		m.evCh = msg.events
		m.outCh = msg.outcome
		return m, tea.Batch(m.idx.spin.Tick, waitEvent(m.evCh), waitOutcome(m.outCh))

	case progressMsg:
		m.idx.foldEvent(prog.Event(msg))
		return m, waitEvent(m.evCh)

	case outcomeMsg:
		o := IndexOutcome(msg)
		m.idx.summaryEntities = o.Entities
		m.idx.summaryRels = o.Rels
		m.idx.elapsed = o.Elapsed
		m.idx.daemonDown = o.DaemonDown
		m.idx.install = o.Install
		if o.Err != nil {
			m.idx.failed = true
			m.idx.errMsg = o.Err.Error()
			m.res.indexErr = o.Err
		} else {
			m.idx.terminal = true
			// The whole index succeeded, so every repo is done. Force any row
			// still on an intermediate phase to Done — its final SSE events may
			// have arrived after the RPC returned and been dropped (#5340).
			m.idx.finalizeRows()
		}
		m.scr = scrDone
		m.step = StepDone
		return m, nil

	default:
		// Spinner ticks while indexing.
		if m.scr == scrIndex {
			var cmd tea.Cmd
			m.idx.spin, cmd = m.idx.spin.Update(msg)
			return m, cmd
		}
		return m, nil
	}
}

// updateKey routes key events to the active screen.
func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.err = ""
	switch m.scr {
	case scrAction:
		return m.updateAction(msg)
	case scrSelect:
		return m.updateSelect(msg)
	case scrGroupPick:
		return m.updateGroupPick(msg)
	case scrName:
		return m.updateName(msg)
	case scrDocs:
		return m.updateDocs(msg)
	case scrMCP:
		return m.updateMCP(msg)
	case scrIndex:
		// Index screen: only ctrl-c (handled globally) interrupts.
		return m, nil
	case scrDone:
		switch msg.String() {
		case "enter", "q", "esc":
			m.quit = true
			return m, tea.Quit
		}
		return m, nil
	}
	return m, nil
}

func (m Model) updateAction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.actionList, cmd = m.actionList.Update(msg)
	if msg.String() == "esc" {
		m.res.Cancelled = true
		m.quit = true
		return m, tea.Quit
	}
	if m.actionList.chosen {
		m.res.Action = Action(m.actionList.value())
		return m.enterSelect()
	}
	return m, cmd
}

// enterSelect transitions to the Select screen, populating candidates.
func (m Model) enterSelect() (tea.Model, tea.Cmd) {
	if m.res.Action == ActionAddGroup {
		groups := m.drv.Groups()
		if len(groups) == 0 {
			m.err = "no existing groups to add to"
			// stay on action screen
			m.actionList.chosen = false
			return m, nil
		}
		m.groupPick = newListModel("Add to which group?", groups)
		m.scr = scrGroupPick
		m.step = StepSelect
		return m, nil
	}

	title, cands := m.drv.Candidates(m.res.Action)
	// Append the rescan entry.
	cands = append(cands, Candidate{Label: "scan a different folder" + g.Ellipsis, Value: RescanSentinel})
	m.selectList = newMultiListModel(title, cands)
	m.selectList.context = "Choose which repositories to include in this group."
	m.scr = scrSelect
	m.step = StepSelect
	return m, nil
}

func (m Model) updateSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.scr = scrAction
		m.step = StepAction
		m.actionList.chosen = false
		return m, nil
	}
	var cmd tea.Cmd
	m.selectList, cmd = m.selectList.Update(msg)
	if m.selectList.chosen {
		vals := m.selectList.values()
		// Rescan path is handled by the caller (re-derive candidates) — here we
		// just treat selecting rescan with nothing else as a no-op error.
		var repos []string
		rescan := false
		for _, v := range vals {
			if v == RescanSentinel {
				rescan = true
				continue
			}
			repos = append(repos, v)
		}
		if rescan && len(repos) == 0 {
			m.err = "scan-a-different-folder isn't available in this view; pass a path via flags"
			m.selectList.chosen = false
			return m, nil
		}
		if len(repos) == 0 {
			m.err = "select at least one repository (space to toggle)"
			m.selectList.chosen = false
			return m, nil
		}
		m.res.Repos = repos
		if m.res.Action == ActionAddGroup {
			return m.enterMCP()
		}
		return m.enterName()
	}
	return m, cmd
}

func (m Model) updateGroupPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.scr = scrAction
		m.step = StepAction
		m.actionList.chosen = false
		return m, nil
	}
	var cmd tea.Cmd
	m.groupPick, cmd = m.groupPick.Update(msg)
	if m.groupPick.chosen {
		m.res.AddToGroup = m.groupPick.value()
		// Now pick repos to add.
		title, cands := m.drv.Candidates(ActionGroup)
		cands = append(cands, Candidate{Label: "scan a different folder" + g.Ellipsis, Value: RescanSentinel})
		m.selectList = newMultiListModel(title, cands)
		m.selectList.context = "Choose which repositories to add to “" + m.res.AddToGroup + "”."
		m.scr = scrSelect
		m.step = StepSelect
		return m, nil
	}
	return m, cmd
}

func (m Model) enterName() (tea.Model, tea.Cmd) {
	def := m.drv.DefaultGroupName(m.res.Repos)
	desc := "The group's registry key and config filename. Repos in this group: " +
		repoNames(m.res.Repos) + "."
	m.nameInput = newInputModel("Group name", desc, def, false)
	m.scr = scrName
	m.step = StepName
	return m, m.nameInput.focusCmd()
}

// repoNames renders a short, comma-joined list of repo basenames for the Name
// screen's explainer (so the user sees exactly which repos they're grouping).
func repoNames(repos []string) string {
	if len(repos) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(repos))
	for _, p := range repos {
		base := p
		if i := strings.LastIndexAny(p, `/\`); i >= 0 && i < len(p)-1 {
			base = p[i+1:]
		}
		names = append(names, base)
	}
	return strings.Join(names, ", ")
}

func (m Model) updateName(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		return m.enterSelect()
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	if m.nameInput.done {
		v := strings.TrimSpace(m.nameInput.value())
		if v == "" {
			m.err = "group name is required"
			m.nameInput.done = false
			return m, nil
		}
		m.res.GroupName = v
		m.docsInput = newInputModel("Path to shared group docs",
			"A folder of shared markdown docs surfaced in the graph. Optional — press enter to skip.", "", true)
		m.scr = scrDocs
		return m, m.docsInput.focusCmd()
	}
	return m, cmd
}

func (m Model) updateDocs(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		return m.enterName()
	}
	var cmd tea.Cmd
	m.docsInput, cmd = m.docsInput.Update(msg)
	if m.docsInput.done {
		m.res.GroupDocs = strings.TrimSpace(m.docsInput.value())
		return m.enterMCP()
	}
	return m, cmd
}

// enterMCP transitions to the "Configure MCP for which tools?" screen (#5344).
// When ≤1 tool is detected the screen is skipped and the detected set is
// auto-used (no screen for a trivial choice).
func (m Model) enterMCP() (tea.Model, tea.Cmd) {
	if len(m.mcpTools) <= 1 {
		// ≤1 detected: auto-use it (or leave nil when none) and index.
		if len(m.mcpTools) == 1 {
			sel := []string{m.mcpTools[0].ID}
			m.res.MCPTools = &sel
		}
		return m.startIndex()
	}
	cands := make([]Candidate, 0, len(m.mcpTools))
	for _, t := range m.mcpTools {
		label := t.DisplayName
		if t.HasGrafel {
			label += " (configured)"
		}
		cands = append(cands, Candidate{Label: label, Value: t.ID, Selected: t.DefaultSelected})
	}
	m.mcpList = newMultiListModel("Configure MCP for which tools?", cands)
	m.mcpList.context = "Your AI agents that can query this graph."
	m.scr = scrMCP
	m.step = StepName
	return m, nil
}

func (m Model) updateMCP(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		// Back to the docs screen.
		m.docsInput = newInputModel("Path to shared group docs",
			"A folder of shared markdown docs surfaced in the graph. Optional — press enter to skip.", m.res.GroupDocs, true)
		m.scr = scrDocs
		m.step = StepName
		return m, m.docsInput.focusCmd()
	}
	var cmd tea.Cmd
	m.mcpList, cmd = m.mcpList.Update(msg)
	if m.mcpList.chosen {
		sel := m.mcpList.values()
		if sel == nil {
			sel = []string{} // distinguish "chose none" from "no choice"
		}
		m.res.MCPTools = &sel
		return m.startIndex()
	}
	return m, cmd
}

// startIndex transitions to the indexing screen and kicks off the index.
func (m Model) startIndex() (tea.Model, tea.Cmd) {
	m.scr = scrIndex
	m.step = StepIndex
	name := m.res.GroupName
	if m.res.AddToGroup != "" {
		name = m.res.AddToGroup
	}
	m.idx = newIndexView(name, len(m.res.Repos))
	m.idx.width = m.width
	res := m.res
	start := func() tea.Msg {
		ev, out := m.index(res)
		return indexStartedMsg{events: ev, outcome: out}
	}
	return m, start
}

// View implements tea.Model.
func (m Model) View() string {
	if m.quit {
		return ""
	}
	var body, hint string
	switch m.scr {
	case scrAction:
		body = m.actionList.view(m.bodyHeight())
		hint = hintList()
	case scrSelect:
		body = m.selectList.view(m.bodyHeight())
		hint = hintMulti()
	case scrGroupPick:
		body = m.groupPick.view(m.bodyHeight())
		hint = hintList()
	case scrName:
		body = m.nameInput.view()
		hint = hintInput()
	case scrDocs:
		body = m.docsInput.view()
		hint = hintInputOpt()
	case scrMCP:
		body = m.mcpList.view(m.bodyHeight())
		hint = hintMulti()
	case scrIndex:
		body = m.idx.view()
		hint = hintIndex()
	case scrDone:
		body = m.idx.view()
		hint = hintDone()
	}
	if m.err != "" {
		body = errStyle.Render(g.Warn+" "+m.err) + "\n\n" + body
	}
	return frame(m.step, body, hint, m.width, m.height)
}

func (m Model) bodyHeight() int {
	h := m.height - 6 // header + footer + padding budget
	if h < 6 {
		h = 6
	}
	return h
}

// waitEvent / waitOutcome block on the index channels inside tea.Cmds.
func waitEvent(ch <-chan prog.Event) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return nil
		}
		return progressMsg(e)
	}
}

func waitOutcome(ch <-chan IndexOutcome) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		o, ok := <-ch
		if !ok {
			return nil
		}
		return outcomeMsg(o)
	}
}
