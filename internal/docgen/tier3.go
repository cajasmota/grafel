// Package docgen — Tier 3 full-repo doc set path (issue #1760).
//
// Tier 3 generates a COMPLETE doc set for ONE repo within a multi-repo group.
// It enumerates all "page-worthy" entities (services, packages, modules, top-level
// scopes) in the repo, runs Tier 2 per seed, aggregates the pages into a
// repo-level docs directory, and enforces repo-level contracts:
//
//   - Every page-worthy entity has a home page.
//   - No two pages claim the same entity as primary.
//   - A repo-level index links to every generated page.
//
// CLI usage:
//
//	archigraph docgen --tier=3 \
//	  --group=<g> \
//	  --repo=<repo-slug> \
//	  --output-dir=<path>
//
// Algorithm:
//  1. Read the group config to find the repo by slug.
//  2. Load the repo's graph.
//  3. Enumerate page-worthy entities (PageWorthyKinds).
//  4. Deduplicate: if an entity is already covered by another seed's slice, skip it.
//  5. Cap at MaxSeedsPerRepo (40) with a warning.
//  6. Run Tier 2 per seed; write pages into <outDir>/<repo>/.
//  7. Generate repo-level index.md + score.json.
//  8. Run repo-level contract checks.
//
// Output layout:
//
//	~/.archigraph/docs/<group>/.tier3-<ts>/<repo>/index.md
//	~/.archigraph/docs/<group>/.tier3-<ts>/<repo>/<entity-id>-page.md
//	~/.archigraph/docs/<group>/.tier3-<ts>/<repo>/score.json
//
// Wall-time target: <20 minutes.
package docgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// MaxSeedsPerRepo is the per-repo cap on page-seed count. If more page-worthy
// entities exist than this limit, the extras are skipped and the count is
// recorded in score.json as skipped_below_budget_count.
const MaxSeedsPerRepo = 40

// PageWorthyKinds is the set of entity-kind substrings that qualify an entity
// as "page-worthy" — i.e. worthy of its own documentation page. The check is
// case-insensitive substring matching against entity.Kind.
var PageWorthyKinds = []string{
	"service",
	"module",
	"package",
	"viewset",
	"router",
	"blueprint",
	"handler",
	"controller",
	"gateway",
	"orchestrator",
	"manager",
	"repository",
	"store",
	"client",
	"server",
}

// Tier3RunOpts contains the resolved inputs for a Tier 3 run.
type Tier3RunOpts struct {
	// Group is the archigraph group name.
	Group string
	// RepoSlug is the repo slug within the group (matches registry.Repo.Slug).
	// When empty, the function returns an error listing available slugs.
	RepoSlug string
	// MaxPages controls how many pages each Tier 2 seed slice may generate.
	// Default 5.
	MaxPages int
	// MermaidBudget overrides the per-slice mermaid budget for Tier 2.
	MermaidBudget int
	// OutputDir overrides the default ~/.archigraph/docs/<group>/.tier3-<ts>/ root.
	OutputDir string
	// ConcurrencyLimit is forwarded to Tier 2 for per-slice page rendering.
	ConcurrencyLimit int
	// LLMMode is propagated through Tier 2 → Tier 1. Valid values are ""
	// (default), "emit", and "apply". "apply" at Tier 3+ returns an error;
	// use Tier 1 apply per page instead.
	LLMMode string
	// CacheDir overrides the section-level LLM cache directory propagated to Tier 2/1.
	// Ignored when NoCache is true.
	CacheDir string
	// NoCache disables both cache reads and writes for all sub-runs.
	NoCache bool
}

// Tier3Score is the repo-level scorecard written by Tier 3.
type Tier3Score struct {
	Tier                     int      `json:"tier"`
	WallTimeMS               int64    `json:"wall_time_ms"`
	Repo                     string   `json:"repo"`
	PageCount                int      `json:"page_count"`
	SliceCount               int      `json:"slice_count"`
	TotalTokenCount          int      `json:"total_token_count"`
	MissingCoverageCount     int      `json:"missing_coverage_count"`
	OwnershipConflictCount   int      `json:"ownership_conflict_count"`
	IndexLinkCount           int      `json:"index_link_count"`
	IndexLinkUnresolved      int      `json:"index_link_unresolved"`
	SkippedBelowBudgetCount  int      `json:"skipped_below_budget_count"`
	Violations               []string `json:"violations,omitempty"`
	// LLMMode is set to "emit" when the run was invoked with --llm-mode=emit.
	// Empty string means the default deterministic-stub-only mode.
	LLMMode string `json:"llm_mode,omitempty"`
}

// repoInfo holds the resolved repo slug + graph-state directory.
type repoInfo struct {
	Slug     string
	Path     string
	StateDir string
}

// RunTier3 executes a Tier 3 full-repo doc-set render.
// Returns the output directory and the repo-level score.
func RunTier3(opts Tier3RunOpts) (outDir string, score Tier3Score, err error) {
	start := time.Now()

	// Tier 3 apply mode is not yet implemented. Emit mode works; for apply, run
	// Tier 1 --llm-mode=apply per page separately.
	if opts.LLMMode == "apply" {
		err = fmt.Errorf(
			"--llm-mode=apply is not yet implemented for --tier=3; " +
				"emit mode works at Tier 3+; use --tier=1 --llm-mode=apply per page instead",
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
	if opts.ConcurrencyLimit <= 0 {
		opts.ConcurrencyLimit = 4
	}

	// Resolve the target repo from the group config.
	repo, err := findRepoBySlug(opts.Group, opts.RepoSlug)
	if err != nil {
		return
	}

	// Resolve output directory: <root>/<repo-slug>/
	rootDir := opts.OutputDir
	if rootDir == "" {
		rootDir, err = defaultTier3OutDir(opts.Group)
		if err != nil {
			return
		}
	}
	repoOutDir := filepath.Join(rootDir, repo.Slug)
	if mkErr := os.MkdirAll(repoOutDir, 0o755); mkErr != nil {
		err = fmt.Errorf("create tier3 output dir %s: %w", repoOutDir, mkErr)
		return
	}
	outDir = rootDir

	// Load the repo's graph.
	doc, loadErr := graph.LoadGraphFromDir(repo.StateDir)
	if loadErr != nil {
		err = fmt.Errorf("load graph for repo %q (state dir %s): %w", repo.Slug, repo.StateDir, loadErr)
		return
	}

	// Enumerate page-worthy seeds.
	seeds, skipped := enumerateRepoSeeds(doc, opts.Group)
	skippedCount := skipped

	// Run Tier 2 for each seed, collecting all pages.
	var allPages []PageOutput
	sliceCount := 0

	for _, seedID := range seeds {
		t2Opts := Tier2RunOpts{
			Group:            opts.Group,
			SeedEntityID:     seedID,
			MaxPages:         opts.MaxPages,
			MermaidBudget:    opts.MermaidBudget,
			OutputDir:        repoOutDir,
			ConcurrencyLimit: opts.ConcurrencyLimit,
			LLMMode:          opts.LLMMode,
			CacheDir:         opts.CacheDir,
			NoCache:          opts.NoCache,
		}
		_, t2Score, t2Err := RunTier2(t2Opts)
		if t2Err != nil {
			// Non-fatal: record failure but continue with other seeds.
			continue
		}
		sliceCount++

		// Reload generated pages from the output directory.
		for _, eid := range t2Score.SliceEntityIDs {
			pagePath := filepath.Join(repoOutDir, sanitizeFilename(eid)+"-page.md")
			md, readErr := os.ReadFile(pagePath)
			if readErr != nil {
				allPages = append(allPages, PageOutput{EntityID: eid})
				continue
			}
			allPages = append(allPages, PageOutput{
				EntityID: eid,
				MDPath:   pagePath,
				MD:       string(md),
			})
		}
	}

	// Deduplicate pages by entity ID (keep first occurrence).
	allPages = deduplicatePages(allPages)

	// Count total tokens.
	totalTokens := 0
	for _, p := range allPages {
		totalTokens += estimateTokens(p.MD)
	}

	// Build the page-worthy entity set for contract checks.
	pageWorthyIDs := extractPageWorthyIDs(doc)

	// Generate repo-level index.
	indexPath := filepath.Join(repoOutDir, "index.md")
	indexLinkCount, indexLinkUnresolved, indexErr := writeRepoIndex(repo.Slug, allPages, indexPath)
	if indexErr != nil {
		err = fmt.Errorf("write repo index: %w", indexErr)
		return
	}

	// Run repo-level contract checks.
	repoViolations := CheckRepoContracts(repo.Slug, pageWorthyIDs, allPages, indexPath)

	score = Tier3Score{
		Tier:                    3,
		WallTimeMS:              time.Since(start).Milliseconds(),
		Repo:                    repo.Slug,
		PageCount:               len(allPages),
		SliceCount:              sliceCount,
		TotalTokenCount:         totalTokens,
		MissingCoverageCount:    countViolationsWithPrefix(repoViolations, "repo-coverage"),
		OwnershipConflictCount:  countViolationsWithPrefix(repoViolations, "page-ownership"),
		IndexLinkCount:          indexLinkCount,
		IndexLinkUnresolved:     indexLinkUnresolved,
		SkippedBelowBudgetCount: skippedCount,
		Violations:              repoViolationStrings(repoViolations),
		LLMMode:                 opts.LLMMode,
	}

	// Write score.json.
	scoreBytes, jErr := json.MarshalIndent(score, "", "  ")
	if jErr != nil {
		err = fmt.Errorf("marshal tier3 score: %w", jErr)
		return
	}
	if wErr := os.WriteFile(filepath.Join(repoOutDir, "score.json"), scoreBytes, 0o644); wErr != nil {
		err = fmt.Errorf("write tier3 score.json: %w", wErr)
		return
	}

	return
}

// ---------------------------------------------------------------------------
// Seed enumeration
// ---------------------------------------------------------------------------

// enumerateRepoSeeds returns the list of entity IDs to use as Tier 2 seeds
// for the given repo document, capped at MaxSeedsPerRepo.
// It returns (seedIDs, skippedCount).
func enumerateRepoSeeds(doc *graph.Document, _ string) (seeds []string, skipped int) {
	// Collect page-worthy entities sorted by PageRank desc (then ID for determinism).
	type rankedEntity struct {
		id       string
		pageRank float64
	}

	var candidates []rankedEntity
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if !isPageWorthy(e) {
			continue
		}
		pr := 0.0
		if e.PageRank != nil {
			pr = *e.PageRank
		}
		candidates = append(candidates, rankedEntity{id: e.ID, pageRank: pr})
	}

	// Sort descending by PageRank, then ascending by ID for determinism.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].pageRank != candidates[j].pageRank {
			return candidates[i].pageRank > candidates[j].pageRank
		}
		return candidates[i].id < candidates[j].id
	})

	// Cap at MaxSeedsPerRepo.
	skipped = 0
	if len(candidates) > MaxSeedsPerRepo {
		skipped = len(candidates) - MaxSeedsPerRepo
		candidates = candidates[:MaxSeedsPerRepo]
	}

	seeds = make([]string, len(candidates))
	for i, c := range candidates {
		seeds[i] = c.id
	}
	return
}

// isPageWorthy returns true if the entity kind matches any entry in PageWorthyKinds.
// The check is case-insensitive substring matching.
func isPageWorthy(e *graph.Entity) bool {
	k := strings.ToLower(e.Kind)
	for _, pw := range PageWorthyKinds {
		if strings.Contains(k, pw) {
			return true
		}
	}
	return false
}

// extractPageWorthyIDs returns the set of IDs of all page-worthy entities in doc.
func extractPageWorthyIDs(doc *graph.Document) map[string]bool {
	ids := make(map[string]bool)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if isPageWorthy(e) {
			ids[e.ID] = true
		}
	}
	return ids
}

// ---------------------------------------------------------------------------
// Test-exported wrappers (names ending in ForTest are visible in _test packages
// via external test package imports; they add zero binary overhead in production
// builds because the Go linker dead-strips unreferenced symbols).
// ---------------------------------------------------------------------------

// EnumerateRepoSeedsForTest exposes enumerateRepoSeeds for unit tests.
func EnumerateRepoSeedsForTest(doc *graph.Document, group string) ([]string, int) {
	return enumerateRepoSeeds(doc, group)
}

// IsPageWorthyForTest exposes isPageWorthy for unit tests.
func IsPageWorthyForTest(kind string) bool {
	e := &graph.Entity{Kind: kind}
	return isPageWorthy(e)
}

// DeduplicatePagesForTest exposes deduplicatePages for unit tests.
func DeduplicatePagesForTest(pages []PageOutput) []PageOutput {
	return deduplicatePages(pages)
}

// ---------------------------------------------------------------------------
// Page deduplication
// ---------------------------------------------------------------------------

// deduplicatePages returns a new slice with duplicate entity IDs removed,
// keeping the first occurrence (which will be the highest-priority seed's version).
func deduplicatePages(pages []PageOutput) []PageOutput {
	seen := make(map[string]bool, len(pages))
	out := make([]PageOutput, 0, len(pages))
	for _, p := range pages {
		if seen[p.EntityID] {
			continue
		}
		seen[p.EntityID] = true
		out = append(out, p)
	}
	return out
}

// ---------------------------------------------------------------------------
// Repo-level index
// ---------------------------------------------------------------------------

// writeRepoIndex writes a markdown index.md linking to every generated page.
// Returns (linkCount, unresolvedCount, error).
func writeRepoIndex(repoSlug string, pages []PageOutput, indexPath string) (int, int, error) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<!-- tier3-generated -->\n# %s — Documentation Index\n\n", repoSlug))
	b.WriteString(fmt.Sprintf("_Generated by archigraph docgen --tier=3. %d pages._\n\n", len(pages)))
	b.WriteString("## Pages\n\n")
	b.WriteString("| Entity ID | Page |\n")
	b.WriteString("|-----------|------|\n")

	unresolvedCount := 0
	for _, p := range pages {
		filename := sanitizeFilename(p.EntityID) + "-page.md"
		// Check if the page file actually exists.
		resolved := false
		if p.MDPath != "" {
			if _, statErr := os.Stat(p.MDPath); statErr == nil {
				resolved = true
			}
		} else {
			// Try relative to the index path's directory.
			candidate := filepath.Join(filepath.Dir(indexPath), filename)
			if _, statErr := os.Stat(candidate); statErr == nil {
				resolved = true
			}
		}
		if !resolved {
			unresolvedCount++
		}
		b.WriteString(fmt.Sprintf("| `%s` | [%s](%s) |\n", p.EntityID, p.EntityID, filename))
	}

	b.WriteString("\n---\n\n")
	b.WriteString(fmt.Sprintf("_Score: [score.json](score.json)_\n"))

	if err := os.WriteFile(indexPath, []byte(b.String()), 0o644); err != nil {
		return 0, 0, err
	}
	return len(pages), unresolvedCount, nil
}

// ---------------------------------------------------------------------------
// Repo-level contracts (see tier3_contracts.go for implementations)
// ---------------------------------------------------------------------------

// countViolationsWithPrefix counts repo violations whose Kind has the given prefix.
func countViolationsWithPrefix(vs []RepoViolation, kindPrefix string) int {
	n := 0
	for _, v := range vs {
		if strings.HasPrefix(v.Kind, kindPrefix) {
			n++
		}
	}
	return n
}

// repoViolationStrings extracts message strings from repo violations.
func repoViolationStrings(vs []RepoViolation) []string {
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

// defaultTier3OutDir returns ~/.archigraph/docs/<group>/.tier3-<ts>/.
func defaultTier3OutDir(group string) (string, error) {
	home, err := tier1HomeDir() // reuse homeDir from tier1
	if err != nil {
		return "", err
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	ts = strings.NewReplacer(":", "-").Replace(ts)
	return filepath.Join(home, "docs", group, ".tier3-"+ts), nil
}

// ---------------------------------------------------------------------------
// Group config helpers
// ---------------------------------------------------------------------------

// findRepoBySlug loads the group config and returns the repo with the given slug.
// If slug is empty, it returns an error listing all available slugs.
func findRepoBySlug(group, slug string) (repoInfo, error) {
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		return repoInfo{}, err
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return repoInfo{}, fmt.Errorf("group config not found for %q (run `archigraph wizard`): %w", group, err)
	}

	var cfg struct {
		Repos []struct {
			Slug string `json:"slug"`
			Path string `json:"path"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return repoInfo{}, fmt.Errorf("parse group config: %w", err)
	}

	if slug == "" {
		var available []string
		for _, r := range cfg.Repos {
			available = append(available, r.Slug)
		}
		return repoInfo{}, fmt.Errorf("--repo is required; available repo slugs in group %q: %s",
			group, strings.Join(available, ", "))
	}

	for _, r := range cfg.Repos {
		if r.Slug == slug {
			if r.Path == "" {
				return repoInfo{}, fmt.Errorf("repo %q in group %q has no path configured", slug, group)
			}
			return repoInfo{
				Slug:     r.Slug,
				Path:     r.Path,
				StateDir: daemon.StateDirForRepo(r.Path),
			}, nil
		}
	}

	// Slug not found — list available.
	var available []string
	for _, r := range cfg.Repos {
		available = append(available, r.Slug)
	}
	return repoInfo{}, fmt.Errorf("repo %q not found in group %q; available slugs: %s",
		slug, group, strings.Join(available, ", "))
}
