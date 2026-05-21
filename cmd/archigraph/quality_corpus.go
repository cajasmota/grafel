package main

// quality_corpus.go — `archigraph quality bug-rate-corpus <dir>`
//
// Scans a directory for archigraph-indexed repos, computes the composite
// graph-health score for each one, and emits a summary.  The composite
// formula is:
//
//	health = 100 - (orphan_rate_pct * 0.3 + bug_rate_pct * 0.5 + recall_miss_pct * 0.2)
//
// Output formats: text (default), json, csv, markdown  (--format flag).
// CI mode: --fail-below=N exits non-zero when any group's score drops below N.
// Baseline mode: --baseline=N exits non-zero when score drops more than N points
// vs the last recorded measurement.
//
// Historic measurements are appended to the health-history JSONL file so
// the dashboard trend line picks them up automatically.

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/quality"
	"github.com/cajasmota/archigraph/internal/quality/audit"
)

// corpusGroupResult holds the per-group measurement result.
type corpusGroupResult struct {
	// Name is the last path component of the group directory.
	Name string `json:"name"`
	// Path is the absolute path to the group root.
	Path string `json:"path"`
	// OrphanRatePct is the fraction of entities with no inbound edges * 100.
	OrphanRatePct float64 `json:"orphan_rate_pct"`
	// BugRatePct is the fraction of unresolved import-edges * 100.
	BugRatePct float64 `json:"bug_rate_pct"`
	// RecallMissPct is 0 unless a recall measurement was supplied externally.
	RecallMissPct float64 `json:"recall_miss_pct"`
	// Composite is the full decomposed score.
	Composite quality.CompositeResult `json:"composite"`
	// Entities is the total entity count across all repos in the group.
	Entities int `json:"entities"`
	// Repos is the number of repos analysed.
	Repos int `json:"repos"`
	// MeasuredAt is when this measurement was taken.
	MeasuredAt time.Time `json:"measured_at"`
	// Errors lists non-fatal per-repo errors encountered during the audit.
	Errors []string `json:"errors,omitempty"`

	// Top5Improvements are the highest-value actions to improve this group's score.
	Top5Improvements []string `json:"top_5_improvements,omitempty"`

	// PreviousScore is the last recorded health score for this group, or -1
	// when no prior measurement exists.
	PreviousScore float64 `json:"previous_score"`
	// ScoreDelta is Composite.Score - PreviousScore (positive = improvement).
	// Set to 0 when PreviousScore is unavailable.
	ScoreDelta float64 `json:"score_delta"`
}

// runBugRateCorpus is the implementation of `archigraph quality bug-rate-corpus`.
func runBugRateCorpus(argv []string) error {
	fs := flag.NewFlagSet("quality bug-rate-corpus", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text|json|csv|markdown")
	failBelow := fs.Float64("fail-below", 0, "exit non-zero when any group scores below N (0 = disabled)")
	baseline := fs.Float64("baseline", 0, "exit non-zero when score drops more than N points vs last measurement (0 = disabled)")
	slackWebhook := fs.String("slack-webhook", "", "post a summary to this Slack incoming-webhook URL (stub)")
	noHistory := fs.Bool("no-history", false, "skip appending to health-history.jsonl")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: archigraph quality bug-rate-corpus [flags] <dir>")
	}

	root, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	results, err := measureCorpus(root)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("no archigraph-indexed groups found under %s", root)
	}

	// Load previous scores for delta computation.
	layout, layoutErr := daemon.DefaultLayout()
	histRoot := ""
	if layoutErr == nil {
		histRoot = layout.Root
		for i := range results {
			prev := lastHealthScore(histRoot, results[i].Name)
			results[i].PreviousScore = prev
			if prev >= 0 {
				results[i].ScoreDelta = math.Round((results[i].Composite.Score-prev)*10) / 10
			}
		}
	}

	// Persist to health history unless suppressed.
	if !*noHistory && histRoot != "" {
		for _, r := range results {
			entry := quality.HealthEntry{
				Timestamp:     r.MeasuredAt,
				Group:         r.Name,
				TotalEntities: r.Entities,
				OrphanRate:    r.OrphanRatePct,
				BugRate:       r.BugRatePct,
				HealthScore:   r.Composite.Score,
			}
			_ = quality.AppendEntry(histRoot, entry)
		}
	}

	// Slack stub.
	if *slackWebhook != "" {
		postSlackSummary(*slackWebhook, results)
	}

	// Render.
	switch strings.ToLower(*format) {
	case "json":
		if err := renderJSON(results); err != nil {
			return err
		}
	case "csv":
		if err := renderCSV(results); err != nil {
			return err
		}
	case "markdown", "md":
		renderMarkdown(results)
	default:
		renderText(results)
	}

	// CI exit codes.
	var ciFailures []string
	for _, r := range results {
		if *failBelow > 0 && r.Composite.Score < *failBelow {
			ciFailures = append(ciFailures, fmt.Sprintf(
				"%s: score %.1f < threshold %.1f", r.Name, r.Composite.Score, *failBelow,
			))
		}
		if *baseline > 0 && r.PreviousScore >= 0 && r.ScoreDelta < -*baseline {
			ciFailures = append(ciFailures, fmt.Sprintf(
				"%s: score dropped %.1f points (threshold %.1f)", r.Name, -r.ScoreDelta, *baseline,
			))
		}
	}
	if len(ciFailures) > 0 {
		for _, f := range ciFailures {
			fmt.Fprintln(os.Stderr, "FAIL:", f)
		}
		os.Exit(1)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Measurement
// ─────────────────────────────────────────────────────────────────────────────

// measureCorpus walks root one level deep, audits each archigraph-indexed
// directory, and returns one result per group.
func measureCorpus(root string) ([]corpusGroupResult, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", root, err)
	}

	// Check whether root itself is a single indexed group.
	if audit.HasGraph(root) {
		r, mErr := measureGroup(root)
		if mErr != nil {
			return nil, mErr
		}
		return []corpusGroupResult{r}, nil
	}

	var results []corpusGroupResult
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		p := filepath.Join(root, e.Name())
		if !audit.HasGraph(p) {
			continue
		}
		r, mErr := measureGroup(p)
		if mErr != nil {
			// Non-fatal: surface the error but continue.
			results = append(results, corpusGroupResult{
				Name:   e.Name(),
				Path:   p,
				Errors: []string{mErr.Error()},
			})
			continue
		}
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results, nil
}

// measureGroup audits a single archigraph-indexed directory and computes the
// composite score.
func measureGroup(path string) (corpusGroupResult, error) {
	now := time.Now().UTC()
	rep, err := audit.AuditPath(path, false)
	if err != nil {
		return corpusGroupResult{}, fmt.Errorf("audit %s: %w", path, err)
	}

	// Aggregate orphan + entity counts across repos.
	totalEntities := 0
	totalOrphans := 0
	totalImports := 0
	goodImports := 0
	repos := 0
	var allErrs []string
	for _, rr := range rep.Repos {
		if rr == nil {
			continue
		}
		repos++
		totalEntities += rr.Entities
		totalOrphans += rr.Orphans
		totalImports += rr.ImportsTotal
		goodImports += rr.ImportsToIDFormat[audit.ImportFormatHex] +
			rr.ImportsToIDFormat[audit.ImportFormatExtQualified]
		allErrs = append(allErrs, rr.Errors...)
	}

	orphanRatePct := 0.0
	if totalEntities > 0 {
		orphanRatePct = 100.0 * float64(totalOrphans) / float64(totalEntities)
	}
	// Bug rate: fraction of IMPORTS edges that are NOT resolved to a hex ID or
	// ext-qualified reference. These are edges to unresolved targets.
	bugRatePct := 0.0
	if totalImports > 0 {
		bugRatePct = 100.0 * float64(totalImports-goodImports) / float64(totalImports)
	}

	composite := quality.CompositeScoreFromPcts(orphanRatePct, bugRatePct, 0)
	top5 := buildTop5(rep, orphanRatePct, bugRatePct)

	return corpusGroupResult{
		Name:             filepath.Base(path),
		Path:             path,
		OrphanRatePct:    math.Round(orphanRatePct*10) / 10,
		BugRatePct:       math.Round(bugRatePct*10) / 10,
		RecallMissPct:    0,
		Composite:        composite,
		Entities:         totalEntities,
		Repos:            repos,
		MeasuredAt:       now,
		Errors:           allErrs,
		Top5Improvements: top5,
		PreviousScore:    -1,
	}, nil
}

// buildTop5 synthesises up to 5 actionable improvement suggestions for a group.
func buildTop5(rep *audit.Report, orphanPct, bugPct float64) []string {
	var tips []string
	if orphanPct > 20 {
		tips = append(tips, fmt.Sprintf("Reduce orphan rate (%.1f%%) — improve REFERENCES emission for disconnected entities", orphanPct))
	}
	if bugPct > 20 {
		tips = append(tips, fmt.Sprintf("Resolve import edges (%.1f%% unresolved) — fix path-string IMPORTS targets", bugPct))
	}
	// Per-language recommendations from the audit engine.
	for _, rec := range rep.Recommendations {
		if len(tips) >= 5 {
			break
		}
		tips = append(tips, rec.Issue)
	}
	if len(tips) == 0 {
		tips = append(tips, "Graph is healthy — no immediate action required")
	}
	if len(tips) > 5 {
		tips = tips[:5]
	}
	return tips
}

// ─────────────────────────────────────────────────────────────────────────────
// History helpers
// ─────────────────────────────────────────────────────────────────────────────

// lastHealthScore reads the most recent HealthEntry for the named group and
// returns its HealthScore, or -1 when no prior entry exists.
func lastHealthScore(histRoot, group string) float64 {
	entries, err := quality.ReadHistory(histRoot, group, 365)
	if err != nil || len(entries) == 0 {
		return -1
	}
	return entries[len(entries)-1].HealthScore
}

// ─────────────────────────────────────────────────────────────────────────────
// Renderers
// ─────────────────────────────────────────────────────────────────────────────

func renderText(results []corpusGroupResult) {
	for _, r := range results {
		delta := ""
		if r.PreviousScore >= 0 {
			sign := "+"
			if r.ScoreDelta < 0 {
				sign = ""
			}
			delta = fmt.Sprintf("  (prev %.1f, %s%.1f)", r.PreviousScore, sign, r.ScoreDelta)
		}
		fmt.Printf("%-30s  score=%.1f (%s)%s  orphan=%.1f%%  bug=%.1f%%  entities=%d\n",
			r.Name, r.Composite.Score, r.Composite.Grade, delta,
			r.OrphanRatePct, r.BugRatePct, r.Entities)
		for _, tip := range r.Top5Improvements {
			fmt.Printf("  • %s\n", tip)
		}
	}
}

func renderMarkdown(results []corpusGroupResult) {
	fmt.Printf("# Graph Health Report — %s\n\n", time.Now().UTC().Format("2006-01-02"))
	fmt.Println("| Group | Score | Grade | Δ | Orphan% | Bug% | Entities | Repos |")
	fmt.Println("|---|---|---|---|---|---|---|---|")
	for _, r := range results {
		delta := "—"
		if r.PreviousScore >= 0 {
			sign := "+"
			if r.ScoreDelta < 0 {
				sign = ""
			}
			delta = fmt.Sprintf("%s%.1f", sign, r.ScoreDelta)
		}
		fmt.Printf("| %s | %.1f | %s | %s | %.1f%% | %.1f%% | %d | %d |\n",
			r.Name, r.Composite.Score, r.Composite.Grade, delta,
			r.OrphanRatePct, r.BugRatePct, r.Entities, r.Repos)
	}
	fmt.Println()
	for _, r := range results {
		if len(r.Top5Improvements) == 0 {
			continue
		}
		fmt.Printf("## %s — top improvements\n\n", r.Name)
		for _, tip := range r.Top5Improvements {
			fmt.Printf("- %s\n", tip)
		}
		fmt.Println()
	}
}

func renderJSON(results []corpusGroupResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

func renderCSV(results []corpusGroupResult) error {
	w := csv.NewWriter(os.Stdout)
	if err := w.Write([]string{
		"name", "path", "score", "grade",
		"orphan_rate_pct", "bug_rate_pct", "recall_miss_pct",
		"entities", "repos", "previous_score", "score_delta",
		"measured_at",
	}); err != nil {
		return err
	}
	for _, r := range results {
		prevScore := ""
		if r.PreviousScore >= 0 {
			prevScore = fmt.Sprintf("%.1f", r.PreviousScore)
		}
		if err := w.Write([]string{
			r.Name,
			r.Path,
			fmt.Sprintf("%.1f", r.Composite.Score),
			r.Composite.Grade,
			fmt.Sprintf("%.1f", r.OrphanRatePct),
			fmt.Sprintf("%.1f", r.BugRatePct),
			fmt.Sprintf("%.1f", r.RecallMissPct),
			fmt.Sprintf("%d", r.Entities),
			fmt.Sprintf("%d", r.Repos),
			prevScore,
			fmt.Sprintf("%.1f", r.ScoreDelta),
			r.MeasuredAt.Format(time.RFC3339),
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// ─────────────────────────────────────────────────────────────────────────────
// Slack stub
// ─────────────────────────────────────────────────────────────────────────────

// postSlackSummary is a stub. A real implementation would POST a JSON payload
// to the incoming-webhook URL. We print to stderr so the user knows it was
// called.
func postSlackSummary(webhookURL string, results []corpusGroupResult) {
	if webhookURL == "" {
		return
	}
	best := results[0]
	worst := results[0]
	for _, r := range results {
		if r.Composite.Score > best.Composite.Score {
			best = r
		}
		if r.Composite.Score < worst.Composite.Score {
			worst = r
		}
	}
	fmt.Fprintf(os.Stderr, "slack-webhook: would POST to %s — best=%s (%.1f) worst=%s (%.1f)\n",
		webhookURL, best.Name, best.Composite.Score, worst.Name, worst.Composite.Score)
}
