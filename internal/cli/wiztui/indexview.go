package wiztui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	prog "github.com/cajasmota/grafel/internal/progress"
)

// indexView holds the state for the per-repo indexing screen. It folds broker
// events into one Row per repo (fixing the dropped-repo bug) and renders an
// overall progress bar plus one styled row per repo.
type indexView struct {
	group         string
	expectedRepos int
	rows          map[string]Row
	// groupPhase holds the phase of the group-scoped event (RepoSlug == group),
	// e.g. the cross-repo links / flows pass added by the granular-phases work.
	// It is NOT a repo and must never render as a per-repo row (#5340); it only
	// surfaces in the overall label/bar.
	groupPhase string
	bar        progress.Model
	spin       spinner.Model
	terminal   bool   // RPC reported done
	failed     bool   // RPC reported an error
	errMsg     string // terminal error text
	width      int

	// final summary, populated when the RPC outcome lands.
	summaryEntities int64
	summaryRels     int64
	elapsed         string
	daemonDown      bool           // group registered but not indexed
	install         InstallSummary // captured applyGroupConfig output (fix C)
}

func newIndexView(group string, expectedRepos int) indexView {
	b := progress.New(
		// Blue → teal/green gradient (matches the light-blue accent), replacing
		// the charm default pink→purple.
		progress.WithScaledGradient("#5FB0FF", "#2FD6A6"),
		progress.WithoutPercentage(),
	)
	s := spinner.New()
	s.Spinner = g.Spinner()
	s.Style = lipgloss.NewStyle().Foreground(colAccent)
	return indexView{
		group:         group,
		expectedRepos: expectedRepos,
		rows:          map[string]Row{},
		bar:           b,
		spin:          s,
	}
}

// foldEvent folds a single broker event into the per-repo rows. A group-scoped
// event (RepoSlug == group, the cross-repo links/flows pass) is NOT a repo: it
// updates the overall group phase instead of spawning a spurious group row
// (#5340). Per-repo events (backend, frontend, …) always fold into rows.
func (v *indexView) foldEvent(e prog.Event) {
	if v.group != "" && e.RepoSlug == v.group {
		// Monotonic: never regress the group phase to a coarser one.
		if phaseRank(e.Phase) >= phaseRank(v.groupPhase) {
			v.groupPhase = e.Phase
		}
		return
	}
	v.rows = Fold(v.rows, e)
}

// done reports whether the indexing screen has fully completed.
func (v indexView) done() bool { return v.terminal || v.failed }

// finalizeRows marks every per-repo row terminal (Done) on a successful index
// completion. Because the index as a whole succeeded, every repo is done — but a
// repo's final SSE events (centrality → writing → done) can arrive after the
// Rebuild RPC returns done and the forwarder stops, leaving that row frozen on
// its last intermediate phase (e.g. "Building communities…"). Rather than depend
// on capturing that final SSE batch, advance any non-terminal row to PhaseDone,
// preserving its files/entities counts. Rows already done/error are left as-is.
// Only call this on SUCCESS; on failure rows keep their existing state (#5340).
func (v *indexView) finalizeRows() {
	for k, r := range v.rows {
		if r.Terminal() {
			continue
		}
		r.Phase = prog.PhaseDone
		v.rows[k] = r
	}
}

// overallLabel derives the header phase for the whole index. While per-repo
// rows are still in flight it reflects the least-advanced repo. Once every repo
// is terminal but the group-scoped pass (cross-repo links / flows) is still
// running, it surfaces THAT group phase instead — so the group-level work shows
// in the overall label rather than as a spurious per-repo row (#5340).
func (v indexView) overallLabel() string {
	if v.terminal {
		return "Done"
	}
	repoLabel := OverallPhaseLabel(v.rows, false)
	// If the group-scoped phase is in flight and more advanced than (or follows)
	// the per-repo work, prefer it. "Done" from OverallPhaseLabel means all rows
	// are terminal, so any non-terminal group phase takes over the label.
	if v.groupPhase != "" {
		gp := v.groupPhase
		if gp != prog.PhaseDone && gp != prog.PhaseError {
			if repoLabel == "Done" || phaseRank(gp) >= phaseRank(leastActivePhase(v.rows)) {
				return PhaseLabel(gp)
			}
		}
	}
	return repoLabel
}

// leastActivePhase returns the phase of the least-advanced non-terminal repo,
// or PhaseDone when every repo is terminal (used to decide whether the
// group-scoped phase should take over the overall label).
func leastActivePhase(rows map[string]Row) string {
	least := ""
	for _, r := range rows {
		if r.Terminal() {
			continue
		}
		if least == "" || phaseRank(r.Phase) < phaseRank(least) {
			least = r.Phase
		}
	}
	if least == "" {
		return prog.PhaseDone
	}
	return least
}

var (
	rowSlugStyle    = lipgloss.NewStyle().Foreground(colText).Bold(true)
	rowPhaseStyle   = lipgloss.NewStyle().Foreground(colYellow)
	rowDoneStyle    = lipgloss.NewStyle().Foreground(colGreen)
	rowErrStyle     = lipgloss.NewStyle().Foreground(colRed)
	rowCountStyle   = lipgloss.NewStyle().Foreground(colMuted)
	rowWarnStyle    = lipgloss.NewStyle().Foreground(colYellow)
	overallLblStyle = lipgloss.NewStyle().Foreground(colAccent2).Bold(true)
)

// renderRow renders one repo row: name · phase · files done/total · entities ·
// a spinner glyph while active.
func (v indexView) renderRow(r Row, spinnerFrame string) string {
	const slugW = 22

	name := truncate(r.RepoSlug, slugW)
	name = rowSlugStyle.Render(fmt.Sprintf("%-*s", slugW, name))

	var glyph, phase string
	switch {
	case r.Phase == prog.PhaseError:
		glyph = rowErrStyle.Render(g.Cross)
		msg := r.Error
		if msg == "" {
			msg = "error"
		}
		phase = rowErrStyle.Render(truncate(msg, 40))
	case r.Phase == prog.PhaseDone:
		glyph = rowDoneStyle.Render(g.Check)
		phase = rowDoneStyle.Render("Done")
	default:
		glyph = spinnerFrame
		phase = rowPhaseStyle.Render(PhaseLabel(r.Phase))
	}

	var extra []string
	if r.FilesTotal > 0 && !r.Terminal() {
		extra = append(extra, fmt.Sprintf("%d/%d files", r.FilesDone, r.FilesTotal))
	}
	if r.EntitiesSoFar > 0 {
		extra = append(extra, fmt.Sprintf("%d entities", r.EntitiesSoFar))
	}
	tail := ""
	if len(extra) > 0 {
		tail = "  " + rowCountStyle.Render(strings.Join(extra, " "+g.MidDot+" "))
	}

	return fmt.Sprintf("%s %s  %s%s", glyph, name, phase, tail)
}

// view renders the full indexing body (overall bar + per-repo rows + summary).
func (v indexView) view() string {
	var b strings.Builder

	rows := SortRows(v.rows)

	// Overall progress bar + label.
	pct := AggregateProgress(v.rows, v.expectedRepos)
	if v.terminal {
		pct = 1
	}
	label := v.overallLabel()
	if v.failed {
		label = "Failed"
	}

	width := v.width - 4
	if width < 20 {
		width = 20
	}
	if width > 80 {
		width = 80
	}
	bar := v.bar
	bar.Width = width

	b.WriteString(overallLblStyle.Render(fmt.Sprintf("Indexing %s — %s", v.group, label)))
	b.WriteString("\n")
	b.WriteString(bar.ViewAs(pct))
	b.WriteString(fmt.Sprintf("  %3d%%\n\n", int(pct*100)))

	if len(rows) == 0 && !v.done() {
		b.WriteString(rowCountStyle.Render("waiting for the indexer to report" + g.Ellipsis))
		return b.String()
	}

	spinnerFrame := v.spin.View()
	for _, r := range rows {
		b.WriteString(v.renderRow(r, spinnerFrame))
		b.WriteString("\n")
	}

	// Done / error summary.
	if v.failed {
		b.WriteString("\n")
		b.WriteString(rowErrStyle.Render("Index failed: " + v.errMsg))
	} else if v.terminal {
		b.WriteString("\n")
		b.WriteString(v.doneSummary())
	}

	return b.String()
}

// doneSummary renders the clean Done block that replaces applyGroupConfig's raw
// stdout (fix C, #5340): a one-line index result, the captured install counts,
// and any watcher warnings as styled non-fatal notes.
func (v indexView) doneSummary() string {
	var b strings.Builder

	// Index result line (entities / relationships / elapsed), or a soft note
	// when the daemon was down and the group was only registered.
	head := "Done"
	if v.daemonDown {
		head = "Registered (not indexed — daemon not running)"
	}
	parts := []string{head}
	if v.summaryEntities > 0 {
		parts = append(parts, fmt.Sprintf("%d entities", v.summaryEntities))
	}
	if v.summaryRels > 0 {
		parts = append(parts, fmt.Sprintf("%d relationships", v.summaryRels))
	}
	if v.elapsed != "" {
		parts = append(parts, v.elapsed)
	}
	b.WriteString(rowDoneStyle.Render(strings.Join(parts, "  "+g.MidDot+"  ")))

	// Captured install summary (hooks · watchers · MCP).
	if v.install.Applied {
		b.WriteString("\n")
		b.WriteString(rowCountStyle.Render(fmt.Sprintf(
			"installed %d hooks "+g.MidDot+" %d watchers "+g.MidDot+" %d MCP",
			v.install.Hooks, v.install.Watchers, v.install.MCP)))
	}

	// Watcher warnings as styled non-fatal notes.
	for _, w := range v.install.WatcherWarnings {
		b.WriteString("\n")
		b.WriteString(rowWarnStyle.Render(g.Warn + " " + w))
	}

	return b.String()
}
