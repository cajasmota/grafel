// Package audit implements the `archigraph quality audit-orphans` workflow:
// load one (or many) graph.json documents and produce a report describing
// orphan rate, IMPORTS edge hygiene, REFERENCES density, root-cause-classified
// orphan breakdowns and a composite risk score per language.
//
// The tool replaces a stack of ad-hoc jq pipelines that previously took
// 1-2 hours to run by hand. It is purely a read-side analyser: it never
// mutates the graph, never re-indexes, and never touches the network.
package audit

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
)

// ImportFormat classifies the shape of an IMPORTS edge's to_id. The bucket
// distribution per language is the single most actionable signal we surface:
// repos dominated by path-string or "other" imports are the ones where the
// resolver never got to attach the import to a real entity id.
type ImportFormat string

const (
	ImportFormatHex          ImportFormat = "hex"           // resolved entity id (16 lowercase hex)
	ImportFormatExtQualified ImportFormat = "ext_qualified" // ext:<module>:<name>
	ImportFormatExtBare      ImportFormat = "ext_bare"      // ext:<module>
	ImportFormatPathString   ImportFormat = "path_string"   // ./foo or /abs/...
	ImportFormatOther        ImportFormat = "other"         // bare module name without prefix
)

// OrphanCause is the bucket we drop each orphan into. The heuristic logic
// lives in heuristics.go; keeping the enum here makes it easy for downstream
// callers (CI dashboards, JSON consumers) to enumerate.
type OrphanCause string

const (
	CauseImportPlaceholder OrphanCause = "import_placeholder"
	CauseConstNoReferences OrphanCause = "const_no_references"
	CauseCrossFileExport   OrphanCause = "cross_file_export"
	CauseFrameworkSynth    OrphanCause = "framework_synthetic"
	CauseRealConstructBug  OrphanCause = "real_construct_bug"
	CauseMisc              OrphanCause = "misc"
)

// structuralRelKinds are containment / declaration edges that do NOT represent
// semantic connectivity. An entity whose only relationship is one of these is
// still effectively an orphan: it is "contained" by its file/parent but has no
// caller, reference, import, or data-flow edge linking it into the graph.
//
// Issue #1597: the audit previously treated ANY relationship (including the
// ~1980 CONTAINS edges every file emits for its members) as proof a node was
// connected, which reported 0 orphans on graphs that visibly render ~24% of
// nodes as isolated dots. We exclude these structural edges so the orphan count
// reflects real connectivity and matches what the graph canvas renders (the
// containment parent is frequently filtered out of the served payload, leaving
// the node edge-less).
var structuralRelKinds = map[string]bool{
	"CONTAINS": true,
	"DECLARES": true,
}

// RepoReport is the per-repo result of an audit. Aggregate-level numbers are
// produced by combining many of these in Report.Aggregate.
type RepoReport struct {
	Path                  string                         `json:"path"`
	Languages             []string                       `json:"languages"`
	Entities              int                            `json:"entities"`
	Relationships         int                            `json:"relationships"`
	EntitiesByLanguage    map[string]int                 `json:"entities_by_language"`
	TopKinds              []KVCount                      `json:"top_kinds"`
	RelKinds              []KVCount                      `json:"rel_kinds"`
	Orphans               int                            `json:"orphans"`
	OrphanRate            float64                        `json:"orphan_rate"`
	OrphansByLanguage     map[string]int                 `json:"orphans_by_language"`
	TopOrphanKinds        []KVCount                      `json:"top_orphan_kinds"`
	ImportsTotal          int                            `json:"imports_total"`
	ImportsToIDFormat     map[ImportFormat]int           `json:"imports_to_id_format"`
	ImportsByLanguage     map[string]ImportHealth        `json:"imports_by_language"`
	ReferencesTotal       int                            `json:"references_total"`
	ReferencesByLanguage  map[string]int                 `json:"references_by_language"`
	FunctionsByLanguage   map[string]int                 `json:"functions_by_language"`
	ReferencesPerFunction float64                        `json:"references_per_function"`
	CallsTotal            int                            `json:"calls_total"`
	CallsByLanguage       map[string]int                 `json:"calls_by_language"`
	OrphanClassification  map[OrphanCause]int            `json:"orphan_classification"`
	ClassificationByLang  map[string]map[OrphanCause]int `json:"orphan_classification_by_language"`
	RiskScore             int                            `json:"risk_score"`
	Errors                []string                       `json:"errors,omitempty"`
}

// ImportHealth summarises IMPORTS edge to_id quality for a single language.
type ImportHealth struct {
	Total        int                  `json:"total"`
	ByFormat     map[ImportFormat]int `json:"by_format"`
	HygieneScore float64              `json:"hygiene_score"` // 0..1 ; share of edges in {hex, ext_qualified}
}

// KVCount is a sortable (key, count) pair used for top-N tables.
type KVCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// Report is the full audit output across one or more repos.
type Report struct {
	Version         int              `json:"version"`
	AuditedAt       time.Time        `json:"audited_at"`
	Repos           []*RepoReport    `json:"repos"`
	Aggregate       AggregateReport  `json:"aggregate"`
	Recommendations []Recommendation `json:"recommendations"`
}

// AggregateReport rolls the per-repo numbers up into per-language buckets so a
// reader can answer "which languages should we invest extractor work in?" in
// one glance.
type AggregateReport struct {
	PerLanguage map[string]LanguageRollup `json:"per_language"`
}

// LanguageRollup is the cross-corpus snapshot per language.
type LanguageRollup struct {
	Repos                 int                  `json:"repos"`
	Entities              int                  `json:"entities"`
	Orphans               int                  `json:"orphans"`
	OrphanRate            float64              `json:"orphan_rate"`
	ImportsTotal          int                  `json:"imports_total"`
	ImportsByFormat       map[ImportFormat]int `json:"imports_by_format"`
	ImportsHygiene        float64              `json:"imports_hygiene"`
	References            int                  `json:"references"`
	Functions             int                  `json:"functions"`
	ReferencesPerFunction float64              `json:"references_per_function"`
	Classification        map[OrphanCause]int  `json:"classification"`
	RiskScore             int                  `json:"risk_score"`
}

// Recommendation is one actionable bullet point synthesised from the
// aggregate numbers. The list is ordered by priority (1 = highest).
type Recommendation struct {
	Priority                    int    `json:"priority"`
	Issue                       string `json:"issue"`
	AffectedRepos               int    `json:"affected_repos"`
	RecoverableEntitiesEstimate int    `json:"recoverable_entities_estimate"`
}

// AuditPath dispatches based on whether path is a single repo (contains
// .archigraph/graph.json) or a directory holding many such repos.
//
// corpus=true forces directory mode regardless of layout, which matches the
// --corpus flag wired by the CLI.
func AuditPath(path string, corpus bool) (*Report, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", abs)
	}

	rep := &Report{Version: 1, AuditedAt: time.Now().UTC()}

	if !corpus && hasGraphJSON(abs) {
		rr, err := auditRepo(abs)
		if err != nil {
			return nil, err
		}
		rep.Repos = append(rep.Repos, rr)
	} else {
		paths, err := findRepos(abs)
		if err != nil {
			return nil, err
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("no .archigraph/graph.json found under %s", abs)
		}
		rep.Repos = auditMany(paths)
	}
	rep.Aggregate = aggregate(rep.Repos)
	rep.Recommendations = recommend(rep.Aggregate)
	return rep, nil
}

// HasGraph returns true if dir has any indexable graph (graph.fb or
// graph.json) in its .archigraph state directory. Exported so corpus
// scanners (e.g. the bug-rate-corpus command) can test directories without
// calling the full AuditPath pipeline.
func HasGraph(dir string) bool {
	return hasGraphJSON(dir)
}

// hasGraph returns true if dir has any indexable graph (graph.fb or
// graph.json) in its .archigraph state directory.
// Renamed from hasGraphJSON for ADR-0016 flip-day (#808).
func hasGraphJSON(dir string) bool {
	stateDir := daemon.StateDirForRepo(dir)
	if _, err := os.Stat(filepath.Join(stateDir, "graph.fb")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(stateDir, "graph.json")); err == nil {
		return true
	}
	return false
}

// findRepos walks one directory level deep looking for subdirectories that
// each contain .archigraph/graph.json. We deliberately do NOT recurse beyond
// depth 1 — corpora are flat by convention and recursive walks on huge
// fixture trees are wasteful.
func findRepos(root string) ([]string, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		p := filepath.Join(root, e.Name())
		if hasGraphJSON(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// auditMany runs auditRepo on each path with a small worker pool (4) so a
// 25-repo corpus completes well under a minute. Each goroutine owns its own
// json.Decoder; the shared mutex only guards the output slice.
func auditMany(paths []string) []*RepoReport {
	out := make([]*RepoReport, len(paths))
	const workers = 4
	var wg sync.WaitGroup
	jobs := make(chan int)
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for idx := range jobs {
				rr, err := auditRepo(paths[idx])
				if err != nil {
					rr = &RepoReport{Path: paths[idx], Errors: []string{err.Error()}}
				}
				out[idx] = rr
			}
		}()
	}
	for i := range paths {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out
}

// auditRepo loads the graph for one repo (graph.fb preferred, graph.json
// fallback — ADR-0016 flip-day, issue #808) and computes every metric
// in RepoReport.
func auditRepo(repoPath string) (*RepoReport, error) {
	stateDir := daemon.StateDirForRepo(repoPath)
	rr := &RepoReport{
		Path:                 repoPath,
		EntitiesByLanguage:   map[string]int{},
		OrphansByLanguage:    map[string]int{},
		ImportsToIDFormat:    map[ImportFormat]int{},
		ImportsByLanguage:    map[string]ImportHealth{},
		ReferencesByLanguage: map[string]int{},
		FunctionsByLanguage:  map[string]int{},
		CallsByLanguage:      map[string]int{},
		OrphanClassification: map[OrphanCause]int{},
		ClassificationByLang: map[string]map[OrphanCause]int{},
	}

	doc, err := graph.LoadGraphFromDir(stateDir)
	if err != nil {
		return nil, err
	}

	rr.Entities = len(doc.Entities)
	rr.Relationships = len(doc.Relationships)

	// Pass 1: index entities by id and tally languages + kind histograms.
	entByID := make(map[string]*graph.Entity, len(doc.Entities))
	kindCounts := map[string]int{}
	langSet := map[string]struct{}{}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		entByID[e.ID] = e
		lang := normalizeLang(e.Language)
		if lang != "" {
			langSet[lang] = struct{}{}
			rr.EntitiesByLanguage[lang]++
		}
		key := e.Kind
		if e.Subtype != "" {
			key = e.Kind + "/" + e.Subtype
		}
		kindCounts[key]++
		if isFunctionLike(e) {
			rr.FunctionsByLanguage[lang]++
		}
	}
	rr.TopKinds = topN(kindCounts, 15)

	// Pass 2: tally relationship kinds, IMPORTS hygiene, REFERENCES density.
	relKinds := map[string]int{}
	touched := make(map[string]struct{}, len(doc.Entities))
	imHealth := map[string]*ImportHealth{}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		k := strings.ToUpper(r.Kind)
		relKinds[k]++
		// Only non-structural edges count toward connectivity. A node linked
		// solely by CONTAINS/DECLARES is treated as an orphan (Issue #1597).
		if !structuralRelKinds[k] {
			touched[r.FromID] = struct{}{}
			touched[r.ToID] = struct{}{}
		}
		lang := relLanguage(r, entByID)
		switch k {
		case "IMPORTS":
			rr.ImportsTotal++
			f := classifyImportToID(r.ToID)
			rr.ImportsToIDFormat[f]++
			ih, ok := imHealth[lang]
			if !ok {
				ih = &ImportHealth{ByFormat: map[ImportFormat]int{}}
				imHealth[lang] = ih
			}
			ih.Total++
			ih.ByFormat[f]++
		case "REFERENCES":
			rr.ReferencesTotal++
			rr.ReferencesByLanguage[lang]++
		case "CALLS":
			rr.CallsTotal++
			rr.CallsByLanguage[lang]++
		}
	}
	rr.RelKinds = topN(relKinds, 20)
	for lang, ih := range imHealth {
		hex := ih.ByFormat[ImportFormatHex]
		extq := ih.ByFormat[ImportFormatExtQualified]
		if ih.Total > 0 {
			ih.HygieneScore = float64(hex+extq) / float64(ih.Total)
		}
		rr.ImportsByLanguage[lang] = *ih
	}

	// Pass 3: orphan detection + heuristic classification.
	orphanKinds := map[string]int{}
	for id, e := range entByID {
		if _, ok := touched[id]; ok {
			continue
		}
		rr.Orphans++
		lang := normalizeLang(e.Language)
		rr.OrphansByLanguage[lang]++
		key := e.Kind
		if e.Subtype != "" {
			key = e.Kind + "/" + e.Subtype
		}
		orphanKinds[key]++

		cause := ClassifyOrphan(e)
		rr.OrphanClassification[cause]++
		if rr.ClassificationByLang[lang] == nil {
			rr.ClassificationByLang[lang] = map[OrphanCause]int{}
		}
		rr.ClassificationByLang[lang][cause]++
	}
	rr.TopOrphanKinds = topN(orphanKinds, 10)
	if rr.Entities > 0 {
		rr.OrphanRate = float64(rr.Orphans) / float64(rr.Entities)
	}

	// REFERENCES/function ratio: total across all languages.
	totalFuncs := 0
	for _, n := range rr.FunctionsByLanguage {
		totalFuncs += n
	}
	if totalFuncs > 0 {
		rr.ReferencesPerFunction = float64(rr.ReferencesTotal) / float64(totalFuncs)
	}

	// Risk score: composite, see riskScore() for the formula.
	rr.RiskScore = riskScore(rr)

	// Final language list (sorted for determinism).
	for lang := range langSet {
		rr.Languages = append(rr.Languages, lang)
	}
	sort.Strings(rr.Languages)

	return rr, nil
}

// relLanguage returns the canonical language for a relationship. We prefer
// the explicit `language` property the extractor sets on most edges; if it's
// missing, fall back to the source entity's language so a CALLS edge whose
// resolver dropped the property still counts toward a real bucket.
func relLanguage(r *graph.Relationship, entByID map[string]*graph.Entity) string {
	if r.Properties != nil {
		if v := r.Properties["language"]; v != "" {
			return normalizeLang(v)
		}
	}
	if e, ok := entByID[r.FromID]; ok {
		return normalizeLang(e.Language)
	}
	return ""
}

// normalizeLang collapses extractor variants ("typescript"/"tsx") to a single
// bucket so per-language aggregates aren't fragmented.
func normalizeLang(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "typescriptreact", "tsx":
		return "typescript"
	case "javascriptreact", "jsx":
		return "javascript"
	}
	return s
}

// isFunctionLike returns true for entities the extractor treats as a
// callable construct. Function/Method/Operation are the three kind tokens
// the production schema uses; we tolerate case variants.
func isFunctionLike(e *graph.Entity) bool {
	k := strings.ToLower(e.Kind)
	if strings.Contains(k, "function") || strings.Contains(k, "method") {
		return true
	}
	if strings.Contains(k, "operation") {
		// Operation/* covers many ts/js function-like subtypes.
		st := strings.ToLower(e.Subtype)
		if st == "function" || st == "method" || strings.HasPrefix(st, "arrow") {
			return true
		}
	}
	return false
}

// classifyImportToID buckets an IMPORTS edge's to_id by shape. Anything that
// doesn't look like an entity id (16 lowercase hex) or an "ext:" placeholder
// is either a path string or some other raw token: both are bugs in
// extractor or resolver and should be surfaced.
func classifyImportToID(toID string) ImportFormat {
	if isHexID(toID) {
		return ImportFormatHex
	}
	if strings.HasPrefix(toID, "ext:") {
		// ext:react:useState  -> qualified (3 segments)
		// ext:antd            -> bare (2 segments)
		parts := strings.SplitN(toID, ":", 3)
		if len(parts) == 3 && parts[2] != "" {
			return ImportFormatExtQualified
		}
		return ImportFormatExtBare
	}
	if strings.HasPrefix(toID, "./") || strings.HasPrefix(toID, "../") || strings.HasPrefix(toID, "/") {
		return ImportFormatPathString
	}
	return ImportFormatOther
}

// isHexID returns true if s is a 16-char lowercase hex string — the canonical
// shape of an entity id (graph.EntityID).
func isHexID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// topN sorts a (key,count) map descending by count and returns the first n.
// Ties are broken alphabetically so output is deterministic across runs.
func topN(m map[string]int, n int) []KVCount {
	out := make([]KVCount, 0, len(m))
	for k, v := range m {
		out = append(out, KVCount{Key: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// riskScore is a 0..100 composite of the three signals that drove the manual
// audit's recommendations:
//
//   - orphan rate (lower = healthier)
//   - imports hygiene (share of resolved or ext-qualified imports)
//   - references density (REFERENCES per function — capped at 1.0)
//
// Equal weighting; we round to int because false precision past the unit
// digit invites readers to over-interpret deltas between 64 and 67.
func riskScore(r *RepoReport) int {
	orphanHealth := 1.0 - r.OrphanRate
	if orphanHealth < 0 {
		orphanHealth = 0
	}
	importHealth := 1.0
	if r.ImportsTotal > 0 {
		hex := r.ImportsToIDFormat[ImportFormatHex]
		extq := r.ImportsToIDFormat[ImportFormatExtQualified]
		importHealth = float64(hex+extq) / float64(r.ImportsTotal)
	}
	refHealth := math.Min(1.0, r.ReferencesPerFunction)
	score := 100.0 * (orphanHealth + importHealth + refHealth) / 3.0
	return int(math.Round(score))
}

// aggregate folds per-repo numbers into the cross-corpus per-language view.
func aggregate(repos []*RepoReport) AggregateReport {
	per := map[string]*LanguageRollup{}
	get := func(lang string) *LanguageRollup {
		lr, ok := per[lang]
		if !ok {
			lr = &LanguageRollup{
				ImportsByFormat: map[ImportFormat]int{},
				Classification:  map[OrphanCause]int{},
			}
			per[lang] = lr
		}
		return lr
	}
	repoLangs := map[string]map[string]struct{}{}
	for _, r := range repos {
		if r == nil {
			continue
		}
		for lang, n := range r.EntitiesByLanguage {
			lr := get(lang)
			lr.Entities += n
			set, ok := repoLangs[lang]
			if !ok {
				set = map[string]struct{}{}
				repoLangs[lang] = set
			}
			set[r.Path] = struct{}{}
			lr.Repos = len(set)
		}
		for lang, n := range r.OrphansByLanguage {
			get(lang).Orphans += n
		}
		for lang, ih := range r.ImportsByLanguage {
			lr := get(lang)
			lr.ImportsTotal += ih.Total
			for f, c := range ih.ByFormat {
				lr.ImportsByFormat[f] += c
			}
		}
		for lang, n := range r.ReferencesByLanguage {
			get(lang).References += n
		}
		for lang, n := range r.FunctionsByLanguage {
			get(lang).Functions += n
		}
		for lang, m := range r.ClassificationByLang {
			lr := get(lang)
			for cause, c := range m {
				lr.Classification[cause] += c
			}
		}
	}
	out := AggregateReport{PerLanguage: map[string]LanguageRollup{}}
	for lang, lr := range per {
		if lr.Entities > 0 {
			lr.OrphanRate = float64(lr.Orphans) / float64(lr.Entities)
		}
		if lr.ImportsTotal > 0 {
			hex := lr.ImportsByFormat[ImportFormatHex] + lr.ImportsByFormat[ImportFormatExtQualified]
			lr.ImportsHygiene = float64(hex) / float64(lr.ImportsTotal)
		}
		if lr.Functions > 0 {
			lr.ReferencesPerFunction = float64(lr.References) / float64(lr.Functions)
		}
		// Composite risk per language using the same formula as per-repo.
		orphanHealth := 1.0 - lr.OrphanRate
		refHealth := math.Min(1.0, lr.ReferencesPerFunction)
		importHealth := 1.0
		if lr.ImportsTotal > 0 {
			importHealth = lr.ImportsHygiene
		}
		lr.RiskScore = int(math.Round(100.0 * (orphanHealth + importHealth + refHealth) / 3.0))
		out.PerLanguage[lang] = *lr
	}
	return out
}

// recommend converts the aggregate snapshot into a small priority-ordered
// punch list. The rules below intentionally mirror the manual analysis we
// are automating: REFERENCES missing + IMPORTS path-strings dominate the
// recoverable orphan population on every multi-language corpus we've seen.
func recommend(a AggregateReport) []Recommendation {
	var recs []Recommendation
	type entry struct {
		lang  string
		score float64 // higher = worse
		rec   Recommendation
	}
	var bag []entry
	for lang, lr := range a.PerLanguage {
		if lr.Functions > 100 && lr.ReferencesPerFunction < 0.5 {
			estimate := lr.Functions * 2 // conservative: ~2 refs/function is the floor we see in healthy corpora
			bag = append(bag, entry{
				lang:  lang,
				score: 1.0 - lr.ReferencesPerFunction,
				rec: Recommendation{
					Issue:                       fmt.Sprintf("%s: REFERENCES emission missing (%.2f refs/function)", lang, lr.ReferencesPerFunction),
					AffectedRepos:               lr.Repos,
					RecoverableEntitiesEstimate: estimate,
				},
			})
		}
		if lr.ImportsTotal > 50 {
			path := lr.ImportsByFormat[ImportFormatPathString]
			other := lr.ImportsByFormat[ImportFormatOther]
			bad := path + other
			if float64(bad)/float64(lr.ImportsTotal) > 0.10 {
				bag = append(bag, entry{
					lang:  lang,
					score: float64(bad) / float64(lr.ImportsTotal),
					rec: Recommendation{
						Issue:                       fmt.Sprintf("%s: IMPORTS to_id stored as path string / unqualified (%d / %d)", lang, bad, lr.ImportsTotal),
						AffectedRepos:               lr.Repos,
						RecoverableEntitiesEstimate: bad,
					},
				})
			}
		}
	}
	sort.Slice(bag, func(i, j int) bool {
		if bag[i].score != bag[j].score {
			return bag[i].score > bag[j].score
		}
		return bag[i].lang < bag[j].lang
	})
	for i := range bag {
		bag[i].rec.Priority = i + 1
		recs = append(recs, bag[i].rec)
	}
	return recs
}
