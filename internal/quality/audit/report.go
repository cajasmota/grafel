package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// WriteJSON emits the report as indented JSON. Used by --json or whenever the
// output path has a .json suffix.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteMarkdown renders the report as a human-friendly markdown document.
// The structure mirrors the manual analysis: per-repo summary table first,
// then orphan classification per language, then a recommendations punch list.
func (r *Report) WriteMarkdown(w io.Writer) error {
	bw := &errWriter{w: w}
	fmt.Fprintf(bw, "# Orphan Audit Report — %s\n\n", r.AuditedAt.Format("2006-01-02"))

	fmt.Fprintln(bw, "## Per-repo summary")
	fmt.Fprintln(bw, "")
	fmt.Fprintln(bw, "| Repo | Lang(s) | Entities | Orphans (%) | IMPORTS health | REFERENCES/fn | Risk |")
	fmt.Fprintln(bw, "|---|---|---|---|---|---|---|")
	for _, rr := range r.Repos {
		if rr == nil {
			continue
		}
		langs := strings.Join(rr.Languages, ",")
		orphanPct := 0.0
		if rr.Entities > 0 {
			orphanPct = 100.0 * float64(rr.Orphans) / float64(rr.Entities)
		}
		importHealth := importSummary(rr)
		fmt.Fprintf(bw, "| %s | %s | %d | %d (%.1f%%) | %s | %.2f | %d |\n",
			filepath.Base(rr.Path), langs, rr.Entities, rr.Orphans, orphanPct,
			importHealth, rr.ReferencesPerFunction, rr.RiskScore)
	}
	fmt.Fprintln(bw, "")

	fmt.Fprintln(bw, "## Orphan root-cause breakdown (per language)")
	fmt.Fprintln(bw, "")
	fmt.Fprintln(bw, "| Language | import-placeholder | const-no-refs | cross-file | framework-synth | real-bug | misc |")
	fmt.Fprintln(bw, "|---|---|---|---|---|---|---|")
	langs := make([]string, 0, len(r.Aggregate.PerLanguage))
	for k := range r.Aggregate.PerLanguage {
		langs = append(langs, k)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		c := r.Aggregate.PerLanguage[lang].Classification
		fmt.Fprintf(bw, "| %s | %d | %d | %d | %d | %d | %d |\n",
			lang,
			c[CauseImportPlaceholder], c[CauseConstNoReferences],
			c[CauseCrossFileExport], c[CauseFrameworkSynth],
			c[CauseRealConstructBug], c[CauseMisc])
	}
	fmt.Fprintln(bw, "")

	fmt.Fprintln(bw, "## Per-language rollup")
	fmt.Fprintln(bw, "")
	fmt.Fprintln(bw, "| Language | Repos | Entities | Orphan% | Imports hygiene | Refs/fn | Risk |")
	fmt.Fprintln(bw, "|---|---|---|---|---|---|---|")
	for _, lang := range langs {
		lr := r.Aggregate.PerLanguage[lang]
		fmt.Fprintf(bw, "| %s | %d | %d | %.1f%% | %.0f%% | %.2f | %d |\n",
			lang, lr.Repos, lr.Entities,
			100*lr.OrphanRate, 100*lr.ImportsHygiene,
			lr.ReferencesPerFunction, lr.RiskScore)
	}
	fmt.Fprintln(bw, "")

	if len(r.Recommendations) > 0 {
		fmt.Fprintln(bw, "## Top recommendations")
		fmt.Fprintln(bw, "")
		for _, rec := range r.Recommendations {
			fmt.Fprintf(bw, "%d. %s — affects %d repo(s), ~%d entities recoverable\n",
				rec.Priority, rec.Issue, rec.AffectedRepos, rec.RecoverableEntitiesEstimate)
		}
		fmt.Fprintln(bw, "")
	}

	return bw.err
}

// importSummary renders one repo's IMPORTS to_id format mix as a short string
// (e.g. "100% hex" or "12% hex / 65% path"). Designed for the summary table
// cell, so we only surface the top one or two buckets.
func importSummary(r *RepoReport) string {
	if r.ImportsTotal == 0 {
		return "n/a"
	}
	type kv struct {
		f ImportFormat
		c int
	}
	var pairs []kv
	for f, c := range r.ImportsToIDFormat {
		pairs = append(pairs, kv{f, c})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].c > pairs[j].c })
	var parts []string
	for i, p := range pairs {
		if i >= 2 || p.c == 0 {
			break
		}
		pct := 100.0 * float64(p.c) / float64(r.ImportsTotal)
		parts = append(parts, fmt.Sprintf("%.0f%% %s", pct, p.f))
	}
	return strings.Join(parts, " / ")
}

// errWriter accumulates the first write error so render code can stay
// straight-line without checking every Fprintf.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	e.err = err
	return n, err
}
