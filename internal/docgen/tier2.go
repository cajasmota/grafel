// Package docgen — Tier 2 coherent-slice path (issue #1760).
//
// Tier 2 generates a coherent SLICE of ~5 pages — one capability (the seed
// entity) plus its highest-priority dependents — and validates CROSS-PAGE
// contracts that Tier 0/1 cannot express.
//
// CLI usage:
//
//	archigraph docgen --tier=2 \
//	  --group=<g> \
//	  --seed-entity=<capability-id> \
//	  --max-pages=5
//
// Algorithm:
//  1. Seed entity → traverse 1-hop outbound (CALLS-out, IMPORTS) relationships.
//  2. Rank dependents by PageRank (if available) else by degree.
//  3. Pick top-N by max-pages (default 5, seed counts as page 1).
//  4. Run Tier 1 per-page render on each entity in the slice.
//  5. Run cross-page contract checks via tier2_contracts.go.
//  6. Write N markdown pages + slice-level score.json.
//
// Output layout:
//
//	~/.archigraph/docs/<group>/.tier2-<RFC3339>/
//	    <entity-id>-page.md   — one per page in the slice
//	    score.json            — slice-level Tier 2 score
//
// Cross-page contracts checked:
//   - No flow (mermaid block body) appears in 2+ pages.
//   - Pattern entities mentioned in one page but absent from related pages.
//   - Cross-page anchor links (<entity-id>#<section>) are format-consistent.
//   - Slice-wide mermaid count ≤ budget (default 15, ~3 per page).
//
// Wall-time target: <10 minutes.
package docgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
)

// MermaidBudgetSlice is the maximum total mermaid blocks allowed across the
// entire Tier 2 slice (default: 15, ~3 per page average).
const MermaidBudgetSlice = 15

// OutboundRelKinds are the relationship kinds considered "outbound" for
// dependent selection. CALLS-out and IMPORTS are the primary signals.
var OutboundRelKinds = map[string]bool{
	"CALLS":         true,
	"CALLS-out":     true,
	"IMPORTS":       true,
	"DEPENDS_ON":    true,
	"USES":          true,
}

// Tier2RunOpts contains the resolved inputs for a Tier 2 run.
type Tier2RunOpts struct {
	// Group is the archigraph group name.
	Group string
	// SeedEntityID is the capability entity to use as the slice seed.
	SeedEntityID string
	// MaxPages is the maximum number of pages to generate (inclusive of seed).
	// Default 5.
	MaxPages int
	// MermaidBudget overrides MermaidBudgetSlice for the run.
	MermaidBudget int
	// OutputDir overrides the default ~/.archigraph/docs/<group>/.tier2-<ts>/
	// location. Useful in tests.
	OutputDir string
	// ConcurrencyLimit controls the goroutine pool used per Tier 1 page render.
	// Default 4.
	ConcurrencyLimit int
	// LLMMode is propagated to each Tier 1 RunOpts. Valid values are "" (default),
	// "emit", and "apply". "apply" at Tier 2+ returns an error; use Tier 1 apply
	// per page instead.
	LLMMode string
	// CacheDir overrides the section-level LLM cache directory propagated to Tier 1.
	// Ignored when NoCache is true.
	CacheDir string
	// NoCache disables both cache reads and writes for all Tier 1 sub-runs.
	NoCache bool
}

// Tier2Score is the slice-level scorecard written by Tier 2.
type Tier2Score struct {
	Tier                        int      `json:"tier"`
	WallTimeMS                  int64    `json:"wall_time_ms"`
	PageCount                   int      `json:"page_count"`
	TotalTokenCount             int      `json:"total_token_count"`
	CrossPageLinkCount          int      `json:"cross_page_link_count"`
	CrossPageLinkUnresolved     int      `json:"cross_page_link_unresolved"`
	FlowDuplicationViolations   int      `json:"flow_duplication_violations"`
	PatternLinkViolations       int      `json:"pattern_link_violations"`
	AnchorConsistencyViolations int      `json:"anchor_consistency_violations"`
	SliceMermaidCount           int      `json:"slice_mermaid_count"`
	SliceMermaidBudgetViolations int     `json:"slice_mermaid_budget_violations"`
	SliceEntityIDs              []string `json:"slice_entity_ids"`
	Violations                  []string `json:"violations,omitempty"`
	// LLMMode is set to "emit" when the run was invoked with --llm-mode=emit.
	// Empty string means the default deterministic-stub-only mode.
	LLMMode string `json:"llm_mode,omitempty"`
}

// PageOutput holds the result of a single Tier 1 page render within the slice.
type PageOutput struct {
	EntityID string
	MDPath   string
	MD       string
	Score    Tier1Score
}

// RunTier2 executes a Tier 2 coherent-slice render.
// It returns the output directory path and the slice-level score.
func RunTier2(opts Tier2RunOpts) (outDir string, score Tier2Score, err error) {
	start := time.Now()

	// Tier 2 apply mode is not yet implemented. Emit mode works; for apply, run
	// Tier 1 --llm-mode=apply per page separately.
	if opts.LLMMode == "apply" {
		err = fmt.Errorf(
			"--llm-mode=apply is not yet implemented for --tier=2; " +
				"emit mode works at Tier 2+; use --tier=1 --llm-mode=apply per page instead",
		)
		return
	}
	if opts.LLMMode != "" && opts.LLMMode != "emit" {
		err = validateLLMMode(opts.LLMMode)
		return
	}

	if opts.MaxPages <= 0 {
		opts.MaxPages = 5
	}
	if opts.MermaidBudget <= 0 {
		opts.MermaidBudget = MermaidBudgetSlice
	}
	if opts.ConcurrencyLimit <= 0 {
		opts.ConcurrencyLimit = 4
	}

	// Resolve output directory.
	outDir = opts.OutputDir
	if outDir == "" {
		outDir, err = defaultTier2OutDir(opts.Group)
		if err != nil {
			return
		}
	}
	if mkErr := os.MkdirAll(outDir, 0o755); mkErr != nil {
		err = fmt.Errorf("create tier2 output dir %s: %w", outDir, mkErr)
		return
	}

	// Load entity context for the seed.
	_, seedEntity, _, loadErr := loadEntityContext(opts.Group, opts.SeedEntityID)
	if loadErr != nil {
		err = fmt.Errorf("load seed entity: %w", loadErr)
		return
	}

	// Pick the slice: seed + top dependents.
	sliceIDs, pickErr := pickSliceEntities(opts.Group, opts.SeedEntityID, opts.MaxPages)
	if pickErr != nil {
		err = fmt.Errorf("pick slice: %w", pickErr)
		return
	}
	_ = seedEntity // available for future enrichment

	// Run Tier 1 on each page in the slice.
	pages, renderErr := renderSlicePages(sliceIDs, opts)
	if renderErr != nil {
		err = fmt.Errorf("render slice pages: %w", renderErr)
		return
	}

	// Run cross-page contract checks.
	allViolations := CheckSliceContracts(pages, opts.MermaidBudget)

	// Count violation categories.
	flowDups := countViolationsByKind(allViolations, "flow-duplication")
	patternLinks := countViolationsByKind(allViolations, "pattern-link")
	anchorConsistency := countViolationsByKind(allViolations, "anchor-consistency")
	mermaidBudget := countViolationsByKind(allViolations, "mermaid-budget")

	// Aggregate slice-level metrics.
	totalTokens := 0
	sliceMermaid := 0
	crossPageLinks := 0
	crossPageUnresolved := 0
	for _, p := range pages {
		totalTokens += p.Score.TokenCountEstimate
		sliceMermaid += p.Score.MermaidCount
		crossPageLinks += countCrossPageLinks(p.MD)
		crossPageUnresolved += countUnresolvedCrossPageLinks(p.MD, sliceIDs)
	}

	entityIDs := make([]string, len(pages))
	for i, p := range pages {
		entityIDs[i] = p.EntityID
	}

	score = Tier2Score{
		Tier:                         2,
		WallTimeMS:                   time.Since(start).Milliseconds(),
		PageCount:                    len(pages),
		TotalTokenCount:              totalTokens,
		CrossPageLinkCount:           crossPageLinks,
		CrossPageLinkUnresolved:      crossPageUnresolved,
		FlowDuplicationViolations:    flowDups,
		PatternLinkViolations:        patternLinks,
		AnchorConsistencyViolations:  anchorConsistency,
		SliceMermaidCount:            sliceMermaid,
		SliceMermaidBudgetViolations: mermaidBudget,
		SliceEntityIDs:               entityIDs,
		Violations:                   violationStrings(allViolations),
		LLMMode:                      opts.LLMMode,
	}

	// Write score.json.
	scoreBytes, jErr := json.MarshalIndent(score, "", "  ")
	if jErr != nil {
		err = fmt.Errorf("marshal tier2 score: %w", jErr)
		return
	}
	if wErr := os.WriteFile(filepath.Join(outDir, "score.json"), scoreBytes, 0o644); wErr != nil {
		err = fmt.Errorf("write tier2 score.json: %w", wErr)
		return
	}

	return
}

// ---------------------------------------------------------------------------
// Slice selection
// ---------------------------------------------------------------------------

// entityRecord holds a resolved entity + its outbound degree for ranking.
type entityRecord struct {
	entity   *graph.Entity
	degree   int
	pageRank float64
}

// pickSliceEntities returns up to maxPages entity IDs for the slice:
// the seed entity first, then the highest-priority dependents.
func pickSliceEntities(group, seedID string, maxPages int) ([]string, error) {
	// Load all entities and relationships for the group.
	repoGraphDirs, err := findGroupGraphDirs(group)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]*graph.Entity)
	var allRels []graph.Relationship

	for _, dir := range repoGraphDirs {
		d, loadErr := graph.LoadGraphFromDir(dir)
		if loadErr != nil {
			continue
		}
		for i := range d.Entities {
			e := d.Entities[i]
			byID[e.ID] = &e
		}
		allRels = append(allRels, d.Relationships...)
	}

	// Resolve the seed entity (exact + prefix/suffix match).
	seedEntity := resolveEntity(byID, seedID)
	if seedEntity == nil {
		// Seed not found — return a single-element slice with the seed ID so
		// callers produce an empty page rather than erroring out hard.
		return []string{seedID}, nil
	}

	// Collect outbound neighbours (CALLS-out, IMPORTS, etc.).
	outboundDegree := make(map[string]int)
	for _, rel := range allRels {
		if rel.FromID != seedEntity.ID {
			continue
		}
		if !OutboundRelKinds[rel.Kind] {
			continue
		}
		outboundDegree[rel.ToID]++
	}

	// Rank candidates: by PageRank if available, else by outbound degree.
	type candidate struct {
		id       string
		priority float64
	}
	var candidates []candidate
	for toID := range outboundDegree {
		if toID == seedEntity.ID {
			continue
		}
		e, ok := byID[toID]
		if !ok {
			continue
		}
		var prio float64
		if e.PageRank != nil {
			prio = *e.PageRank
		} else {
			prio = float64(outboundDegree[toID])
		}
		candidates = append(candidates, candidate{id: toID, priority: prio})
	}

	// Sort descending by priority for determinism (secondary: ID string sort).
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		return candidates[i].id < candidates[j].id
	})

	// Build slice: seed first, then top-N dependents.
	result := []string{seedEntity.ID}
	for _, c := range candidates {
		if len(result) >= maxPages {
			break
		}
		result = append(result, c.id)
	}

	return result, nil
}

// resolveEntity finds an entity by exact ID, then by prefix/suffix.
func resolveEntity(byID map[string]*graph.Entity, seedID string) *graph.Entity {
	if e, ok := byID[seedID]; ok {
		return e
	}
	for id, e := range byID {
		if strings.HasPrefix(id, seedID) || strings.HasSuffix(id, seedID) {
			return e
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Slice page rendering
// ---------------------------------------------------------------------------

// renderSlicePages runs Tier 1 on each entity in the slice and returns the
// page outputs. Pages are rendered sequentially to keep wall time predictable
// (Tier 1 already parallelises internally).
func renderSlicePages(entityIDs []string, opts Tier2RunOpts) ([]PageOutput, error) {
	var pages []PageOutput
	for _, eid := range entityIDs {
		t1Opts := Tier1RunOpts{
			Group:            opts.Group,
			SeedEntityID:     eid,
			OutputDir:        opts.OutputDir,
			ConcurrencyLimit: opts.ConcurrencyLimit,
			LLMMode:          opts.LLMMode,
			CacheDir:         opts.CacheDir,
			NoCache:          opts.NoCache,
		}
		mdPath, _, score, err := RunTier1(t1Opts)
		if err != nil {
			// Non-fatal: record an empty page and continue.
			pages = append(pages, PageOutput{EntityID: eid, Score: score})
			continue
		}
		md, readErr := os.ReadFile(mdPath)
		if readErr != nil {
			pages = append(pages, PageOutput{EntityID: eid, MDPath: mdPath, Score: score})
			continue
		}
		pages = append(pages, PageOutput{
			EntityID: eid,
			MDPath:   mdPath,
			MD:       string(md),
			Score:    score,
		})
	}
	return pages, nil
}

// ---------------------------------------------------------------------------
// Cross-page link helpers
// ---------------------------------------------------------------------------

// crossPageLinkRE matches links of the form [text](entity-id#section) —
// the canonical cross-page anchor format: `<entity-id>#<section>`.
// We match anything that contains '#' but not '://' (not a URL).
var crossPageLinkRE = strings.NewReplacer() // see countCrossPageLinks below

// countCrossPageLinks counts cross-page anchor links: [text](target#section).
func countCrossPageLinks(md string) int {
	n := 0
	// Simple parser: find all markdown links and count those with '#' in target.
	i := 0
	for i < len(md) {
		bracket := strings.Index(md[i:], "](")
		if bracket < 0 {
			break
		}
		rest := md[i+bracket+2:]
		paren := strings.Index(rest, ")")
		if paren < 0 {
			break
		}
		target := rest[:paren]
		if strings.Contains(target, "#") && !strings.Contains(target, "://") {
			n++
		}
		i += bracket + 2 + paren + 1
	}
	return n
}

// countUnresolvedCrossPageLinks counts cross-page links whose target entity-id
// is not present in the slice.
func countUnresolvedCrossPageLinks(md string, sliceIDs []string) int {
	sliceSet := make(map[string]bool, len(sliceIDs))
	for _, id := range sliceIDs {
		sliceSet[id] = true
	}

	n := 0
	i := 0
	for i < len(md) {
		bracket := strings.Index(md[i:], "](")
		if bracket < 0 {
			break
		}
		rest := md[i+bracket+2:]
		paren := strings.Index(rest, ")")
		if paren < 0 {
			break
		}
		target := rest[:paren]
		if strings.Contains(target, "#") && !strings.Contains(target, "://") {
			parts := strings.SplitN(target, "#", 2)
			entityID := parts[0]
			if entityID != "" && !sliceSet[entityID] {
				n++
			}
		}
		i += bracket + 2 + paren + 1
	}
	return n
}

// ---------------------------------------------------------------------------
// Violation helpers
// ---------------------------------------------------------------------------

// countViolationsByKind counts violations with a given kind prefix.
func countViolationsByKind(violations []Violation, kind string) int {
	n := 0
	for _, v := range violations {
		if v.Kind == kind {
			n++
		}
	}
	return n
}

// violationStrings extracts the message strings from a slice of Violations.
func violationStrings(vs []Violation) []string {
	if len(vs) == 0 {
		return nil
	}
	msgs := make([]string, len(vs))
	for i, v := range vs {
		msgs[i] = fmt.Sprintf("[%s] %s", v.Kind, v.Message)
	}
	return msgs
}

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

// defaultTier2OutDir returns ~/.archigraph/docs/<group>/.tier2-<ts>/.
func defaultTier2OutDir(group string) (string, error) {
	home, err := tier1HomeDir() // reuse tier1's homeDir resolution
	if err != nil {
		return "", err
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	ts = strings.NewReplacer(":", "-").Replace(ts)
	return filepath.Join(home, "docs", group, ".tier2-"+ts), nil
}
