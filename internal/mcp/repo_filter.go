package mcp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// repo_filter matching + self-correcting error hints (#5648).
//
// repo_filter entries arrive from agents in many shapes: an exact repo
// name/slug, an `owner/repo` form, a bare basename, a full absolute path, or a
// path with a different case. Exact-only map lookup (the historical behaviour)
// silently excluded everything when the shape didn't match the group's keys,
// and the caller got the opaque "# no repos loaded for this group". This file
// makes the matching tolerant and the failure modes actionable.

// resolveRepoFilter maps each repo_filter entry to a repo in lg using the
// lenient precedence below, preserving exact-match semantics. It returns the
// matched repos (de-duplicated, sorted by key) and, when an entry could not be
// resolved unambiguously, a non-nil *repoFilterMiss describing why so callers
// can surface a self-correcting error instead of a dead end.
//
// Precedence per entry (first tier that yields a hit wins):
//  1. exact repo key            (lg.Repos[entry])
//  2. exact full path           (filepath.Clean(entry) == Clean(repo.Path))
//  3. case-insensitive key      (EqualFold against repo key)
//  4. basename / last segment   (basename(entry) vs repo key or basename(path),
//     case-insensitive — handles owner/repo, /abs/path/repo, REPO)
//
// When tiers 1-4 all miss, the entry is reported unresolved with a closest-match
// suggestion (normalized Levenshtein >= 2/3 on basename) surfaced in the error
// hint — we deliberately do NOT silently auto-resolve a fuzzy guess.
//
// Ambiguity guard: within the first tier that produces any hit, if 2+ DISTINCT
// repos match, the entry is reported as ambiguous (not silently resolved).
func resolveRepoFilter(lg *LoadedGroup, filter []string) ([]*LoadedRepo, *repoFilterMiss) {
	// Wildcard / empty → all indexed repos (unchanged semantics).
	if len(filter) == 0 || (len(filter) == 1 && filter[0] == "*") {
		return reposAll(lg), nil
	}

	seen := make(map[*LoadedRepo]bool)
	var out []*LoadedRepo
	add := func(rs []*LoadedRepo) {
		for _, r := range rs {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}

	for _, entry := range filter {
		matches, fuzzy := matchRepoEntry(lg, entry)
		switch {
		case len(matches) == 1:
			add(matches)
		case len(matches) > 1:
			// Ambiguous: surface candidates rather than guessing.
			return nil, &repoFilterMiss{
				entry:      entry,
				ambiguous:  repoKeys(matches),
				suggestion: "",
			}
		default:
			// No match at any tier: report with a closest-match suggestion.
			return nil, &repoFilterMiss{
				entry:      entry,
				suggestion: fuzzy,
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })
	return out, nil
}

// matchRepoEntry resolves a single repo_filter entry against lg's repos using
// the lenient precedence. It returns the matching repos for the first tier that
// produces any hit (0, 1, or — for the ambiguity guard — 2+), plus the
// closest-by-name repo key (suggestion) used only when nothing matched.
func matchRepoEntry(lg *LoadedGroup, entry string) (matches []*LoadedRepo, suggestion string) {
	if lg == nil || strings.TrimSpace(entry) == "" {
		return nil, ""
	}

	// Tier 1: exact key.
	if r := lg.Repos[entry]; r != nil && r.Doc != nil {
		return []*LoadedRepo{r}, ""
	}

	entryClean := filepath.Clean(entry)
	entryBase := strings.ToLower(filepath.Base(entryClean))

	// Tiers 2-4: collect hits per tier, honoring precedence.
	var pathHits, ciKeyHits, baseHits []*LoadedRepo
	for _, r := range indexedRepos(lg) {
		// Tier 2: exact full path.
		if r.Path != "" && filepath.Clean(r.Path) == entryClean {
			pathHits = append(pathHits, r)
		}
		// Tier 3: case-insensitive key.
		if strings.EqualFold(r.Repo, entry) {
			ciKeyHits = append(ciKeyHits, r)
		}
		// Tier 4: basename / last segment of entry vs repo key or repo path base.
		if entryBase != "" {
			if strings.EqualFold(r.Repo, entryBase) {
				baseHits = append(baseHits, r)
			} else if r.Path != "" && strings.EqualFold(filepath.Base(filepath.Clean(r.Path)), entryBase) {
				baseHits = append(baseHits, r)
			}
		}
	}
	if len(pathHits) > 0 {
		return dedupeRepos(pathHits), ""
	}
	if len(ciKeyHits) > 0 {
		return dedupeRepos(ciKeyHits), ""
	}
	if len(baseHits) > 0 {
		return dedupeRepos(baseHits), ""
	}

	// No structural match. Compute the closest-by-name repo so the caller can
	// surface a "Did you mean" hint. We do not auto-resolve a fuzzy guess.
	suggestion = closestRepo(lg, entryBase)
	return nil, suggestion
}

// closestRepo returns the indexed repo key whose name is closest (normalized
// Levenshtein) to target, requiring >= 2/3 similarity. Returns "" when nothing
// is close enough. Used for the "Did you mean" suggestion in error hints.
func closestRepo(lg *LoadedGroup, target string) string {
	if target == "" {
		return ""
	}
	best := ""
	bestDist := 1 << 30
	for _, r := range indexedRepos(lg) {
		cand := strings.ToLower(r.Repo)
		d := levenshteinDist(target, cand)
		m := len(target)
		if len(cand) > m {
			m = len(cand)
		}
		if m == 0 || d*3 > m { // require >2/3 similarity
			continue
		}
		if d < bestDist {
			bestDist = d
			best = r.Repo
		}
	}
	return best
}

// repoFilterMiss records why a repo_filter entry could not be resolved, so a
// caller can render a self-correcting error.
type repoFilterMiss struct {
	entry      string   // the unresolved filter entry
	suggestion string   // closest available repo key, or "" if nothing close
	ambiguous  []string // repo keys when the entry matched 2+ repos (else nil)
}

// repoFilterError renders the self-correcting error for a failed repo_filter,
// distinguishing the genuine cases (#5648):
//   - group has zero indexed repos       → say so + how to index
//   - filter excluded all (no match)     → list available + suggest closest
//   - lenient match was ambiguous        → list candidates
//
// Messages are concise and machine-parseable-ish (an agent reads them).
func repoFilterError(lg *LoadedGroup, miss *repoFilterMiss) string {
	group := ""
	if lg != nil {
		group = lg.Name
	}
	avail := repoKeys(indexedRepos(lg))

	// Group genuinely has no indexed repos: the filter is irrelevant.
	if len(avail) == 0 {
		return fmt.Sprintf(
			"group %q has no indexed repos. Index one with: grafel index <path> --group %s",
			group, group)
	}

	if miss != nil && len(miss.ambiguous) > 0 {
		return fmt.Sprintf(
			"repo_filter [%s] ambiguously matched %d repos in group %q: %s. Pass an exact repo name.",
			miss.entry, len(miss.ambiguous), group, strings.Join(miss.ambiguous, ", "))
	}

	entry := ""
	if miss != nil {
		entry = miss.entry
	}
	msg := fmt.Sprintf(
		"repo_filter [%s] matched no repos in group %q. Available repos: %s.",
		entry, group, strings.Join(avail, ", "))
	if miss != nil && miss.suggestion != "" {
		msg += fmt.Sprintf(" Did you mean %q?", miss.suggestion)
	}
	return msg
}

// ---- small repo-collection helpers --------------------------------------

// reposAll returns all indexed repos in lg, sorted by key. Equivalent to the
// wildcard/empty-filter branch of reposToConsider.
func reposAll(lg *LoadedGroup) []*LoadedRepo {
	return indexedRepos(lg)
}

// indexedRepos returns lg's repos that have a loaded Doc, sorted by key.
func indexedRepos(lg *LoadedGroup) []*LoadedRepo {
	if lg == nil {
		return nil
	}
	names := make([]string, 0, len(lg.Repos))
	for n := range lg.Repos {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*LoadedRepo, 0, len(names))
	for _, n := range names {
		if r := lg.Repos[n]; r != nil && r.Doc != nil {
			out = append(out, r)
		}
	}
	return out
}

func dedupeRepos(rs []*LoadedRepo) []*LoadedRepo {
	seen := make(map[*LoadedRepo]bool, len(rs))
	out := make([]*LoadedRepo, 0, len(rs))
	for _, r := range rs {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

func repoKeys(rs []*LoadedRepo) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Repo)
	}
	sort.Strings(out)
	return out
}
