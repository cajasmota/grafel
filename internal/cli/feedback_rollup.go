package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// feedbackEventRecord mirrors mcp.FeedbackEvent for parsing the JSONL written
// by archigraph_feedback_event. Kept local to avoid a cli→mcp import.
type feedbackEventRecord struct {
	Timestamp  string `json:"ts"`
	Group      string `json:"group,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Library    string `json:"library,omitempty"`
	Capability string `json:"capability,omitempty"`
	Outcome    string `json:"outcome"`
	Note       string `json:"note,omitempty"`
}

// outcomeOrder is the canonical column order for rollup tables. "milestone" is
// a neutral narrative beat (#3206) — it renders as a column but buildFixQueue
// deliberately excludes it (only wrong+missing_capability count as negative).
var outcomeOrder = []string{"helped", "partial", "wrong", "missing_capability", "milestone"}

// newFeedbackRollupCmd returns `archigraph feedback rollup`, which aggregates
// the agent-experience feedback JSONL (written by archigraph_feedback_event)
// into an analyzable bundle. Internal testing harness (#3204).
func newFeedbackRollupCmd() *cobra.Command {
	var (
		since     string
		outDir    string
		eventsDir string
	)
	cmd := &cobra.Command{
		Use:   "rollup",
		Short: "Aggregate agent-experience feedback events into a report (internal test harness)",
		Long: `rollup scans the local feedback-events JSONL (written by the
archigraph_feedback_event MCP tool) and produces an analyzable bundle:
outcome distribution per group, per capability, and per library, plus a ranked
"fix queue" of the capabilities and libraries with the most wrong/missing
outcomes.

It is an internal testing aid (e.g. for an end-to-end backend rewrite). All
data is local; no network calls.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFeedbackRollup(cmd, since, outDir, eventsDir)
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "only include events on/after this UTC date (YYYY-MM-DD); default: all")
	cmd.Flags().StringVar(&outDir, "out", "", "output directory (default: ~/.archigraph/feedback/rollup-<timestamp>)")
	cmd.Flags().StringVar(&eventsDir, "events-dir", "", "events directory (default: ~/.archigraph/events)")
	return cmd
}

func runFeedbackRollup(cmd *cobra.Command, since, outDir, eventsDir string) error {
	w := cmd.OutOrStdout()

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("feedback rollup: resolve home: %w", err)
	}
	if eventsDir == "" {
		eventsDir = filepath.Join(home, ".archigraph", "events")
	}
	if outDir == "" {
		outDir = filepath.Join(home, ".archigraph", "feedback",
			"rollup-"+time.Now().UTC().Format("20060102-150405"))
	}

	events, scanned, err := loadFeedbackEvents(eventsDir, since)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Fprintf(w, "no feedback events found in %s%s\n", eventsDir, sinceSuffix(since))
		return nil
	}

	agg := aggregateFeedback(events)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("feedback rollup: mkdir %s: %w", outDir, err)
	}
	jsonPath := filepath.Join(outDir, "rollup.json")
	mdPath := filepath.Join(outDir, "rollup.md")

	jb, err := json.MarshalIndent(agg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(jb, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(mdPath, []byte(renderFeedbackMarkdown(agg, since)), 0o644); err != nil {
		return err
	}

	fmt.Fprintf(w, "rolled up %d feedback events (from %d JSONL files)%s\n",
		agg.TotalEvents, scanned, sinceSuffix(since))
	fmt.Fprintf(w, "  %s\n  %s\n", jsonPath, mdPath)
	return nil
}

func sinceSuffix(since string) string {
	if since == "" {
		return ""
	}
	return " since " + since
}

// loadFeedbackEvents reads feedback-events-*.jsonl from dir, filtering to lines
// whose ts date is >= since (when set). Returns events, files-scanned, error.
func loadFeedbackEvents(dir, since string) ([]feedbackEventRecord, int, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "feedback-events-*.jsonl"))
	if err != nil {
		return nil, 0, err
	}
	sort.Strings(matches)
	var out []feedbackEventRecord
	scanned := 0
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			return nil, scanned, fmt.Errorf("feedback rollup: open %s: %w", path, err)
		}
		scanned++
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var rec feedbackEventRecord
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				continue // skip malformed lines rather than abort the rollup
			}
			if since != "" && rec.Timestamp != "" {
				// ts is RFC3339; lexical date compare on the first 10 chars works.
				if len(rec.Timestamp) >= 10 && rec.Timestamp[:10] < since {
					continue
				}
			}
			out = append(out, rec)
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, scanned, fmt.Errorf("feedback rollup: read %s: %w", path, err)
		}
	}
	return out, scanned, nil
}

// feedbackRollup is the aggregated, serializable result.
type feedbackRollup struct {
	TotalEvents  int                       `json:"total_events"`
	ByOutcome    map[string]int            `json:"by_outcome"`
	ByGroup      map[string]map[string]int `json:"by_group"`
	ByCapability map[string]map[string]int `json:"by_capability"`
	ByLibrary    map[string]map[string]int `json:"by_library"`
	// FixQueue ranks capabilities+libraries by negative outcomes (wrong +
	// missing_capability), highest first — the prioritized work list.
	FixQueue []fixQueueItem `json:"fix_queue"`
}

type fixQueueItem struct {
	Dimension string `json:"dimension"` // "capability" | "library"
	Key       string `json:"key"`
	Negative  int    `json:"negative"` // wrong + missing_capability
	Total     int    `json:"total"`
}

func aggregateFeedback(events []feedbackEventRecord) feedbackRollup {
	r := feedbackRollup{
		ByOutcome:    map[string]int{},
		ByGroup:      map[string]map[string]int{},
		ByCapability: map[string]map[string]int{},
		ByLibrary:    map[string]map[string]int{},
	}
	bump := func(m map[string]map[string]int, key, outcome string) {
		if key == "" {
			key = "(unspecified)"
		}
		if m[key] == nil {
			m[key] = map[string]int{}
		}
		m[key][outcome]++
	}
	for _, e := range events {
		r.TotalEvents++
		r.ByOutcome[e.Outcome]++
		bump(r.ByGroup, e.Group, e.Outcome)
		bump(r.ByCapability, e.Capability, e.Outcome)
		bump(r.ByLibrary, e.Library, e.Outcome)
	}
	r.FixQueue = buildFixQueue(r.ByCapability, "capability")
	r.FixQueue = append(r.FixQueue, buildFixQueue(r.ByLibrary, "library")...)
	sort.SliceStable(r.FixQueue, func(i, j int) bool {
		if r.FixQueue[i].Negative != r.FixQueue[j].Negative {
			return r.FixQueue[i].Negative > r.FixQueue[j].Negative
		}
		return r.FixQueue[i].Key < r.FixQueue[j].Key
	})
	return r
}

func buildFixQueue(m map[string]map[string]int, dim string) []fixQueueItem {
	var out []fixQueueItem
	for key, counts := range m {
		neg := counts["wrong"] + counts["missing_capability"]
		if neg == 0 {
			continue
		}
		total := 0
		for _, n := range counts {
			total += n
		}
		out = append(out, fixQueueItem{Dimension: dim, Key: key, Negative: neg, Total: total})
	}
	return out
}

func renderFeedbackMarkdown(r feedbackRollup, since string) string {
	var b strings.Builder
	b.WriteString("# archigraph feedback rollup\n\n")
	if since != "" {
		fmt.Fprintf(&b, "Events since **%s**. ", since)
	}
	fmt.Fprintf(&b, "Total events: **%d**\n\n", r.TotalEvents)

	b.WriteString("## Outcomes\n\n| Outcome | Count |\n|---|---:|\n")
	for _, o := range outcomeOrder {
		fmt.Fprintf(&b, "| %s | %d |\n", o, r.ByOutcome[o])
	}
	b.WriteString("\n")

	writeMatrix(&b, "By group (old vs new)", r.ByGroup)
	writeMatrix(&b, "By capability", r.ByCapability)
	writeMatrix(&b, "By library / framework", r.ByLibrary)

	b.WriteString("## Fix queue (most wrong/missing first)\n\n")
	if len(r.FixQueue) == 0 {
		b.WriteString("_No wrong or missing-capability outcomes recorded._\n")
		return b.String()
	}
	b.WriteString("| Dimension | Key | Negative (wrong+missing) | Total |\n|---|---|---:|---:|\n")
	for _, it := range r.FixQueue {
		fmt.Fprintf(&b, "| %s | %s | %d | %d |\n", it.Dimension, it.Key, it.Negative, it.Total)
	}
	return b.String()
}

func writeMatrix(b *strings.Builder, title string, m map[string]map[string]int) {
	fmt.Fprintf(b, "## %s\n\n", title)
	if len(m) == 0 {
		b.WriteString("_none_\n\n")
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("| Key |")
	for _, o := range outcomeOrder {
		fmt.Fprintf(b, " %s |", o)
	}
	b.WriteString(" total |\n|---|")
	for range outcomeOrder {
		b.WriteString("---:|")
	}
	b.WriteString("---:|\n")
	for _, k := range keys {
		fmt.Fprintf(b, "| %s |", k)
		total := 0
		for _, o := range outcomeOrder {
			fmt.Fprintf(b, " %d |", m[k][o])
			total += m[k][o]
		}
		// include any non-canonical outcomes in the total
		for o, n := range m[k] {
			if !isCanonicalOutcome(o) {
				total += n
			}
		}
		fmt.Fprintf(b, " %d |\n", total)
	}
	b.WriteString("\n")
}

func isCanonicalOutcome(o string) bool {
	for _, c := range outcomeOrder {
		if c == o {
			return true
		}
	}
	return false
}
