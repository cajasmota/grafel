// discover.go implements the `discover` subcommand: walk the repo and
// catalog archigraph capabilities from structural signals (YAML rules,
// synthesizer functions, extractor directories, test fixtures, engine
// pattern files). Emits a proposal of records that SHOULD exist based on
// code evidence, plus orphan + cite-drift reports against the existing
// registry.
//
// Determinism: every map iteration is funneled through sorted key slices,
// every output slice is sort-stable, no time-based fields are emitted.
//
// Standalone scope: stdlib only. No imports from internal/ packages.
// (We treat yaml files as opaque — only the filename and directory
// structure are used as signal, so no yaml parser is needed.)
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DiscoverResult is the top-level JSON shape produced by the discover
// subcommand. Field order in this struct controls JSON ordering when
// serialised with encoding/json + sorted maps. Slices are sorted by ID.
type DiscoverResult struct {
	Proposal                 []Candidate              `json:"proposal"`
	OrphansInRegistry        []Orphan                 `json:"orphans_in_registry"`
	StatusUpgradeCandidates  []StatusUpgradeCandidate `json:"status_upgrade_candidates"`
	CiteDrift                []CiteDriftItem          `json:"cite_drift"`
	Summary                  DiscoverSummary          `json:"summary"`
}

// Candidate is a discovered record proposal keyed by stable slug ID.
type Candidate struct {
	CandidateID           string                          `json:"candidate_id"`
	Category              string                          `json:"category"`
	Language              string                          `json:"language"`
	Label                 string                          `json:"label"`
	Evidence              []Evidence                      `json:"evidence"`
	InferredCapabilities  map[string]InferredCapability   `json:"inferred_capabilities"`
	AlreadyInRegistry     bool                            `json:"already_in_registry"`
	RegistryID            string                          `json:"registry_id,omitempty"`
}

// Evidence is a single citation: kind of signal + repo-relative path,
// optionally pinned to a specific symbol (e.g. a synthesizer function).
type Evidence struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Symbol string `json:"symbol,omitempty"`
}

// InferredCapability is a single capability inference for a candidate.
type InferredCapability struct {
	Status     string  `json:"status"`
	Confidence float64 `json:"confidence"`
}

// Orphan is a registry record with no code-side evidence AND a status of
// full or partial (claims to be supported but no implementation found).
type Orphan struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// StatusUpgradeCandidate is a registry record with status=missing but
// code-side evidence was discovered. The record should be updated from
// missing to the suggested status.
type StatusUpgradeCandidate struct {
	ID              string   `json:"id"`
	CurrentStatus   string   `json:"current_status"`
	EvidenceFound   []string `json:"evidence_found"`
	SuggestedStatus string   `json:"suggested_status"`
}

// CiteDriftItem reports a registry record whose declared cites no longer
// exist on disk, alongside the cites discover found for the same record.
type CiteDriftItem struct {
	ID              string   `json:"id"`
	StaleCites      []string `json:"stale_cites"`
	DiscoveredCites []string `json:"discovered_cites"`
}

// DiscoverSummary aggregates counters for the result.
type DiscoverSummary struct {
	ProposalTotal        int `json:"proposal_total"`
	InRegistry           int `json:"in_registry"`
	NewCandidates        int `json:"new_candidates"`
	Orphans              int `json:"orphans"`
	StatusUpgradeCandidates int `json:"status_upgrade_candidates"`
	CitesDrifted         int `json:"cites_drifted"`
}

// confidence values are hard-coded per evidence pattern (v1). Future
// revisions may make these data-driven.
const (
	confidenceFull          = 0.95 // YAML + synthesizer + fixture
	confidenceStrong        = 0.85 // YAML + synthesizer
	confidenceFixtureBoost  = 0.80 // YAML + fixture
	confidencePartial       = 0.60 // YAML only
	confidenceLanguageStrong = 0.90 // extractor + rules dir
	confidenceLanguagePartial = 0.70 // one of (extractor | rules dir)
)

// languageDirAlias maps rules-directory names to the registry's language
// slug. The rule tree uses combined slugs like "javascript_typescript";
// the registry splits them or shortens them.
var languageDirAlias = map[string]string{
	"javascript_typescript": "javascript",
	"objective_c":           "objc",
}

// engineNamePrefixes are the file-prefix tokens we treat as framework
// identifiers when matching <framework>_<kind>.go in internal/engine/.
// Each pair declares which capability the suffix evidences.
var enginePatternSuffixes = []struct {
	suffix     string
	capability string
}{
	{"_routes.go", "endpoint_synthesis"},
	{"_orm.go", "model_extraction"},
	{"_auth.go", "auth_coverage"},
	{"_migration.go", "migration_parsing"},
	{"_migrations.go", "migration_parsing"},
}

// synthesizerRE captures the framework name from a synthesizer function
// declaration. Example matches:
//
//	func synthesizeFlask(...
//	func (r *runtime) synthesizeFastify(...
var synthesizerRE = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s+)?synthesize([A-Z][A-Za-z0-9_]*)\s*\(`)

// Discover walks repoRoot and merges the result with the registry on
// disk at registryPath. registryPath may be empty to skip the merge
// step; in that case all candidates are emitted as new.
func Discover(repoRoot, registryPath string) (DiscoverResult, error) {
	discovered := walkAll(repoRoot)
	var reg *Registry
	if registryPath != "" {
		r, err := loadRegistry(registryPath)
		if err != nil {
			return DiscoverResult{}, err
		}
		reg = r
	}
	return MergeWithRegistry(discovered, reg, repoRoot), nil
}

// walkAll runs every discovery source against repoRoot and returns a
// map of candidate-id -> Candidate. The map is intermediate; the caller
// converts it to a sorted slice during merge.
func walkAll(repoRoot string) map[string]*Candidate {
	cands := map[string]*Candidate{}
	yamlWalker(repoRoot, cands)
	synthesizerGrep(repoRoot, cands)
	extractorDirLister(repoRoot, cands)
	fixtureLister(repoRoot, cands)
	enginePatternMatcher(repoRoot, cands)
	return cands
}

// ensureCandidate returns an existing candidate or constructs a new one
// in the map. Repeated calls are stable: evidence merges by-kind+path.
func ensureCandidate(m map[string]*Candidate, id, category, language, label string) *Candidate {
	if c, ok := m[id]; ok {
		// Upgrade label if previously empty (synthesizer-only path).
		if c.Label == "" && label != "" {
			c.Label = label
		}
		if c.Category == "" && category != "" {
			c.Category = category
		}
		if c.Language == "" && language != "" {
			c.Language = language
		}
		return c
	}
	c := &Candidate{
		CandidateID:          id,
		Category:             category,
		Language:             language,
		Label:                label,
		InferredCapabilities: map[string]InferredCapability{},
	}
	m[id] = c
	return c
}

// addEvidence appends evidence to a candidate idempotently.
func addEvidence(c *Candidate, kind, path, symbol string) {
	for _, e := range c.Evidence {
		if e.Kind == kind && e.Path == path && e.Symbol == symbol {
			return
		}
	}
	c.Evidence = append(c.Evidence, Evidence{Kind: kind, Path: path, Symbol: symbol})
}

// slugifyFramework normalises a yaml framework filename or synthesizer
// suffix into a stable lowercase slug suitable for an ID segment.
//
// Examples:
//
//	"strawberry_graphql" -> "strawberry-graphql"
//	"Flask"              -> "flask"
//	"NestJS"             -> "nestjs"
//	"GorillaMux"         -> "gorillamux"
func slugifyFramework(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			// Lowercase only, no extra dash inserted: keeps slugs
			// short and matches existing registry conventions
			// (e.g. "fastapi", "gorillamux").
			b.WriteRune(r + ('a' - 'A'))
		case r == '_':
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normaliseLanguage maps a rules-tree directory name to the registry's
// language slug.
func normaliseLanguage(dir string) string {
	if alias, ok := languageDirAlias[dir]; ok {
		return alias
	}
	return dir
}

// labelize produces a human-readable label from a slug.
//
//	"fastapi" -> "FastAPI"  (heuristic: capitalise; rule-name-specific
//	cases stay simple — humans refine in the proposal.)
func labelize(slug string) string {
	if slug == "" {
		return ""
	}
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// yamlWalker walks internal/engine/rules/<lang>/{frameworks,orms,queues}/
// and registers one candidate per yaml file.
func yamlWalker(repoRoot string, cands map[string]*Candidate) {
	root := filepath.Join(repoRoot, "internal", "engine", "rules")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	subdirCategory := map[string]string{
		"frameworks": "http_framework",
		"orms":       "orm",
		"queues":     "message_broker",
	}
	subdirIDSeg := map[string]string{
		"frameworks": "framework",
		"orms":       "orm",
		"queues":     "queue",
	}
	for _, lang := range entries {
		if !lang.IsDir() {
			continue
		}
		if strings.HasPrefix(lang.Name(), "_") {
			continue
		}
		langSlug := normaliseLanguage(lang.Name())
		// Top-level language candidate (boosted later by extractor dir).
		langID := "lang." + langSlug
		c := ensureCandidate(cands, langID, "language", langSlug, labelize(langSlug))
		relRoot := filepath.Join("internal", "engine", "rules", lang.Name())
		addEvidence(c, "rules_dir", relRoot, "")
		for sub, cat := range subdirCategory {
			subPath := filepath.Join(root, lang.Name(), sub)
			files, err := os.ReadDir(subPath)
			if err != nil {
				continue
			}
			seg := subdirIDSeg[sub]
			for _, f := range files {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
					continue
				}
				base := strings.TrimSuffix(f.Name(), ".yaml")
				slug := slugifyFramework(base)
				id := fmt.Sprintf("lang.%s.%s.%s", langSlug, seg, slug)
				label := labelize(slug)
				fc := ensureCandidate(cands, id, cat, langSlug, label)
				rel := filepath.Join(relRoot, sub, f.Name())
				addEvidence(fc, "yaml_rule", rel, "")
			}
		}
	}
}

// synthesizerGrep scans internal/engine/*.go for synthesizer functions
// and attaches evidence to a framework candidate matched by name.
//
// Matching strategy: build a map of slugified framework names from the
// existing candidates, then resolve the synthesizer suffix against that
// map. Synthesizers whose names don't resolve to a known framework are
// recorded as standalone "orphan" candidates under a synthesizer-only
// ID — this surfaces cases where engine code exists without a YAML rule.
func synthesizerGrep(repoRoot string, cands map[string]*Candidate) {
	dir := filepath.Join(repoRoot, "internal", "engine")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	// Index existing candidates by their last-segment slug so we can
	// resolve "Flask" -> "lang.python.framework.flask".
	indexBySlug := map[string][]string{}
	for id := range cands {
		parts := strings.Split(id, ".")
		if len(parts) < 2 {
			continue
		}
		last := parts[len(parts)-1]
		indexBySlug[last] = append(indexBySlug[last], id)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		files = append(files, n)
	}
	sort.Strings(files)
	for _, name := range files {
		path := filepath.Join(dir, name)
		rel := filepath.Join("internal", "engine", name)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			m := synthesizerRE.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			sym := "synthesize" + m[1]
			slug := slugifyFramework(m[1])
			// Resolve to an existing candidate, preferring framework
			// IDs over language/orm/queue.
			ids := indexBySlug[slug]
			resolved := ""
			for _, id := range ids {
				if strings.Contains(id, ".framework.") {
					resolved = id
					break
				}
			}
			if resolved == "" && len(ids) > 0 {
				resolved = ids[0]
			}
			if resolved == "" {
				// Unresolved: synthesizer with no YAML rule.
				resolved = "synth." + slug
				c := ensureCandidate(cands, resolved, "http_framework", "multi", labelize(slug))
				addEvidence(c, "synthesizer", rel, sym)
				indexBySlug[slug] = append(indexBySlug[slug], resolved)
				continue
			}
			c := cands[resolved]
			addEvidence(c, "synthesizer", rel, sym)
		}
		f.Close()
	}
}

// extractorDirLister walks internal/extractors/<lang>/ and tags each
// language candidate with extractor evidence. Each directory entry that
// is itself a directory implies a language.
func extractorDirLister(repoRoot string, cands map[string]*Candidate) {
	root := filepath.Join(repoRoot, "internal", "extractors")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := normaliseLanguage(e.Name())
		id := "lang." + slug
		c := ensureCandidate(cands, id, "language", slug, labelize(slug))
		rel := filepath.Join("internal", "extractors", e.Name())
		addEvidence(c, "extractor_dir", rel, "")
	}
}

// fixtureLister walks cmd/archigraph/testdata/audit*/ and adds evidence
// to candidates whose slugged framework name matches a subdir.
func fixtureLister(repoRoot string, cands map[string]*Candidate) {
	root := filepath.Join(repoRoot, "cmd", "archigraph", "testdata")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	// Build an index of framework-candidate IDs keyed by last slug.
	indexBySlug := map[string][]string{}
	for id := range cands {
		if !strings.Contains(id, ".framework.") {
			continue
		}
		parts := strings.Split(id, ".")
		indexBySlug[parts[len(parts)-1]] = append(indexBySlug[parts[len(parts)-1]], id)
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "audit") {
			continue
		}
		fixtureRoot := filepath.Join(root, e.Name())
		// Each subdirectory inside the audit fixture names a framework.
		subs, err := os.ReadDir(fixtureRoot)
		if err != nil {
			continue
		}
		for _, sub := range subs {
			if !sub.IsDir() {
				continue
			}
			// Strip trailing "_app" / "_pages" / "_api" etc, then try
			// resolving the resulting slug against framework IDs.
			name := sub.Name()
			for _, suf := range []string{"_app", "_pages", "_api", "_service", "_server", "_client"} {
				name = strings.TrimSuffix(name, suf)
			}
			slug := slugifyFramework(name)
			ids := indexBySlug[slug]
			rel := filepath.Join("cmd", "archigraph", "testdata", e.Name(), sub.Name())
			for _, id := range ids {
				addEvidence(cands[id], "test_fixture", rel, "")
			}
		}
	}
}

// enginePatternMatcher walks internal/engine/*.go and matches filenames
// against framework-name + suffix patterns (e.g. spring_routes.go).
func enginePatternMatcher(repoRoot string, cands map[string]*Candidate) {
	dir := filepath.Join(repoRoot, "internal", "engine")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	indexBySlug := map[string][]string{}
	for id := range cands {
		parts := strings.Split(id, ".")
		if len(parts) < 2 {
			continue
		}
		indexBySlug[parts[len(parts)-1]] = append(indexBySlug[parts[len(parts)-1]], id)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		files = append(files, n)
	}
	sort.Strings(files)
	for _, name := range files {
		for _, p := range enginePatternSuffixes {
			if !strings.HasSuffix(name, p.suffix) {
				continue
			}
			prefix := strings.TrimSuffix(name, p.suffix)
			slug := slugifyFramework(prefix)
			ids := indexBySlug[slug]
			rel := filepath.Join("internal", "engine", name)
			if len(ids) == 0 {
				// Standalone framework-named .go file with no
				// matching YAML rule.
				id := "synth." + slug
				c := ensureCandidate(cands, id, "http_framework", "multi", labelize(slug))
				addEvidence(c, "engine_file", rel, p.capability)
				continue
			}
			for _, id := range ids {
				addEvidence(cands[id], "engine_file", rel, p.capability)
			}
		}
	}
}

// inferCapabilities maps the evidence on a candidate into a capability
// map per the heuristics in the issue.
func inferCapabilities(c *Candidate) {
	hasYAML := false
	hasSynth := false
	hasFixture := false
	engineCaps := map[string]bool{}
	hasExtractor := false
	for _, e := range c.Evidence {
		switch e.Kind {
		case "yaml_rule":
			hasYAML = true
		case "synthesizer":
			hasSynth = true
		case "test_fixture":
			hasFixture = true
		case "engine_file":
			if e.Symbol != "" {
				engineCaps[e.Symbol] = true
			}
		case "extractor_dir":
			hasExtractor = true
		case "rules_dir":
			// language-level signal, no specific capability
		}
	}
	if c.InferredCapabilities == nil {
		c.InferredCapabilities = map[string]InferredCapability{}
	}
	switch c.Category {
	case "language":
		status := "partial"
		conf := confidenceLanguagePartial
		if hasExtractor && len(c.Evidence) >= 2 {
			status = "full"
			conf = confidenceLanguageStrong
		}
		c.InferredCapabilities["core_extraction"] = InferredCapability{Status: status, Confidence: conf}
	case "http_framework":
		// endpoint_synthesis: full if YAML+synth, partial if YAML only,
		// full+0.95 if YAML+synth+fixture.
		switch {
		case hasYAML && hasSynth && hasFixture:
			c.InferredCapabilities["endpoint_synthesis"] = InferredCapability{Status: "full", Confidence: confidenceFull}
		case hasYAML && hasSynth:
			c.InferredCapabilities["endpoint_synthesis"] = InferredCapability{Status: "full", Confidence: confidenceStrong}
		case hasYAML && hasFixture:
			c.InferredCapabilities["endpoint_synthesis"] = InferredCapability{Status: "partial", Confidence: confidenceFixtureBoost}
		case hasSynth:
			c.InferredCapabilities["endpoint_synthesis"] = InferredCapability{Status: "full", Confidence: confidenceStrong}
		case hasYAML:
			c.InferredCapabilities["endpoint_synthesis"] = InferredCapability{Status: "partial", Confidence: confidencePartial}
		}
		if hasSynth && hasFixture {
			c.InferredCapabilities["handler_attribution"] = InferredCapability{Status: "full", Confidence: confidenceFull}
		}
	case "orm":
		status := "partial"
		conf := confidencePartial
		if engineCaps["migration_parsing"] {
			c.InferredCapabilities["migration_parsing"] = InferredCapability{Status: "full", Confidence: confidenceStrong}
		}
		c.InferredCapabilities["model_extraction"] = InferredCapability{Status: status, Confidence: conf}
	case "message_broker":
		c.InferredCapabilities["consumer_extraction"] = InferredCapability{Status: "partial", Confidence: confidencePartial}
		c.InferredCapabilities["producer_extraction"] = InferredCapability{Status: "partial", Confidence: confidencePartial}
	}
	// engine_file evidence boosts category-specific capabilities.
	if engineCaps["model_extraction"] {
		c.InferredCapabilities["model_extraction"] = InferredCapability{Status: "full", Confidence: confidenceStrong}
	}
	if engineCaps["auth_coverage"] {
		c.InferredCapabilities["auth_coverage"] = InferredCapability{Status: "full", Confidence: confidenceStrong}
	}
}

// MergeWithRegistry combines discovered candidates with the existing
// registry, producing the final DiscoverResult. reg may be nil.
func MergeWithRegistry(discovered map[string]*Candidate, reg *Registry, repoRoot string) DiscoverResult {
	ids := make([]string, 0, len(discovered))
	for id := range discovered {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	regByID := map[string]*Record{}
	if reg != nil {
		for i := range reg.Records {
			r := reg.Records[i]
			regByID[r.ID] = &r
		}
	}
	proposal := make([]Candidate, 0, len(ids))
	inRegistry := 0
	newCands := 0
	for _, id := range ids {
		c := discovered[id]
		// Stable evidence ordering.
		sort.SliceStable(c.Evidence, func(i, j int) bool {
			if c.Evidence[i].Kind != c.Evidence[j].Kind {
				return c.Evidence[i].Kind < c.Evidence[j].Kind
			}
			if c.Evidence[i].Path != c.Evidence[j].Path {
				return c.Evidence[i].Path < c.Evidence[j].Path
			}
			return c.Evidence[i].Symbol < c.Evidence[j].Symbol
		})
		inferCapabilities(c)
		if _, ok := regByID[id]; ok {
			c.AlreadyInRegistry = true
			c.RegistryID = id
			inRegistry++
		} else {
			newCands++
		}
		proposal = append(proposal, *c)
	}
	// Orphans, status_upgrade_candidates, + cite drift.
	var orphans []Orphan
	var statusUpgradeCandidates []StatusUpgradeCandidate
	var drifts []CiteDriftItem
	if reg != nil {
		regIDs := make([]string, 0, len(reg.Records))
		for _, r := range reg.Records {
			regIDs = append(regIDs, r.ID)
		}
		sort.Strings(regIDs)
		for _, id := range regIDs {
			rec := regByID[id]
			c, found := discovered[id]
			if !found {
				// Record has no discovered code evidence. Classification depends on status.
				// Only records with status full/partial are orphans; status=missing is intentional.
				isOrphan := false
				for _, cap := range rec.Capabilities {
					if cap.Status == StatusFull || cap.Status == StatusPartial {
						isOrphan = true
						break
					}
				}
				if isOrphan {
					orphans = append(orphans, Orphan{ID: id, Reason: "no code-side evidence found"})
				}
				// Records with status=missing and no evidence are intentional (aspirational) — skip.
				continue
			}
			// Record has discovered code evidence. Check if it should be a status_upgrade_candidate.
			isStatusUpgradeCandidate := true
			suggestedStatus := "partial"
			for _, cap := range rec.Capabilities {
				if cap.Status != StatusMissing {
					isStatusUpgradeCandidate = false
					break
				}
			}
			if isStatusUpgradeCandidate && len(rec.Capabilities) > 0 {
				// All non-empty capabilities have status=missing, and we found evidence.
				// Suggest partial (conservative) unless evidence suggests full.
				if len(c.Evidence) > 3 {
					// Heuristic: multiple evidence sources suggest fuller support
					suggestedStatus = "partial" // conservative; humans refine
				}
				evidencePaths := []string{}
				seenEv := map[string]bool{}
				for _, e := range c.Evidence {
					if e.Path == "" || seenEv[e.Path] {
						continue
					}
					seenEv[e.Path] = true
					evidencePaths = append(evidencePaths, e.Path)
				}
				sort.Strings(evidencePaths)
				statusUpgradeCandidates = append(statusUpgradeCandidates, StatusUpgradeCandidate{
					ID:              id,
					CurrentStatus:   StatusMissing,
					EvidenceFound:   evidencePaths,
					SuggestedStatus: suggestedStatus,
				})
			}
			// Cite drift: any cite listed in the registry that does not
			// exist on disk (resolved relative to repoRoot).
			stale := []string{}
			seen := map[string]bool{}
			for _, cap := range rec.Capabilities {
				for _, cite := range cap.Cites {
					if seen[cite] {
						continue
					}
					seen[cite] = true
					full := filepath.Join(repoRoot, cite)
					if _, err := os.Stat(full); err != nil {
						stale = append(stale, cite)
					}
				}
			}
			if len(stale) > 0 {
				sort.Strings(stale)
				discoveredCites := []string{}
				seenD := map[string]bool{}
				for _, e := range c.Evidence {
					if e.Path == "" || seenD[e.Path] {
						continue
					}
					seenD[e.Path] = true
					discoveredCites = append(discoveredCites, e.Path)
				}
				sort.Strings(discoveredCites)
				drifts = append(drifts, CiteDriftItem{
					ID:              id,
					StaleCites:      stale,
					DiscoveredCites: discoveredCites,
				})
			}
		}
	}
	return DiscoverResult{
		Proposal:                 proposal,
		OrphansInRegistry:        orphans,
		StatusUpgradeCandidates:  statusUpgradeCandidates,
		CiteDrift:                drifts,
		Summary: DiscoverSummary{
			ProposalTotal:           len(proposal),
			InRegistry:              inRegistry,
			NewCandidates:           newCands,
			Orphans:                 len(orphans),
			StatusUpgradeCandidates: len(statusUpgradeCandidates),
			CitesDrifted:            len(drifts),
		},
	}
}

// cmdDiscover wires the subcommand. Defaults: --json on for non-tty stdout,
// off otherwise; --registry docs/coverage/registry.json; orphans+drift+upgrades on.
func cmdDiscover(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	registry := fs.String("registry", defaultRegistryPath, "path to the registry JSON")
	repoRoot := fs.String("repo-root", ".", "repository root to walk")
	asJSON := fs.Bool("json", isNonTTY(out), "emit machine-readable JSON (default true for non-tty)")
	includeOrphans := fs.Bool("include-orphans", true, "include orphan records in the output")
	includeUpgrades := fs.Bool("include-upgrades", true, "include status-upgrade candidates in the output")
	includeDrift := fs.Bool("include-drift", true, "include cite-drift records in the output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	res, err := Discover(*repoRoot, *registry)
	if err != nil {
		return err
	}
	if !*includeOrphans {
		res.OrphansInRegistry = nil
		res.Summary.Orphans = 0
	}
	if !*includeUpgrades {
		res.StatusUpgradeCandidates = nil
		res.Summary.StatusUpgradeCandidates = 0
	}
	if !*includeDrift {
		res.CiteDrift = nil
		res.Summary.CitesDrifted = 0
	}
	if *asJSON {
		return writeDiscoverJSON(out, res)
	}
	writeDiscoverText(out, res)
	return nil
}

// isNonTTY returns true when w is not connected to a terminal. Used so
// piping discover into a JSON consumer Just Works without --json.
func isNonTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return true
	}
	fi, err := f.Stat()
	if err != nil {
		return true
	}
	return (fi.Mode() & os.ModeCharDevice) == 0
}

// writeDiscoverJSON marshals res deterministically (indent + sorted maps)
// and writes it to w with a trailing newline.
func writeDiscoverJSON(w io.Writer, res DiscoverResult) error {
	// Encode capability map keys in sorted order by marshalling through
	// an intermediate ordered representation.
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(orderedDiscover(res))
}

// orderedDiscover wraps each Candidate's InferredCapabilities in a value
// whose MarshalJSON emits keys in sorted order, preserving determinism
// regardless of Go map-iteration ordering.
func orderedDiscover(res DiscoverResult) DiscoverResult {
	for i := range res.Proposal {
		if len(res.Proposal[i].InferredCapabilities) == 0 {
			continue
		}
		res.Proposal[i].InferredCapabilities = sortedMap(res.Proposal[i].InferredCapabilities)
	}
	return res
}

// sortedMap copies m into a fresh map. encoding/json sorts string keys
// lexicographically on marshal, so this is mostly future-proofing — but
// it also ensures the in-memory snapshot is the same shape every run.
func sortedMap(m map[string]InferredCapability) map[string]InferredCapability {
	out := make(map[string]InferredCapability, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}

// writeDiscoverText emits a compact human-readable report.
func writeDiscoverText(w io.Writer, res DiscoverResult) {
	fmt.Fprintf(w, "discover: %d candidates (%d in registry, %d new), %d orphans, %d status_upgrade_candidates, %d drifted\n",
		res.Summary.ProposalTotal, res.Summary.InRegistry, res.Summary.NewCandidates,
		res.Summary.Orphans, res.Summary.StatusUpgradeCandidates, res.Summary.CitesDrifted)
	if res.Summary.NewCandidates > 0 {
		fmt.Fprintln(w, "\nnew candidates:")
		for _, c := range res.Proposal {
			if c.AlreadyInRegistry {
				continue
			}
			fmt.Fprintf(w, "  + %-48s evidence=%d\n", c.CandidateID, len(c.Evidence))
		}
	}
	if len(res.OrphansInRegistry) > 0 {
		fmt.Fprintln(w, "\norphans (in registry with status full/partial, no code evidence):")
		for _, o := range res.OrphansInRegistry {
			fmt.Fprintf(w, "  - %s (%s)\n", o.ID, o.Reason)
		}
	}
	if len(res.StatusUpgradeCandidates) > 0 {
		fmt.Fprintln(w, "\nstatus_upgrade_candidates (status=missing, but code evidence found):")
		for _, s := range res.StatusUpgradeCandidates {
			fmt.Fprintf(w, "  ^ %s (current: %s -> suggested: %s)\n", s.ID, s.CurrentStatus, s.SuggestedStatus)
			for _, e := range s.EvidenceFound {
				fmt.Fprintf(w, "      evidence: %s\n", e)
			}
		}
	}
	if len(res.CiteDrift) > 0 {
		fmt.Fprintln(w, "\ncite drift:")
		for _, d := range res.CiteDrift {
			fmt.Fprintf(w, "  ~ %s\n", d.ID)
			for _, s := range d.StaleCites {
				fmt.Fprintf(w, "      stale: %s\n", s)
			}
			for _, ds := range d.DiscoveredCites {
				fmt.Fprintf(w, "      found: %s\n", ds)
			}
		}
	}
}
