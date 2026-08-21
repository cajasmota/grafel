package main

// rebuild_history.go — capture quality metrics after every daemon rebuild
// and persist them to health-history.jsonl (#1329).
//
// All metric collection is best-effort: errors are logged and silently
// skipped so a failed scan never blocks the rebuild response.
//
// #5954 — ONE PASS, AND VISIBLE.
//
// This batch used to walk every repo's graph FOUR times, each walk going
// through graph.LoadGraphFromDir (the full-materialise loader: ~608 MB of heap
// for a 325 MB graph.fb):
//
//	1. audit.AuditPath        — orphan rate + IMPORTS hygiene
//	2. loadGraphFromStateDir  — coverage + import cycles + endpoint/flow tallies
//	3. countAuthUncoveredEndpoints — a third load, for one integer
//	4. buildAgentsMapStats    — a fourth load, in the index loop
//
// Every one of those numbers is a counting scan over the same rows. They are
// now computed by internal/quality/analytics in a SINGLE pass over a single
// open, mmap-backed view of each repo's graph — no Document is materialised at
// all. See that package for what survives a scan (id-keyed sets) and what does
// not (rows).
//
// The batch also ran in a bare `go func(){}` that took no stage-gate token and
// was invisible to the scheduler's busyLocked() accounting, so it could
// co-reside with the link and group-algo children the epic had just forked out
// of the engine, and it distorted the debounced FreeOSMemory release. It now
// registers a foreground hold on the heavy write-stage gate for its whole
// window — see the comment on the registration below for why a barge rather
// than the EXCLUSIVE token.

import (
	"fmt"
	"os"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/quality"
	"github.com/cajasmota/grafel/internal/quality/analytics"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/secrets"
)

// scanRepoAnalytics is the single-pass, mmap-backed analytics scan. Indirected
// so tests can prove the whole batch touches each repo's graph exactly once.
var scanRepoAnalytics = analytics.ScanRepo

// appendRebuildHistory measures the current graph quality for a group and
// appends one HealthEntry to the JSONL history file.
//
// root is the daemon layout root (typically ~/.grafel).
// group is the grafel group name.
// cfg is the already-loaded group configuration.
// rebuiltPaths is the list of repo paths that were successfully indexed.
//
// The function is designed to run in a goroutine: all errors are surfaced
// via the returned error rather than panicking.
func appendRebuildHistory(root, group string, cfg *registry.GroupConfig, rebuiltPaths []string) error {
	// Register with the heavy write-stage gate for the whole batch (#5954).
	//
	// WHY A BARGE, NOT THE EXCLUSIVE TOKEN. The exclusive token is for stages
	// that must be serialised against each other and that can afford to DEFER
	// and retry — the link pass and group-algo both re-arm their own timers and
	// lose no work when they yield. This batch has neither property: it has no
	// retry timer (it fires exactly once, at the end of a rebuild, and a skipped
	// run is a permanently missing history point), and taking a token that
	// background stages block on would make a metrics write able to stall real
	// write-side work. The barge is the registration built for precisely this
	// shape — out-of-scheduler work that must never wait but must be SEEN: it
	// records unconditionally, returns immediately, makes background exclusive
	// stages defer around the batch instead of co-residing with it, and (with
	// the busyLocked() clause added alongside this change) stops the debounced
	// FreeOSMemory from firing underneath a walk of the whole corpus graph.
	//
	// `defer release()` so the registration unwinds on every exit path,
	// including the early error returns below.
	defer sched.BargeForeground("analytics:" + group)()

	entry := quality.HealthEntry{
		Timestamp: time.Now().UTC(),
		Group:     group,
	}

	// ── one scan per repo, every metric folded from it ───────────────────────
	totalEntities := 0
	totalOrphans := 0
	totalImports := 0
	goodImports := 0
	totalProduction := 0
	coveredProduction := 0
	totalCycles := 0
	totalFlows := 0
	totalEndpoints := 0
	authUncovered := 0
	scanned := false

	for _, repoPath := range rebuiltPaths {
		sc, err := scanRepoAnalytics(stateDirForRepoPath(repoPath))
		if err != nil {
			continue
		}
		scanned = true

		totalEntities += sc.Entities
		totalOrphans += sc.Orphans
		totalImports += sc.ImportsTotal
		goodImports += sc.ImportsResolved

		totalProduction += sc.TotalProduction
		coveredProduction += sc.CoveredProduction
		totalCycles += sc.Cycles

		totalEndpoints += sc.Endpoints
		totalFlows += sc.Flows
		authUncovered += sc.AuthUncovered
	}

	entry.TotalEntities = totalEntities
	if totalEntities > 0 {
		entry.OrphanRate = 100.0 * float64(totalOrphans) / float64(totalEntities)
	}
	if totalImports > 0 {
		entry.BugRate = 100.0 * float64(totalImports-goodImports) / float64(totalImports)
	}
	entry.HealthScore = quality.ComputeHealthScore(entry.OrphanRate, entry.BugRate)

	entry.TotalFlows = totalFlows
	entry.TotalEndpoints = totalEndpoints

	if scanned && totalProduction > 0 {
		covPct := 100.0 * float64(coveredProduction) / float64(totalProduction)
		entry.CoveragePct = &covPct
	}
	if scanned {
		entry.Cycles = &totalCycles
	}
	entry.AuthUncovered = &authUncovered

	// ── Secrets ──────────────────────────────────────────────────────────────
	// Run a quick secret scan across all repos. Cap per-file size at 256 KB to
	// bound memory; skip on scan error rather than failing the whole snapshot.
	const maxSecretFileSizeBytes = 256 * 1024
	totalSecrets := 0
	for _, repoPath := range rebuiltPaths {
		// Only Findings is consumed here (#6483). The quality snapshot is a
		// single per-run integer and deliberately does not grow a skip count:
		// scan.Skipped exists for the two interactive callers that answer
		// "is this repo clean?" to a human, not for the history series.
		scan, err := secrets.ScanPath(repoPath, maxSecretFileSizeBytes)
		if err != nil {
			// Non-fatal: log and skip.
			fmt.Fprintf(os.Stderr, "grafel: secret scan %s: %v (skipped)\n", repoPath, err)
			continue
		}
		totalSecrets += len(scan.Findings)
	}
	entry.Secrets = &totalSecrets

	return quality.AppendEntry(root, entry)
}

// stateDirForRepoPath returns the daemon state directory for a repo path.
// Delegates to the same helper used by the daemon indexer.
func stateDirForRepoPath(repoPath string) string {
	return daemon.StateDirForRepo(repoPath)
}

// loadGraphFromStateDir is the full-materialise loader. Nothing on the
// analytics path calls it any more (#5954) — it is retained as the escape
// hatch for callers that genuinely need a whole Document, and indirected so
// tests can assert the analytics batch never reaches it.
var loadGraphFromStateDir = func(stateDir string) (*graph.Document, error) {
	return graph.LoadGraphFromDir(stateDir)
}
