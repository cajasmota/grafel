// Package docgen — Tier 4 full multi-repo group path (issue #1760).
//
// Tier 4 generates a COMPLETE doc set for ALL repos in a multi-repo group
// and enforces CROSS-REPO coherence contracts. It is the terminal stage of
// the deterministic-tier ladder: Tiers 0→1→2→3 must pass before Tier 4.
//
// CLI usage:
//
//	archigraph docgen --tier=4 --group=<g>
//
// Algorithm:
//  1. Load the group config to enumerate all repos.
//  2. Run Tier 3 for each repo CONCURRENTLY (pool size MaxGroupConcurrency).
//  3. Aggregate all pages across all repos.
//  4. Run cross-repo contract checks:
//     a. checkCrossRepoCoverage   — every cross-repo link target resolves.
//     b. checkGroupIndex          — group index.md links to every repo index.
//     c. checkCrossRepoFlowDedup  — same flow not described in 2+ repos.
//  5. Write group-level index.md + score.json.
//
// Output layout:
//
//	~/.archigraph/docs/<group>/.tier4-<ts>/
//	    index.md              # group-level index
//	    score.json            # group-level rollup
//	    <repo-slug>/
//	        index.md          # repo-level index (from Tier 3)
//	        score.json        # repo-level score (from Tier 3)
//	        <entity-id>-page.md
//
// Wall-time target: <60 seconds (deterministic stubs; parallel repo runs).
package docgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cajasmota/archigraph/internal/registry"
)

// MaxGroupConcurrency is the goroutine pool size for concurrent Tier 3 runs.
// Three at a time keeps parallelism meaningful without thrashing I/O.
const MaxGroupConcurrency = 3

// Tier4RunOpts contains the resolved inputs for a Tier 4 run.
type Tier4RunOpts struct {
	// Group is the archigraph group name.
	Group string
	// MaxPages is forwarded to each Tier 3 run per seed. Default 5.
	MaxPages int
	// MermaidBudget is forwarded to each Tier 3 run. Default 0 (uses Tier 3 default).
	MermaidBudget int
	// OutputDir overrides the default ~/.archigraph/docs/<group>/.tier4-<ts>/ root.
	OutputDir string
	// ConcurrencyLimit controls the per-repo Tier 3 internal concurrency.
	// Default 4.
	ConcurrencyLimit int
	// LLMMode is propagated through Tier 3 → Tier 2 → Tier 1. Valid values are
	// "" (default), "emit", and "apply". "apply" at Tier 4 returns an error;
	// use Tier 1 apply per page instead.
	LLMMode string
	// CacheDir overrides the section-level LLM cache directory propagated to all sub-runs.
	// Ignored when NoCache is true.
	CacheDir string
	// NoCache disables both cache reads and writes for all sub-runs.
	NoCache bool
}

// Tier4Score is the group-level scorecard written by Tier 4.
type Tier4Score struct {
	Tier                        int          `json:"tier"`
	WallTimeMS                  int64        `json:"wall_time_ms"`
	Group                       string       `json:"group"`
	RepoCount                   int          `json:"repo_count"`
	TotalPageCount              int          `json:"total_page_count"`
	TotalTokenCount             int          `json:"total_token_count"`
	CrossRepoLinkCount          int          `json:"cross_repo_link_count"`
	CrossRepoLinkUnresolved     int          `json:"cross_repo_link_unresolved"`
	CrossRepoFlowDedupViolations int         `json:"cross_repo_flow_dedup_violations"`
	GroupIndexUnresolved        int          `json:"group_index_unresolved"`
	PerRepoScores               []Tier3Score `json:"per_repo_scores"`
	Violations                  []string     `json:"violations,omitempty"`
	// LLMMode is set to "emit" when the run was invoked with --llm-mode=emit.
	// Empty string means the default deterministic-stub-only mode.
	LLMMode string `json:"llm_mode,omitempty"`
}

// repoTier3Result is the result of a single concurrent Tier 3 invocation.
type repoTier3Result struct {
	slug   string
	outDir string
	score  Tier3Score
	pages  []PageOutput
	err    error
}

// RunTier4 executes a Tier 4 full-group doc-set render.
// Returns the output directory and the group-level score.
func RunTier4(opts Tier4RunOpts) (outDir string, score Tier4Score, err error) {
	start := time.Now()

	// Tier 4 apply mode is not yet implemented. Emit mode works; for apply, run
	// Tier 1 --llm-mode=apply per page separately.
	if opts.LLMMode == "apply" {
		err = fmt.Errorf(
			"--llm-mode=apply is not yet implemented for --tier=4; " +
				"emit mode works at Tier 4; use --tier=1 --llm-mode=apply per page instead",
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

	// Load group config to enumerate repos.
	repos, err := listGroupRepos(opts.Group)
	if err != nil {
		return
	}
	if len(repos) == 0 {
		err = fmt.Errorf("group %q has no repos configured", opts.Group)
		return
	}

	// Resolve output directory: ~/.archigraph/docs/<group>/.tier4-<ts>/
	rootDir := opts.OutputDir
	if rootDir == "" {
		rootDir, err = defaultTier4OutDir(opts.Group)
		if err != nil {
			return
		}
	}
	if mkErr := os.MkdirAll(rootDir, 0o755); mkErr != nil {
		err = fmt.Errorf("create tier4 output dir %s: %w", rootDir, mkErr)
		return
	}
	outDir = rootDir

	// Run Tier 3 per repo concurrently (pool of MaxGroupConcurrency).
	results := runTier3Concurrently(repos, opts, rootDir)

	// Aggregate per-repo results.
	var perRepoScores []Tier3Score
	var allPages []PageOutput            // all pages across all repos, tagged by repo
	repoPageMap := make(map[string][]PageOutput) // slug → pages

	for _, r := range results {
		if r.err != nil {
			// Non-fatal: record partial error in violations; continue aggregation.
			score.Violations = append(score.Violations,
				fmt.Sprintf("[tier3-error][%s] %v", r.slug, r.err))
			// Still include an empty score entry so per_repo_scores is complete.
			perRepoScores = append(perRepoScores, Tier3Score{
				Tier:       3,
				Repo:       r.slug,
				Violations: []string{r.err.Error()},
			})
			continue
		}
		perRepoScores = append(perRepoScores, r.score)
		allPages = append(allPages, r.pages...)
		repoPageMap[r.slug] = r.pages
	}

	// Build repo-slug → page set map for cross-repo contract checks.
	// We need the list of repo slugs that succeeded.
	succeededSlugs := make([]string, 0, len(repoPageMap))
	for slug := range repoPageMap {
		succeededSlugs = append(succeededSlugs, slug)
	}

	// Load cross-repo links for this group (best-effort; missing file is not fatal).
	crossRepoLinks, _ := loadGroupCrossRepoLinks(opts.Group)

	// Run cross-repo contract checks.
	crViolations, linkCount, linkUnresolved := checkCrossRepoCoverage(opts.Group, crossRepoLinks, repoPageMap)
	groupIndexUnresolved := 0
	giViolations := checkGroupIndex(opts.Group, succeededSlugs, rootDir, &groupIndexUnresolved)
	flowViolations, flowDedupCount := checkCrossRepoFlowDedup(repoPageMap)

	// Aggregate violations.
	var allViolations []string
	allViolations = append(allViolations, score.Violations...) // tier3 errors collected above
	allViolations = append(allViolations, crossRepoViolationStrings(crViolations)...)
	allViolations = append(allViolations, crossRepoViolationStrings(giViolations)...)
	allViolations = append(allViolations, crossRepoViolationStrings(flowViolations)...)

	// Count totals.
	totalPages := 0
	totalTokens := 0
	for _, p := range allPages {
		totalPages++
		totalTokens += estimateTokens(p.MD)
	}

	// Write group-level index.md.
	groupIndexPath := filepath.Join(rootDir, "index.md")
	if wErr := writeGroupIndex(opts.Group, succeededSlugs, groupIndexPath); wErr != nil {
		// Non-fatal: record but continue.
		allViolations = append(allViolations, fmt.Sprintf("[group-index-write] %v", wErr))
	}

	score = Tier4Score{
		Tier:                         4,
		WallTimeMS:                   time.Since(start).Milliseconds(),
		Group:                        opts.Group,
		RepoCount:                    len(repos),
		TotalPageCount:               totalPages,
		TotalTokenCount:              totalTokens,
		CrossRepoLinkCount:           linkCount,
		CrossRepoLinkUnresolved:      linkUnresolved,
		CrossRepoFlowDedupViolations: flowDedupCount,
		GroupIndexUnresolved:         groupIndexUnresolved,
		PerRepoScores:                perRepoScores,
		Violations:                   nilIfEmpty(allViolations),
		LLMMode:                      opts.LLMMode,
	}

	// Write group-level score.json.
	scoreBytes, jErr := json.MarshalIndent(score, "", "  ")
	if jErr != nil {
		err = fmt.Errorf("marshal tier4 score: %w", jErr)
		return
	}
	if wErr := os.WriteFile(filepath.Join(rootDir, "score.json"), scoreBytes, 0o644); wErr != nil {
		err = fmt.Errorf("write tier4 score.json: %w", wErr)
		return
	}

	return
}

// ---------------------------------------------------------------------------
// Concurrent Tier 3 execution
// ---------------------------------------------------------------------------

// runTier3Concurrently runs Tier 3 for every repo slug with a bounded goroutine
// pool (MaxGroupConcurrency). Results are returned in the same order as repos.
func runTier3Concurrently(repos []groupRepo, opts Tier4RunOpts, rootDir string) []repoTier3Result {
	type workItem struct {
		idx  int
		repo groupRepo
	}

	results := make([]repoTier3Result, len(repos))
	work := make(chan workItem, len(repos))

	// Fill work channel.
	for i, r := range repos {
		work <- workItem{idx: i, repo: r}
	}
	close(work)

	var wg sync.WaitGroup
	poolSize := MaxGroupConcurrency
	if poolSize > len(repos) {
		poolSize = len(repos)
	}

	for range make([]struct{}, poolSize) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				r := item.repo
				t3Opts := Tier3RunOpts{
					Group:            opts.Group,
					RepoSlug:         r.Slug,
					MaxPages:         opts.MaxPages,
					MermaidBudget:    opts.MermaidBudget,
					OutputDir:        rootDir,
					ConcurrencyLimit: opts.ConcurrencyLimit,
					LLMMode:          opts.LLMMode,
					CacheDir:         opts.CacheDir,
					NoCache:          opts.NoCache,
				}
				repoOutDir, t3Score, t3Err := RunTier3(t3Opts)

				result := repoTier3Result{
					slug:   r.Slug,
					outDir: repoOutDir,
					score:  t3Score,
					err:    t3Err,
				}

				// Reload generated pages for cross-repo contract checks.
				if t3Err == nil {
					result.pages = loadRepoPages(filepath.Join(rootDir, r.Slug))
				}

				results[item.idx] = result
			}
		}()
	}

	wg.Wait()
	return results
}

// loadRepoPages reads all *-page.md files from a repo output directory.
func loadRepoPages(repoDir string) []PageOutput {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil
	}
	var pages []PageOutput
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "-page.md") {
			continue
		}
		fullPath := filepath.Join(repoDir, e.Name())
		md, readErr := os.ReadFile(fullPath)
		if readErr != nil {
			continue
		}
		// Derive entity ID from filename: strip "-page.md" suffix.
		entityID := strings.TrimSuffix(e.Name(), "-page.md")
		pages = append(pages, PageOutput{
			EntityID: entityID,
			MDPath:   fullPath,
			MD:       string(md),
		})
	}
	return pages
}

// ---------------------------------------------------------------------------
// Group config helpers
// ---------------------------------------------------------------------------

// groupRepo is a minimal repo descriptor for Tier 4 enumeration.
// The Slug and Path fields are exported for test inspection via ListGroupReposForTest.
type groupRepo struct {
	Slug string
	Path string
}

// GroupRepoForTest is the exported alias of groupRepo for external test packages.
type GroupRepoForTest = groupRepo

// listGroupRepos loads the group config and returns all repo slugs + paths.
func listGroupRepos(group string) ([]groupRepo, error) {
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("group config not found for %q (run `archigraph wizard`): %w", group, err)
	}

	var cfg struct {
		Repos []struct {
			Slug string `json:"slug"`
			Path string `json:"path"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse group config: %w", err)
	}

	repos := make([]groupRepo, 0, len(cfg.Repos))
	for _, r := range cfg.Repos {
		if r.Slug == "" {
			continue
		}
		repos = append(repos, groupRepo{Slug: r.Slug, Path: r.Path})
	}
	return repos, nil
}

// ---------------------------------------------------------------------------
// Group-level index
// ---------------------------------------------------------------------------

// writeGroupIndex writes a group-level index.md linking to every repo's index.
func writeGroupIndex(group string, repoSlugs []string, indexPath string) error {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<!-- tier4-generated -->\n# %s — Group Documentation Index\n\n", group))
	b.WriteString(fmt.Sprintf("_Generated by archigraph docgen --tier=4. %d repositories._\n\n", len(repoSlugs)))
	b.WriteString("## Repositories\n\n")
	b.WriteString("| Repository | Index |\n")
	b.WriteString("|------------|-------|\n")
	for _, slug := range repoSlugs {
		repoIndexLink := filepath.Join(slug, "index.md")
		b.WriteString(fmt.Sprintf("| `%s` | [index](%s) |\n", slug, repoIndexLink))
	}
	b.WriteString("\n---\n\n")
	b.WriteString("_Score: [score.json](score.json)_\n")
	return os.WriteFile(indexPath, []byte(b.String()), 0o644)
}

// ---------------------------------------------------------------------------
// Cross-repo link loading
// ---------------------------------------------------------------------------

// groupCrossRepoLink is a minimal shape for the group-links.json entries.
type groupCrossRepoLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

// loadGroupCrossRepoLinks loads the cross-repo links for a group (best-effort).
// Missing file → empty slice, nil error (not all groups have cross-repo links yet).
func loadGroupCrossRepoLinks(group string) ([]groupCrossRepoLink, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	linksPath := filepath.Join(home, ".archigraph", "groups", group+"-links.json")
	data, err := os.ReadFile(linksPath)
	if err != nil {
		// File absent is normal for groups without cross-repo wiring.
		return nil, nil
	}

	// Accept both array form and {"links":[...]} form.
	var asArr []groupCrossRepoLink
	if json.Unmarshal(data, &asArr) == nil {
		return asArr, nil
	}
	var asObj struct {
		Links []groupCrossRepoLink `json:"links"`
	}
	if err := json.Unmarshal(data, &asObj); err != nil {
		return nil, fmt.Errorf("parse group links %s: %w", linksPath, err)
	}
	return asObj.Links, nil
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

// nilIfEmpty returns nil when the slice is empty (keeps JSON output clean).
func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

// crossRepoViolationStrings converts CrossRepoViolation slices to strings.
func crossRepoViolationStrings(vs []CrossRepoViolation) []string {
	if len(vs) == 0 {
		return nil
	}
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = fmt.Sprintf("[%s] %s", v.Kind, v.Message)
	}
	return out
}

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

// defaultTier4OutDir returns ~/.archigraph/docs/<group>/.tier4-<ts>/.
func defaultTier4OutDir(group string) (string, error) {
	home, err := tier1HomeDir()
	if err != nil {
		return "", err
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	ts = strings.NewReplacer(":", "-").Replace(ts)
	return filepath.Join(home, "docs", group, ".tier4-"+ts), nil
}

// ---------------------------------------------------------------------------
// Test-exported wrappers
// ---------------------------------------------------------------------------

// ListGroupReposForTest exposes listGroupRepos for unit tests.
func ListGroupReposForTest(group string) ([]groupRepo, error) {
	return listGroupRepos(group)
}

// LoadRepoPagessForTest exposes loadRepoPages for unit tests.
func LoadRepoPagesForTest(repoDir string) []PageOutput {
	return loadRepoPages(repoDir)
}

// WriteGroupIndexForTest exposes writeGroupIndex for unit tests.
func WriteGroupIndexForTest(group string, repoSlugs []string, indexPath string) error {
	return writeGroupIndex(group, repoSlugs, indexPath)
}
