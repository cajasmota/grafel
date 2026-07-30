package wiztui

import (
	"fmt"
	"strings"
	"time"

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

	// queryable marks the split-mode "graph queryable, background enhancement
	// still running" sub-state: an interim IndexOutcome landed but the terminal
	// one hasn't yet (or the user hasn't pressed enter to finish early). See
	// Model's outcomeMsg handling and the scrIndex enter key case.
	queryable bool

	// interimRepoStats holds the per-repo classify stats carried by the interim
	// (queryable) outcome, stashed so that if the user presses enter to FINISH
	// EARLY (before the terminal outcome lands) the enter-early finalize can
	// still overlay real per-repo counts onto the rows — otherwise a repo that
	// emitted zero progress events would show "Done · 0 entities" on early
	// finish. Empty in monolith mode / when no interim carried stats.
	interimRepoStats []RepoStat

	// startedAt / finishedAt bound the live elapsed timer shown in the index
	// header. startedAt is stamped when the index screen begins (startIndex);
	// finishedAt is stamped once terminal/failed so the header FREEZES at the
	// real elapsed instead of continuing to grow with wall-clock time.
	startedAt  time.Time
	finishedAt time.Time

	// queryableAt is stamped the instant the interim ("graph queryable")
	// outcome lands (see Model's outcomeMsg handling). Two things key off it:
	// (1) the MAIN header elapsed (elapsedText) freezes at startedAt..
	// queryableAt instead of continuing to grow while only the background
	// enhancement pass is still running — a still-ticking "Done · 1m14s…" reads
	// as "indexing is stuck" when the graph is actually ready; (2) the
	// secondary background-enhancement bar's own elapsed (bgElapsedText) counts
	// up from this moment, clearly attributing the running time to background
	// work rather than stalled indexing. Zero until the interim outcome lands.
	queryableAt time.Time

	// bgBar is the secondary progress bar shown only in the interim/queryable
	// sub-state, rendered below the queryable banner (reuses bar's gradient
	// styling — see newIndexView). The background enhancement pass (relationship
	// linking / enrichment) doesn't report a percentage to the wizard, so this
	// is driven as an INDETERMINATE animated sweep via bgPct/bgAnimDir rather
	// than a fabricated determinate percentage.
	bgBar progress.Model
	// bgPct is the current fill fraction [0,1] of the indeterminate sweep,
	// advanced by advanceBgAnim on each bgAnimMsg tick (see model.go). It
	// bounces between 0 and 1 (a "breathing" gradient) rather than monotonically
	// filling, since there is no real percentage to represent.
	bgPct float64
	// bgAnimDir is the current direction (+1 or -1) of the bgPct sweep.
	bgAnimDir float64

	// rssMB / cpuPct are the ENGINE process's CPU/RAM readout (wizard CPU/RAM
	// readout — see internal/statusfile.File's RSSMB/CPUPct doc), polled
	// periodically from the engine-liveness status-plane sidecar via Model's
	// metricsFn (see model.go). Zero/absent means "unknown or not yet polled"
	// and the readout is omitted entirely — never rendered as a misleading
	// 0%/0.0 GB.
	//
	// rssMB is the RUNNING PEAK of that ONE process, not the latest
	// instantaneous sample (#6047 round 1) and NOT the whole run's footprint
	// (#6047 round 2 correction): Model's metricsMsg handler only ever raises
	// it, never lowers it, so it tracks the engine's own high-water mark
	// instead of whatever it happened to be using at the exact instant the
	// wizard polled last.
	//
	// It is still explicitly SCOPED to the engine (internal/daemon/
	// statuswriter.go populates the sidecar field from
	// process.RSSBytes(os.Getpid()) — the writing process alone), which is
	// NOT the whole run: extraction runs in separate child OS processes and
	// is the dominant consumer (measured on #6047's own repro: extract child
	// peak 3411 MB vs. engine peak 1444 MB, whole-installation peak 3525 MB —
	// the engine alone is under half the true peak). Aggregating the extract
	// children's own RSS is NOT done here: their self-reported figure
	// (extract/subproc.go, runtime.MemStats.Sys) excludes the CGO/tree-sitter
	// heap that dominates that process, so it would need its own fix first —
	// filed separately as #6048, out of scope here. metricSuffix therefore
	// labels this reading "engine peak", not just "peak" — an unqualified
	// "peak" would be a second, differently-wrong claim about the run's
	// actual maximum (understating it by roughly the same ~2.5x the original
	// instantaneous-at-exit reading did). cpuPct stays a live instantaneous
	// engine reading (best-effort, independently omittable) — there is no
	// equivalent "peak CPU%" complaint from #6047, and peak-CPU% is a much
	// noisier, less meaningful number than peak RSS.
	rssMB  int64
	cpuPct float64

	// outstanding names the post-extraction stage(s) (group-algo / links /
	// analytics) for this group that were still running or pending the
	// instant the wizard's terminal outcome landed, in the same vocabulary
	// `grafel status`'s annotation/stage_gate lines use (#6047) — or "" when
	// nothing was outstanding (the common case: a small group, or an
	// already-warm overlay). The wizard deliberately does NOT block on this:
	// the graph itself is queryable the instant extraction + the cross-repo
	// link merge finish, and waiting for the community/pagerank/centrality
	// overlay too would turn a ~16-minute run into a ~26-minute one for no
	// user benefit. This is purely a completion-honesty caveat appended to
	// doneSummary, set once — either from IndexOutcome.Outstanding on a
	// normal terminal outcome (a fresh daemon status query, see
	// wizard_outstanding.go), or to earlyFinishOutstandingCaveat when the
	// user finishes EARLY via enter from the queryable sub-state (see
	// Model's updateKey scrIndex case — that path has no outcome to read
	// Outstanding from, but by construction — queryable && !terminal — the
	// background enhancement pass has NOT yet acked, so the caveat is
	// unconditional there, not queried) — never updated afterward (the
	// wizard has already exited its polling loops by then).
	outstanding string
}

// earlyFinishOutstandingCaveat is the completion-honesty caveat (#6047 round
// 2) shown when the user finishes EARLY from the queryable/interim sub-state
// (enter pressed before the background enhancement pass has acked — see
// Model's updateKey scrIndex case). Before enter, that sub-state is rendered
// by queryableBanner ("Graph queryable ... enhancing relationships in the
// background"); pressing enter sets v.terminal, which makes view() dispatch
// to doneSummary INSTEAD (see view()'s v.terminal/v.queryable branch order)
// — so without this, the banner's honesty is simply discarded the moment the
// user acts on it, landing on a screen that claims plain "Done" while the
// same background work the banner just described is still running.
//
// Unlike the normal terminal-outcome caveat (computed by the cli package
// from a fresh daemon status query and able to name specific stages — see
// wizard_outstanding.go's outstandingCaveat), finishing early has, BY
// CONSTRUCTION, no such query available: queryable && !terminal means the
// engine has not yet acked the background enhancement pass, full stop, so
// this is a fixed, honest caveat rather than a queried one — phrased with
// the SAME "graph itself is queryable" wording `grafel status` uses.
const earlyFinishOutstandingCaveat = "background enhancement still running (communities/pagerank/centrality overlay not yet current; the graph itself is queryable)"

func newIndexView(group string, expectedRepos int) indexView {
	b := progress.New(
		// Blue → teal/green gradient (matches the light-blue accent), replacing
		// the charm default pink→purple.
		progress.WithScaledGradient("#5FB0FF", "#2FD6A6"),
		progress.WithoutPercentage(),
	)
	// bgBar mirrors the main bar's gradient styling so the secondary
	// background-enhancement bar reads as visually part of the same system.
	bg := progress.New(
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
		bgBar:         bg,
		bgAnimDir:     1,
		spin:          s,
	}
}

// advanceBgAnim advances the indeterminate secondary bar's sweep by one tick,
// bouncing bgPct between 0 and 1 (a "breathing" gradient) rather than filling
// monotonically — there is no real percentage for the background enhancement
// pass to report (see bgBar's doc), so an honest indeterminate sweep is used
// instead of a fabricated one.
func (v *indexView) advanceBgAnim() {
	const step = 0.055
	v.bgPct += step * v.bgAnimDir
	if v.bgPct >= 1 {
		v.bgPct = 1
		v.bgAnimDir = -1
	} else if v.bgPct <= 0 {
		v.bgPct = 0
		v.bgAnimDir = 1
	}
}

// foldEvent folds a single broker event into the per-repo (or, for a
// monorepo, per-module) rows. A group-scoped event (RepoSlug == group and no
// Module, the cross-repo links/flows pass) is NOT a repo: it updates the
// overall group phase instead of spawning a spurious group row (#5340).
// Per-repo events (backend, frontend, …) and per-module monorepo events
// always fold into rows — the Module check keeps a monorepo whose single
// repo shares its slug with the group name (a common case) from having its
// module ticks misrouted into the group-phase guard.
func (v *indexView) foldEvent(e prog.Event) {
	// A PhaseQueued seed event is UNAMBIGUOUSLY a per-repo placeholder (never
	// emitted for the group-scoped cross-repo pass), so it always folds into a
	// row — bypassing the group-slug-collision guard below. This matters for a
	// solo repo whose slug happens to equal the group name (the common
	// single-repo default), which would otherwise never get a seeded row.
	if e.Phase != PhaseQueued && v.group != "" && e.RepoSlug == v.group && e.Module == "" {
		// Monotonic: never regress the group phase to a coarser one.
		if phaseRank(e.Phase) >= phaseRank(v.groupPhase) {
			v.groupPhase = e.Phase
		}
		return
	}
	v.rows = Fold(v.rows, e)
}

// applyRepoStats overlays the split-mode classify's authoritative per-repo
// final stats onto rows keyed by slug (rowKey(s.Slug, "")). This is the fix
// for the dropped-row bug: a repo that emitted ZERO progress events (a fast
// repo racing the SSE forwarder, a dropped batch, a subprocess-IPC gap) still
// gets its real entity count and terminal Done/Error state here, sourced from
// the status-plane classify rather than folded SSE ticks. Creates the row
// defensively if it is somehow still missing (should not happen once seeding
// runs, but never silently drops a repo). Call this BEFORE finalizeRows,
// which remains the fallback for any row this doesn't cover (e.g. monolith
// mode, which has no per-repo classify).
func (v *indexView) applyRepoStats(stats []RepoStat) {
	if v.rows == nil {
		v.rows = map[string]Row{}
	}
	for _, s := range stats {
		key := rowKey(s.Slug, "")
		row, had := v.rows[key]
		if !had {
			row = Row{Key: key, RepoSlug: s.Slug}
		}
		if s.Failed {
			// Never DOWNGRADE a row that already reported Done over SSE to
			// Error via the classify overlay: the live SSE stream saw the repo
			// finish, which is more authoritative than a status-plane classify
			// that (e.g. on a mtime/ack race) transiently reads it as
			// not-advanced. Trust the row that actually reported success and
			// leave it as-is.
			if had && row.Phase == prog.PhaseDone {
				continue
			}
			row.Phase = prog.PhaseError
			if s.Error != "" {
				row.Error = s.Error
			}
			v.rows[key] = row
			continue
		}
		// Success: overlay the authoritative final entity count and mark Done.
		row.EntitiesSoFar = int(s.Entities)
		row.Phase = prog.PhaseDone
		v.rows[key] = row
	}
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

// rowLabel derives the primary display label for a row. For a monorepo
// module (r.Module != ""), the MODULE — not the shared repo-slug prefix — is
// the distinguishing part, so it is the label (its basename, if it happens to
// carry path separators). Using "RepoSlug/Module" here regressed to every
// module row rendering identically once RepoSlug alone filled the whole slug
// column (e.g. a long monorepo slug like "some-example-monorepo-main") — see
// renderRow's slugW.
// A plain group repo (Module=="") keeps RepoSlug, which is already a short
// basename.
func rowLabel(r Row) string {
	if r.Module == "" {
		return r.RepoSlug
	}
	m := r.Module
	if i := strings.LastIndexAny(m, `/\`); i >= 0 && i < len(m)-1 {
		m = m[i+1:]
	}
	return m
}

// renderRow renders one repo row: name · phase · files done/total · entities ·
// a spinner glyph while active.
func (v indexView) renderRow(r Row, spinnerFrame string) string {
	const slugW = 30

	// Elide from the LEFT on overflow (keep the distinguishing tail) rather
	// than the right — a long label's differentiating suffix matters more than
	// its common prefix.
	name := truncateLeft(rowLabel(r), slugW)
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
	case r.Phase == PhaseQueued:
		// Queued-but-not-started: a muted static marker, NOT the spinner — the
		// spinner reads as "actively working," which a seeded-but-unreported
		// repo is not (yet).
		glyph = rowCountStyle.Render(g.MidDot)
		phase = rowCountStyle.Render(PhaseLabel(PhaseQueued))
	default:
		glyph = spinnerFrame
		phase = rowPhaseStyle.Render(PhaseLabel(r.Phase))
	}

	var extra []string
	if r.FilesTotal > 0 && !r.Terminal() {
		extra = append(extra, fmt.Sprintf("%s/%s files", commafy(int64(r.FilesDone)), commafy(int64(r.FilesTotal))))
	}
	if r.EntitiesSoFar > 0 {
		extra = append(extra, fmt.Sprintf("%s entities", commafy(int64(r.EntitiesSoFar))))
	}
	tail := ""
	if len(extra) > 0 {
		tail = "  " + rowCountStyle.Render(strings.Join(extra, " "+g.MidDot+" "))
	}

	return fmt.Sprintf("%s %s  %s%s", glyph, name, phase, tail)
}

// metricSuffix renders the live "CPU / peak RAM" readout that appears to the
// right of the overall progress bar's percentage — reassurance that a
// large-monorepo rebuild's multi-minute post-index enrichment phase (where the
// bar sits near 100% for a long stretch) is still doing real work, not stuck
// (motivation for this whole feature).
//
// The RAM figure is explicitly labelled "engine peak" (#6047, corrected in
// round 2 review from a bare "peak"): v.rssMB is the max of every poll this
// run has seen (see indexView.rssMB's doc) — fixing the original "live
// instant read as a total" defect — but it is ONLY the wizard-owning engine
// process, not the whole run. Extraction runs in separate child OS processes
// and is the DOMINANT consumer (measured on #6047's own repro: extract child
// peak 3411 MB vs. engine peak 1444 MB): an unqualified "peak 1.4 GB" would
// be a new, still-wrong, now-FALSIFIABLE claim about the run's actual
// maximum — worse than the original unlabelled instant, not better. "engine
// peak" is the honest, cheap label: true of what it names, silent about what
// it doesn't cover (aggregating the extract children is #6048, not this
// file's scope — see indexView.rssMB's doc for why). cpuPct stays a live
// instantaneous reading, so it keeps its plain "CPU" label rather than "peak
// CPU" (no equivalent complaint, and a live number still reads correctly as
// a live number even while indexing is ongoing).
//
// Omits gracefully: an absent/zero rssMB (old status file predating this
// field, engine metric read failed, or no poll has landed yet) returns "" and
// the bar renders exactly as it did before this feature — no dangling
// separator, no misleading "0.0 GB". cpuPct is independently optional: a
// positive rssMB with cpuPct==0 renders RAM only (matches the spec's
// "best-effort CPU%" contract — RSS is the must-have signal).
func (v indexView) metricSuffix() string {
	if v.rssMB <= 0 {
		return ""
	}
	gb := float64(v.rssMB) / 1024.0
	var text string
	if v.cpuPct > 0 {
		text = fmt.Sprintf("%s CPU %.0f%% %s engine peak %.1f GB", g.MidDot, v.cpuPct, g.MidDot, gb)
	} else {
		text = fmt.Sprintf("%s engine peak %.1f GB", g.MidDot, gb)
	}
	return "  " + rowCountStyle.Render(text)
}

// elapsedText renders the live/frozen elapsed segment for the index header
// ("2m14s"). Absent (startedAt zero, e.g. a headless test that never called
// startIndex) returns "" so the header renders exactly as before this
// feature. While indexing it is computed against time.Now() (so it advances
// every ~1s tick — see Model's timerMsg handling); once done() it freezes at
// finishedAt-startedAt so it stops growing with wall-clock time. In the
// split-mode interim/queryable sub-state (graph queryable, background
// enhancement still running, not yet terminal) it ALSO freezes — at
// queryableAt-startedAt — so the header doesn't read as "stuck" while only
// optional background work remains; see bgElapsedText for the secondary
// timer that keeps counting for that background work.
func (v indexView) elapsedText() string {
	if v.startedAt.IsZero() {
		return ""
	}
	end := time.Now()
	switch {
	case v.done() && !v.finishedAt.IsZero():
		end = v.finishedAt
	case v.queryable && !v.queryableAt.IsZero():
		end = v.queryableAt
	}
	return fmtElapsed(end.Sub(v.startedAt))
}

// bgElapsedText renders the secondary bar's own elapsed segment ("1m02s"),
// counting up from the moment the graph became queryable (queryableAt) rather
// than from startedAt — so the running time is clearly attributed to the
// background enhancement pass, not indexing itself. Returns "" before
// queryableAt is stamped. Freezes at finishedAt once done(), mirroring
// elapsedText, so it doesn't keep growing after the screen finishes.
func (v indexView) bgElapsedText() string {
	if v.queryableAt.IsZero() {
		return ""
	}
	end := time.Now()
	if v.done() && !v.finishedAt.IsZero() {
		end = v.finishedAt
	}
	d := end.Sub(v.queryableAt)
	if d < 0 {
		d = 0
	}
	return fmtElapsed(d)
}

// view renders the full indexing body (overall bar + per-repo rows + summary).
func (v indexView) view() string {
	var b strings.Builder

	rows := SortRows(v.rows)

	// Overall progress bar + label. In the interim/queryable sub-state the
	// graph is already usable, so the MAIN bar reads 100% too — only the
	// secondary bar (below) represents the still-running background work.
	pct := AggregateProgress(v.rows, v.expectedRepos)
	if (v.terminal || v.queryable) && !v.failed {
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

	head := fmt.Sprintf("Indexing %s — %s", v.group, label)
	if e := v.elapsedText(); e != "" {
		head += "  " + g.MidDot + " " + e
	}
	b.WriteString(overallLblStyle.Render(head))
	b.WriteString("\n")
	b.WriteString(bar.ViewAs(pct))
	b.WriteString(fmt.Sprintf("  %3d%%", int(pct*100)))
	if suffix := v.metricSuffix(); suffix != "" {
		b.WriteString(suffix)
	}
	b.WriteString("\n\n")

	if len(rows) == 0 && !v.done() && !v.queryable {
		b.WriteString(rowCountStyle.Render("waiting for the indexer to report" + g.Ellipsis))
		return b.String()
	}

	spinnerFrame := v.spin.View()
	for _, r := range rows {
		b.WriteString(v.renderRow(r, spinnerFrame))
		b.WriteString("\n")
	}

	// Done / error / queryable summary.
	if v.failed {
		b.WriteString("\n")
		b.WriteString(rowErrStyle.Render("Index failed: " + v.errMsg))
	} else if v.terminal {
		b.WriteString("\n")
		b.WriteString(v.doneSummary())
	} else if v.queryable {
		b.WriteString("\n")
		b.WriteString(v.queryableBanner())
		b.WriteString("\n\n")
		b.WriteString(v.bgProgressBlock())
	}

	return b.String()
}

// bgProgressBlock renders the secondary background-enhancement bar shown only
// in the interim/queryable sub-state (queryable && !terminal): a label with
// its own independently-running elapsed timer, followed by an indeterminate
// animated bar (bgPct, advanced by advanceBgAnim on each bgAnimMsg tick — see
// model.go). It is indeterminate rather than a fabricated percentage because
// the background enhancement pass genuinely reports no progress signal to the
// wizard (runSplitIndex just awaits completion; see wizard_tui_run.go).
func (v indexView) bgProgressBlock() string {
	var b strings.Builder

	label := "Enhancing relationships in the background"
	if e := v.bgElapsedText(); e != "" {
		label += "  " + g.MidDot + " +" + e
	}
	b.WriteString(rowCountStyle.Render(label))
	b.WriteString("\n")

	width := v.width - 4
	if width < 20 {
		width = 20
	}
	if width > 80 {
		width = 80
	}
	bar := v.bgBar
	bar.Width = width
	b.WriteString(bar.ViewAs(v.bgPct))

	return b.String()
}

// queryableBanner renders the split-mode "graph queryable, background
// enhancement still running" sub-state: a checkmark line naming the
// queryable-time entity count, and a two-choice hint (finish now vs. wait for
// the full background pass). Shown instead of the Done summary while
// v.queryable is true and v.terminal is still false.
func (v indexView) queryableBanner() string {
	var b strings.Builder
	b.WriteString(rowDoneStyle.Render(fmt.Sprintf(
		"%s Graph queryable (%s entities) %s enhancing relationships in the background",
		g.Check, commafy(v.summaryEntities), g.MidDot)))
	b.WriteString("\n")
	b.WriteString(rowCountStyle.Render(
		"press enter to finish now (safe) " + g.MidDot + " or wait for background to complete"))
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
		parts = append(parts, fmt.Sprintf("%s entities", commafy(v.summaryEntities)))
	}
	if v.summaryRels > 0 {
		parts = append(parts, fmt.Sprintf("%s relationships", commafy(v.summaryRels)))
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

	// Completion-honesty caveat (#6047): "Done" covers extraction + the
	// cross-repo link merge — the graph itself is queryable — but the
	// community/pagerank/centrality overlay (and, while links itself is the
	// outstanding stage, cross-repo edges/flows) may still be computing in the
	// background. Silent when nothing was outstanding at completion (the
	// common case), never printed for a failed/daemon-down outcome.
	if v.outstanding != "" {
		b.WriteString("\n")
		b.WriteString(rowWarnStyle.Render(g.Warn + " " + v.outstanding))
	}

	return b.String()
}
