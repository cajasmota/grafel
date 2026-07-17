package feedback

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Render writes the markdown feedback report to w. Section order is stable.
// If r.IsSuppressed() is true it emits only the suppression notice.
func Render(w io.Writer, r *Report) error {
	if r.IsSuppressed() {
		fmt.Fprintf(w, "# grafel feedback report — suppressed\n\n")
		fmt.Fprintf(w, "Generated: %s\n", r.GeneratedAt.Format(time.RFC3339))
		fmt.Fprintf(w, "Group: %s\n\n", r.GroupName)
		fmt.Fprintf(w, "> **Report suppressed**: total entities = %d (minimum %d required).\n", r.TotalEntities, minEntitiesForReport)
		fmt.Fprintf(w, ">\n")
		fmt.Fprintf(w, "> Small codebases produce statistically unreliable metrics and are more\n")
		fmt.Fprintf(w, "> fingerprinting-prone. Index a larger group and re-run `grafel feedback`.\n")
		return nil
	}

	// Header
	fmt.Fprintf(w, "# grafel feedback report\n\n")
	fmt.Fprintf(w, "Generated: %s\n", r.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "grafel version: %s\n", r.Version)
	langList := strings.Join(r.Languages, ", ")
	if langList == "" {
		langList = "(unknown)"
	}
	fmt.Fprintf(w, "Group profile: %d language(s) (%s), %s entities, %s relationships\n",
		len(r.Languages), langList,
		rangeLabel(r.TotalEntities), rangeLabel(r.TotalRelationships))
	fmt.Fprintf(w, "Confidence: %d%% (%d/%d sanity checks passed)\n\n",
		r.Confidence, countPassed(r.SanityResults), len(r.SanityResults))

	// Section 1 — Extractor Coverage
	fmt.Fprintf(w, "## 1. Extractor Coverage\n\n")
	fmt.Fprintf(w, "### Entities by language\n\n")
	if len(r.EntitiesByLanguage) == 0 {
		fmt.Fprintf(w, "_No language with >= 10 entities found._\n\n")
	} else {
		langs := sortedStringIntKeys(r.EntitiesByLanguage)
		fmt.Fprintf(w, "| Language | Entity count (range) |\n|---|---|\n")
		for _, lang := range langs {
			fmt.Fprintf(w, "| %s | %s |\n", lang, countRangeLabel(r.EntitiesByLanguage[lang]))
		}
		fmt.Fprintf(w, "\n")
	}

	fmt.Fprintf(w, "### Entity kind distribution\n\n")
	if len(r.EntityKindDist) == 0 {
		fmt.Fprintf(w, "_No kind x language combination with >= 10 entities._\n\n")
	} else {
		fmt.Fprintf(w, "| Kind | Language | Count (range) |\n|---|---|---|\n")
		rows := make([]EntityKindLang, len(r.EntityKindDist))
		copy(rows, r.EntityKindDist)
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Language != rows[j].Language {
				return rows[i].Language < rows[j].Language
			}
			return rows[i].Kind < rows[j].Kind
		})
		for _, row := range rows {
			fmt.Fprintf(w, "| %s | %s | %s |\n", row.Kind, row.Language, countRangeLabel(row.Count))
		}
		fmt.Fprintf(w, "\n")
	}

	fmt.Fprintf(w, "### Source-window completeness\n\n")
	fmt.Fprintf(w, "Entities with valid start/end line: **%.1f%%** (%d of %d)\n\n",
		r.SourceWindow.PctComplete,
		r.SourceWindow.TotalWithWindow,
		r.SourceWindow.TotalEntities)

	fmt.Fprintf(w, "### Annotation coverage\n\n")
	if r.AnnotationCoverage.Total > 0 {
		fmt.Fprintf(w, "Framework-annotated entities: **%.1f%%** (%d of %d)\n\n",
			r.AnnotationCoverage.PctAnnotated,
			r.AnnotationCoverage.TotalAnnotated,
			r.AnnotationCoverage.Total)
	} else {
		fmt.Fprintf(w, "_No entities with annotation data._\n\n")
	}

	fmt.Fprintf(w, "### Field extraction rate\n\n")
	if r.FieldExtractionRate.ClassTotal > 0 {
		fmt.Fprintf(w, "Class/Model entities with zero fields: **%.1f%%** (%d total class-like entities)\n\n",
			r.FieldExtractionRate.ZeroFieldsPct,
			r.FieldExtractionRate.ClassTotal)
	} else {
		fmt.Fprintf(w, "_No class or model entities found._\n\n")
	}

	// Section 2 — Orphan Rate
	fmt.Fprintf(w, "## 2. Orphan Rate\n\n")
	fmt.Fprintf(w, "An entity is orphan when it has no outgoing semantic edges (CONTAINS/DECLARES excluded).\n\n")
	if len(r.OrphanByKind) == 0 {
		fmt.Fprintf(w, "_No entity kind with >= 10 entities found._\n\n")
	} else {
		fmt.Fprintf(w, "| Kind | Total | Orphan | Orphan %% |\n|---|---|---|---|\n")
		kinds := sortedKindStatsKeys(r.OrphanByKind)
		for _, kind := range kinds {
			ks := r.OrphanByKind[kind]
			fmt.Fprintf(w, "| %s | %d | %d | %.1f%% |\n", kind, ks.Total, ks.OrphanCount, ks.OrphanPct)
		}
		fmt.Fprintf(w, "\n")

		// Highlight high-orphan kinds.
		fmt.Fprintf(w, "**High-orphan kinds** (> 30%%):\n\n")
		any := false
		for _, kind := range kinds {
			ks := r.OrphanByKind[kind]
			if ks.OrphanPct > 30.0 {
				fmt.Fprintf(w, "- `%s`: %.1f%% orphan rate\n", kind, ks.OrphanPct)
				any = true
			}
		}
		if !any {
			fmt.Fprintf(w, "_None — all kinds with >= 10 entities have orphan rate <= 30%%._\n")
		}
		fmt.Fprintf(w, "\n")

		// Expected/terminal orphans: container-terminal Components and
		// field-leaf terminals are not defects — routed here instead of the
		// defect table above so the raw signal is not silently dropped.
		if len(r.OrphanTerminalByKind) > 0 {
			fmt.Fprintf(w, "**Expected/terminal orphans** (container-terminal by design, not defects):\n\n")
			fmt.Fprintf(w, "| Kind | Total | Terminal orphan | Terminal orphan %% |\n|---|---|---|---|\n")
			for _, kind := range sortedKindStatsKeys(r.OrphanTerminalByKind) {
				tks := r.OrphanTerminalByKind[kind]
				fmt.Fprintf(w, "| %s | %d | %d | %.1f%% |\n", kind, tks.Total, tks.OrphanCount, tks.OrphanPct)
			}
			fmt.Fprintf(w, "\n")
		}
	}

	// Section 3 — Resolution Disposition
	fmt.Fprintf(w, "## 3. Resolution Disposition\n\n")
	if r.ResolutionTotal == 0 {
		fmt.Fprintf(w, "_No relationship resolution data available (no `resolution` property found on edges)._\n\n")
	} else {
		rv := r.Resolution
		fmt.Fprintf(w, "| Disposition | Percentage |\n|---|---|\n")
		fmt.Fprintf(w, "| resolved | %.2f%% |\n", rv.ResolvedPct)
		fmt.Fprintf(w, "| external-known | %.2f%% |\n", rv.ExternalKnownPct)
		fmt.Fprintf(w, "| external-unknown | %.2f%% |\n", rv.ExternalUnknownPct)
		fmt.Fprintf(w, "| bug-extractor | %.2f%% |\n", rv.BugExtractorPct)
		fmt.Fprintf(w, "| bug-resolver | %.2f%% |\n", rv.BugResolverPct)
		fmt.Fprintf(w, "| dynamic | %.2f%% |\n\n", rv.DynamicPct)
		fmt.Fprintf(w, "_Total edges examined: %s_\n\n", countRangeLabel(r.ResolutionTotal))
	}

	// Section 4 — Framework Recognition
	fmt.Fprintf(w, "## 4. Framework Recognition\n\n")
	fmt.Fprintf(w, "### Detector hits\n\n")
	if len(r.FrameworkHits) == 0 {
		fmt.Fprintf(w, "_No framework with >= 10 tagged entities. This may indicate the framework detector did not fire, or this is a vanilla codebase._\n\n")
	} else {
		fmt.Fprintf(w, "| Framework | Entities tagged (range) |\n|---|---|\n")
		fws := sortedStringIntKeys(r.FrameworkHits)
		for _, fw := range fws {
			fmt.Fprintf(w, "| %s | %s |\n", fw, countRangeLabel(r.FrameworkHits[fw]))
		}
		fmt.Fprintf(w, "\n")
	}

	// Section 5 — Cross-Stack Flows (Phase 1 placeholder)
	fmt.Fprintf(w, "## 5. Cross-Stack Flows\n\n")
	fmt.Fprintf(w, "_(not in Phase 1)_\n\n")

	// Section 6 — Docgen Quality (Phase 1 placeholder)
	fmt.Fprintf(w, "## 6. Docgen Quality\n\n")
	fmt.Fprintf(w, "_(not in Phase 1)_\n\n")

	// Section 7 — Sanity Check Details
	fmt.Fprintf(w, "## 7. Sanity Check Details\n\n")
	fmt.Fprintf(w, "| Check | Result | Note |\n|---|---|---|\n")
	for _, sr := range r.SanityResults {
		status := "PASS"
		if !sr.Passed {
			status = "FAIL"
		}
		note := sr.Note
		if note == "" {
			note = "-"
		}
		fmt.Fprintf(w, "| `%s` | %s | %s |\n", sr.Name, status, note)
	}
	fmt.Fprintf(w, "\n")

	// Footer
	fmt.Fprintf(w, "---\n\n")
	fmt.Fprintf(w, "_This report was generated by `grafel feedback`. No source code, file paths,_\n")
	fmt.Fprintf(w, "_or identifier names are included. Entity names are replaced with per-report_\n")
	fmt.Fprintf(w, "_ephemeral 4-hex hashes. Entity counts are shown as ranges. Salt: ephemeral,_\n")
	fmt.Fprintf(w, "_not persisted, not logged._\n")
	return nil
}

// countPassed counts how many sanity results passed.
func countPassed(results []SanityResult) int {
	n := 0
	for _, r := range results {
		if r.Passed {
			n++
		}
	}
	return n
}

// rangeLabel returns a "X-Y" bucket label for a raw count (used in the header).
func rangeLabel(n int) string {
	switch {
	case n < 50:
		return "< 50"
	case n < 1000:
		return fmt.Sprintf("%d-%d", roundDown(n, 100), roundDown(n, 100)+100)
	case n < 10000:
		return fmt.Sprintf("%d-%d", roundDown(n, 500), roundDown(n, 500)+500)
	default:
		return fmt.Sprintf("%d-%d", roundDown(n, 1000), roundDown(n, 1000)+1000)
	}
}

// countRangeLabel returns a range label for per-kind counts in tables.
func countRangeLabel(n int) string {
	switch {
	case n <= 5:
		return "1-5"
	case n <= 20:
		return "6-20"
	case n <= 100:
		return "21-100"
	default:
		return "100+"
	}
}

func roundDown(n, step int) int {
	return (n / step) * step
}

// sortedStringIntKeys returns sorted keys of a map[string]int.
func sortedStringIntKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedKindStatsKeys returns sorted keys of a map[string]KindStats.
func sortedKindStatsKeys(m map[string]KindStats) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
