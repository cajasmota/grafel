// Package docgen provides the Tier 0 fast-path for documentation generation.
//
// Tier 0 renders a SINGLE section for a SINGLE seed entity with a <30 s
// feedback loop. It is the inner iteration harness for prompt-quality work:
// edit a prompt template, run Tier 0, read the diff in SCORE.json, repeat.
//
// Output layout:
//
//	~/.grafel/docs/<group>/.tier0-<RFC3339>/
//	    <entity-id>-<section>.md   — the rendered section
//	    score.json                 — machine-readable quality metrics
//
// Tier 0 does NOT call any external LLM. It builds a deterministic, fully
// resolved "section context" from the local graph — entity record, 1-hop
// neighbourhood, call-graph slice — and writes it as a structured markdown
// stub. The stub is the canonical input for prompt-iteration: a human or a
// follow-up agent can run the section prompt against the stub and compare
// scores between runs.
//
// Section types currently supported (mirrors generate-docs output-templates):
//
//	overview, capabilities, flows, patterns, api, reference-config,
//	reference-dependencies, reference-deployment, reference-scripts,
//	reference-misc, module-readme, glossary, how-to-local-dev
package docgen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

// ValidLLMModes lists the accepted values for the --llm-mode flag.
var ValidLLMModes = []string{"", "emit", "apply"}

// validateLLMMode returns an error when mode is not one of the accepted values.
func validateLLMMode(mode string) error {
	for _, v := range ValidLLMModes {
		if mode == v {
			return nil
		}
	}
	return fmt.Errorf("unknown --llm-mode=%q; valid values: \"\" (default), \"emit\", \"apply\"", mode)
}

// KnownSections is the canonical list accepted by --section.
var KnownSections = []string{
	"overview",
	"capabilities",
	"flows",
	"patterns",
	"api",
	"child-methods",
	"reference-config",
	"reference-dependencies",
	"reference-deployment",
	"reference-scripts",
	"reference-misc",
	"module-readme",
	"glossary",
	"how-to-local-dev",
}

// Score is the machine-readable quality scorecard written next to every Tier 0
// output file.
type Score struct {
	Tier                   int    `json:"tier"`
	Section                string `json:"section"`
	SeedEntity             string `json:"seed_entity"`
	WallTimeMS             int64  `json:"wall_time_ms"`
	TokenCountEstimate     int    `json:"token_count_estimate"`
	MermaidCount           int    `json:"mermaid_count"`
	InternalLinkCount      int    `json:"internal_link_count"`
	InternalLinkUnresolved int    `json:"internal_link_unresolved"`
	Lines                  int    `json:"lines"`
	Words                  int    `json:"words"`
	NeighboursIncluded     int    `json:"neighbours_included"`
	SeedEntityFound        bool   `json:"seed_entity_found"`
	// LLMMode is set to "emit" when the run was invoked with --llm-mode=emit.
	// Empty string means the default deterministic-stub-only mode.
	LLMMode string `json:"llm_mode,omitempty"`
}

// RunOpts contains the resolved inputs for a Tier 0 run.
type RunOpts struct {
	// Group is the grafel group name (resolved from --group or sole group).
	Group string
	// SeedEntityID is the entity ID to render the section for.
	SeedEntityID string
	// Section is one of KnownSections.
	Section string
	// OutputDir overrides the default ~/.grafel/docs/<group>/.tier0-<ts>/
	// location. Useful in tests.
	OutputDir string
	// LLMMode controls the LLM integration mode. Valid values:
	//   "" — default: write stub .md + score.json only (existing behaviour).
	//   "emit" — write stub .md + score.json AND an LLMPromptBundle JSON file.
	//   "apply" — (ticket D) read *-result.json and rebuild with prose fill.
	// Any other value is an error.
	LLMMode string
	// CacheDir overrides the default section-level LLM cache directory:
	//   ~/.grafel/docs/<group>/.llm-cache/
	// Ignored when NoCache is true.
	CacheDir string
	// NoCache disables both cache reads and writes (useful for benchmark /
	// quality-check runs that must not use or pollute the section cache).
	NoCache bool
}

// Run executes a Tier 0 section snippet render and returns the path to the
// output markdown file and its score.
//
// When opts.LLMMode == "emit" the function also writes a sibling
// <entity-id>-<section>-bundle.json containing the LLMPromptBundle for this
// section. The bundle is emitted ALONGSIDE the stub; no LLM is called.
func Run(opts RunOpts) (mdPath string, scoreFile string, score Score, err error) {
	start := time.Now()

	if err = validateSection(opts.Section); err != nil {
		return
	}
	if err = validateLLMMode(opts.LLMMode); err != nil {
		return
	}

	// Resolve the docs output directory.
	outDir := opts.OutputDir
	if outDir == "" {
		outDir, err = defaultOutDir(opts.Group)
		if err != nil {
			return
		}
	}
	if mkErr := os.MkdirAll(outDir, 0o755); mkErr != nil {
		err = fmt.Errorf("create output dir %s: %w", outDir, mkErr)
		return
	}

	// Load the group's graphs and locate the seed entity.
	doc, entity, neighbours, _, _, _, _, err := loadEntityContext(opts.Group, opts.SeedEntityID)
	if err != nil {
		return
	}
	_ = doc // full document available for future richer context

	// Render the section stub.
	md := renderSection(opts.Section, entity, neighbours)

	// Validate mermaid node IDs for uniqueness.
	if vErr := ValidateMermaidNodeIDs(md); vErr != nil {
		err = fmt.Errorf("mermaid validation failed: %w", vErr)
		return
	}

	// Write the markdown file.
	mdFile := filepath.Join(outDir, sanitizeFilename(opts.SeedEntityID)+"-"+opts.Section+".md")
	if wErr := os.WriteFile(mdFile, []byte(md), 0o644); wErr != nil {
		err = fmt.Errorf("write section file: %w", wErr)
		return
	}

	// Build and write the score.
	elapsed := time.Since(start).Milliseconds()
	score = buildScore(opts.Section, opts.SeedEntityID, md, elapsed, len(neighbours), entity != nil)
	score.LLMMode = opts.LLMMode

	scoreBytes, jErr := json.MarshalIndent(score, "", "  ")
	if jErr != nil {
		err = fmt.Errorf("marshal score: %w", jErr)
		return
	}
	scoreFile = filepath.Join(outDir, "score.json")
	if wErr := os.WriteFile(scoreFile, scoreBytes, 0o644); wErr != nil {
		err = fmt.Errorf("write score.json: %w", wErr)
		return
	}

	// --llm-mode=emit: build and persist the LLMPromptBundle alongside the stub.
	if opts.LLMMode == "emit" {
		bundleOpts := BuildBundleOpts{
			RunOpts: opts,
			Tier:    0,
		}
		bundle, bErr := BuildBundle(context.Background(), bundleOpts)
		if bErr != nil {
			err = fmt.Errorf("build llm bundle: %w", bErr)
			return
		}
		bundleBytes, mErr := json.MarshalIndent(bundle, "", "  ")
		if mErr != nil {
			err = fmt.Errorf("marshal llm bundle: %w", mErr)
			return
		}
		bundleFile := filepath.Join(outDir, sanitizeFilename(opts.SeedEntityID)+"-"+opts.Section+"-bundle.json")
		if wErr := os.WriteFile(bundleFile, bundleBytes, 0o644); wErr != nil {
			err = fmt.Errorf("write bundle file: %w", wErr)
			return
		}
	}

	mdPath = mdFile
	return
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func validateSection(s string) error {
	for _, k := range KnownSections {
		if k == s {
			return nil
		}
	}
	return fmt.Errorf("unknown section %q — valid values: %s",
		s, strings.Join(KnownSections, ", "))
}

func defaultOutDir(group string) (string, error) {
	home, err := registry.HomeDir()
	if err != nil {
		return "", err
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	// RFC3339 contains ':' which is not safe on all filesystems.
	ts = strings.NewReplacer(":", "-").Replace(ts)
	return filepath.Join(home, "docs", group, ".tier0-"+ts), nil
}

// NormalizeSeedEntityID strips an optional <group>:: or <repo>:: prefix from
// a seed entity ID and returns the raw 16-char hex. This lets users pass the
// prefixed form returned by grafel_find (e.g. "grafel::7a349f6cd77984c9"
// or "acme-core::7a349f6cd77984c9") directly to --seed-entity without having
// to manually trim the prefix.
//
// Accepted forms (all resolve to the same raw hex):
//   - "7a349f6cd77984c9"               — raw hex (unchanged)
//   - "grafel::7a349f6cd77984c9"   — group-prefixed
//   - "acme-core::7a349f6cd77984c9"  — repo-prefixed
//
// Returns an error when the input contains "::" but the RHS is empty.
func NormalizeSeedEntityID(id string) (string, error) {
	if !strings.Contains(id, "::") {
		return id, nil
	}
	parts := strings.SplitN(id, "::", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", fmt.Errorf("invalid --seed-entity %q: prefixed form must be <group>::<hex> with a non-empty hex part", id)
	}
	return parts[1], nil
}

// normalizeSeedEntityID is the unexported alias used internally.
func normalizeSeedEntityID(id string) (string, error) { return NormalizeSeedEntityID(id) }

// loadEntityContext loads all graphs for the group, finds the seed entity,
// and returns it along with its 1-hop neighbours and the absolute repo root
// path of the seed entity. The returned seedRepo is always an absolute path
// resolved from the fleet config — never a bare slug — so callers can safely
// join it with entity.SourceFile regardless of the working directory.
//
// neighbourKinds is index-aligned with neighbours: neighbourKinds[i] is the
// graph edge `kind` (e.g. "CALLS", "IMPORTS", "CONTAINS", "REFERENCES",
// "DEPENDS_ON", "FK_TO") of the first relationship discovered between the seed
// and neighbours[i]. The kind is preserved verbatim from the graph regardless
// of edge direction so docgen can surface typed relationships in
// NeighbourBrief.Relationship (#1879). When a neighbour is connected through
// multiple relationships, the first edge encountered (in document iteration
// order) wins; this is deterministic for a given graph state, which matches
// the determinism guarantee already documented on this function.
//
// neighbourDirections is index-aligned with neighbours:
// neighbourDirections[i] is NeighbourDirectionOutbound when the seed is the
// source of the edge (seed → neighbour) and NeighbourDirectionInbound when the
// seed is the target (neighbour → seed). This lets callers distinguish inbound
// callers from outbound callees when the edge kind alone is ambiguous (#1965).
func loadEntityContext(group, seedID string) (doc *graph.Document, seed *graph.Entity, neighbours []graph.Entity, neighbourKinds []string, neighbourDirections []string, neighbourProperties []map[string]string, seedRepo string, err error) {
	seedID, err = normalizeSeedEntityID(seedID)
	if err != nil {
		return
	}
	entries, err := findGroupRepoEntries(group)
	if err != nil {
		return
	}

	// Build a combined entity index and relationship index across all repos.
	// repoByEntityID maps entity ID → absolute repo path (from the fleet config)
	// so that callers can always form a valid absolute path to source files,
	// regardless of the current working directory (#1834).
	//
	// We use the fleet config's absRepoPath rather than Document.Repo because
	// Document.Repo stores the indexer's repoTag (a short slug such as
	// "grafel"), not an absolute filesystem path.
	//
	// Backward-compat note: if Document.Repo is already absolute (as in test
	// harnesses that write the full path), we prefer it so existing tests keep
	// working without changes. Otherwise we fall back to absRepoPath.
	byID := make(map[string]*graph.Entity)
	repoByEntityID := make(map[string]string)
	var allRels []graph.Relationship
	for _, entry := range entries {
		d, loadErr := graph.LoadGraphFromDir(entry.stateDir)
		if loadErr != nil {
			// Non-fatal: some repos may not be indexed yet.
			continue
		}
		if doc == nil {
			doc = d // keep first as nominal doc; not critical for Tier 0
		}
		// Resolve the canonical absolute path for this document's repo.
		// If Document.Repo is already absolute (test harnesses write it that
		// way), prefer it; otherwise use the config-derived absolute path.
		absPath := entry.absRepoPath
		if filepath.IsAbs(d.Repo) {
			absPath = d.Repo
		}
		for i := range d.Entities {
			e := d.Entities[i]
			byID[e.ID] = &e
			repoByEntityID[e.ID] = absPath
		}
		allRels = append(allRels, d.Relationships...)
	}

	if len(byID) == 0 {
		err = fmt.Errorf("no indexed repos found for group %q — run `grafel index` first", group)
		return
	}

	// Locate seed entity (exact match first, then prefix match).
	if e, ok := byID[seedID]; ok {
		seed = e
		seedRepo = repoByEntityID[seedID]
	} else {
		for id, e := range byID {
			if strings.HasPrefix(id, seedID) || strings.HasSuffix(id, seedID) {
				seed = e
				seedRepo = repoByEntityID[id]
				break
			}
		}
	}

	// Collect 1-hop neighbours via relationships. neighbourKinds,
	// neighbourDirections, and neighbourProperties are built in lockstep with
	// neighbours so that downstream NeighbourBrief construction can surface
	// the typed edge kind (#1879), the direction relative to the seed (#1965),
	// and the per-edge Properties map stamped by extractor post-passes
	// (#2018 — e.g. dead_import, re_export, live, is_async, cross_repo).
	seen := make(map[string]bool)
	// collect collects a (rel, neighbourID, dir) triple from the neighbour
	// of the supplied anchor entity (seed itself, or a sibling entity
	// reachable from the seed via CONTAINS — see the Module→file pass
	// below for #2020). Maintains the seen set across both walks so a
	// neighbour reached via both anchors is surfaced only once.
	collect := func(anchorID string) {
		for _, rel := range allRels {
			var neighbourID string
			var dir string
			switch {
			case rel.FromID == anchorID:
				// Outbound: anchor → neighbour (anchor is the source).
				neighbourID = rel.ToID
				dir = NeighbourDirectionOutbound
			case rel.ToID == anchorID:
				// Inbound: neighbour → anchor (anchor is the target).
				neighbourID = rel.FromID
				dir = NeighbourDirectionInbound
			default:
				continue
			}
			if seen[neighbourID] {
				continue
			}
			seen[neighbourID] = true
			if n, ok := byID[neighbourID]; ok {
				neighbours = append(neighbours, *n)
				neighbourKinds = append(neighbourKinds, rel.Kind)
				neighbourDirections = append(neighbourDirections, dir)
				neighbourProperties = append(neighbourProperties, rel.PropsSnapshot())
			}
		}
	}
	if seed != nil {
		// Mark the seed itself so the second walk (Module→file pass below)
		// never echoes the seed back as a neighbour.
		seen[seed.ID] = true
		collect(seed.ID)

		// Issue #1867 — Class seeds: surface typed dependencies from method
		// bodies as 1-hop neighbours. Direct CONTAINS children (methods)
		// dominate the raw 1-hop neighbourhood, but the LLM-useful
		// neighbours for a ViewSet/Model class are the foreign Models +
		// services its methods touch (REFERENCES / CALLS / DEPENDS_ON
		// targets reachable through a method). We walk one extra hop
		// through each contained Operation to surface those typed
		// dependencies on the class's neighbour_briefs. Cap the total
		// expansion at classMethodHopCap so a god-class with hundreds of
		// methods doesn't blow up the bundle.
		if seed != nil && isClassSeedForMethodHop(graph.EntityViewOf(seed)) {
			classMethodHopExpand(seed.ID, allRels, byID,
				&neighbours, &neighbourKinds, &neighbourDirections, &neighbourProperties, seen)
		}

		// Issue #2020 — Python Module entities are dual-emitted alongside a
		// parallel SCOPE.Component(file) entity for the same __init__.py
		// source file. IMPORTS / CONTAINS edges attach to the file entity,
		// not the Module, so Module-seeded docgen previously surfaced zero
		// neighbours. The extractor now emits a CONTAINS edge from Module
		// → the file SCOPE.Component (see emitPackageModuleEntity in
		// internal/extractors/python/package_module.go), and we walk that
		// contained file's 1-hop neighbourhood here so all IMPORTS edges
		// surface on the Module's bundle. The first walk above already
		// added the file entity itself as a neighbour (CONTAINS); this
		// pass surfaces its outbound IMPORTS / inbound CALLS / etc.
		if isPythonModuleEntity(graph.EntityViewOf(seed)) {
			for _, rel := range allRels {
				if rel.Kind != "CONTAINS" || rel.FromID != seed.ID {
					continue
				}
				containedID := rel.ToID
				contained, ok := byID[containedID]
				if !ok {
					continue
				}
				if contained.Kind != "SCOPE.Component" || contained.Subtype != "file" {
					continue
				}
				collect(containedID)
			}
		}
	}

	return
}

// isPythonModuleEntity reports whether e is a Python package-Module entity
// emitted by internal/extractors/python/package_module.go (#1884). Used by
// loadEntityContext to widen the neighbour walk so Module-seeded bundles
// surface the IMPORTS edges attached to the parallel file SCOPE.Component
// (#2020). The check is intentionally narrow — only the python Module
// emission has this dual-entity shape today.
func isPythonModuleEntity(e graph.EntityView) bool {
	if e == nil {
		return false
	}
	if e.Kind() != "Module" {
		return false
	}
	return e.Language() == "python" || e.Language() == ""
}

// classMethodHopCap bounds the number of depth-2 neighbours added by
// classMethodHopExpand so a god-class with hundreds of methods cannot
// produce a runaway bundle. Tuned to keep the neighbour_briefs payload
// within the same ~25-entry visual budget the dogfood evidence
// referenced in #1867.
const classMethodHopCap = 25

// isClassSeedForMethodHop reports whether seed should trigger the
// #1867 method-body typed-dependency expansion. Class-like entities
// across languages are recognised:
//
//   - SCOPE.Component subtype=class / view / viewset / model / controller / service / repository
//   - Top-level Class / Model / View / Controller / Service kinds (other extractors)
//
// Module / file / function / operation kinds are intentionally excluded
// so the second-hop walk runs only where the dogfood evidence found
// useful: per-class api/flows/patterns sections.
func isClassSeedForMethodHop(seed graph.EntityView) bool {
	if seed == nil {
		return false
	}
	switch seed.Kind() {
	case "SCOPE.Component":
		switch seed.Subtype() {
		case "class", "view", "viewset", "model", "controller", "service", "repository":
			return true
		}
	case "Class", "Model", "View", "Controller", "Service":
		return true
	}
	return false
}

// classMethodHopExpand adds depth-2 neighbours reachable through any of
// the seed's CONTAINS-children that are SCOPE.Operation entities. It
// follows OUTBOUND REFERENCES / CALLS / DEPENDS_ON / FK_TO / IMPLEMENTS
// edges from each contained method and surfaces the targets as
// inbound-to-class neighbours so the LLM sees typed deps directly on the
// class's neighbour_briefs.
//
// The expansion respects the shared `seen` map (so a target already
// present as a direct child is not duplicated), caps total additions at
// classMethodHopCap, and preserves the per-edge Properties map (so any
// annotations the extractor stamped — disposition_hint, cross_repo,
// import_alias, etc. — survive onto the depth-2 NeighbourBrief).
//
// Direction is recorded as outbound because, conceptually, the class
// "depends on" the target (seed → target via its method body). Edge kind
// is preserved verbatim so docgen can render the typed relationship.
func classMethodHopExpand(
	seedID string,
	allRels []graph.Relationship,
	byID map[string]*graph.Entity,
	neighbours *[]graph.Entity,
	neighbourKinds *[]string,
	neighbourDirections *[]string,
	neighbourProperties *[]map[string]string,
	seen map[string]bool,
) {
	// 1. Collect the method/operation IDs contained by the class.
	methodIDs := make([]string, 0, 16)
	for _, rel := range allRels {
		if rel.Kind != "CONTAINS" || rel.FromID != seedID {
			continue
		}
		child, ok := byID[rel.ToID]
		if !ok {
			continue
		}
		if child.Kind != "SCOPE.Operation" {
			continue
		}
		methodIDs = append(methodIDs, child.ID)
	}
	if len(methodIDs) == 0 {
		return
	}

	// Build a set for O(1) source-id lookup.
	methodSet := make(map[string]bool, len(methodIDs))
	for _, id := range methodIDs {
		methodSet[id] = true
	}

	// 2. Walk outbound REFERENCES / CALLS / DEPENDS_ON / FK_TO / IMPLEMENTS
	//    edges from any contained method. Cap total additions.
	added := 0
	for _, rel := range allRels {
		if added >= classMethodHopCap {
			return
		}
		if !methodSet[rel.FromID] {
			continue
		}
		if !isTypedDepKind(rel.Kind) {
			continue
		}
		if seen[rel.ToID] {
			continue
		}
		target, ok := byID[rel.ToID]
		if !ok {
			continue
		}
		// Skip targets that are themselves the seed or any of the seed's
		// own contained methods — those are already in the bundle as
		// direct CONTAINS children.
		if target.ID == seedID || methodSet[target.ID] {
			continue
		}
		seen[target.ID] = true
		*neighbours = append(*neighbours, *target)
		*neighbourKinds = append(*neighbourKinds, rel.Kind)
		*neighbourDirections = append(*neighbourDirections, NeighbourDirectionOutbound)
		// Clone+stamp a `via_method_hop=true` marker so consumers can
		// distinguish a depth-2 typed-dep from a depth-1 direct child.
		// Preserve any existing per-edge Properties stamped by the
		// extractor (#2018 carries cross_repo / import_alias / etc.).
		props := map[string]string{"via_method_hop": "true"}
		rel.
			PropRange(func(k, v string) bool { props[k] = v; return true })
		*neighbourProperties = append(*neighbourProperties, props)
		added++
	}
}

// isTypedDepKind reports whether edgeKind expresses a typed dependency
// the #1867 method-hop expansion should surface. CONTAINS / IMPORTS /
// EXTENDS are intentionally excluded — CONTAINS is the seed's own
// container relationship, IMPORTS is already a direct 1-hop file/module
// neighbour, and EXTENDS sits on the class itself rather than its
// methods.
func isTypedDepKind(edgeKind string) bool {
	switch edgeKind {
	case "REFERENCES", "CALLS", "DEPENDS_ON", "FK_TO", "IMPLEMENTS",
		"DEPENDS_ON_CONFIG", "RESOLVED_BY":
		return true
	}
	return false
}

// repoEntry pairs a graph state directory with its absolute repo path from the
// fleet config. Both paths are resolved by the time they leave findGroupRepoEntries.
type repoEntry struct {
	stateDir    string // daemon state dir (contains graph.fb / graph.json)
	absRepoPath string // absolute path to the repo root on disk
}

// findGroupRepoEntries reads the fleet config for the given group and returns
// a slice of repoEntry values — one per registered repo with a non-empty path.
// Both stateDir and absRepoPath are ready-to-use absolute paths.
func findGroupRepoEntries(group string) ([]repoEntry, error) {
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("group config not found for %q (run `grafel wizard`): %w", group, err)
	}

	var cfg struct {
		Repos []struct {
			Path string `json:"path"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse group config: %w", err)
	}

	var entries []repoEntry
	for _, r := range cfg.Repos {
		if r.Path == "" {
			continue
		}
		entries = append(entries, repoEntry{
			stateDir:    daemon.StateDirForRepo(r.Path),
			absRepoPath: r.Path,
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no repos registered in group %q", group)
	}
	return entries, nil
}

// findGroupGraphDirs returns all state directories for repos in the given
// group. It reads the fleet config and resolves each repo's store path via
// daemon.StateDirForRepo — the canonical location since issue #1626.
func findGroupGraphDirs(group string) ([]string, error) {
	entries, err := findGroupRepoEntries(group)
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		dirs = append(dirs, e.stateDir)
	}
	return dirs, nil
}

// renderSection produces the deterministic markdown stub for a section.
// Tier 0 does NOT call an LLM — it renders a structured context block that
// a human or agent can feed to the section prompt template.
func renderSection(section string, seed *graph.Entity, neighbours []graph.Entity) string {
	var b strings.Builder

	// --- Header ---
	b.WriteString("<!-- tier0-generated -->\n")
	b.WriteString(fmt.Sprintf("# Section: %s\n\n", section))

	// --- Seed entity block ---
	b.WriteString("## Seed Entity\n\n")
	if seed == nil {
		b.WriteString("> **Warning:** seed entity not found in any indexed repo for this group.\n")
		b.WriteString("> Run `grafel index` and retry with a valid entity ID.\n\n")
	} else {
		b.WriteString(fmt.Sprintf("- **ID:** `%s`\n", seed.ID))
		b.WriteString(fmt.Sprintf("- **Name:** `%s`\n", seed.Name))
		if seed.QualifiedName != "" {
			b.WriteString(fmt.Sprintf("- **Qualified:** `%s`\n", seed.QualifiedName))
		}
		b.WriteString(fmt.Sprintf("- **Kind:** `%s`\n", seed.Kind))
		if seed.Subtype != "" {
			b.WriteString(fmt.Sprintf("- **Subtype:** `%s`\n", seed.Subtype))
		}
		b.WriteString(fmt.Sprintf("- **Language:** `%s`\n", seed.Language))
		b.WriteString(fmt.Sprintf("- **Source:** `%s` (lines %d–%d)\n", seed.SourceFile, seed.StartLine, seed.EndLine))
		if seed.Signature != "" {
			b.WriteString(fmt.Sprintf("- **Signature:** `%s`\n", seed.Signature))
		}
		if len(seed.Tags) > 0 {
			b.WriteString(fmt.Sprintf("- **Tags:** %s\n", strings.Join(seed.Tags, ", ")))
		}
		if seed.IsGodNode {
			b.WriteString("- **God node:** yes\n")
		}
		if seed.IsArticulationPt {
			b.WriteString("- **Articulation point:** yes\n")
		}
		if seed.Centrality != nil {
			b.WriteString(fmt.Sprintf("- **Centrality:** %.4f\n", *seed.Centrality))
		}
		if seed.PageRank != nil {
			b.WriteString(fmt.Sprintf("- **PageRank:** %.6f\n", *seed.PageRank))
		}
		// Properties
		if seed.PropLen() > 0 {
			b.WriteString("\n**Properties:**\n\n")
			b.WriteString("```\n")
			seed.
				PropRange(func(k, v string) bool { b.WriteString(fmt.Sprintf("%s = %s\n", k, v)); return true })
			b.WriteString("```\n")
		}
	}

	// --- 1-hop neighbourhood ---
	b.WriteString("\n## 1-Hop Neighbourhood\n\n")
	if len(neighbours) == 0 {
		b.WriteString("_No relationships found in indexed graphs._\n")
	} else {
		b.WriteString(fmt.Sprintf("_Total neighbours: %d_\n\n", len(neighbours)))
		b.WriteString("| ID | Name | Kind | Source |\n")
		b.WriteString("|----|------|------|--------|\n")
		// Cap at 50 rows to stay within token budget.
		limit := len(neighbours)
		if limit > 50 {
			limit = 50
		}
		for _, n := range neighbours[:limit] {
			b.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` | `%s` |\n",
				n.ID, n.Name, n.Kind, n.SourceFile))
		}
		if len(neighbours) > 50 {
			b.WriteString(fmt.Sprintf("\n_… and %d more neighbours (truncated to 50)_\n", len(neighbours)-50))
		}
	}

	// --- Section-specific guidance ---
	b.WriteString("\n## Section Guidance\n\n")
	b.WriteString(sectionGuidance(section))

	// --- Mermaid placeholder (for sections that expect one) ---
	if sectionExpectsMermaid(section) {
		b.WriteString("\n## Diagram Placeholder\n\n")
		b.WriteString("```mermaid\n")
		b.WriteString("graph LR\n")
		if seed != nil {
			b.WriteString(fmt.Sprintf("    seed[\"%s\"]\n", seed.Name))
			for i, n := range neighbours {
				nodeID := fmt.Sprintf("nb%d", i)
				b.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", nodeID, n.Name))
				b.WriteString(fmt.Sprintf("    seed --> %s\n", nodeID))
			}
		} else {
			b.WriteString("    %% seed entity not found\n")
		}
		b.WriteString("```\n")
	}

	return b.String()
}

// sectionGuidance returns the docgen-skill guidance blurb for a section.
// This mirrors the intent of the generate-docs output-template prompts.
func sectionGuidance(section string) string {
	m := map[string]string{
		"overview": "Describe what this entity does in 2–3 sentences for an engineer. " +
			"Link to callers and callees. Highlight if it is a god node or articulation point.",
		"capabilities": "List the product capabilities this entity implements. " +
			"Group by business outcome, not by function name.",
		"flows": "Trace the request/event flow through this entity. " +
			"Use a mermaid sequence or flowchart. Reference upstream callers and downstream callees.",
		"patterns": "Identify structural patterns (ADR-0018): adapter, gateway, orchestrator, etc. " +
			"Cite evidence from the graph neighbourhood.",
		"api": "Document the public API surface: exported functions, HTTP endpoints, or event topics. " +
			"Include signatures and brief usage notes.",
		"reference-config": "List all configuration keys consumed or produced by this entity. " +
			"Source from Properties and Metadata fields.",
		"reference-dependencies": "List direct external and internal dependencies. " +
			"Separate production vs test dependencies.",
		"reference-deployment": "Describe deployment concerns: env vars, ports, scaling constraints. " +
			"Source from graph metadata and Properties.",
		"reference-scripts": "List CLI commands, Makefile targets, or scripts associated with this entity.",
		"reference-misc":    "Any additional reference material not covered by other sections.",
		"module-readme": "Write a README-style intro for the module containing this entity. " +
			"Cover purpose, key entities, and local-dev setup.",
		"glossary": "Define domain terms appearing in this entity's name, signature, or neighbourhood. " +
			"One term per row.",
		"how-to-local-dev": "Step-by-step local development guide for working with this entity. " +
			"Cover build, test, and run commands.",
	}
	if g, ok := m[section]; ok {
		return g + "\n"
	}
	return "_No guidance available for this section type._\n"
}

// sectionExpectsMermaid returns true for sections that should include a
// diagram in their output.
func sectionExpectsMermaid(section string) bool {
	switch section {
	case "flows", "overview", "capabilities", "module-readme":
		return true
	}
	return false
}

// ValidateMermaidNodeIDs checks that all node IDs in the mermaid stub are unique.
// Returns an error if any node ID collisions are detected.
func ValidateMermaidNodeIDs(md string) error {
	// Extract all node IDs from mermaid blocks (e.g., "nb0", "nb1", "seed").
	nodeIDPattern := regexp.MustCompile(`(?m)^\s*([a-zA-Z_]\w*)\[`)

	seen := make(map[string]bool)
	lines := strings.Split(md, "\n")
	inMermaid := false

	for _, line := range lines {
		if strings.Contains(line, "```mermaid") {
			inMermaid = true
			continue
		}
		if strings.Contains(line, "```") && inMermaid {
			inMermaid = false
			continue
		}
		if !inMermaid {
			continue
		}

		matches := nodeIDPattern.FindStringSubmatch(line)
		if len(matches) > 1 {
			nodeID := matches[1]
			if seen[nodeID] {
				return fmt.Errorf("duplicate mermaid node ID: %q", nodeID)
			}
			seen[nodeID] = true
		}
	}

	return nil
}

// buildScore computes the SCORE.json metrics from the rendered markdown.
func buildScore(section, seedID, md string, wallMS int64, neighbourCount int, seedFound bool) Score {
	lines := strings.Count(md, "\n")
	words := countWords(md)
	tokens := estimateTokens(md)
	mermaid := strings.Count(md, "```mermaid")
	links := countInternalLinks(md)

	return Score{
		Tier:                   0,
		Section:                section,
		SeedEntity:             seedID,
		WallTimeMS:             wallMS,
		TokenCountEstimate:     tokens,
		MermaidCount:           mermaid,
		InternalLinkCount:      links,
		InternalLinkUnresolved: 0, // Tier 0 stubs have no outbound links yet.
		Lines:                  lines,
		Words:                  words,
		NeighboursIncluded:     neighbourCount,
		SeedEntityFound:        seedFound,
	}
}

// estimateTokens estimates the GPT/Claude token count of a string using the
// rule of thumb: 1 token ≈ 4 bytes of English text.
func estimateTokens(s string) int {
	return (len(s) + 3) / 4
}

func countWords(s string) int {
	return len(strings.Fields(s))
}

var internalLinkRE = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

func countInternalLinks(md string) int {
	matches := internalLinkRE.FindAllString(md, -1)
	n := 0
	for _, m := range matches {
		// Count only relative links (no http/https scheme).
		if !strings.Contains(m, "://") {
			n++
		}
	}
	return n
}

// sanitizeFilename replaces characters that are unsafe in filenames.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}
