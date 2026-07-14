package wiztui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
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

// IndexOutcome is the terminal result of indexing — OR, when Interim is true,
// an intermediate "graph queryable" checkpoint (#split-mode queryable state):
// the group's graph is already queryable but the background enhancement pass
// (linksFn tail) is still running. An interim outcome carries the
// queryable-time Entities/Rels + the Install summary (install always finishes
// before indexing starts, so it is safe/complete at interim time too); the
// model stays on the index screen and keeps waiting for the FINAL
// (Interim==false) outcome, unless the user presses enter to finish early.
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
	// Interim marks a non-terminal "graph queryable, background enhancement
	// still running" checkpoint. At most one interim outcome is ever sent,
	// always followed by exactly one terminal (Interim==false) outcome.
	Interim bool
	// RepoStats carries the split-mode status-plane classify's per-repo final
	// stats (slug, entities, rels, advanced-vs-failed), sourced independently
	// of the folded SSE progress rows. The model applies these over the
	// seeded/folded rows on the terminal outcome (see applyRepoStats) so a
	// repo that emitted ZERO progress events still shows its real entity count
	// and Done state instead of remaining blank/queued (the dropped-row bug).
	// nil/empty in monolith mode, which has no per-repo classify — rows there
	// fall back entirely to finalizeRows.
	RepoStats []RepoStat
}

// RepoStat is one selected repo's final classified result (see
// IndexOutcome.RepoStats). Slug must match the same repo-slug keying used by
// progress.Event.RepoSlug / Row.RepoSlug (rowKey) so it overlays the correct
// row rather than creating a duplicate.
type RepoStat struct {
	Slug     string
	Entities int64
	Rels     int64
	Failed   bool
	Error    string
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

// Metrics is a single live-process reading — the engine's CPU/RAM at the
// moment of the poll — surfaced to the right of the index screen's overall
// progress bar (wizard CPU/RAM readout). Both fields are independently
// omittable: RSSMB<=0 hides the whole readout (the must-have signal is
// absent, so the CPU% alone would be misleading); CPUPct<=0 with a positive
// RSSMB renders the RAM portion only (CPU% is best-effort).
type Metrics struct {
	RSSMB  int64
	CPUPct float64
}

// MetricsFunc polls the live engine process metrics (RSS/CPU) for the CPU/RAM
// readout. It is called on a periodic tea.Tick while the index screen is
// active (see metricsTick) — implemented by the cli package, which reads the
// engine-liveness status-plane sidecar (internal/daemon.EngineLivenessStatus)
// so wiztui itself stays decoupled from the daemon package. Must be cheap and
// non-blocking (a single disk read) and must NEVER panic or hang: a missing/
// stale status file (old engine, not-yet-started engine, monolith mode with
// no split) is a completely normal "unknown" case and should return the zero
// Metrics, not an error — there is no error return, by design, so a caller
// cannot forget to handle one and wedge the TUI. nil is a valid MetricsFunc:
// New leaves the ticker command it feeds unscheduled to skip and the readout
// simply never appears.
type MetricsFunc func() Metrics

// metricsMsg carries one MetricsFunc poll result into Update.
type metricsMsg Metrics

// metricsPollInterval is how often the index screen polls MetricsFunc.
// Deliberately loose — this only feeds a "still alive" readout, not a
// precision meter — so it costs nothing noticeable against the heartbeat
// writer's own ~5-30s cadence (see internal/daemon's
// defaultStatusHeartbeatInterval): a value close to that cadence would just
// re-read the same on-disk sample repeatedly.
const metricsPollInterval = 1500 * time.Millisecond

// metricsTick schedules the next MetricsFunc poll as a tea.Cmd. Returns nil
// (no-op Cmd) when fn is nil, so a caller with no metrics wiring (e.g. a test
// model, or a build where the cli package chose not to supply one) never
// starts a ticker at all.
func metricsTick(fn MetricsFunc) tea.Cmd {
	if fn == nil {
		return nil
	}
	return tea.Tick(metricsPollInterval, func(time.Time) tea.Msg {
		return metricsMsg(fn())
	})
}

// timerMsg is a no-payload tick used only to force a re-render of the index
// screen's live elapsed timer (see indexView.elapsedText). The elapsed value
// itself is computed from indexView.startedAt at render time; this message
// carries nothing but a "wake up and redraw" signal.
type timerMsg time.Time

// timerTickInterval is how often the index screen re-renders to advance the
// live elapsed timer. 1s matches the compact "MmSSs" display's resolution —
// any faster would waste CPU on a value that can't visibly change.
const timerTickInterval = 1 * time.Second

// timerTick schedules the next timer tick as a tea.Cmd.
func timerTick() tea.Cmd {
	return tea.Tick(timerTickInterval, func(t time.Time) tea.Msg {
		return timerMsg(t)
	})
}

// bgAnimMsg drives the secondary (background-enhancement) bar's indeterminate
// sweep animation (see indexView.advanceBgAnim). Scheduled only while the
// model is in the interim/queryable sub-state (see the outcomeMsg Interim
// branch and the bgAnimMsg case in Update) and deliberately NOT rescheduled
// once that state ends (terminal outcome lands, or the user finishes early) —
// so the ticker never leaks into the background after the screen is done.
type bgAnimMsg time.Time

// bgAnimTickInterval is the cadence of the indeterminate sweep. Faster than
// timerTickInterval (which only needs to redraw a 1s-resolution clock) since
// this drives a visibly moving/pulsing bar — smooth motion needs a shorter
// period.
const bgAnimTickInterval = 90 * time.Millisecond

// bgAnimTick schedules the next background-animation tick as a tea.Cmd.
func bgAnimTick() tea.Cmd {
	return tea.Tick(bgAnimTickInterval, func(t time.Time) tea.Msg {
		return bgAnimMsg(t)
	})
}

// Model is the full-screen Bubble Tea wizard model.
type Model struct {
	drv   Driver
	index IndexFunc
	// metricsFn polls the live engine CPU/RAM readout during the index screen
	// (wizard CPU/RAM readout). nil disables the readout entirely — see
	// MetricsFunc's doc.
	metricsFn MetricsFunc

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
// metricsFn polls the live engine CPU/RAM readout (wizard CPU/RAM readout) —
// pass nil to disable the readout entirely (e.g. a test model, or a caller
// that has no status-plane wiring). See MetricsFunc's doc.
func New(drv Driver, index IndexFunc, watchers, gitHooks bool, mcpTools []MCPToolOption, metricsFn MetricsFunc) Model {
	m := Model{
		drv:       drv,
		index:     index,
		metricsFn: metricsFn,
		scr:       scrAction,
		step:      StepAction,
		mcpTools:  mcpTools,
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
		return m, tea.Batch(m.idx.spin.Tick, waitEvent(m.evCh), waitOutcome(m.outCh), metricsTick(m.metricsFn), timerTick())

	case progressMsg:
		m.idx.foldEvent(prog.Event(msg))
		return m, waitEvent(m.evCh)

	case metricsMsg:
		m.idx.rssMB = msg.RSSMB
		m.idx.cpuPct = msg.CPUPct
		if m.idx.done() {
			// Index screen is finished (Done/Failed) — stop polling rather
			// than ticking forever in the background.
			return m, nil
		}
		return m, metricsTick(m.metricsFn)

	case outcomeMsg:
		o := IndexOutcome(msg)
		if o.Interim {
			// Graph queryable, background enhancement still running: capture the
			// queryable-time stats + install summary, enter the queryable
			// sub-state, and KEEP waiting — outCh still owes us the final
			// outcome. Never transitions to scrDone on its own (the user can
			// press enter to finish early; see updateKey's scrIndex case).
			m.idx.summaryEntities = o.Entities
			m.idx.summaryRels = o.Rels
			m.idx.install = o.Install
			m.idx.queryable = true
			// Stash the interim per-repo stats so an enter-early finalize (see
			// updateKey's scrIndex case) can still overlay real per-repo counts
			// instead of leaving a silent repo at "Done · 0 entities".
			m.idx.interimRepoStats = o.RepoStats
			// Stamp the queryable moment (freezes the main header elapsed there,
			// and anchors the secondary bar's own elapsed — see elapsedText /
			// bgElapsedText) and kick off the indeterminate sweep animation. At
			// most one interim outcome is ever sent, so this normally runs once;
			// gate BOTH the stamp and the tick-start on IsZero so a (spurious)
			// second interim outcome can't spawn a second concurrent tick chain.
			cmds := []tea.Cmd{waitOutcome(m.outCh)}
			if m.idx.queryableAt.IsZero() {
				m.idx.queryableAt = time.Now()
				cmds = append(cmds, bgAnimTick())
			}
			return m, tea.Batch(cmds...)
		}
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
			// Overlay the classify's authoritative per-repo stats FIRST (fixes
			// the dropped-row bug: a repo with zero folded progress events still
			// gets its real count + Done here), then finalizeRows as the
			// fallback for anything applyRepoStats didn't cover (e.g. monolith
			// mode, or a row somehow still non-terminal after the overlay).
			m.idx.applyRepoStats(o.RepoStats)
			// The whole index succeeded, so every repo is done. Force any row
			// still on an intermediate phase to Done — its final SSE events may
			// have arrived after the RPC returned and been dropped (#5340).
			m.idx.finalizeRows()
		}
		m.idx.finishedAt = time.Now()
		m.scr = scrDone
		m.step = StepDone
		return m, nil

	case timerMsg:
		// Live elapsed timer: re-render at ~1s cadence while the index screen is
		// active; stop scheduling once it's done (Done/Failed) so the ticker
		// doesn't run forever in the background.
		if m.scr == scrIndex && !m.idx.done() {
			return m, timerTick()
		}
		return m, nil

	case bgAnimMsg:
		// Secondary bar's indeterminate sweep: advance one frame and reschedule
		// only while still in the interim/queryable sub-state. Once terminal (the
		// background pass finished, or the user finished early) this simply stops
		// rescheduling — no goroutine/ticker leak.
		if m.idx.queryable && !m.idx.terminal {
			m.idx.advanceBgAnim()
			return m, bgAnimTick()
		}
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
		// Index screen: ctrl-c (handled globally) interrupts. Once the graph is
		// queryable but the background enhancement pass hasn't acked yet, enter
		// also finishes the wizard immediately as SUCCESS using the
		// already-captured interim stats — the alternative to just waiting for
		// the final outcome to land on its own.
		if msg.String() == "enter" && m.idx.queryable && !m.idx.terminal {
			m.idx.terminal = true
			// Overlay the interim classify's per-repo stats FIRST (so a repo
			// that emitted no progress events shows its real count, not 0),
			// then finalizeRows as the fallback — mirrors the terminal-outcome
			// path so finishing early is consistent with waiting.
			m.idx.applyRepoStats(m.idx.interimRepoStats)
			m.idx.finalizeRows()
			m.idx.finishedAt = time.Now()
			m.scr = scrDone
			m.step = StepDone
			return m, nil
		}
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
	// newInputModel already focuses the input; kick off the cursor blink.
	return m, textinput.Blink
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
		// newInputModel already focuses the input; kick off the cursor blink.
		return m, textinput.Blink
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
		// newInputModel already focuses the input; kick off the cursor blink.
		return m, textinput.Blink
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
	m.idx.startedAt = time.Now()
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
