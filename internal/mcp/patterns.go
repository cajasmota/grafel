package mcp

// patterns.go — MCP handler for grafel_patterns (ADR-0018, PR γ).
//
// Implements action=query, action=record (PR β), and the γ lifecycle actions:
// refine, apply, reject, promote.
//
// Storage delegates entirely to internal/agentpatterns/. No duplicate logic.

import (
	"context"
	"fmt"
	"github.com/cajasmota/grafel/internal/agentpatterns"
	"github.com/cajasmota/grafel/internal/types"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Compile-time checks that we use the right RelationshipKind constants.
var (
	_ = types.RelationshipKindExemplar
	_ = types.RelationshipKindConflictsWith
)

// ---------------------------------------------------------------------------
// Concurrent-write guard for patterns.json
// ---------------------------------------------------------------------------

// patternsMu serialises concurrent writes to the patterns store. Because the
// daemon may handle multiple MCP calls at the same time, Load+Upsert+Save
// must be atomic. A single server-wide mutex is sufficient for β (one group
// at a time; cross-group isolation is trivial).
var patternsMu sync.Mutex

// ---------------------------------------------------------------------------
// Valid categories (from ADR-0018)
// ---------------------------------------------------------------------------

var validCategories = map[agentpatterns.Category]bool{
	agentpatterns.CategoryCode:         true,
	agentpatterns.CategoryProcess:      true,
	agentpatterns.CategoryTeam:         true,
	agentpatterns.CategoryTooling:      true,
	agentpatterns.CategoryArchitecture: true,
}

// ---------------------------------------------------------------------------
// Main dispatcher — handlePatterns
// ---------------------------------------------------------------------------

func (s *Server) handlePatterns(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	action, err := req.RequireString("action")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	switch action {
	case "query":
		return s.handlePatternsQuery(ctx, req)
	case "record":
		return s.handlePatternsRecord(ctx, req)
	case "refine":
		return s.handlePatternsRefine(ctx, req)
	case "apply":
		return s.handlePatternsApply(ctx, req)
	case "reject":
		return s.handlePatternsReject(ctx, req)
	case "promote":
		return s.handlePatternsPromote(ctx, req)
	case "get":
		return s.handlePatternsGet(ctx, req)
	default:
		return mcpapi.NewToolResultError(fmt.Sprintf(
			"unknown action %q (allowed: query, record, refine, apply, reject, promote, get)", action,
		)), nil
	}
}

// ---------------------------------------------------------------------------
// patternsDir — per-group storage directory
// ---------------------------------------------------------------------------

// patternsDir returns the <group>/.grafel/ directory for pattern storage.
// We follow the same convention as enrichment-resolutions.json: groups store
// their side-car files inside the group's first repo's .grafel dir, OR we
// use the group-level default path under ~/.grafel/groups/.
//
// For β we resolve the path the same way save_finding resolves memory_dir:
// registry MemoryDir is the group-scoped dir; absent that, default path.
// Patterns are stored at <patternsDir>/patterns.json.
func patternsDir(groupName string, lg *LoadedGroup) string {
	// Prefer registry-configured memory dir (same directory family).
	if lg.MemoryDir != "" {
		return lg.MemoryDir
	}
	return defaultPatternsDir(groupName)
}

// defaultPatternsDir returns ~/.grafel/groups/<group>-patterns/.
func defaultPatternsDir(group string) string {
	// Prefer $HOME so tests using t.Setenv("HOME", tmpDir) work on Windows
	// where os.UserHomeDir() reads USERPROFILE and ignores HOME.
	home := os.Getenv("HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return ""
		}
	}
	return filepath.Join(home, ".grafel", "groups", group+"-patterns")
}

// ---------------------------------------------------------------------------
// action=query
// ---------------------------------------------------------------------------

func (s *Server) handlePatternsQuery(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	text := argString(req, "text", "")
	if text == "" {
		return mcpapi.NewToolResultError("text is required for action=query"), nil
	}
	category := agentpatterns.Category(argString(req, "category", ""))
	includeCandidates := argBool(req, "include_candidates", false)
	includePrivate := argBool(req, "include_private", false)
	limit := argInt(req, "limit", 10)
	if limit <= 0 {
		limit = 10
	}

	// Resolve group.
	groupName, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}

	// Explicit scope override (optional).
	explicitScope := parseScopeArg(req)

	// Caller-context scope from CWD.
	cwdScope := deriveCWDScope(s.inferCWD(req), lg)

	// Effective scope for matching: explicit overrides CWD.
	queryScope := explicitScope
	if queryScope == nil {
		queryScope = &cwdScope
	}

	// Load patterns.
	dir := patternsDir(groupName, lg)
	patterns, err := agentpatterns.Load(dir)
	if err != nil {
		return mcpapi.NewToolResultError("load patterns: " + err.Error()), nil
	}

	// Filter + score.
	type candidate struct {
		p          agentpatterns.Pattern
		bm25       float64
		specific   int     // count of non-empty scope fields
		recency    float64 // 1/(1+days/30)
		finalScore float64
	}

	now := agentpatterns.NowUnix()
	var cands []candidate
	for _, p := range patterns {
		// Candidate filter.
		if p.IsCandidate && !includeCandidates {
			continue
		}
		// Category filter.
		if category != "" && p.Category != category {
			continue
		}
		// Scope match.
		if !scopeMatches(p.Scope, queryScope) {
			continue
		}
		// BM25 over trigger text.
		score := bm25Score(text, p.Trigger.NaturalLanguage, p.Trigger.Keywords)
		// Specificity.
		spec := scopeSpecificity(p.Scope)
		// Recency.
		rec := recencyScore(p.LastApplied, now)
		// Final score: BM25 weighted by confidence*recency (secondary).
		// Primary sort: specificity. Secondary: confidence*recency. Tertiary: BM25.
		final := float64(spec)*1000 + p.Confidence*rec*100 + score
		cands = append(cands, candidate{p: p, bm25: score, specific: spec, recency: rec, finalScore: final})
	}

	// Sort descending.
	sort.SliceStable(cands, func(i, j int) bool {
		return cands[i].finalScore > cands[j].finalScore
	})

	// Apply limit.
	if len(cands) > limit {
		cands = cands[:limit]
	}

	// Serialise results.
	out := make([]map[string]any, 0, len(cands))
	for _, c := range cands {
		row := patternToMap(c.p, includePrivate)
		row["_rank_score"] = c.finalScore
		out = append(out, row)
	}
	return jsonResult(map[string]any{
		"patterns": out,
		"count":    len(out),
	}), nil
}

// ---------------------------------------------------------------------------
// action=record
// ---------------------------------------------------------------------------

func (s *Server) handlePatternsRecord(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	// --- Required args ---
	triggerRaw := argObject(req, "trigger")
	if triggerRaw == nil {
		return mcpapi.NewToolResultError("trigger is required for action=record"), nil
	}
	trigger := parseTrigger(triggerRaw)
	if trigger.NaturalLanguage == "" {
		return mcpapi.NewToolResultError("trigger.natural_language is required"), nil
	}

	steps := argStringSliceFromAny(req, "steps")
	if len(steps) == 0 {
		return mcpapi.NewToolResultError("steps is required and must have at least one entry"), nil
	}

	exemplars := argStringSliceFromAny(req, "exemplars")
	if len(exemplars) == 0 {
		return mcpapi.NewToolResultError("exemplars is required (minimum 1 entity id)"), nil
	}

	categoryStr := argString(req, "category", "")
	if categoryStr == "" {
		return mcpapi.NewToolResultError("category is required"), nil
	}
	category := agentpatterns.Category(categoryStr)
	if !validCategories[category] {
		return mcpapi.NewToolResultError(fmt.Sprintf(
			"invalid category %q (allowed: code, process, team, tooling, architecture)", categoryStr,
		)), nil
	}

	// --- Optional args ---
	antiPatternsRaw := argArrayOfObjects(req, "anti_patterns")
	antiPatterns := parseAntiPatterns(antiPatternsRaw)

	asCandidate := argBool(req, "as_candidate", false)
	proposerSubagent := argString(req, "proposer_subagent", "")
	documentationURL := argString(req, "documentation_url", "")

	// Resolve group.
	groupName, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}

	// Scope: explicit override or auto-derive from exemplars.
	explicitScope := parseScopeArg(req)
	var scope agentpatterns.Scope
	if explicitScope != nil {
		scope = *explicitScope
	} else {
		scope = deriveScopeFromExemplars(exemplars, lg)
	}

	dir := patternsDir(groupName, lg)

	patternsMu.Lock()
	defer patternsMu.Unlock()

	patterns, err := agentpatterns.Load(dir)
	if err != nil {
		return mcpapi.NewToolResultError("load patterns: " + err.Error()), nil
	}

	// Build new pattern.
	p := agentpatterns.New(groupName, trigger, category)
	p.Steps = steps
	p.AntiPatterns = antiPatterns
	p.Scope = scope
	p.Exemplars = exemplars
	p.IsCandidate = asCandidate
	p.DocumentationURL = documentationURL
	if asCandidate {
		p.ConvergenceCount = 1 // first proposal; each merge increments this
	}
	if proposerSubagent != "" {
		p.ProposerSubagents = []string{proposerSubagent}
	}

	// Convergence detection for candidates.
	convergenceCount := 0
	mergedIntoID := ""
	if asCandidate {
		if existing, merged := tryMergeCandidate(patterns, p, proposerSubagent); existing != nil {
			// Merged into an existing candidate.
			patterns = agentpatterns.Upsert(patterns, *existing)
			mergedIntoID = existing.ID
			convergenceCount = existing.ConvergenceCount
			if err := agentpatterns.Save(dir, patterns); err != nil {
				return mcpapi.NewToolResultError("save patterns: " + err.Error()), nil
			}
			resp := map[string]any{
				"id":                mergedIntoID,
				"merged":            true,
				"convergence_count": convergenceCount,
			}
			_ = merged
			return jsonResult(resp), nil
		}
	}

	// Detect conflicts with existing patterns covering same scope + entity_kinds.
	conflicts := detectConflicts(patterns, p)

	// Upsert (insert or replace by ID).
	patterns = agentpatterns.Upsert(patterns, *p)

	if err := agentpatterns.Save(dir, patterns); err != nil {
		return mcpapi.NewToolResultError("save patterns: " + err.Error()), nil
	}

	// Emit graph edges (EXEMPLAR, TOUCHES, ANTI_EXEMPLAR) — recorded in the
	// response for audit; actual graph write is out of scope until the daemon
	// graph-write API is available.
	edges := buildPatternEdges(p)

	resp := map[string]any{
		"id":                p.ID,
		"merged":            false,
		"convergence_count": convergenceCount,
		"edges_emitted":     edges,
	}
	if len(conflicts) > 0 {
		resp["conflicts"] = conflicts
	}
	return jsonResult(resp), nil
}

// ---------------------------------------------------------------------------
// Helpers: parsing
// ---------------------------------------------------------------------------

// argObject extracts an object argument as map[string]any.
func argObject(req mcpapi.CallToolRequest, key string) map[string]any {
	args := req.GetArguments()
	v, ok := args[key]
	if !ok {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// argArrayOfObjects extracts a []map[string]any argument.
func argArrayOfObjects(req mcpapi.CallToolRequest, key string) []map[string]any {
	args := req.GetArguments()
	v, ok := args[key]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []map[string]any:
		return t
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// argStringSliceFromAny handles both []string and []any inputs for string arrays.
func argStringSliceFromAny(req mcpapi.CallToolRequest, key string) []string {
	args := req.GetArguments()
	v, ok := args[key]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	}
	return nil
}

// parseTrigger converts a raw map to Trigger.
func parseTrigger(m map[string]any) agentpatterns.Trigger {
	t := agentpatterns.Trigger{}
	if v, ok := m["natural_language"].(string); ok {
		t.NaturalLanguage = v
	}
	t.Keywords = stringSliceFromAny(m["keywords"])
	t.TargetEntityKinds = stringSliceFromAny(m["target_entity_kinds"])
	return t
}

// parseAntiPatterns converts raw []map to []AntiPattern.
func parseAntiPatterns(raw []map[string]any) []agentpatterns.AntiPattern {
	if len(raw) == 0 {
		return nil
	}
	out := make([]agentpatterns.AntiPattern, 0, len(raw))
	for _, m := range raw {
		ap := agentpatterns.AntiPattern{}
		if v, ok := m["do_not"].(string); ok {
			ap.DoNot = v
		}
		if v, ok := m["reason"].(string); ok {
			ap.Reason = v
		}
		if v, ok := m["private"].(bool); ok {
			ap.Private = v
		}
		out = append(out, ap)
	}
	return out
}

// parseScopeArg extracts the optional scope override from the request.
// Returns nil if no scope arg was provided.
func parseScopeArg(req mcpapi.CallToolRequest) *agentpatterns.Scope {
	m := argObject(req, "scope")
	if m == nil {
		return nil
	}
	return &agentpatterns.Scope{
		Repos:       stringSliceFromAny(m["repos"]),
		ModulePaths: stringSliceFromAny(m["module_paths"]),
		Languages:   stringSliceFromAny(m["languages"]),
		Stacks:      stringSliceFromAny(m["stacks"]),
		EntityKinds: stringSliceFromAny(m["entity_kinds"]),
	}
}

// stringSliceFromAny converts an interface{} to []string, handling []any.
func stringSliceFromAny(v any) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers: scope
// ---------------------------------------------------------------------------

// deriveCWDScope infers a scope from the caller's CWD by walking the loaded
// repos to find the one whose path is a prefix of cwd.
func deriveCWDScope(cwd string, lg *LoadedGroup) agentpatterns.Scope {
	if cwd == "" || lg == nil {
		return agentpatterns.Scope{}
	}
	for _, r := range lg.Repos {
		if r.Path != "" && strings.HasPrefix(cwd, r.Path) {
			langs := repoLanguages(r)
			return agentpatterns.Scope{
				Repos:     []string{r.Repo},
				Languages: langs,
			}
		}
	}
	return agentpatterns.Scope{}
}

// deriveScopeFromExemplars auto-derives scope from exemplar entity IDs.
// Logic per ADR-0018:
//   - All exemplars in same repo → scope.repos = [that repo]
//   - Mixed repos → scope.repos = [] (group-wide), only languages set
//   - All same language → scope.languages = [that language]
//   - Common path prefix → scope.module_paths = [that prefix]
func deriveScopeFromExemplars(exemplars []string, lg *LoadedGroup) agentpatterns.Scope {
	if lg == nil || len(exemplars) == 0 {
		return agentpatterns.Scope{}
	}

	// Collect repo and entity info for each exemplar.
	type eInfo struct {
		repo     string
		srcFile  string
		language string
	}
	infos := make([]eInfo, 0, len(exemplars))
	for _, eid := range exemplars {
		rName, local := splitPrefixed(eid)
		if rName == "" {
			// No prefix — search all repos.
			for _, r := range lg.Repos {
				if r.Doc == nil {
					continue
				}
				if e := r.LabelIndex.ByID(local); e != nil {
					infos = append(infos, eInfo{repo: r.Repo, srcFile: e.SourceFile, language: e.Language})
					break
				}
				if e := r.LabelIndex.ByID(eid); e != nil {
					infos = append(infos, eInfo{repo: r.Repo, srcFile: e.SourceFile, language: e.Language})
					break
				}
			}
		} else {
			if r, ok := lg.Repos[rName]; ok && r.Doc != nil {
				if e := r.LabelIndex.ByID(local); e != nil {
					infos = append(infos, eInfo{repo: rName, srcFile: e.SourceFile, language: e.Language})
				}
			}
		}
	}

	if len(infos) == 0 {
		return agentpatterns.Scope{}
	}

	// Repo uniqueness.
	repoSet := map[string]bool{}
	for _, info := range infos {
		repoSet[info.repo] = true
	}
	repos := sortedKeys(repoSet)

	// Language uniqueness.
	langSet := map[string]bool{}
	for _, info := range infos {
		if info.language != "" {
			langSet[info.language] = true
		}
	}
	langs := sortedKeys(langSet)

	// Common path prefix across source files.
	paths := make([]string, 0, len(infos))
	for _, info := range infos {
		if info.srcFile != "" {
			paths = append(paths, filepath.Dir(info.srcFile))
		}
	}
	commonPrefix := longestCommonPathPrefix(paths)

	scope := agentpatterns.Scope{}
	if len(repos) == 1 {
		scope.Repos = repos
	}
	// Multi-repo: leave repos empty (group-wide), keep languages.
	if len(langs) > 0 {
		scope.Languages = langs
	}
	if commonPrefix != "" && commonPrefix != "." {
		scope.ModulePaths = []string{commonPrefix}
	}
	return scope
}

// scopeMatches returns true if the pattern's scope is satisfied by the
// provided caller scope. An empty constraint in pattern.Scope is a wildcard.
func scopeMatches(patternScope agentpatterns.Scope, callerScope *agentpatterns.Scope) bool {
	if callerScope == nil {
		return true // no caller scope = no constraint applied
	}
	if len(patternScope.Repos) > 0 && len(callerScope.Repos) > 0 {
		if !anyOverlap(patternScope.Repos, callerScope.Repos) {
			return false
		}
	}
	if len(patternScope.Languages) > 0 && len(callerScope.Languages) > 0 {
		if !anyOverlap(patternScope.Languages, callerScope.Languages) {
			return false
		}
	}
	if len(patternScope.Stacks) > 0 && len(callerScope.Stacks) > 0 {
		if !anyOverlap(patternScope.Stacks, callerScope.Stacks) {
			return false
		}
	}
	if len(patternScope.ModulePaths) > 0 && len(callerScope.ModulePaths) > 0 {
		if !anyPathOverlap(patternScope.ModulePaths, callerScope.ModulePaths) {
			return false
		}
	}
	return true
}

// scopeSpecificity returns the count of non-empty scope fields.
func scopeSpecificity(s agentpatterns.Scope) int {
	count := 0
	if len(s.Repos) > 0 {
		count++
	}
	if len(s.ModulePaths) > 0 {
		count++
	}
	if len(s.Languages) > 0 {
		count++
	}
	if len(s.Stacks) > 0 {
		count++
	}
	if len(s.EntityKinds) > 0 {
		count++
	}
	return count
}

// anyOverlap returns true if any element in a is also in b.
func anyOverlap(a, b []string) bool {
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		if set[x] {
			return true
		}
	}
	return false
}

// anyPathOverlap checks whether any path in a is a prefix of (or equal to)
// any path in b, or vice-versa.
func anyPathOverlap(a, b []string) bool {
	for _, pa := range a {
		for _, pb := range b {
			if strings.HasPrefix(pa, pb) || strings.HasPrefix(pb, pa) {
				return true
			}
		}
	}
	return false
}

// longestCommonPathPrefix returns the longest common directory prefix.
func longestCommonPathPrefix(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return paths[0]
	}
	prefix := paths[0]
	for _, p := range paths[1:] {
		prefix = commonPrefix(prefix, p)
		if prefix == "" || prefix == "." {
			return ""
		}
	}
	return prefix
}

// commonPrefix returns the longest common path prefix of a and b.
func commonPrefix(a, b string) string {
	aParts := strings.Split(filepath.ToSlash(a), "/")
	bParts := strings.Split(filepath.ToSlash(b), "/")
	var parts []string
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		if aParts[i] != bParts[i] {
			break
		}
		parts = append(parts, aParts[i])
	}
	return strings.Join(parts, "/")
}

// sortedKeys returns sorted keys of a map.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// repoLanguages collects distinct languages from a loaded repo's entities.
func repoLanguages(r *LoadedRepo) []string {
	if r.Doc == nil {
		return nil
	}
	set := map[string]bool{}
	for i := range r.Doc.Entities {
		if lang := r.Doc.Entities[i].Language; lang != "" {
			set[lang] = true
		}
	}
	return sortedKeys(set)
}

// ---------------------------------------------------------------------------
// Helpers: BM25 score (lightweight, no full corpus)
// ---------------------------------------------------------------------------

// bm25Score computes a simple TF-IDF-style relevance score of query against
// the pattern's trigger text and keywords. A full BM25 corpus would need all
// patterns; instead we use a simplified single-document variant that is good
// enough for ranking.
func bm25Score(query, naturalLang string, keywords []string) float64 {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return 0
	}
	docTokens := tokenize(naturalLang)
	for _, kw := range keywords {
		docTokens = append(docTokens, tokenize(kw)...)
	}
	if len(docTokens) == 0 {
		return 0
	}

	// Build document term frequency.
	tf := map[string]float64{}
	for _, t := range docTokens {
		tf[t]++
	}

	// Score: sum over query tokens of normalized TF.
	docLen := float64(len(docTokens))
	avgDocLen := 10.0 // assumed average
	k1, b := 1.5, 0.75

	score := 0.0
	for _, qt := range queryTokens {
		freq := tf[qt]
		if freq == 0 {
			continue
		}
		// BM25 term score.
		idf := math.Log(1 + 1.0/freq) // simplified IDF (no corpus)
		tfNorm := freq * (k1 + 1) / (freq + k1*(1-b+b*docLen/avgDocLen))
		score += idf * tfNorm
	}
	return score
}

// patternTokenize splits text into lowercase alphabetic tokens for BM25 scoring.
// (tokenize is already defined in scoring.go; this is an alias for patterns.)
func patternTokenize(s string) []string {
	return tokenize(s)
}

// recencyScore returns 1/(1 + days_since_last_applied/30).
// If lastApplied == 0 (never applied), returns 1.0 (full recency).
func recencyScore(lastApplied int64, nowUnix int64) float64 {
	if lastApplied == 0 {
		return 1.0
	}
	days := float64(nowUnix-lastApplied) / 86400.0
	if days < 0 {
		days = 0
	}
	return 1.0 / (1.0 + days/30.0)
}

// ---------------------------------------------------------------------------
// Helpers: convergence detection
// ---------------------------------------------------------------------------

// tryMergeCandidate checks if the new pattern candidate should be merged into
// an existing one. Returns the updated existing pattern if merged, or nil.
func tryMergeCandidate(patterns []agentpatterns.Pattern, newP *agentpatterns.Pattern, proposer string) (*agentpatterns.Pattern, bool) {
	for i := range patterns {
		existing := &patterns[i]
		if !existing.IsCandidate {
			continue
		}
		// BM25 similarity threshold = 0.8 (ADR-0018 cluster_similarity_threshold).
		sim := triggerSimilarity(newP.Trigger, existing.Trigger)
		if sim < 0.8 {
			continue
		}
		// At least one overlapping exemplar.
		if !anyOverlap(newP.Exemplars, existing.Exemplars) {
			continue
		}
		// Merge: union exemplars, append proposer, increment convergence_count.
		existing.Exemplars = unionStrings(existing.Exemplars, newP.Exemplars)
		if proposer != "" && !containsString(existing.ProposerSubagents, proposer) {
			existing.ProposerSubagents = append(existing.ProposerSubagents, proposer)
		}
		existing.ConvergenceCount++
		return existing, true
	}
	return nil, false
}

// triggerSimilarity returns a score in [0,1] for how similar two triggers are.
// We use a Jaccard-like token overlap as a proxy for BM25-cosine.
func triggerSimilarity(a, b agentpatterns.Trigger) float64 {
	tokA := tokenSet(a.NaturalLanguage, a.Keywords)
	tokB := tokenSet(b.NaturalLanguage, b.Keywords)
	if len(tokA) == 0 || len(tokB) == 0 {
		return 0
	}
	intersection := 0
	for t := range tokA {
		if tokB[t] {
			intersection++
		}
	}
	union := len(tokA) + len(tokB) - intersection
	if union == 0 {
		return 1
	}
	return float64(intersection) / float64(union)
}

// tokenSet returns a deduplicated token set from text and keywords.
func tokenSet(text string, keywords []string) map[string]bool {
	set := map[string]bool{}
	for _, t := range tokenize(text) {
		set[t] = true
	}
	for _, kw := range keywords {
		for _, t := range tokenize(kw) {
			set[t] = true
		}
	}
	return set
}

// unionStrings returns the union of two slices (deduped, order-preserving).
func unionStrings(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// containsString returns true if s is in slice.
func containsString(slice []string, s string) bool {
	for _, x := range slice {
		if x == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers: conflict detection
// ---------------------------------------------------------------------------

// detectConflicts identifies existing non-candidate patterns that overlap
// in scope.entity_kinds with the new pattern. Per ADR-0018 Q2 resolution,
// we write a CONFLICTS_WITH note in the record response (not a graph edge yet).
func detectConflicts(patterns []agentpatterns.Pattern, newP *agentpatterns.Pattern) []map[string]any {
	if len(newP.Scope.EntityKinds) == 0 {
		return nil
	}
	var out []map[string]any
	for _, p := range patterns {
		if p.ID == newP.ID || p.IsCandidate {
			continue
		}
		if !scopeOverlap(p.Scope, newP.Scope) {
			continue
		}
		if !anyOverlap(p.Scope.EntityKinds, newP.Scope.EntityKinds) {
			continue
		}
		out = append(out, map[string]any{
			"conflict_with_id": p.ID,
			"trigger":          p.Trigger.NaturalLanguage,
			"edge_kind":        string(types.RelationshipKindConflictsWith),
		})
	}
	return out
}

// scopeOverlap returns true if the two scopes overlap (non-empty intersection
// or one side is a wildcard).
func scopeOverlap(a, b agentpatterns.Scope) bool {
	if len(a.Repos) > 0 && len(b.Repos) > 0 && !anyOverlap(a.Repos, b.Repos) {
		return false
	}
	if len(a.Languages) > 0 && len(b.Languages) > 0 && !anyOverlap(a.Languages, b.Languages) {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Helpers: graph edge emission
// ---------------------------------------------------------------------------

// buildPatternEdges constructs the list of edges to emit for a newly recorded
// pattern. The actual graph write will happen via the daemon in a future PR;
// here we record the intent so it can be replayed or logged.
func buildPatternEdges(p *agentpatterns.Pattern) []map[string]any {
	var edges []map[string]any
	for _, eid := range p.Exemplars {
		edges = append(edges, map[string]any{
			"from":      p.ID,
			"to":        eid,
			"edge_kind": string(types.RelationshipKindExemplar),
		})
	}
	return edges
}

// ---------------------------------------------------------------------------
// Helpers: serialisation
// ---------------------------------------------------------------------------

// patternToMap converts a Pattern to a map for JSON output.
// Private anti-patterns are excluded unless includePrivate is true.
func patternToMap(p agentpatterns.Pattern, includePrivate bool) map[string]any {
	aps := make([]map[string]any, 0, len(p.AntiPatterns))
	for _, ap := range p.AntiPatterns {
		if ap.Private && !includePrivate {
			continue
		}
		row := map[string]any{
			"do_not": ap.DoNot,
			"reason": ap.Reason,
		}
		if includePrivate {
			row["private"] = ap.Private
		}
		aps = append(aps, row)
	}
	out := map[string]any{
		"id":                p.ID,
		"trigger":           p.Trigger,
		"steps":             p.Steps,
		"anti_patterns":     aps,
		"scope":             p.Scope,
		"category":          string(p.Category),
		"confidence":        p.Confidence,
		"observations":      p.Observations,
		"last_applied":      p.LastApplied,
		"last_validated":    p.LastValidated,
		"is_candidate":      p.IsCandidate,
		"convergence_count": p.ConvergenceCount,
		"exemplars":         p.Exemplars,
		"documentation_url": p.DocumentationURL,
	}
	if len(p.ProposerSubagents) > 0 {
		out["proposer_subagents"] = p.ProposerSubagents
	}
	if len(p.Touches) > 0 {
		out["touches"] = p.Touches
	}
	if len(p.AntiExemplars) > 0 {
		out["anti_exemplars"] = p.AntiExemplars
	}
	if p.RejectReason != "" {
		out["reject_reason"] = p.RejectReason
		out["reject_timestamp"] = p.RejectTimestamp
	}
	if p.ApprovalNote != "" {
		out["approval_note"] = p.ApprovalNote
	}
	return out
}

// ---------------------------------------------------------------------------
// Exemplars field on Pattern struct
// ---------------------------------------------------------------------------
// NOTE: The Exemplars field is defined in agentpatterns.Pattern (added by β).
// The agentpatterns package does not yet have this field. We need to ensure
// the Pattern struct carries it. Since we cannot import unexported symbols, we
// rely on the type having been extended in the agentpatterns package. If not
// yet present, we embed it via a local extension mechanism. See patterns_ext.go.

// formatTimestamp formats a unix timestamp as RFC3339. Zero → "".
func formatTimestamp(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
}

// ---------------------------------------------------------------------------
// action=get — single-id lookup (bypass BM25 ranking)
// ---------------------------------------------------------------------------

func (s *Server) handlePatternsGet(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	patternID, err := req.RequireString("pattern_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	includePrivate := argBool(req, "include_private", false)

	groupName, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	dir := patternsDir(groupName, lg)
	patterns, loadErr := agentpatterns.Load(dir)
	if loadErr != nil {
		return mcpapi.NewToolResultError("load patterns: " + loadErr.Error()), nil
	}
	p := agentpatterns.ByID(patterns, patternID)
	if p == nil {
		return mcpapi.NewToolResultError(fmt.Sprintf("pattern %q not found", patternID)), nil
	}
	return jsonResult(patternToMap(*p, includePrivate)), nil
}

// ---------------------------------------------------------------------------
// action=refine
// ---------------------------------------------------------------------------

// handlePatternsRefine applies structural edits to an existing pattern.
// Confidence is unchanged (refinement is neutral per ADR-0018).
func (s *Server) handlePatternsRefine(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	patternID, err := req.RequireString("pattern_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	changes := argObject(req, "changes")
	if changes == nil {
		return mcpapi.NewToolResultError("changes is required for action=refine"), nil
	}

	groupName, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	dir := patternsDir(groupName, lg)

	patternsMu.Lock()
	defer patternsMu.Unlock()

	patterns, loadErr := agentpatterns.Load(dir)
	if loadErr != nil {
		return mcpapi.NewToolResultError("load patterns: " + loadErr.Error()), nil
	}
	p := agentpatterns.ByID(patterns, patternID)
	if p == nil {
		return mcpapi.NewToolResultError(fmt.Sprintf("pattern %q not found", patternID)), nil
	}

	// Apply each change, collecting edge change descriptions.
	var edgeChanges []map[string]any
	now := agentpatterns.NowUnix()

	// --- add_step ---
	if v, ok := changes["add_step"].(string); ok && v != "" {
		p.Steps = append(p.Steps, v)
	}

	// --- remove_step_index ---
	if raw, ok := changes["remove_step_index"]; ok {
		idx := toInt(raw)
		if idx >= 0 && idx < len(p.Steps) {
			p.Steps = append(p.Steps[:idx], p.Steps[idx+1:]...)
		}
	}

	// --- edit_step: {index, new_text} ---
	if m, ok := changes["edit_step"].(map[string]any); ok {
		idx := toInt(m["index"])
		newText, _ := m["new_text"].(string)
		if idx >= 0 && idx < len(p.Steps) && newText != "" {
			p.Steps[idx] = newText
		}
	}

	// --- add_anti_pattern: {do_not, reason, private} ---
	if m, ok := changes["add_anti_pattern"].(map[string]any); ok {
		ap := agentpatterns.AntiPattern{}
		if v, ok2 := m["do_not"].(string); ok2 {
			ap.DoNot = v
		}
		if v, ok2 := m["reason"].(string); ok2 {
			ap.Reason = v
		}
		if v, ok2 := m["private"].(bool); ok2 {
			ap.Private = v
		}
		p.AntiPatterns = append(p.AntiPatterns, ap)
	}

	// --- remove_anti_pattern_index ---
	if raw, ok := changes["remove_anti_pattern_index"]; ok {
		idx := toInt(raw)
		if idx >= 0 && idx < len(p.AntiPatterns) {
			p.AntiPatterns = append(p.AntiPatterns[:idx], p.AntiPatterns[idx+1:]...)
		}
	}

	// --- add_exemplar ---
	if v, ok := changes["add_exemplar"].(string); ok && v != "" {
		if !containsString(p.Exemplars, v) {
			p.Exemplars = append(p.Exemplars, v)
			edgeChanges = append(edgeChanges, map[string]any{
				"op":        "add",
				"from":      p.ID,
				"to":        v,
				"edge_kind": "EXEMPLAR",
			})
		}
	}

	// --- remove_exemplar ---
	if v, ok := changes["remove_exemplar"].(string); ok && v != "" {
		if containsString(p.Exemplars, v) {
			p.Exemplars = removeString(p.Exemplars, v)
			edgeChanges = append(edgeChanges, map[string]any{
				"op":        "remove",
				"from":      p.ID,
				"to":        v,
				"edge_kind": "EXEMPLAR",
			})
		}
	}

	// --- change_scope: partial — only non-nil fields overwrite existing ---
	if m, ok := changes["change_scope"].(map[string]any); ok {
		if v := stringSliceFromAny(m["repos"]); v != nil {
			p.Scope.Repos = v
		}
		if v := stringSliceFromAny(m["module_paths"]); v != nil {
			p.Scope.ModulePaths = v
		}
		if v := stringSliceFromAny(m["languages"]); v != nil {
			p.Scope.Languages = v
		}
		if v := stringSliceFromAny(m["stacks"]); v != nil {
			p.Scope.Stacks = v
		}
		if v := stringSliceFromAny(m["entity_kinds"]); v != nil {
			p.Scope.EntityKinds = v
		}
	}

	// --- change_category ---
	if v, ok := changes["change_category"].(string); ok && v != "" {
		cat := agentpatterns.Category(v)
		if validCategories[cat] {
			p.Category = cat
		}
	}

	// --- set_documentation_url ---
	if v, ok := changes["set_documentation_url"].(string); ok {
		p.DocumentationURL = v
	}

	// Refinement is neutral — confidence unchanged. Update last_validated.
	p.LastValidated = now
	// Increment observations (refinement is an interaction).
	p.Observations++

	patterns = agentpatterns.Upsert(patterns, *p)
	if saveErr := agentpatterns.Save(dir, patterns); saveErr != nil {
		return mcpapi.NewToolResultError("save patterns: " + saveErr.Error()), nil
	}

	return jsonResult(map[string]any{
		"pattern":      patternToMap(*p, true),
		"edge_changes": edgeChanges,
	}), nil
}

// ---------------------------------------------------------------------------
// action=apply
// ---------------------------------------------------------------------------

// handlePatternsApply records a use outcome and adjusts confidence.
func (s *Server) handlePatternsApply(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	patternID, err := req.RequireString("pattern_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	args := req.GetArguments()
	successRaw, ok := args["success"]
	if !ok {
		return mcpapi.NewToolResultError("success is required for action=apply"), nil
	}
	success, _ := successRaw.(bool)
	createdEntities := argStringSliceFromAny(req, "created_entities")

	groupName, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	dir := patternsDir(groupName, lg)

	patternsMu.Lock()
	defer patternsMu.Unlock()

	patterns, loadErr := agentpatterns.Load(dir)
	if loadErr != nil {
		return mcpapi.NewToolResultError("load patterns: " + loadErr.Error()), nil
	}
	p := agentpatterns.ByID(patterns, patternID)
	if p == nil {
		return mcpapi.NewToolResultError(fmt.Sprintf("pattern %q not found", patternID)), nil
	}

	now := agentpatterns.NowUnix()

	// Confidence adjustment.
	if success {
		p.Confidence = agentpatterns.ApplyConfidenceDelta(p.Confidence, agentpatterns.EventApplySuccess)
		p.LastApplied = now
	} else {
		p.Confidence = agentpatterns.ApplyConfidenceDelta(p.Confidence, agentpatterns.EventApplyFailure)
	}
	p.Observations++
	p.LastValidated = now

	// Build CREATED_BY edges with apply-call provenance.
	applyCallID := fmt.Sprintf("apply:%s:%d", patternID[:8], now)
	var createdByEdges []map[string]any
	for _, eid := range createdEntities {
		createdByEdges = append(createdByEdges, map[string]any{
			"from":          eid,
			"to":            p.ID,
			"edge_kind":     "CREATED_BY",
			"apply_call_id": applyCallID,
			"success":       success,
			"timestamp":     now,
		})
	}

	patterns = agentpatterns.Upsert(patterns, *p)
	if saveErr := agentpatterns.Save(dir, patterns); saveErr != nil {
		return mcpapi.NewToolResultError("save patterns: " + saveErr.Error()), nil
	}

	return jsonResult(map[string]any{
		"pattern":          patternToMap(*p, true),
		"created_by_edges": createdByEdges,
		"created_by_count": len(createdByEdges),
		"apply_call_id":    applyCallID,
	}), nil
}

// ---------------------------------------------------------------------------
// action=reject
// ---------------------------------------------------------------------------

// handlePatternsReject marks a pattern as rejected, adjusting confidence.
func (s *Server) handlePatternsReject(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	patternID, err := req.RequireString("pattern_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	reason, err := req.RequireString("reason")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	setToZero := argBool(req, "set_to_zero", false)

	groupName, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	dir := patternsDir(groupName, lg)

	patternsMu.Lock()
	defer patternsMu.Unlock()

	patterns, loadErr := agentpatterns.Load(dir)
	if loadErr != nil {
		return mcpapi.NewToolResultError("load patterns: " + loadErr.Error()), nil
	}
	p := agentpatterns.ByID(patterns, patternID)
	if p == nil {
		return mcpapi.NewToolResultError(fmt.Sprintf("pattern %q not found", patternID)), nil
	}

	now := agentpatterns.NowUnix()

	if setToZero {
		// Hard-set to 0 (bypasses floor — intentional hard rejection).
		// Note: normal floor is 0.2; set_to_zero explicitly overrides it.
		p.Confidence = 0
	} else {
		p.Confidence = agentpatterns.ApplyConfidenceDelta(p.Confidence, agentpatterns.EventReject)
	}
	p.Observations++
	p.LastValidated = now
	p.RejectReason = reason
	p.RejectTimestamp = now

	patterns = agentpatterns.Upsert(patterns, *p)
	if saveErr := agentpatterns.Save(dir, patterns); saveErr != nil {
		return mcpapi.NewToolResultError("save patterns: " + saveErr.Error()), nil
	}

	return jsonResult(map[string]any{
		"pattern":       patternToMap(*p, true),
		"reject_reason": reason,
	}), nil
}

// ---------------------------------------------------------------------------
// action=promote
// ---------------------------------------------------------------------------

// handlePatternsPromote promotes a candidate pattern to approved.
func (s *Server) handlePatternsPromote(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	candidateID, err := req.RequireString("candidate_id")
	if err != nil {
		return mcpapi.NewToolResultError(err.Error()), nil
	}
	approvalNote := argString(req, "approval_note", "")

	groupName, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	dir := patternsDir(groupName, lg)

	patternsMu.Lock()
	defer patternsMu.Unlock()

	patterns, loadErr := agentpatterns.Load(dir)
	if loadErr != nil {
		return mcpapi.NewToolResultError("load patterns: " + loadErr.Error()), nil
	}
	p := agentpatterns.ByID(patterns, candidateID)
	if p == nil {
		return mcpapi.NewToolResultError(fmt.Sprintf("pattern %q not found", candidateID)), nil
	}
	if !p.IsCandidate {
		return mcpapi.NewToolResultError(fmt.Sprintf(
			"pattern %q is already approved (is_candidate=false); cannot promote again", candidateID,
		)), nil
	}

	now := agentpatterns.NowUnix()
	p.IsCandidate = false
	p.LastValidated = now
	if approvalNote != "" {
		p.ApprovalNote = approvalNote
	}

	patterns = agentpatterns.Upsert(patterns, *p)
	if saveErr := agentpatterns.Save(dir, patterns); saveErr != nil {
		return mcpapi.NewToolResultError("save patterns: " + saveErr.Error()), nil
	}

	return jsonResult(patternToMap(*p, true)), nil
}

// ---------------------------------------------------------------------------
// Helpers: slice utilities used by refine
// ---------------------------------------------------------------------------

// removeString removes the first occurrence of s from slice.
func removeString(slice []string, s string) []string {
	out := make([]string, 0, len(slice))
	removed := false
	for _, x := range slice {
		if !removed && x == s {
			removed = true
			continue
		}
		out = append(out, x)
	}
	return out
}

// toInt converts an interface{} numeric to int. Returns -1 on failure.
func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	}
	return -1
}
