package cli

// feedback_timeline.go — `archigraph feedback timeline` (#3206).
//
// Stitches the internal rewrite-test signals into a chronological STORY of how
// archigraph helped migrate a backend to a new language with 1:1 parity:
//
//   - feedback-events JSONL (#3204): outcome + phase + capability + library
//   - persona-events JSONL (optional)
//   - mcp_rpc daemon log: per-call query timeline + token/byte cost
//   - git history of the new repo(s): ported code landing over time (milestones)
//
// All four sources are timestamped; they are merged into one chronological
// stream, segmented by phase (planning first), and rendered as timeline.json
// (machine) + timeline.md (narrative). LOCAL ONLY — no network calls.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// unphasedPhase is the bucket for entries with no resolvable phase.
const unphasedPhase = "(unphased)"

// planningPhase is forced to sort first in the rendered narrative (#3206
// "planning-first" requirement) — it is just a normal phase value agents emit.
const planningPhase = "planning"

// timelineEntry is one beat in the merged, chronologically-sorted stream.
type timelineEntry struct {
	TS       string `json:"ts"`    // UTC ISO-8601 instant
	Kind     string `json:"kind"`  // feedback | persona | query | commit
	Phase    string `json:"phase"` // resolved phase (or "(unphased)")
	Group    string `json:"group,omitempty"`
	Summary  string `json:"summary"`             // human-readable beat
	TokenEst int    `json:"token_est,omitempty"` // query payload token estimate
	Outcome  string `json:"outcome,omitempty"`   // feedback outcome
}

// phaseAggregate is the per-phase rollup emitted in timeline.json.
type phaseAggregate struct {
	FirstTS        string         `json:"first_ts"`
	LastTS         string         `json:"last_ts"`
	Queries        int            `json:"queries"`
	TokenEstSum    int            `json:"token_est_sum"`
	FeedbackCounts map[string]int `json:"feedback_counts"`
	Commits        int            `json:"commits"`
}

// timelineDoc is the serialized timeline.json payload.
type timelineDoc struct {
	Since           string                     `json:"since"`
	GeneratedPhases []string                   `json:"generated_phases"`
	Entries         []timelineEntry            `json:"entries"`
	PerPhase        map[string]*phaseAggregate `json:"per_phase"`
}

// newFeedbackTimelineCmd returns `archigraph feedback timeline` (#3206).
func newFeedbackTimelineCmd() *cobra.Command {
	var (
		since        string
		outDir       string
		eventsDir    string
		rpcLog       string
		repos        []string
		phaseTrailer string
	)
	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "Reconstruct a phase-segmented narrative of a migration from feedback + rpc + git (internal test harness)",
		Long: `timeline stitches the internal rewrite-test signals into a chronological
story of how archigraph helped migrate a backend with 1:1 parity:

  - feedback-events JSONL (archigraph_feedback_event): outcome + phase
  - persona-events JSONL (optional)
  - the mcp_rpc daemon log: per-call query timeline + token cost
  - git history of the new repo(s): ported code landing over time

Events are merged chronologically, segmented by phase (a phase literally named
"planning" sorts first), and emitted as timeline.json + timeline.md.

Commit→phase correlation: a commit carrying an "Archigraph-Phase: <phase>"
git trailer is matched exactly; otherwise it (and each rpc query) falls into
the time window of the nearest preceding feedback phase checkpoint.

All data is local; no network calls.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFeedbackTimeline(cmd, timelineOpts{
				since:        since,
				outDir:       outDir,
				eventsDir:    eventsDir,
				rpcLog:       rpcLog,
				repos:        repos,
				phaseTrailer: phaseTrailer,
			})
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "only include events on/after this UTC date (YYYY-MM-DD); default: all")
	cmd.Flags().StringVar(&outDir, "out", "", "output directory (default: ~/.archigraph/feedback/timeline-<timestamp>)")
	cmd.Flags().StringVar(&eventsDir, "events-dir", "", "events directory (default: ~/.archigraph/events)")
	cmd.Flags().StringVar(&rpcLog, "rpc-log", "", "daemon log path for mcp_rpc query timeline (default: ~/.archigraph/logs/daemon.log; skipped if absent)")
	cmd.Flags().StringArrayVar(&repos, "repo", nil, "path to a new repo whose git log supplies commit milestones (repeatable)")
	cmd.Flags().StringVar(&phaseTrailer, "phase-trailer", "Archigraph-Phase", "git trailer key used for exact commit→phase correlation")
	return cmd
}

type timelineOpts struct {
	since        string
	outDir       string
	eventsDir    string
	rpcLog       string
	repos        []string
	phaseTrailer string
}

func runFeedbackTimeline(cmd *cobra.Command, opts timelineOpts) error {
	w := cmd.OutOrStdout()
	errW := cmd.ErrOrStderr()

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("feedback timeline: resolve home: %w", err)
	}
	if opts.eventsDir == "" {
		opts.eventsDir = filepath.Join(home, ".archigraph", "events")
	}
	if opts.outDir == "" {
		opts.outDir = filepath.Join(home, ".archigraph", "feedback",
			"timeline-"+time.Now().UTC().Format("20060102-150405"))
	}
	if opts.rpcLog == "" {
		opts.rpcLog = filepath.Join(home, ".archigraph", "logs", "daemon.log")
	}
	if opts.phaseTrailer == "" {
		opts.phaseTrailer = "Archigraph-Phase"
	}

	// 1. Feedback events (reuse the rollup loader). These carry phase directly
	//    and act as the phase checkpoints for the time-window fallback.
	fbEvents, _, err := loadFeedbackEvents(opts.eventsDir, opts.since)
	if err != nil {
		return err
	}

	var entries []timelineEntry
	// feedbackCheckpoints are (ts, phase) pairs used for the nearest-preceding
	// time-window fallback. Built only from feedback events that name a phase.
	type checkpoint struct {
		t     time.Time
		phase string
	}
	var checkpoints []checkpoint
	for _, e := range fbEvents {
		phase := e.Phase
		entries = append(entries, timelineEntry{
			TS:      e.Timestamp,
			Kind:    "feedback",
			Phase:   phase, // feedback events carry phase directly
			Group:   e.Group,
			Summary: feedbackSummary(e),
			Outcome: e.Outcome,
		})
		if phase != "" {
			if t, ok := parseTS(e.Timestamp); ok {
				checkpoints = append(checkpoints, checkpoint{t: t, phase: phase})
			}
		}
	}

	// 2. Persona events (optional). Group-agnostic; no phase of their own —
	//    they get a phase via the time-window fallback.
	personaEntries, err := loadPersonaTimelineEvents(opts.eventsDir, opts.since)
	if err != nil {
		return err
	}
	entries = append(entries, personaEntries...)

	// 3. mcp_rpc query timeline + cost (optional). Skipped silently if absent.
	queryEntries, err := loadRPCTimelineEvents(opts.rpcLog, opts.since)
	if err != nil {
		fmt.Fprintf(errW, "warning: rpc-log %s: %v (skipping query timeline)\n", opts.rpcLog, err)
	}
	entries = append(entries, queryEntries...)

	// 4. Commit milestones from each --repo (optional, never fatal).
	for _, repo := range opts.repos {
		commitEntries, gerr := loadGitTimelineEvents(repo, opts.since, opts.phaseTrailer)
		if gerr != nil {
			fmt.Fprintf(errW, "warning: --repo %s: %v (skipping its commits)\n", repo, gerr)
			continue
		}
		entries = append(entries, commitEntries...)
	}

	// Sort all entries chronologically by TS (RFC3339 lexical sort is valid
	// for UTC instants; ties broken by kind then summary for determinism).
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].TS != entries[j].TS {
			return entries[i].TS < entries[j].TS
		}
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].Summary < entries[j].Summary
	})

	// Sort the checkpoints chronologically for the nearest-preceding lookup.
	sort.SliceStable(checkpoints, func(i, j int) bool {
		return checkpoints[i].t.Before(checkpoints[j].t)
	})

	// Phase assignment for entries that don't already carry one (commit without
	// a trailer, persona, query): fall into the time window of the nearest
	// preceding feedback phase checkpoint. Entries with no resolvable phase get
	// "(unphased)".
	assignPhase := func(tsStr string) string {
		t, ok := parseTS(tsStr)
		if !ok {
			return unphasedPhase
		}
		phase := unphasedPhase
		for _, cp := range checkpoints {
			if cp.t.After(t) {
				break
			}
			phase = cp.phase // nearest preceding (checkpoints are sorted ascending)
		}
		return phase
	}
	for i := range entries {
		if entries[i].Phase == "" {
			entries[i].Phase = assignPhase(entries[i].TS)
		}
	}

	// Build per-phase aggregates and the phase ordering (first-seen ts, but
	// "planning" forced first).
	doc := buildTimelineDoc(opts.since, entries)

	if err := os.MkdirAll(opts.outDir, 0o755); err != nil {
		return fmt.Errorf("feedback timeline: mkdir %s: %w", opts.outDir, err)
	}
	jsonPath := filepath.Join(opts.outDir, "timeline.json")
	mdPath := filepath.Join(opts.outDir, "timeline.md")

	jb, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(jb, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(mdPath, []byte(renderTimelineMarkdown(doc)), 0o644); err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Fprintf(w, "no timeline events found in %s%s\n", opts.eventsDir, sinceSuffix(opts.since))
		fmt.Fprintf(w, "  %s\n  %s\n", jsonPath, mdPath)
		return nil
	}

	fmt.Fprintf(w, "built timeline of %d events across %d phase(s)%s\n",
		len(entries), len(doc.GeneratedPhases), sinceSuffix(opts.since))
	fmt.Fprintf(w, "  %s\n  %s\n", jsonPath, mdPath)
	return nil
}

// buildTimelineDoc computes per-phase aggregates and the phase ordering.
// Phases are ordered by first-seen timestamp, but a phase literally named
// "planning" is forced FIRST if present (#3206 planning-first requirement).
func buildTimelineDoc(since string, entries []timelineEntry) timelineDoc {
	perPhase := map[string]*phaseAggregate{}
	var firstSeen []string // phases in first-seen-ts order

	for _, e := range entries {
		agg := perPhase[e.Phase]
		if agg == nil {
			agg = &phaseAggregate{
				FirstTS:        e.TS,
				LastTS:         e.TS,
				FeedbackCounts: map[string]int{},
			}
			perPhase[e.Phase] = agg
			firstSeen = append(firstSeen, e.Phase) // entries are pre-sorted by ts
		}
		if e.TS < agg.FirstTS {
			agg.FirstTS = e.TS
		}
		if e.TS > agg.LastTS {
			agg.LastTS = e.TS
		}
		switch e.Kind {
		case "query":
			agg.Queries++
			agg.TokenEstSum += e.TokenEst
		case "feedback":
			agg.FeedbackCounts[e.Outcome]++
		case "commit":
			agg.Commits++
		}
	}

	ordered := orderPhases(firstSeen)
	return timelineDoc{
		Since:           since,
		GeneratedPhases: ordered,
		Entries:         entries,
		PerPhase:        perPhase,
	}
}

// orderPhases keeps first-seen order but forces "planning" to the front.
func orderPhases(firstSeen []string) []string {
	ordered := make([]string, 0, len(firstSeen))
	hasPlanning := false
	for _, p := range firstSeen {
		if p == planningPhase {
			hasPlanning = true
			continue
		}
		ordered = append(ordered, p)
	}
	if hasPlanning {
		ordered = append([]string{planningPhase}, ordered...)
	}
	return ordered
}

func feedbackSummary(e feedbackEventRecord) string {
	var parts []string
	parts = append(parts, e.Outcome)
	if e.Capability != "" {
		parts = append(parts, "cap="+e.Capability)
	}
	if e.Library != "" {
		parts = append(parts, "lib="+e.Library)
	}
	s := strings.Join(parts, " ")
	if e.Note != "" {
		s += ": " + e.Note
	}
	return s
}

// ---- persona events --------------------------------------------------------

// personaTimelineRecord is a lightweight local parser for persona-events JSONL
// (we deliberately avoid importing internal/mcp).
type personaTimelineRecord struct {
	Timestamp     string `json:"ts"`
	Persona       string `json:"persona"`
	EventType     string `json:"event_type"`
	TargetPersona string `json:"target_persona,omitempty"`
}

func loadPersonaTimelineEvents(dir, since string) ([]timelineEntry, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "persona-events-*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var out []timelineEntry
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("feedback timeline: open %s: %w", path, err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var rec personaTimelineRecord
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				continue
			}
			if !afterSince(rec.Timestamp, since) {
				continue
			}
			summary := "persona " + rec.Persona + " " + rec.EventType
			if rec.TargetPersona != "" {
				summary += " -> " + rec.TargetPersona
			}
			out = append(out, timelineEntry{
				TS:      rec.Timestamp,
				Kind:    "persona",
				Summary: summary,
			})
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("feedback timeline: read %s: %w", path, err)
		}
	}
	return out, nil
}

// ---- mcp_rpc daemon log ----------------------------------------------------

// rpcDoneLineRe mirrors the daemon's slog-format mcp_rpc phase=done line. The
// real format (confirmed against cmd/archigraph/bench_capture.go) is, with a
// leading slog "time=" field carrying a per-line timestamp:
//
//	time=2026-05-27T05:33:46.256+05:45 level=INFO msg=mcp_rpc phase=done tool=archigraph_whoami elapsed_ms=1008 wire_bytes=B payload_token_estimate=T repo=/path
//
// We capture the timestamp (for the timeline), the tool, and (optionally) the
// payload_token_estimate + repo for per-query cost. wire_bytes/token fields are
// optional (added by #2828); repo is optional. The daemon DOUBLE-LOGS each line
// consecutively, so we dedup consecutive identical lines (same as the bench
// parser).
var (
	rpcTimeRe  = regexp.MustCompile(`time=(\S+)`)
	rpcDoneRe  = regexp.MustCompile(`msg=mcp_rpc phase=done tool=(\S+) elapsed_ms=(\d+)`)
	rpcTokenRe = regexp.MustCompile(`payload_token_estimate=(\d+)`)
	rpcRepoRe  = regexp.MustCompile(`repo=(\S+)`)
)

func loadRPCTimelineEvents(logPath, since string) ([]timelineEntry, error) {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // absent → skip silently
		}
		return nil, err
	}
	defer f.Close()

	var out []timelineEntry
	var prevLine string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		// Dedup the daemon's consecutive double-log (same scheme as the bench
		// capture parser).
		if line == prevLine {
			prevLine = ""
			continue
		}
		prevLine = line

		m := rpcDoneRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tool := m[1]

		// Extract the per-line timestamp and normalize to UTC RFC3339.
		ts := ""
		if tm := rpcTimeRe.FindStringSubmatch(line); tm != nil {
			if t, ok := parseTS(tm[1]); ok {
				ts = t.UTC().Format(time.RFC3339)
			}
		}
		if ts == "" {
			continue // no usable timestamp → can't place on the timeline
		}
		if !afterSince(ts, since) {
			continue
		}

		tokenEst := 0
		if tk := rpcTokenRe.FindStringSubmatch(line); tk != nil {
			tokenEst, _ = strconv.Atoi(tk[1])
		}
		group := ""
		if rm := rpcRepoRe.FindStringSubmatch(line); rm != nil {
			group = rm[1]
		}

		summary := "query " + tool
		if tokenEst > 0 {
			summary += fmt.Sprintf(" (~%d tok)", tokenEst)
		}
		out = append(out, timelineEntry{
			TS:       ts,
			Kind:     "query",
			Group:    group,
			Summary:  summary,
			TokenEst: tokenEst,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- git commit milestones -------------------------------------------------

// gitLogSep is an unlikely field separator for the --pretty format.
const gitLogSep = "\x1f"

// loadGitTimelineEvents reads `git -C <repo> log` and emits one commit entry per
// commit in the window. The phase trailer (e.g. "Archigraph-Phase: planning")
// gives an EXACT commit→phase correlation when present; otherwise Phase is left
// empty and the caller's time-window fallback assigns it.
//
// Returns an error (warned, non-fatal by the caller) if the path has no .git or
// git fails.
func loadGitTimelineEvents(repo, since, phaseTrailer string) ([]timelineEntry, error) {
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		return nil, fmt.Errorf("no .git directory")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("git not on PATH")
	}

	// %H hash, %aI author date (strict ISO-8601), %s subject, %(trailers...)
	// We read the trailer value for the configured key directly via the
	// %(trailers:key=...) pretty-format, which yields just the value(s).
	format := strings.Join([]string{"%aI", "%h", "%s",
		fmt.Sprintf("%%(trailers:key=%s,valueonly=true)", phaseTrailer)}, gitLogSep)

	args := []string{"-C", repo, "log", "--no-merges", "--pretty=format:" + format + "%x1e"}
	if since != "" {
		// git understands YYYY-MM-DD for --since (local midnight); good enough
		// for the daily window granularity used elsewhere here.
		args = append(args, "--since="+since)
	}

	cmd := exec.Command("git", args...)
	outBytes, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	var out []timelineEntry
	records := strings.Split(string(outBytes), "\x1e")
	for _, rec := range records {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, gitLogSep)
		if len(fields) < 3 {
			continue
		}
		authorDate := strings.TrimSpace(fields[0])
		hash := strings.TrimSpace(fields[1])
		subject := strings.TrimSpace(fields[2])
		phase := ""
		if len(fields) >= 4 {
			phase = strings.TrimSpace(fields[3])
		}

		t, ok := parseTS(authorDate)
		if !ok {
			continue
		}
		ts := t.UTC().Format(time.RFC3339)
		if !afterSince(ts, since) {
			continue
		}
		out = append(out, timelineEntry{
			TS:      ts,
			Kind:    "commit",
			Phase:   phase, // exact correlation via the trailer when present
			Summary: fmt.Sprintf("commit %s: %s", hash, subject),
		})
	}
	return out, nil
}

// ---- shared helpers --------------------------------------------------------

// parseTS parses a timestamp in any of the formats we encounter (RFC3339 with
// or without fractional seconds / numeric zone) and reports success.
func parseTS(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999999Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// afterSince reports whether the RFC3339 ts is on/after the since date
// (YYYY-MM-DD). Empty since → always true. Lexical date compare on the first 10
// chars (same convention as loadFeedbackEvents).
func afterSince(ts, since string) bool {
	if since == "" {
		return true
	}
	if len(ts) >= 10 {
		return ts[:10] >= since
	}
	return true
}

// renderTimelineMarkdown produces the narrative timeline.md: a title + overall
// span + totals, then one "## <phase>" chapter per phase (in GeneratedPhases
// order) with a quantified header line and a chronological bullet list.
func renderTimelineMarkdown(doc timelineDoc) string {
	var b strings.Builder
	b.WriteString("# archigraph migration timeline\n\n")
	if doc.Since != "" {
		fmt.Fprintf(&b, "Events since **%s**. ", doc.Since)
	}

	overallFirst, overallLast := "", ""
	totalQueries, totalTokens, totalCommits := 0, 0, 0
	for _, agg := range doc.PerPhase {
		if overallFirst == "" || agg.FirstTS < overallFirst {
			overallFirst = agg.FirstTS
		}
		if agg.LastTS > overallLast {
			overallLast = agg.LastTS
		}
		totalQueries += agg.Queries
		totalTokens += agg.TokenEstSum
		totalCommits += agg.Commits
	}

	if len(doc.Entries) == 0 {
		b.WriteString("\n_No timeline events found._\n")
		return b.String()
	}

	fmt.Fprintf(&b, "Span: **%s → %s**. %d events, %d queries (~%d tokens), %d commits across %d phase(s).\n\n",
		overallFirst, overallLast, len(doc.Entries), totalQueries, totalTokens, totalCommits, len(doc.GeneratedPhases))

	for _, phase := range doc.GeneratedPhases {
		agg := doc.PerPhase[phase]
		if agg == nil {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", phase)
		fmt.Fprintf(&b, "%s → %s · %d queries (~%d tokens) · %s · %d commits\n\n",
			agg.FirstTS, agg.LastTS, agg.Queries, agg.TokenEstSum,
			outcomeMix(agg.FeedbackCounts), agg.Commits)

		for _, e := range doc.Entries {
			if e.Phase != phase {
				continue
			}
			b.WriteString(renderEntryBullet(e))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// outcomeMix renders the feedback outcome counts in canonical order, e.g.
// "helped=2 wrong=1 milestone=1" — or "no feedback" when empty.
func outcomeMix(counts map[string]int) string {
	var parts []string
	for _, o := range outcomeOrder {
		if n := counts[o]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", o, n))
		}
	}
	// Include any non-canonical outcomes too, sorted for determinism.
	var extra []string
	for o, n := range counts {
		if n > 0 && !isCanonicalOutcome(o) {
			extra = append(extra, fmt.Sprintf("%s=%d", o, n))
		}
	}
	sort.Strings(extra)
	parts = append(parts, extra...)
	if len(parts) == 0 {
		return "no feedback"
	}
	return strings.Join(parts, " ")
}

// renderEntryBullet renders one chronological bullet for a phase chapter.
// Milestone feedback is rendered prominently; commits read as milestones.
func renderEntryBullet(e timelineEntry) string {
	switch e.Kind {
	case "feedback":
		if e.Outcome == "milestone" {
			return fmt.Sprintf("- **%s MILESTONE** — %s\n", e.TS, e.Summary)
		}
		return fmt.Sprintf("- %s feedback %s\n", e.TS, e.Summary)
	case "commit":
		return fmt.Sprintf("- %s %s\n", e.TS, e.Summary)
	case "query":
		return fmt.Sprintf("- %s %s\n", e.TS, e.Summary)
	case "persona":
		return fmt.Sprintf("- %s %s\n", e.TS, e.Summary)
	default:
		return fmt.Sprintf("- %s %s\n", e.TS, e.Summary)
	}
}
