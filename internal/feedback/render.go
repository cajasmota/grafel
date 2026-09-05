package feedback

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/resolve"
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
		// #6405: this table and the Section-2 orphan table were never
		// comparable — different unit (occurrences vs unique entity IDs),
		// different scope (this one drops language-less entities), different
		// publication floor (kind x language vs kind). Say so rather than let
		// a reader subtract one from the other. Neither counter is changed:
		// unifying the unit alone would leave the other two axes and make a
		// "same units" label false.
		fmt.Fprintf(w, "Counts entity OCCURRENCES (one entity emitted into two documents counts twice) as a bucketed range, and only entities that carry a language — entities with no language are excluded from this table entirely. A row is published when that kind x language pair has >= 10 occurrences. Not comparable with `Total` in Section 2: different unit, different scope, different floor.\n\n")
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

	renderRelKindByLanguage(w, r)

	fmt.Fprintf(w, "### Source-window completeness\n\n")
	// #6827: this metric was labelled "entities with valid start/END line" and
	// has only ever checked `StartLine > 0` — see the derivation comment beside
	// the counter in report.go. The one-sided check is deliberate and stays;
	// the LABEL was the defect, and it produced a real misreading (on #6726 a
	// reader took 89.2% to mean end lines had been checked and found present).
	//
	// The limitation is stated OUT LOUD rather than left implicit: a narrowed
	// metric that does not say it narrowed is how this issue happened. It is
	// emitted here and nowhere else — it is false over every other metric.
	fmt.Fprintf(w, "Counts entities carrying a START LINE (`start_line > 0`) — the anchor `get_source` navigates on. **End lines are NOT examined**: this percentage says nothing about whether any entity carries an end line, and must not be read as span completeness. Counts entity OCCURRENCES (one entity emitted into two documents counts twice), the same unit as the kind x language table above and NOT the unique-entity-ID unit of Section 2 — not comparable with `Total` there.\n\n")
	fmt.Fprintf(w, "Entities with a start line: **%.1f%%** (%d of %d)\n\n",
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
	fmt.Fprintf(w, "An entity is orphan when it has no semantic edge in EITHER direction (CONTAINS/DECLARES excluded, in both directions). The table below counts only orphans of kinds that carry a semantic edge SOMEWHERE in this group; kinds where no entity does are listed separately under **Expected/terminal orphans**, so a kind reading 0 here may still be entirely unwired there. Unwired STORAGE LEAVES (`field`, `column`, `property`) are excluded from the count too — they declare state and nothing else, so they have nothing to link. Where they are listed depends on their kind: under **Terminal-by-subtype leaves** when the kind carries semantic edges elsewhere, and under **Expected/terminal orphans** when it does not, because then the whole kind is terminal.\n\n")
	if len(r.OrphanByKind) == 0 {
		fmt.Fprintf(w, "_No entity kind with >= 10 entities found._\n\n")
	} else {
		// #6405: unit label for the Section-2 tables. It lives here, in prose,
		// and NOT in the pipe-delimited header row below: history.go matches
		// "| Kind | Total | Orphan | Orphan % |" literally to recover per-kind
		// participation from reports already on disk, and that key is OR-ed
		// across every stored report and never expires. Editing that row is a
		// one-way door.
		//
		// Emitted inside this branch, not above it: a report with no
		// qualifying kind has no `Total` column to describe.
		fmt.Fprintf(w, "`Total` counts UNIQUE entity IDs of the kind — an ID collision merges two entities into one — in every language, including entities carrying none. A kind reaches this table when it has >= 10 of them; the Expected/terminal table below additionally requires at least one terminal orphan. That is a different unit, scope and floor from the occurrence ranges in Section 1 — the two tables are not comparable row-for-row.\n\n")
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

		// Expected/terminal orphans: kinds with zero observed semantic
		// participation anywhere in the group. Routed here instead of the
		// defect table above so the raw signal is not silently dropped, and
		// labelled with the ambiguity rather than asserted as healthy — see
		// the derivation comment in report.go (#6346).
		// Terminal-by-subtype leaves (#6599): storage leaves inside a kind
		// that DOES participate. Rendered as a prose list and NOT as a
		// four-column pipe table on purpose: history.go's kindRowRe matches
		// any "| kind | int | int | pct% |" row inside Section 2 and
		// attributes it to whichever of the two known headers it last saw, so
		// a third table here would be read back as defect or terminal rows.
		// The terminal table in particular is authoritative proof a kind never
		// participated, OR-ed across every stored report and never expiring —
		// exactly the kinds listed below would be libelled by it.
		if len(r.OrphanLeafByKind) > 0 {
			fmt.Fprintf(w, "**Terminal-by-subtype leaves** — unwired entities whose subtype is a storage leaf (`field`, `column`, `property`) inside a kind that DOES carry semantic edges elsewhere. A leaf declares state and nothing else, so its only edge is the structural CONTAINS this metric excludes; it is not an edge gap. Excluded from the orphan defect count above, and still counted in that table's `Total`. Same unit, scope and floor as that table — unique entity IDs of the kind, in every language, published only once the kind has >= 10 of them, so a kind under the floor is suppressed here too.\n\n")
			for _, kind := range sortedKindStatsKeys(r.OrphanLeafByKind) {
				lks := r.OrphanLeafByKind[kind]
				fmt.Fprintf(w, "- `%s`: %d of %d (%.1f%%)\n", kind, lks.OrphanCount, lks.Total, lks.OrphanPct)
			}
			fmt.Fprintf(w, "\n")
		}

		if len(r.OrphanTerminalByKind) > 0 {
			fmt.Fprintf(w, "**Expected/terminal orphans** — no entity of these kinds carries a semantic edge in either direction anywhere in the group, so they are terminal by construction as far as the graph can show. Excluded from the orphan defect count above, but each one raises a `kind-carries-semantic-edges` sanity check for triage: a total resolver regression looks identical from the graph alone, so if one of these used to be wired, it is a defect.\n\n")
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
		// Rows are driven by resolve.AllDispositions, not by a hand-written
		// list: before #6836 three of six rows were wired to counters nothing
		// incremented and rendered a permanent 0.00%. Iterating the taxonomy
		// makes that shape impossible — every disposition the classifier can
		// return has a row, and every row is fed by the classifier.
		for _, d := range resolve.AllDispositions {
			fmt.Fprintf(w, "| %s | %.2f%% |\n", d.String(), rv.Pct(d.String()))
		}
		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "_Edges with a target to resolve: %s (edges with an empty target are excluded)_\n\n",
			countRangeLabel(r.ResolutionTotal))
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
// renderRelKindByLanguage renders the (language × relationship kind) matrix
// (#6479). It is REPORT-ONLY: no sanity check reads it and nothing fails on it.
// A gate here would fire on every language that emits no hierarchy edge today
// and would need a suppression list for a third of its inputs on day one; the
// gate is a separate decision, to be taken once this table has been read.
//
// Unlike the four tables around it, no row is dropped for being small. A
// language that emits zero of a kind is small by construction, so a `< 10`
// floor would hide exactly the rows this table exists to publish.
func renderRelKindByLanguage(w io.Writer, r *Report) {
	fmt.Fprintf(w, "### Relationship kinds by language\n\n")
	if len(r.RelKindByLanguage) == 0 && len(r.RelKindUnattributed) == 0 {
		fmt.Fprintf(w, "_No language observed._\n\n")
		return
	}

	fmt.Fprintf(w, "Edge counts keyed on (source-entity language, relationship kind) — the language of the entity the edge is emitted FROM. Every kind is counted, structural ones included. No row or kind is suppressed for being small: a language emitting zero of a kind is what this table is for. **Report-only** — no sanity check reads it and nothing fails on it. The last column names kinds that OTHER observed languages emit and this one does not, with how many of them do; it is a prompt to look, not a verdict, since plenty of languages legitimately have no inheritance.\n\n")
	fmt.Fprintf(w, "| Language | Relationship kinds emitted | Kinds emitted by peer languages but not this one |\n|---|---|---|\n")

	langs := make([]string, 0, len(r.RelKindByLanguage))
	for lang := range r.RelKindByLanguage {
		langs = append(langs, lang)
	}
	sort.Strings(langs)

	// Languages that emit each kind, so "missing" can carry a peer count
	// instead of nagging on a kind exactly one outlier emits (#6377).
	emittersOf := make(map[string]map[string]bool)
	for lang, kinds := range r.RelKindByLanguage {
		for kind, n := range kinds {
			if n <= 0 {
				continue
			}
			if emittersOf[kind] == nil {
				emittersOf[kind] = make(map[string]bool)
			}
			emittersOf[kind][lang] = true
		}
	}

	peerTotal := len(langs) - 1
	for _, lang := range langs {
		kinds := r.RelKindByLanguage[lang]

		emitted := make([]string, 0, len(kinds))
		for kind, n := range kinds {
			if n > 0 {
				emitted = append(emitted, fmt.Sprintf("%s (%s)", kind, countRangeLabel(n)))
			}
		}
		sort.Strings(emitted)
		emittedCol := "_none_"
		if len(emitted) > 0 {
			emittedCol = strings.Join(emitted, ", ")
		}

		var missing []string
		for kind, emitters := range emittersOf {
			if emitters[lang] {
				continue
			}
			// One peer is enough to report. Requiring a majority would
			// silently drop the weakest signals, which are the ones a reader
			// most needs to see.
			peers := len(emitters)
			if peers == 0 {
				continue
			}
			missing = append(missing, fmt.Sprintf("%s (%d/%d peers)", kind, peers, peerTotal))
		}
		sort.Strings(missing)
		missingCol := "—"
		if len(missing) > 0 {
			missingCol = strings.Join(missing, ", ")
		}

		fmt.Fprintf(w, "| %s | %s | %s |\n", lang, emittedCol, missingCol)
	}

	// Edges whose source language could not be determined -- a dangling FromID
	// or a source entity with no Language. Rendered on every table that renders,
	// including when the bucket is EMPTY, so a reader can tell measured-and-zero
	// from never-measured rather than having to assume. (When no language is
	// observed AND nothing is unattributable there is no table at all -- the
	// guard above says so in one line; that is the whole scope of "every".) Dropping these silently would
	// leave the table not summing to the relationship total, with nothing
	// saying so -- the same shape of unnoticed relationship #6479 is about.
	// It is not a language, so it takes no part in the peer arithmetic above.
	unattributed := make([]string, 0, len(r.RelKindUnattributed))
	for kind, n := range r.RelKindUnattributed {
		if n > 0 {
			unattributed = append(unattributed, fmt.Sprintf("%s (%s)", kind, countRangeLabel(n)))
		}
	}
	sort.Strings(unattributed)
	unattributedCol := "_none_"
	if len(unattributed) > 0 {
		unattributedCol = strings.Join(unattributed, ", ")
	}
	fmt.Fprintf(w, "| _unattributed_ | %s | — |\n", unattributedCol)

	fmt.Fprintf(w, "\n_`_unattributed_` counts edges whose source entity is missing from the graph or carries no language. They are reported, not dropped: with them omitted this table would not sum to the relationship total and nothing would say so._\n")
	fmt.Fprintf(w, "\n")
}

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
