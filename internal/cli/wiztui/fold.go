// Package wiztui implements the cohesive Bubble Tea TUI for `grafel wizard`.
//
// fold.go is a Go port of the dashboard's per-repo progress folding
// (webui-v2/src/lib/index-progress-fold.ts). It collapses the broker's
// firehose of progress.Event records into exactly ONE row per repo — or, for
// a monorepo, one row per module — keyed by rowKey(repo_slug, module), with a
// monotonic phase so a stale lower phase never regresses a more-advanced one.
// This is the model that fixes the dropped-repo display bug in the CLI
// indexing view: instead of overwriting a single carriage-return line (which
// dropped all-but-one repo), every repo (or monorepo module) renders as its
// own row.
package wiztui

import (
	"sort"

	"github.com/cajasmota/grafel/internal/progress"
)

// Phase labels, kept in lock-step with the dashboard PHASE_LABEL and the CLI
// renderer so all three surfaces speak the same human phrase for the same
// phase (#5334).
// PhaseQueued is the synthetic seed phase folded into a row for every
// selected repo the moment indexing starts, BEFORE any real broker/SSE
// progress event has arrived for it. Rows are keyed by repo slug (see
// rowKey), so a queued row created with this phase MERGES with the real
// progress events that follow for the same repo instead of duplicating —
// Fold's monotonic-phase rule (phaseRank(PhaseQueued) is unknown → -1, lower
// than every real phase) guarantees the first real event always advances
// past it. This closes the dropped-row bug where a repo whose progress
// events were missed/dropped/raced never got a row at all: seeding
// unconditionally guarantees one row per selected repo up front, and
// indexView.applyRepoStats backstops the final count/state from the
// split-mode classify even if no real event ever arrived.
const PhaseQueued = "queued"

var phaseLabel = map[string]string{
	PhaseQueued:                     "Queued…",
	progress.PhaseScan:              "Scanning…",
	progress.PhaseExtractAST:        "Extracting AST…",
	progress.PhaseResolveRefs:       "Resolving references…",
	progress.PhaseAlgorithms:        "Running algorithms…",
	progress.PhaseMaterialize:       "Materializing graph…",
	progress.PhaseBuildCommunities:  "Building communities…",
	progress.PhaseComputeCentrality: "Computing centrality…",
	progress.PhaseDetectLinks:       "Detecting cross-repo links…",
	progress.PhaseComputeFlows:      "Computing flows…",
	progress.PhaseWriteGraph:        "Writing graph…",
	progress.PhaseDone:              "Done",
	progress.PhaseError:             "Done",
}

// phaseOrder is the monotonic phase ranking, mirroring the backend's real
// emission sequence so a later, finer phase never regresses to a coarse one.
var phaseOrder = map[string]int{
	progress.PhaseScan:              0,
	progress.PhaseExtractAST:        1,
	progress.PhaseResolveRefs:       2,
	progress.PhaseMaterialize:       3,
	progress.PhaseAlgorithms:        4,
	progress.PhaseBuildCommunities:  5,
	progress.PhaseComputeCentrality: 6,
	progress.PhaseComputeFlows:      7,
	progress.PhaseDetectLinks:       8,
	progress.PhaseWriteGraph:        9,
	progress.PhaseDone:              10,
	progress.PhaseError:             10,
}

// phaseBands is the number of bands [0,100] is split into — one per
// non-terminal phase index, matching PHASE_BANDS in the dashboard.
const phaseBands = 10

// PhaseLabel returns the human label for a phase, or the raw phase when unknown.
func PhaseLabel(phase string) string {
	if l, ok := phaseLabel[phase]; ok {
		return l
	}
	return phase
}

// phaseRank returns the monotonic rank of a phase (unknown phases sort first).
func phaseRank(phase string) int {
	if r, ok := phaseOrder[phase]; ok {
		return r
	}
	return -1
}

// Row is one per-repo progress row, the Go analogue of the dashboard ProgressRow.
type Row struct {
	Key           string
	RepoSlug      string
	Module        string
	Phase         string
	FilesDone     int
	FilesTotal    int
	EntitiesSoFar int
	CurrentFile   string
	Error         string
	TS            int64
}

// Terminal reports whether the row has reached a terminal phase.
func (r Row) Terminal() bool {
	return r.Phase == progress.PhaseDone || r.Phase == progress.PhaseError
}

// rowKey computes the row map key for an event: repoSlug alone for a plain
// repo / whole-repo event (module == "" or module == repoSlug, e.g. fleet
// indexing or a monorepo's repo-level phases), or "repoSlug/module" for a
// true per-module event (monorepo per-file attribution) so each module gets
// its own row instead of collapsing into the repo's row.
func rowKey(repoSlug, module string) string {
	if module == "" || module == repoSlug {
		return repoSlug
	}
	return repoSlug + "/" + module
}

// Fold collapses progress events into one row per repo (or, for a monorepo,
// one row per module). It is a value-semantics fold: callers hold a
// map[string]Row keyed by rowKey(repoSlug, module) and call Fold per event.
// Stale (older-ts) events and lower-ordered phases never regress an existing
// row; terminal rows are never pulled back to an in-flight phase by a late
// module-scoped event. A faithful port of fold() in index-progress-fold.ts,
// extended with module-aware keying (#5751 monorepo one-row-per-module).
func Fold(rows map[string]Row, e progress.Event) map[string]Row {
	if rows == nil {
		rows = map[string]Row{}
	}
	key := rowKey(e.RepoSlug, e.Module)
	existing, had := rows[key]

	// Ignore stale events that predate what we already have for this row.
	if had && e.TS < existing.TS {
		return rows
	}

	// Don't let a late, lower-ordered phase event overwrite a more-advanced
	// phase already shown for this repo (the dropped/stale-row symptom #5326).
	advance := !had || phaseRank(e.Phase) >= phaseRank(existing.Phase)
	phase := e.Phase
	if !advance {
		phase = existing.Phase
	}

	next := Row{
		Key:      key,
		RepoSlug: e.RepoSlug,
		Phase:    phase,
		TS:       e.TS,
	}

	// Keep a module label only when it genuinely differs from the repo slug
	// (true sub-module reporting) — never split a repo into a duplicate row.
	if e.Module != "" && e.Module != e.RepoSlug {
		next.Module = e.Module
	} else if had {
		next.Module = existing.Module
	}

	// Never regress files_done; carry forward the discovered total.
	next.FilesDone = e.FilesDone
	if had && existing.FilesDone > next.FilesDone {
		next.FilesDone = existing.FilesDone
	}
	next.FilesTotal = e.FilesTotal
	if next.FilesTotal == 0 && had {
		next.FilesTotal = existing.FilesTotal
	}
	next.EntitiesSoFar = e.EntitiesSoFar
	if had && existing.EntitiesSoFar > next.EntitiesSoFar {
		next.EntitiesSoFar = existing.EntitiesSoFar
	}
	next.CurrentFile = e.CurrentFile
	if next.CurrentFile == "" && had {
		next.CurrentFile = existing.CurrentFile
	}
	next.Error = e.Error
	if next.Error == "" && had {
		next.Error = existing.Error
	}

	// Clone so callers can compare old vs new safely.
	out := make(map[string]Row, len(rows)+1)
	for k, v := range rows {
		out[k] = v
	}
	out[key] = next
	return out
}

// SortRows returns rows in a stable order (by repo slug, then module).
func SortRows(rows map[string]Row) []Row {
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RepoSlug != out[j].RepoSlug {
			return out[i].RepoSlug < out[j].RepoSlug
		}
		return out[i].Module < out[j].Module
	})
	return out
}

// rowFraction is the phase-weighted completion fraction (0..1) of one row.
func rowFraction(r Row) float64 {
	if r.Terminal() {
		return 1
	}
	idx := phaseRank(r.Phase)
	if idx < 0 {
		return 0
	}
	base := float64(idx) / phaseBands
	if r.Phase == progress.PhaseExtractAST && r.FilesTotal > 0 {
		slice := float64(r.FilesDone) / float64(r.FilesTotal)
		if slice < 0 {
			slice = 0
		}
		if slice > 1 {
			slice = 1
		}
		return base + slice/phaseBands
	}
	return base
}

// AggregateProgress is the [0,1] aggregate progress across all repos, averaged
// over expectedRepos when known (so not-yet-reported repos count as 0 and the
// bar doesn't jump when a late repo first appears).
func AggregateProgress(rows map[string]Row, expectedRepos int) float64 {
	denom := len(rows)
	if expectedRepos > 0 && expectedRepos > denom {
		denom = expectedRepos
	}
	if denom <= 0 {
		return 0
	}
	var sum float64
	for _, r := range rows {
		sum += rowFraction(r)
	}
	pct := sum / float64(denom)
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	return pct
}

// OverallPhaseLabel returns the overall indexing label, derived from the
// least-advanced active (non-terminal) repo — the phase the whole index is
// gated on. When terminal or every row is terminal, the label is "Done".
func OverallPhaseLabel(rows map[string]Row, terminal bool) string {
	if terminal {
		return "Done"
	}
	var least *Row
	for _, r := range rows {
		if r.Terminal() {
			continue
		}
		rc := r
		if least == nil || phaseRank(rc.Phase) < phaseRank(least.Phase) {
			least = &rc
		}
	}
	if least == nil {
		return "Done"
	}
	return PhaseLabel(least.Phase)
}

// RowsTerminal reports whether every expected repo has reached a terminal
// phase. Mirrors rowsTerminal(): without a known expected count it returns
// false (defers to the RPC outcome), and it requires len(rows) >= expectedRepos
// so an early "first repo done" never looks terminal before later repos report.
func RowsTerminal(rows map[string]Row, expectedRepos int) bool {
	if len(rows) == 0 {
		return false
	}
	if expectedRepos <= 0 {
		return false
	}
	if len(rows) < expectedRepos {
		return false
	}
	for _, r := range rows {
		if !r.Terminal() {
			return false
		}
	}
	return true
}
