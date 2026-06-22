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
	bar           progress.Model
	spin          spinner.Model
	terminal      bool   // RPC reported done
	failed        bool   // RPC reported an error
	errMsg        string // terminal error text
	width         int

	// final summary, populated when the RPC outcome lands.
	summaryEntities int64
	summaryRels     int64
	elapsed         string
	daemonDown      bool           // group registered but not indexed
	install         InstallSummary // captured applyGroupConfig output (fix C)
}

func newIndexView(group string, expectedRepos int) indexView {
	b := progress.New(
		progress.WithDefaultGradient(),
		progress.WithoutPercentage(),
	)
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colAccent)
	return indexView{
		group:         group,
		expectedRepos: expectedRepos,
		rows:          map[string]Row{},
		bar:           b,
		spin:          s,
	}
}

// foldEvent folds a single broker event into the per-repo rows.
func (v *indexView) foldEvent(e prog.Event) {
	v.rows = Fold(v.rows, e)
}

// done reports whether the indexing screen has fully completed.
func (v indexView) done() bool { return v.terminal || v.failed }

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
		glyph = rowErrStyle.Render("✗")
		msg := r.Error
		if msg == "" {
			msg = "error"
		}
		phase = rowErrStyle.Render(truncate(msg, 40))
	case r.Phase == prog.PhaseDone:
		glyph = rowDoneStyle.Render("✓")
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
		tail = "  " + rowCountStyle.Render(strings.Join(extra, " · "))
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
	label := OverallPhaseLabel(v.rows, v.terminal)
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
		b.WriteString(rowCountStyle.Render("waiting for the indexer to report…"))
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
	b.WriteString(rowDoneStyle.Render(strings.Join(parts, "  ·  ")))

	// Captured install summary (hooks · watchers · MCP).
	if v.install.Applied {
		b.WriteString("\n")
		b.WriteString(rowCountStyle.Render(fmt.Sprintf(
			"installed %d hooks · %d watchers · %d MCP",
			v.install.Hooks, v.install.Watchers, v.install.MCP)))
	}

	// Watcher warnings as styled non-fatal notes.
	for _, w := range v.install.WatcherWarnings {
		b.WriteString("\n")
		b.WriteString(rowWarnStyle.Render("⚠ " + w))
	}

	return b.String()
}
