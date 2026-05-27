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
	"javascript_typescript": "jsts",
	"objective_c":           "objc",
}

// frameworkSlugAliases canonicalises yaml-file-derived framework slugs to
// the registry's shortened convention (e.g. "actix-web" → "actix",
// "ruby-on-rails" → "rails"). Applied per (langSlug, categorySegment).
// The key is "<langSlug>.<seg>.<file-derived-slug>", the value is the
// registry's expected slug for that ID.
var frameworkSlugAliases = map[string]string{
	"rust.framework.actix-web":              "actix",
	"ruby.framework.ruby-on-rails":          "rails",
	"ruby.orm.rom-rb-ruby-object-mapper":    "rom-rb",
	"python.orm.django-orm":                 "django",
	"python.orm.pony-orm":                   "pony",
	"python.orm.tortoise-orm":               "tortoise",
	"ruby.orm.datamapper-hanami-model-legacy": "datamapper",
	"php.orm.doctrine-orm":                  "doctrine",
	"php.orm.eloquent-laravel":              "eloquent",
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
	buildWalker(repoRoot, cands)
	ciWalker(repoRoot, cands)
	observabilityWalker(repoRoot, cands)
	brokerWalker(repoRoot, cands)
	containerWalker(repoRoot, cands)
	iacWalker(repoRoot, cands)
	databaseWalker(repoRoot, cands)
	configWalker(repoRoot, cands)
	messagingWalker(repoRoot, cands)
	packageManifestWalker(repoRoot, cands)
	infraResourceWalker(repoRoot, cands)
	protocolWalker(repoRoot, cands)
	securityWalker(repoRoot, cands)
	testFrameworkWalker(repoRoot, cands)
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
				if alias, ok := frameworkSlugAliases[langSlug+"."+seg+"."+slug]; ok {
					slug = alias
				}
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

// buildBuilderSlugs is the closed set of build/package-manager names the
// buildWalker recognises in extractor code or build_tools.yaml rule files.
// Each entry is normalised to its registry-side slug (so "Go modules" → "go-modules").
// Sorted by registry slug; keep alphabetic for review.
var buildBuilderSlugs = []struct {
	slug    string   // registry id segment: build.<slug>
	needles []string // case-insensitive match tokens
}{
	{"bazel", []string{"bazel", "WORKSPACE", "BUILD.bazel"}},
	{"bun", []string{"bun.lockb", "bunfig.toml"}},
	{"bundler", []string{"Gemfile", "bundler"}},
	{"cargo", []string{"Cargo.toml", "cargo"}},
	{"composer", []string{"composer.json", "composer.lock", "composer"}},
	{"dockerfile", []string{"Dockerfile", "dockerfile"}},
	{"dotnet", []string{"dotnet", ".csproj", "nuget"}},
	{"esbuild", []string{"esbuild"}},
	{"flit", []string{"flit"}},
	{"go-modules", []string{"go.mod", "go.sum", "go_mod", "gomod"}},
	{"gradle", []string{"build.gradle", "gradle"}},
	{"hatch", []string{"hatchling", "[tool.hatch"}},
	{"hex", []string{"hex.pm", "mix.exs"}},
	{"justfile", []string{"justfile", "Justfile"}},
	{"lerna", []string{"lerna.json", "lerna"}},
	{"mage", []string{"magefile"}},
	{"makefile", []string{"Makefile", "makefile"}},
	{"maven", []string{"pom.xml", "maven"}},
	{"mill", []string{"build.sc", "mill"}},
	{"mix", []string{"mix.exs", "mix.lock"}},
	{"npm", []string{"package.json", "package-lock.json", "npm"}},
	{"nuget", []string{"nuget", "packages.config"}},
	{"nx", []string{"nx.json"}},
	{"parcel", []string{"parcel"}},
	{"pip", []string{"requirements.txt", "pip"}},
	{"pipenv", []string{"Pipfile", "Pipfile.lock"}},
	{"pnpm", []string{"pnpm-lock.yaml", "pnpm-workspace", "pnpm"}},
	{"poetry", []string{"poetry.lock", "[tool.poetry"}},
	{"rake", []string{"Rakefile", "rake"}},
	{"rollup", []string{"rollup.config", "rollup"}},
	{"sbt", []string{"build.sbt", "sbt"}},
	{"setuptools", []string{"setup.py", "setup.cfg"}},
	{"swift-pm", []string{"Package.swift"}},
	{"task", []string{"Taskfile.yml"}},
	{"turborepo", []string{"turbo.json"}},
	{"uv", []string{"uv.lock"}},
	{"vite", []string{"vite.config", "vitejs"}},
	{"webpack", []string{"webpack.config", "webpack"}},
	{"yarn", []string{"yarn.lock", "yarn"}},
}

// buildWalker walks per-language build_tools.yaml rule files plus the
// build-related extractors and records evidence for the canonical build.*
// candidate IDs from buildBuilderSlugs.
func buildWalker(repoRoot string, cands map[string]*Candidate) {
	// 1. Per-language build_tools.yaml rule files (and build_tools/ dirs).
	rulesRoot := filepath.Join(repoRoot, "internal", "engine", "rules")
	langs, _ := os.ReadDir(rulesRoot)
	langNames := make([]string, 0, len(langs))
	for _, l := range langs {
		if l.IsDir() && !strings.HasPrefix(l.Name(), "_") {
			langNames = append(langNames, l.Name())
		}
	}
	sort.Strings(langNames)
	for _, lang := range langNames {
		// Flat file: rules/<lang>/build_tools.yaml
		flat := filepath.Join(rulesRoot, lang, "build_tools.yaml")
		if data, err := readFileCapped(flat, 256*1024); err == nil {
			rel := filepath.Join("internal", "engine", "rules", lang, "build_tools.yaml")
			scanBuildTools(data, rel, cands)
		}
		// Directory form: rules/<lang>/build_tools/*.yaml
		dir := filepath.Join(rulesRoot, lang, "build_tools")
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(files))
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".yaml") {
				names = append(names, f.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			rel := filepath.Join("internal", "engine", "rules", lang, "build_tools", n)
			full := filepath.Join(dir, n)
			data, err := readFileCapped(full, 256*1024)
			if err != nil {
				continue
			}
			scanBuildTools(data, rel, cands)
		}
	}
	// 2. Dedicated build-system extractor directories.
	buildExtractors := []struct {
		dirName string
		slug    string
	}{
		{"bazel", "bazel"},
		{"dockerfile", "dockerfile"},
		{"just", "justfile"},
	}
	for _, be := range buildExtractors {
		dir := filepath.Join(repoRoot, "internal", "extractors", be.dirName)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		id := "build." + be.slug
		c := ensureCandidate(cands, id, "build_system", "multi", labelize(be.slug))
		rel := filepath.Join("internal", "extractors", be.dirName)
		addEvidence(c, "extractor_dir", rel, "")
	}
	// 3. Sweep config/manifest extractor source files for build-tool tokens.
	scanFilesForBuildTokens(repoRoot, cands)
}

// scanBuildTools reads one build_tools.yaml payload and emits candidates
// for each canonical build slug whose detection tokens appear in the file.
func scanBuildTools(data []byte, rel string, cands map[string]*Candidate) {
	text := strings.ToLower(string(data))
	for _, b := range buildBuilderSlugs {
		hit := false
		for _, n := range b.needles {
			if strings.Contains(text, strings.ToLower(n)) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		id := "build." + b.slug
		c := ensureCandidate(cands, id, "build_system", "multi", labelize(b.slug))
		addEvidence(c, "yaml_rule", rel, "")
	}
}

// scanFilesForBuildTokens walks a small set of extractor source files
// looking for build-tool name tokens. Used to attach evidence to build.*
// candidates that have no yaml rule but DO have extractor code.
func scanFilesForBuildTokens(repoRoot string, cands map[string]*Candidate) {
	targets := []string{
		filepath.Join("internal", "extractors", "cross", "manifest", "extractor.go"),
		filepath.Join("internal", "extractors", "config", "discover.go"),
		filepath.Join("internal", "extractors", "golang", "gomod.go"),
	}
	for _, rel := range targets {
		full := filepath.Join(repoRoot, rel)
		data, err := readFileCapped(full, 256*1024)
		if err != nil {
			continue
		}
		text := string(data)
		lower := strings.ToLower(text)
		for _, b := range buildBuilderSlugs {
			hit := false
			for _, n := range b.needles {
				if strings.Contains(lower, strings.ToLower(n)) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
			id := "build." + b.slug
			c := ensureCandidate(cands, id, "build_system", "multi", labelize(b.slug))
			addEvidence(c, "extractor_source", rel, "")
		}
	}
}

// readFileCapped reads up to maxBytes from path. Returns the raw bytes
// or an error. Capping is purely defensive against pathological inputs.
func readFileCapped(path string, maxBytes int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	lim := io.LimitReader(f, int64(maxBytes))
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := lim.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}

// ciSlugs maps cicd/frameworks/<file>.yaml basenames to registry slugs.
// The registry uses short slugs (e.g. "ci.gitlab") that don't always
// match the yaml file basename (e.g. "gitlab_ci.yaml") — so map both ways.
var ciSlugs = []struct {
	yamlBase string // basename without extension under cicd/frameworks/
	slug     string // registry id segment: ci.<slug>
}{
	{"azure_pipelines", "azure-pipelines"},
	{"bitbucket_pipelines", "bitbucket"},
	{"buildkite", "buildkite"},
	{"circleci", "circleci"},
	{"drone", "drone"},
	{"github_actions", "github-actions"},
	{"gitlab_ci", "gitlab"},
	{"jenkins", "jenkins"},
	{"travis_ci", "travis"},
}

// ciWalker scans the CI/CD rule directory and the YAML extractor for
// CI-system signatures.
func ciWalker(repoRoot string, cands map[string]*Candidate) {
	// Build a basename -> slug index keyed by lowercase yaml-base.
	bySlug := map[string]string{}
	for _, c := range ciSlugs {
		bySlug[c.yamlBase] = c.slug
	}
	// 1. cicd/frameworks/<name>.yaml -> ci.<slug>
	dir := filepath.Join(repoRoot, "internal", "engine", "rules", "cicd", "frameworks")
	if entries, err := os.ReadDir(dir); err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			base := strings.TrimSuffix(n, ".yaml")
			slug, ok := bySlug[base]
			if !ok {
				slug = slugifyFramework(base)
			}
			id := "ci." + slug
			c := ensureCandidate(cands, id, "ci_system", "multi", labelize(slug))
			rel := filepath.Join("internal", "engine", "rules", "cicd", "frameworks", n)
			addEvidence(c, "yaml_rule", rel, "")
		}
	}
	// 2. cicd/_manifest.yaml + language.yaml may reference systems even
	// if no per-framework yaml exists yet (e.g. Jenkins).
	for _, rel := range []string{
		filepath.Join("internal", "engine", "rules", "cicd", "_manifest.yaml"),
		filepath.Join("internal", "engine", "rules", "cicd", "language.yaml"),
	} {
		data, err := readFileCapped(filepath.Join(repoRoot, rel), 256*1024)
		if err != nil {
			continue
		}
		lower := strings.ToLower(string(data))
		for _, c := range ciSlugs {
			if strings.Contains(lower, c.yamlBase) || strings.Contains(lower, c.slug) {
				id := "ci." + c.slug
				cand := ensureCandidate(cands, id, "ci_system", "multi", labelize(c.slug))
				addEvidence(cand, "yaml_rule", rel, "")
			}
		}
	}
	// 3. YAML extractor source file: the file mentions each CI system by
	// path pattern (".github/workflows", ".gitlab-ci", "Jenkinsfile", etc.)
	yamlExtractorRel := filepath.Join("internal", "extractors", "yaml", "extractor.go")
	if data, err := readFileCapped(filepath.Join(repoRoot, yamlExtractorRel), 256*1024); err == nil {
		lower := strings.ToLower(string(data))
		patterns := map[string]string{
			"github-actions":  ".github/workflows",
			"gitlab":          ".gitlab-ci",
			"circleci":        ".circleci",
			"travis":          ".travis",
			"azure-pipelines": "azure-pipelines",
			"bitbucket":       "bitbucket-pipelines",
			"jenkins":         "jenkinsfile",
			"drone":           ".drone",
			"buildkite":       "buildkite",
		}
		keys := make([]string, 0, len(patterns))
		for k := range patterns {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if strings.Contains(lower, patterns[k]) {
				id := "ci." + k
				c := ensureCandidate(cands, id, "ci_system", "multi", labelize(k))
				addEvidence(c, "extractor_source", yamlExtractorRel, "")
			}
		}
	}
}

// observabilitySlugs maps registry slugs to source-text needles for the
// observabilityWalker. The walker only scans extractor + engine code, so
// the needles are import paths and SDK function names.
var observabilitySlugs = []struct {
	slug    string
	needles []string
}{
	{"datadog", []string{"datadog", "dd-trace", "ddtrace"}},
	{"elastic-apm", []string{"elastic-apm", "elasticapm"}},
	{"grafana-loki", []string{"grafana/loki", "grafana-loki", "loki-client"}},
	{"honeycomb", []string{"honeycombio", "honeycomb-beeline", "honeycomb"}},
	{"newrelic", []string{"newrelic", "newrelic-agent", "newrelic.agent"}},
	{"opentelemetry", []string{"opentelemetry", "go.opentelemetry.io/otel", "@opentelemetry/"}},
	{"prometheus", []string{"prometheus/client_golang", "prom_client", "prometheus-client", "prometheus.io"}},
	{"sentry", []string{"sentry-sdk", "@sentry/", "getsentry/sentry-go", "sentry-go", "raven"}},
}

// observabilityWalker scans engine + extractor sources for known
// observability SDK signatures and emits infra.observability.<slug>
// candidates.
func observabilityWalker(repoRoot string, cands map[string]*Candidate) {
	files := collectSourceFiles(repoRoot, []string{
		filepath.Join("internal", "engine"),
		filepath.Join("internal", "extractors"),
	})
	for _, rel := range files {
		full := filepath.Join(repoRoot, rel)
		data, err := readFileCapped(full, 256*1024)
		if err != nil {
			continue
		}
		lower := strings.ToLower(string(data))
		for _, o := range observabilitySlugs {
			hit := false
			for _, n := range o.needles {
				if strings.Contains(lower, strings.ToLower(n)) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
			id := "infra.observability." + o.slug
			c := ensureCandidate(cands, id, "observability", "multi", labelize(o.slug))
			addEvidence(c, "extractor_source", rel, "")
		}
	}
	// Engine-rule yaml files commonly enumerate logging-config patterns.
	loggingRel := filepath.Join("internal", "engine", "rules", "_engine", "logging_config_extractor.yaml")
	if _, err := os.Stat(filepath.Join(repoRoot, loggingRel)); err == nil {
		c := ensureCandidate(cands, "infra.observability.logging-config", "observability", "multi", "Logging Config")
		addEvidence(c, "yaml_rule", loggingRel, "")
	}
}

// brokerSlugs maps engine-file basenames to registry slugs for messaging
// infrastructure. brokerWalker scans internal/engine/ for these names.
var brokerSlugs = []struct {
	slug      string
	fileBases []string // engine file basenames (without _test.go)
	imports   []string // additional source-text needles (lowercased)
}{
	{"amqp", []string{"rabbitmq_edges.go", "amqp_edges.go"}, []string{"amqp091", "amqp.dial"}},
	{"azure-service-bus", []string{"azure_service_bus_edges.go"}, []string{"azservicebus", "servicebus-messaging"}},
	{"cloudevents", []string{"event_bus_edges.go", "cloudevents_edges.go"}, []string{"cloudevents"}},
	{"debezium", []string{"debezium_cdc_edges.go"}, []string{"debezium"}},
	{"eventbridge", []string{"event_bus_edges.go", "eventbridge_edges.go"}, []string{"eventbridge"}},
	{"eventgrid", []string{"event_bus_edges.go", "eventgrid_edges.go"}, []string{"eventgrid"}},
	{"gcp-pubsub", []string{"pubsub_edges.go"}, []string{"cloud.google.com/go/pubsub"}},
	{"kafka", []string{"kafka_edges.go", "kafka_wrapper_edges.go"}, []string{"sarama", "confluent-kafka", "kafkajs"}},
	{"mqtt", []string{"event_bus_edges.go", "mqtt_edges.go"}, []string{"paho.mqtt", "mqtt-client"}},
	{"nats", []string{"nats_edges.go"}, []string{"nats.go", "nats-io"}},
	{"pulsar", []string{"pulsar_edges.go"}, []string{"apache/pulsar", "pulsar-client"}},
	{"rabbitmq", []string{"rabbitmq_edges.go"}, []string{"amqplib", "amqp091"}},
	{"redis", []string{"redis_pubsub_edges.go"}, []string{"redis.publish", "redis-pubsub"}},
	{"sns", []string{"iac_sns_edges.go", "sns_edges.go"}, []string{"aws-sdk/sns", "awssdk.sns"}},
	{"sqs", []string{"sqs_edges.go"}, []string{"aws-sdk/sqs", "awssdk.sqs"}},
}

// brokerWalker scans internal/engine/ for broker-specific *_edges.go files
// (deterministic filename-based evidence) and emits infra.broker.<slug>
// candidates that mirror the registry's msg.broker.<slug> taxonomy.
func brokerWalker(repoRoot string, cands map[string]*Candidate) {
	engineDir := filepath.Join(repoRoot, "internal", "engine")
	entries, err := os.ReadDir(engineDir)
	if err != nil {
		return
	}
	existing := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			existing[e.Name()] = true
		}
	}
	for _, b := range brokerSlugs {
		for _, base := range b.fileBases {
			if !existing[base] {
				continue
			}
			rel := filepath.Join("internal", "engine", base)
			id := "msg.broker." + b.slug
			c := ensureCandidate(cands, id, "message_broker", "multi", labelize(b.slug))
			addEvidence(c, "engine_file", rel, "")
		}
	}
	// Engine-flow scaffolding files reference observability + broker SDK
	// names — pick those out so msg.broker.kafka can still resolve via
	// import-name evidence even when the .go file is named generically.
	scaffoldFiles := []string{
		filepath.Join("internal", "engine", "event_bus_edges.go"),
		filepath.Join("internal", "engine", "event_flow.go"),
	}
	for _, rel := range scaffoldFiles {
		data, err := readFileCapped(filepath.Join(repoRoot, rel), 256*1024)
		if err != nil {
			continue
		}
		lower := strings.ToLower(string(data))
		for _, b := range brokerSlugs {
			for _, n := range b.imports {
				if strings.Contains(lower, strings.ToLower(n)) {
					id := "msg.broker." + b.slug
					c := ensureCandidate(cands, id, "message_broker", "multi", labelize(b.slug))
					addEvidence(c, "extractor_source", rel, "")
					break
				}
			}
		}
	}
}

// containerSlugs maps docker / kubernetes / helm rule filenames to the
// registry's infra.container.<slug> ids.
var containerSlugs = []struct {
	relPath string // repo-relative path to the yaml rule
	slug    string // registry id segment
}{
	{filepath.Join("internal", "engine", "rules", "docker", "frameworks", "dockerfile.yaml"), "dockerfile"},
	{filepath.Join("internal", "engine", "rules", "docker", "frameworks", "docker_compose.yaml"), "docker-compose"},
	{filepath.Join("internal", "engine", "rules", "kubernetes", "frameworks", "kubernetes_manifests.yaml"), "kubernetes"},
}

// containerWalker emits infra.container.<slug> candidates for container
// orchestration rule files and dedicated extractor directories.
func containerWalker(repoRoot string, cands map[string]*Candidate) {
	for _, cs := range containerSlugs {
		full := filepath.Join(repoRoot, cs.relPath)
		if _, err := os.Stat(full); err != nil {
			continue
		}
		id := "infra.container." + cs.slug
		c := ensureCandidate(cands, id, "container", "multi", labelize(cs.slug))
		addEvidence(c, "yaml_rule", cs.relPath, "")
	}
	// Dockerfile extractor — dedicated directory.
	if _, err := os.Stat(filepath.Join(repoRoot, "internal", "extractors", "dockerfile")); err == nil {
		c := ensureCandidate(cands, "infra.container.dockerfile", "container", "multi", "Dockerfile")
		addEvidence(c, "extractor_dir", filepath.Join("internal", "extractors", "dockerfile"), "")
	}
	// YAML extractor recognises helm chart / k8s / docker-compose files;
	// scan the extractor source for those tokens.
	yamlExtractorRel := filepath.Join("internal", "extractors", "yaml", "extractor.go")
	data, err := readFileCapped(filepath.Join(repoRoot, yamlExtractorRel), 256*1024)
	if err == nil {
		lower := strings.ToLower(string(data))
		tokens := map[string]string{
			"docker-compose": "docker-compose",
			"helm":           "helm",
			"kubernetes":     "kubernetes",
			"kustomize":      "kustomize",
		}
		keys := make([]string, 0, len(tokens))
		for k := range tokens {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if strings.Contains(lower, tokens[k]) {
				id := "infra.container." + k
				c := ensureCandidate(cands, id, "container", "multi", labelize(k))
				addEvidence(c, "extractor_source", yamlExtractorRel, "")
			}
		}
	}
}

// iacSlugs maps the IaC rule-tree directories (and well-known engine
// files) to registry slugs.
var iacRuleDirs = []struct {
	dir  string // immediate child of internal/engine/rules
	slug string
}{
	{"ansible", "ansible"},
	{"cdk", "cdk"},
	{"hcl", "terraform"}, // hcl tree carries terraform/opentofu/etc.
	{"pulumi", "pulumi"},
}

// iacWalker emits infra.iac.<slug> candidates for IaC engine rule trees
// plus serverless / cloudformation evidence pulled from engine sources.
func iacWalker(repoRoot string, cands map[string]*Candidate) {
	for _, dir := range iacRuleDirs {
		rel := filepath.Join("internal", "engine", "rules", dir.dir, "_manifest.yaml")
		if _, err := os.Stat(filepath.Join(repoRoot, rel)); err != nil {
			continue
		}
		id := "infra.iac." + dir.slug
		c := ensureCandidate(cands, id, "iac", "multi", labelize(dir.slug))
		addEvidence(c, "yaml_rule", rel, "")
	}
	// hcl extractor → terraform evidence.
	hclExtractorRel := filepath.Join("internal", "extractors", "hcl", "extractor.go")
	if _, err := os.Stat(filepath.Join(repoRoot, hclExtractorRel)); err == nil {
		c := ensureCandidate(cands, "infra.iac.terraform", "iac", "multi", "Terraform")
		addEvidence(c, "extractor_source", hclExtractorRel, "")
	}
	// serverless-framework + cloudformation: look in engine sources by name.
	engineRel := func(name string) string {
		return filepath.Join("internal", "engine", name)
	}
	known := []struct {
		file string
		slug string
	}{
		{"serverless_edges.go", "serverless-framework"},
		{"cloudformation_edges.go", "cloudformation"},
		{"bicep_edges.go", "bicep"},
	}
	for _, k := range known {
		if _, err := os.Stat(filepath.Join(repoRoot, engineRel(k.file))); err != nil {
			continue
		}
		id := "infra.iac." + k.slug
		c := ensureCandidate(cands, id, "iac", "multi", labelize(k.slug))
		addEvidence(c, "engine_file", engineRel(k.file), "")
	}
	// CloudFormation: scan a small set of engine sources by content.
	cfScanFiles := []string{
		filepath.Join("internal", "extractors", "yaml", "extractor.go"),
		filepath.Join("internal", "engine", "iac_sns_edges.go"),
		filepath.Join("internal", "engine", "workflow_edges.go"),
	}
	for _, rel := range cfScanFiles {
		data, err := readFileCapped(filepath.Join(repoRoot, rel), 256*1024)
		if err != nil {
			continue
		}
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, "cloudformation") {
			c := ensureCandidate(cands, "infra.iac.cloudformation", "iac", "multi", "Cloudformation")
			addEvidence(c, "extractor_source", rel, "")
		}
		if strings.Contains(lower, "bicep") {
			c := ensureCandidate(cands, "infra.iac.bicep", "iac", "multi", "Bicep")
			addEvidence(c, "extractor_source", rel, "")
		}
	}
	// HCL extractor sometimes carries CDK metadata.
	if data, err := readFileCapped(filepath.Join(repoRoot, hclExtractorRel), 256*1024); err == nil {
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, "aws-cdk") || strings.Contains(lower, "aws_cdk") {
			c := ensureCandidate(cands, "infra.resource.aws-cdk", "iac", "multi", "Aws Cdk")
			addEvidence(c, "extractor_source", hclExtractorRel, "")
		}
	}
}

// driverSlugByOrmFile maps an ORM yaml-file basename (without .yaml) to a
// canonical driver slug used in lang.<lang>.driver.<slug>. Only ORM files
// that genuinely represent a raw database driver are mapped here; full
// ORMs (alembic, sqlalchemy, ecto, ...) stay on the orm side.
var driverSlugByOrmFile = []struct {
	contains string // substring of the lowercased yaml basename
	slug     string
}{
	{"cassandra", "cassandra"},
	{"xandra", "xandra"},
	{"dynamodb", "dynamodb"},
	{"dynamo", "dynamodb"},
	{"elasticsearch", "elastic"},
	{"elastic", "elastic"},
	{"mongo", "mongodb"},
	{"mongodb", "mongodb"},
	{"myxql", "myxql"},
	{"mysql", "mysql"},
	{"neo4j", "neo4j"},
	{"npgsql", "npgsql"},
	{"pg_", "postgres"},
	{"postgres", "postgres"},
	{"postgrex", "postgrex"},
	{"redix", "redix"},
	{"redis", "redis"},
	{"sqlite", "sqlite"},
	{"supabase", "supabase"},
}

// databaseWalker emits lang.<lang>.driver.<dbname> candidates by walking
// per-language orms/ + drivers/ + databases/ subtrees and matching file
// basenames against driverSlugByOrmFile.
func databaseWalker(repoRoot string, cands map[string]*Candidate) {
	rulesRoot := filepath.Join(repoRoot, "internal", "engine", "rules")
	langs, err := os.ReadDir(rulesRoot)
	if err != nil {
		return
	}
	langNames := make([]string, 0, len(langs))
	for _, l := range langs {
		if l.IsDir() && !strings.HasPrefix(l.Name(), "_") {
			langNames = append(langNames, l.Name())
		}
	}
	sort.Strings(langNames)
	for _, lang := range langNames {
		langSlug := normaliseLanguage(lang)
		// Three possible per-language driver dirs.
		for _, sub := range []string{"orms", "drivers", "databases", "frameworks"} {
			subPath := filepath.Join(rulesRoot, lang, sub)
			files, err := os.ReadDir(subPath)
			if err != nil {
				continue
			}
			names := make([]string, 0, len(files))
			for _, f := range files {
				if !f.IsDir() && strings.HasSuffix(f.Name(), ".yaml") {
					names = append(names, f.Name())
				}
			}
			sort.Strings(names)
			for _, n := range names {
				base := strings.ToLower(strings.TrimSuffix(n, ".yaml"))
				for _, m := range driverSlugByOrmFile {
					if !strings.Contains(base, m.contains) {
						continue
					}
					rel := filepath.Join("internal", "engine", "rules", lang, sub, n)
					id := fmt.Sprintf("lang.%s.driver.%s", langSlug, m.slug)
					c := ensureCandidate(cands, id, "database_driver", langSlug, labelize(m.slug))
					addEvidence(c, "yaml_rule", rel, "")
					// Also emit a top-level db.<slug> candidate. Some
					// driver slugs use language-specific names (npgsql,
					// postgrex, redix, myxql, xandra) — fold those into
					// the canonical db.* family.
					top := dbTopSlug(m.slug)
					if top != "" {
						tid := "db." + top
						tc := ensureCandidate(cands, tid, "database", "multi", labelize(top))
						addEvidence(tc, "yaml_rule", rel, "")
					}
					break // first match wins per file
				}
			}
		}
	}
	// Engine-side orm_queries*.go files cite multiple DB engines by name.
	dbEngineFiles := []string{
		filepath.Join("internal", "engine", "orm_queries.go"),
		filepath.Join("internal", "engine", "orm_queries_other.go"),
		filepath.Join("internal", "engine", "orm_queries_python.go"),
		filepath.Join("internal", "engine", "orm_queries_jsts.go"),
	}
	dbTopNeedles := []struct {
		needle string
		slug   string
	}{
		{"cassandra", "cassandra"},
		{"clickhouse", "clickhouse"},
		{"dynamodb", "dynamodb"},
		{"elasticsearch", "elasticsearch"},
		{"mongodb", "mongodb"},
		{"mongo", "mongodb"},
		{"mysql", "mysql"},
		{"neo4j", "neo4j"},
		{"postgres", "postgres"},
		{"redis", "redis"},
		{"snowflake", "snowflake"},
		{"sqlite", "sqlite"},
	}
	for _, rel := range dbEngineFiles {
		data, err := readFileCapped(filepath.Join(repoRoot, rel), 256*1024)
		if err != nil {
			continue
		}
		lower := strings.ToLower(string(data))
		for _, n := range dbTopNeedles {
			if !strings.Contains(lower, n.needle) {
				continue
			}
			id := "db." + n.slug
			c := ensureCandidate(cands, id, "database", "multi", labelize(n.slug))
			addEvidence(c, "engine_file", rel, "")
		}
	}
	// SQL extractor → db.sqlite signal in particular.
	sqlExtractorRel := filepath.Join("internal", "extractors", "sql")
	if _, err := os.Stat(filepath.Join(repoRoot, sqlExtractorRel)); err == nil {
		c := ensureCandidate(cands, "db.sqlite", "database", "multi", "Sqlite")
		addEvidence(c, "extractor_dir", sqlExtractorRel, "")
	}
}

// dbTopSlug maps a language-specific driver slug to the canonical
// top-level db.<slug> id. Returns "" when no canonical mapping exists
// (e.g. supabase is JSTS-only, no db.supabase record).
func dbTopSlug(driverSlug string) string {
	switch driverSlug {
	case "cassandra", "dynamodb", "mysql", "neo4j", "redis", "sqlite", "mongodb":
		return driverSlug
	case "elastic":
		return "elasticsearch"
	case "postgres", "postgrex", "npgsql":
		return "postgres"
	case "redix":
		return "redis"
	case "myxql":
		return "mysql"
	case "xandra":
		return "cassandra"
	}
	return ""
}

// msgEngineSignals maps engine-file basenames to non-broker messaging
// candidate IDs (e.g. msg.bullmq, msg.celery, msg.django-signals).
var msgEngineSignals = []struct {
	file string // basename of internal/engine/<file>
	id   string // registry record id
}{
	{"scheduled_jobs_edges.go", "msg.bullmq"},
	{"scheduled_jobs_edges.go", "msg.celery"},
	{"scheduled_jobs_edges.go", "msg.sidekiq"},
	{"django_signal_pubsub_edges.go", "msg.django-signals"},
	{"graphql_subscriptions.go", "msg.graphql-subscriptions"},
	{"sse_edges.go", "msg.sse"},
	{"webhooks_edges.go", "msg.webhook"},
	{"websocket_edges.go", "msg.websocket"},
	{"kafka_edges.go", "msg.kafka-streams"},
	{"debezium_cdc_edges.go", "msg.kafka-streams"},
	{"dramatiq_edges.go", "msg.dramatiq"},
}

// messagingWalker emits non-broker msg.* candidates (background-job,
// signals, pubsub-protocol records) by checking for the existence of
// canonical engine source files.
func messagingWalker(repoRoot string, cands map[string]*Candidate) {
	engineDir := filepath.Join(repoRoot, "internal", "engine")
	for _, m := range msgEngineSignals {
		rel := filepath.Join("internal", "engine", m.file)
		if _, err := os.Stat(filepath.Join(engineDir, m.file)); err != nil {
			continue
		}
		c := ensureCandidate(cands, m.id, "messaging", "multi", labelize(strings.TrimPrefix(m.id, "msg.")))
		addEvidence(c, "engine_file", rel, "")
	}
}

// pkgManifestSlugs maps a manifest-source needle to its pkg.<slug> id.
var pkgManifestSlugs = []struct {
	needle string
	slug   string
}{
	{"cargo.toml", "cargo"},
	{"composer.json", "composer"},
	{".csproj", "csproj"},
	{"gemfile", "gemfile"},
	{"go.mod", "go-mod"},
	{"build.gradle", "gradle"},
	{"mix.exs", "mix"},
	{"package.json", "npm"},
	{"pipfile", "pipfile"},
	{"pom.xml", "pom"},
	{"pubspec.yaml", "pubspec"},
	{"pyproject.toml", "pyproject"},
	{"requirements.txt", "requirements"},
	{"build.sbt", "sbt"},
	{"package.swift", "swift-package"},
}

// packageManifestWalker scans the cross-language manifest extractor for
// per-format token signatures and emits pkg.<slug> candidates.
func packageManifestWalker(repoRoot string, cands map[string]*Candidate) {
	rel := filepath.Join("internal", "extractors", "cross", "manifest", "extractor.go")
	data, err := readFileCapped(filepath.Join(repoRoot, rel), 256*1024)
	if err != nil {
		return
	}
	lower := strings.ToLower(string(data))
	for _, p := range pkgManifestSlugs {
		if !strings.Contains(lower, p.needle) {
			continue
		}
		id := "pkg." + p.slug
		c := ensureCandidate(cands, id, "package_manifest", "multi", labelize(p.slug))
		addEvidence(c, "extractor_source", rel, "")
	}
}

// infraResourceWalker emits infra.resource.<slug> candidates for cloud
// resource extractors. Mirrors infra.iac.<slug> but the resource side is
// about per-resource extraction (not whole IaC tooling).
func infraResourceWalker(repoRoot string, cands map[string]*Candidate) {
	known := []struct {
		path string // repo-relative path of the cite
		slug string
	}{
		{filepath.Join("internal", "extractors", "hcl", "extractor.go"), "terraform"},
		{filepath.Join("internal", "extractors", "hcl", "extractor.go"), "aws-cdk"},
		{filepath.Join("internal", "engine", "rules", "kubernetes", "_manifest.yaml"), "kubernetes"},
		{filepath.Join("internal", "engine", "rules", "pulumi", "_manifest.yaml"), "pulumi"},
		{filepath.Join("internal", "engine", "rules", "cdk", "_manifest.yaml"), "aws-cdk"},
	}
	for _, k := range known {
		if _, err := os.Stat(filepath.Join(repoRoot, k.path)); err != nil {
			continue
		}
		id := "infra.resource." + k.slug
		c := ensureCandidate(cands, id, "iac_resource", "multi", labelize(k.slug))
		kind := "extractor_source"
		if strings.HasSuffix(k.path, ".yaml") {
			kind = "yaml_rule"
		}
		addEvidence(c, kind, k.path, "")
	}
	// CloudFormation: scan engine + yaml sources for CF references.
	cfScan := []string{
		filepath.Join("internal", "extractors", "yaml", "extractor.go"),
		filepath.Join("internal", "engine", "iac_sns_edges.go"),
		filepath.Join("internal", "engine", "workflow_edges.go"),
	}
	for _, rel := range cfScan {
		data, err := readFileCapped(filepath.Join(repoRoot, rel), 256*1024)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(string(data)), "cloudformation") {
			c := ensureCandidate(cands, "infra.resource.cloudformation", "iac_resource", "multi", "Cloudformation")
			addEvidence(c, "extractor_source", rel, "")
		}
	}
}

// configFormatSlugs maps a needle that appears in the config extractor
// source to a registry config.<slug> id.
var configFormatSlugs = []struct {
	needle string
	slug   string
}{
	{"docker-compose", "docker-compose"},
	{"docker_compose", "docker-compose"},
	{"dockerfile", "dockerfile"},
	{"dotenv", "dotenv"},
	{".env", "dotenv"},
	{"github-actions", "github-actions"},
	{"github_actions", "github-actions"},
	{"gitlab-ci", "gitlab-ci"},
	{"gitlab_ci", "gitlab-ci"},
	{"ini", "ini"},
	{"jenkins", "jenkins"},
	{"makefile", "makefile"},
	{"pipfile", "pipfile"},
	{"properties", "properties"},
	{"toml", "toml"},
	{"tsconfig", "tsconfig"},
	{"yaml", "yaml"},
}

// configWalker scans the config-discovery extractor for known
// configuration-format tokens and emits config.<slug> candidates.
func configWalker(repoRoot string, cands map[string]*Candidate) {
	sourceRel := filepath.Join("internal", "extractors", "config", "discover.go")
	data, err := readFileCapped(filepath.Join(repoRoot, sourceRel), 256*1024)
	if err == nil {
		lower := strings.ToLower(string(data))
		seen := map[string]bool{}
		// Iterate in sorted slug order for determinism.
		sortedConfigFormats := append([]struct {
			needle string
			slug   string
		}(nil), configFormatSlugs...)
		sort.SliceStable(sortedConfigFormats, func(i, j int) bool {
			return sortedConfigFormats[i].slug < sortedConfigFormats[j].slug
		})
		for _, cf := range sortedConfigFormats {
			if !strings.Contains(lower, cf.needle) {
				continue
			}
			if seen[cf.slug] {
				continue
			}
			seen[cf.slug] = true
			id := "config." + cf.slug
			c := ensureCandidate(cands, id, "config_format", "multi", labelize(cf.slug))
			addEvidence(c, "extractor_source", sourceRel, "")
		}
	}
	// YAML extractor evidence for yaml/docker-compose/gitlab-ci/k8s-manifest.
	yamlExtractorRel := filepath.Join("internal", "extractors", "yaml", "extractor.go")
	if data, err := readFileCapped(filepath.Join(repoRoot, yamlExtractorRel), 256*1024); err == nil {
		lower := strings.ToLower(string(data))
		yamlSignals := []struct {
			needle string
			slug   string
		}{
			{"yaml", "yaml"},
			{"docker-compose", "docker-compose"},
			{"docker_compose", "docker-compose"},
			{"gitlab-ci", "gitlab-ci"},
			{"github-actions", "github-actions"},
		}
		for _, ys := range yamlSignals {
			if !strings.Contains(lower, ys.needle) {
				continue
			}
			id := "config." + ys.slug
			c := ensureCandidate(cands, id, "config_format", "multi", labelize(ys.slug))
			addEvidence(c, "extractor_source", yamlExtractorRel, "")
		}
	}
	// Cross-manifest extractor cites toml + json + xml manifests.
	manifestRel := filepath.Join("internal", "extractors", "cross", "manifest", "extractor.go")
	if data, err := readFileCapped(filepath.Join(repoRoot, manifestRel), 256*1024); err == nil {
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, "toml") {
			c := ensureCandidate(cands, "config.toml", "config_format", "multi", "Toml")
			addEvidence(c, "extractor_source", manifestRel, "")
		}
	}
	// scheduled-jobs engine file cites .github/workflows for cron parsing.
	schedRel := filepath.Join("internal", "engine", "scheduled_jobs_edges.go")
	if data, err := readFileCapped(filepath.Join(repoRoot, schedRel), 256*1024); err == nil {
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, ".github/workflows") || strings.Contains(lower, "github-actions") {
			c := ensureCandidate(cands, "config.github-actions", "config_format", "multi", "Github Actions")
			addEvidence(c, "engine_file", schedRel, "")
		}
	}
}

// protocolSignals maps protocol.<slug> ids to engine-side evidence files.
var protocolSignals = []struct {
	id    string
	files []string
}{
	{"protocol.graphql", []string{
		filepath.Join("internal", "engine", "graphql_subscriptions.go"),
		filepath.Join("internal", "engine", "http_endpoint_match.go"),
	}},
	{"protocol.grpc", []string{
		filepath.Join("internal", "engine", "grpc_edges.go"),
	}},
	{"protocol.openapi", []string{
		filepath.Join("internal", "engine", "rules", "openapi", "language.yaml"),
		filepath.Join("internal", "engine", "http_endpoint_match.go"),
	}},
	{"protocol.protobuf", []string{
		filepath.Join("internal", "engine", "grpc_edges.go"),
		filepath.Join("internal", "extractors", "proto", "proto.go"),
	}},
	{"protocol.soap", []string{
		filepath.Join("internal", "engine", "soap_edges.go"),
	}},
	{"protocol.jsonrpc", []string{
		filepath.Join("internal", "engine", "jsonrpc_edges.go"),
	}},
}

// protocolWalker emits protocol.<slug> candidates by checking for the
// existence of the canonical engine/extractor cite files.
func protocolWalker(repoRoot string, cands map[string]*Candidate) {
	for _, p := range protocolSignals {
		for _, rel := range p.files {
			if _, err := os.Stat(filepath.Join(repoRoot, rel)); err != nil {
				continue
			}
			slug := strings.TrimPrefix(p.id, "protocol.")
			c := ensureCandidate(cands, p.id, "protocol", "multi", labelize(slug))
			kind := "engine_file"
			if strings.HasSuffix(rel, ".yaml") {
				kind = "yaml_rule"
			} else if strings.Contains(rel, "/extractors/") {
				kind = "extractor_source"
			}
			addEvidence(c, kind, rel, "")
		}
	}
}

// securitySignals maps security.<slug> ids to engine-side evidence files.
var securitySignals = []struct {
	id    string
	files []string
}{
	{"security.auth-java", []string{filepath.Join("internal", "engine", "java_auth_policy.go")}},
	{"security.auth.basic", []string{filepath.Join("internal", "engine", "java_auth_policy.go")}},
	{"security.auth.jwt", []string{filepath.Join("internal", "engine", "java_auth_policy.go")}},
	{"security.auth.oauth2", []string{filepath.Join("internal", "engine", "java_auth_policy.go")}},
	{"security.auth.oidc", []string{filepath.Join("internal", "engine", "java_auth_policy.go")}},
	{"security.auth.session", []string{filepath.Join("internal", "engine", "java_auth_policy.go")}},
	{"security.csrf", []string{filepath.Join("internal", "engine", "rules", "_engine", "csrf_heuristic_detector.yaml")}},
	{"security.secrets", []string{filepath.Join("internal", "secrets", "secrets.go")}},
	{"security.sql-injection", []string{filepath.Join("internal", "engine", "rules", "_engine", "sql_injection_detector.yaml")}},
}

// securityWalker emits security.<slug> candidates by checking for the
// existence of the canonical cite files. Defensive: most security
// records cite java_auth_policy.go as a placeholder.
func securityWalker(repoRoot string, cands map[string]*Candidate) {
	for _, s := range securitySignals {
		for _, rel := range s.files {
			if _, err := os.Stat(filepath.Join(repoRoot, rel)); err != nil {
				continue
			}
			slug := strings.TrimPrefix(s.id, "security.")
			c := ensureCandidate(cands, s.id, "security", "multi", labelize(slug))
			kind := "engine_file"
			if strings.HasSuffix(rel, ".yaml") {
				kind = "yaml_rule"
			} else if strings.Contains(rel, "/extractors/") || strings.Contains(rel, "/secrets/") {
				kind = "extractor_source"
			}
			addEvidence(c, kind, rel, "")
		}
	}
}

// testFrameworkSignals maps test.<slug> ids to language + framework yaml
// + tests_edges.go evidence. The walker only needs to confirm the
// per-framework yaml exists OR a generic test_patterns.yaml mentions it.
var testFrameworkSignals = []struct {
	id           string
	frameworkRel string // optional explicit framework yaml
	testPattern  string // language-relative test_patterns.yaml needle
	language     string
}{
	{"test.cargo-test", "", "cargo", "rust"},
	{"test.exunit", "", "exunit", "elixir"},
	{"test.go-testing", "", "testing", "go"},
	{"test.jest", filepath.Join("internal", "engine", "rules", "javascript_typescript", "frameworks", "jest.yaml"), "", "jsts"},
	{"test.junit4", "", "junit", "java"},
	{"test.junit5", filepath.Join("internal", "engine", "rules", "java", "frameworks", "junit5.yaml"), "", "java"},
	{"test.minitest", "", "minitest", "ruby"},
	{"test.mocha", "", "mocha", "jsts"},
	{"test.mstest", "", "mstest", "csharp"},
	{"test.nunit", "", "nunit", "csharp"},
	{"test.phpunit", "", "phpunit", "php"},
	{"test.pytest", filepath.Join("internal", "engine", "rules", "python", "frameworks", "pytest.yaml"), "", "python"},
	{"test.rspec", filepath.Join("internal", "engine", "rules", "ruby", "frameworks", "rspec.yaml"), "", "ruby"},
	{"test.testify", "", "testify", "go"},
	{"test.testng", "", "testng", "java"},
	{"test.unittest", "", "unittest", "python"},
	{"test.vitest", "", "vitest", "jsts"},
	{"test.xunit", "", "xunit", "csharp"},
}

// testFrameworkWalker emits test.<slug> candidates based on per-framework
// yaml files plus a language-level test_patterns.yaml needle scan.
func testFrameworkWalker(repoRoot string, cands map[string]*Candidate) {
	// Map registry language slug back to rule-tree directory.
	langDir := map[string]string{
		"jsts":   "javascript_typescript",
		"objc":   "objective_c",
		"java":   "java",
		"python": "python",
		"ruby":   "ruby",
		"go":     "go",
		"rust":   "rust",
		"csharp": "csharp",
		"elixir": "elixir",
		"php":    "php",
	}
	testsEdgesRel := filepath.Join("internal", "engine", "tests_edges.go")
	testsEdgesExists := false
	if _, err := os.Stat(filepath.Join(repoRoot, testsEdgesRel)); err == nil {
		testsEdgesExists = true
	}
	for _, t := range testFrameworkSignals {
		slug := strings.TrimPrefix(t.id, "test.")
		// 1. explicit framework yaml
		if t.frameworkRel != "" {
			if _, err := os.Stat(filepath.Join(repoRoot, t.frameworkRel)); err == nil {
				c := ensureCandidate(cands, t.id, "test_framework", t.language, labelize(slug))
				addEvidence(c, "yaml_rule", t.frameworkRel, "")
			}
		}
		// 2. test_patterns.yaml needle scan for the framework name
		if t.testPattern != "" {
			dir := langDir[t.language]
			if dir == "" {
				continue
			}
			rel := filepath.Join("internal", "engine", "rules", dir, "test_patterns.yaml")
			if data, err := readFileCapped(filepath.Join(repoRoot, rel), 256*1024); err == nil {
				if strings.Contains(strings.ToLower(string(data)), t.testPattern) {
					c := ensureCandidate(cands, t.id, "test_framework", t.language, labelize(slug))
					addEvidence(c, "yaml_rule", rel, "")
				}
			}
		}
		// 3. tests_edges.go is cited for most test records — attach if the
		// candidate already has any other evidence (avoids polluting all
		// 50+ test.* ids with the same generic cite).
		if testsEdgesExists {
			if c, ok := cands[t.id]; ok && len(c.Evidence) > 0 {
				addEvidence(c, "engine_file", testsEdgesRel, "")
			}
		}
	}
}

// collectSourceFiles walks each rootRel (relative to repoRoot) and
// returns a sorted slice of repo-relative .go source paths (excluding
// *_test.go and vendored/.git/.venv noise).
func collectSourceFiles(repoRoot string, rootRels []string) []string {
	var out []string
	for _, root := range rootRels {
		full := filepath.Join(repoRoot, root)
		// WalkDir's outer error is always nil here: the inner function
		// returns nil on every failure mode (we treat I/O hiccups as
		// "skip this file" rather than as a discovery failure).
		walkErr := filepath.WalkDir(full, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if name == ".git" || name == "vendor" || name == "node_modules" || name == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			n := d.Name()
			if !strings.HasSuffix(n, ".go") {
				return nil
			}
			if strings.HasSuffix(n, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return nil
			}
			out = append(out, rel)
			return nil
		})
		if walkErr != nil {
			// Outer WalkDir returned a non-nil error (only happens if the
			// supplied root is unreadable). Skip this root.
			continue
		}
	}
	sort.Strings(out)
	return out
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
				for _, cap := range rec.AllCapabilities() {
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
			recCaps := rec.AllCapabilities()
			for _, cap := range recCaps {
				if cap.Status != StatusMissing {
					isStatusUpgradeCandidate = false
					break
				}
			}
			if isStatusUpgradeCandidate && len(recCaps) > 0 {
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
			for _, cap := range recCaps {
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
