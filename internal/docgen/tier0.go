// Package docgen provides the Tier 0 fast-path for documentation generation.
//
// Tier 0 renders a SINGLE section for a SINGLE seed entity with a <30 s
// feedback loop. It is the inner iteration harness for prompt-quality work:
// edit a prompt template, run Tier 0, read the diff in SCORE.json, repeat.
//
// Output layout:
//
//	~/.archigraph/docs/<group>/.tier0-<RFC3339>/
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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/cajasmota/archigraph/internal/daemon"
	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/registry"
)

// KnownSections is the canonical list accepted by --section.
var KnownSections = []string{
	"overview",
	"capabilities",
	"flows",
	"patterns",
	"api",
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
	Tier                    int    `json:"tier"`
	Section                 string `json:"section"`
	SeedEntity              string `json:"seed_entity"`
	WallTimeMS              int64  `json:"wall_time_ms"`
	TokenCountEstimate      int    `json:"token_count_estimate"`
	MermaidCount            int    `json:"mermaid_count"`
	InternalLinkCount       int    `json:"internal_link_count"`
	InternalLinkUnresolved  int    `json:"internal_link_unresolved"`
	Lines                   int    `json:"lines"`
	Words                   int    `json:"words"`
	NeighboursIncluded      int    `json:"neighbours_included"`
	SeedEntityFound         bool   `json:"seed_entity_found"`
}

// RunOpts contains the resolved inputs for a Tier 0 run.
type RunOpts struct {
	// Group is the archigraph group name (resolved from --group or sole group).
	Group string
	// SeedEntityID is the entity ID to render the section for.
	SeedEntityID string
	// Section is one of KnownSections.
	Section string
	// OutputDir overrides the default ~/.archigraph/docs/<group>/.tier0-<ts>/
	// location. Useful in tests.
	OutputDir string
}

// Run executes a Tier 0 section snippet render and returns the path to the
// output markdown file and its score.
func Run(opts RunOpts) (mdPath string, scoreFile string, score Score, err error) {
	start := time.Now()

	if err = validateSection(opts.Section); err != nil {
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
	doc, entity, neighbours, err := loadEntityContext(opts.Group, opts.SeedEntityID)
	if err != nil {
		return
	}
	_ = doc // full document available for future richer context

	// Render the section stub.
	md := renderSection(opts.Section, entity, neighbours)

	// Write the markdown file.
	mdFile := filepath.Join(outDir, sanitizeFilename(opts.SeedEntityID)+"-"+opts.Section+".md")
	if wErr := os.WriteFile(mdFile, []byte(md), 0o644); wErr != nil {
		err = fmt.Errorf("write section file: %w", wErr)
		return
	}

	// Build and write the score.
	elapsed := time.Since(start).Milliseconds()
	score = buildScore(opts.Section, opts.SeedEntityID, md, elapsed, len(neighbours), entity != nil)

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

// loadEntityContext loads all graphs for the group, finds the seed entity,
// and returns it along with its 1-hop neighbours.
func loadEntityContext(group, seedID string) (doc *graph.Document, seed *graph.Entity, neighbours []graph.Entity, err error) {
	repoGraphDirs, err := findGroupGraphDirs(group)
	if err != nil {
		return
	}

	// Build a combined entity index and relationship index across all repos.
	byID := make(map[string]*graph.Entity)
	var allRels []graph.Relationship
	for _, dir := range repoGraphDirs {
		d, loadErr := graph.LoadGraphFromDir(dir)
		if loadErr != nil {
			// Non-fatal: some repos may not be indexed yet.
			continue
		}
		if doc == nil {
			doc = d // keep first as nominal doc; not critical for Tier 0
		}
		for i := range d.Entities {
			e := d.Entities[i]
			byID[e.ID] = &e
		}
		allRels = append(allRels, d.Relationships...)
	}

	if len(byID) == 0 {
		err = fmt.Errorf("no indexed repos found for group %q — run `archigraph index` first", group)
		return
	}

	// Locate seed entity (exact match first, then prefix match).
	if e, ok := byID[seedID]; ok {
		seed = e
	} else {
		for id, e := range byID {
			if strings.HasPrefix(id, seedID) || strings.HasSuffix(id, seedID) {
				seed = e
				break
			}
		}
	}

	// Collect 1-hop neighbours via relationships.
	seen := make(map[string]bool)
	if seed != nil {
		for _, rel := range allRels {
			var neighbourID string
			switch {
			case rel.FromID == seed.ID:
				neighbourID = rel.ToID
			case rel.ToID == seed.ID:
				neighbourID = rel.FromID
			default:
				continue
			}
			if seen[neighbourID] {
				continue
			}
			seen[neighbourID] = true
			if n, ok := byID[neighbourID]; ok {
				neighbours = append(neighbours, *n)
			}
		}
	}

	return
}

// findGroupGraphDirs returns all state directories for repos in the given
// group. It reads the fleet config and resolves each repo's store path via
// daemon.StateDirForRepo — the canonical location since issue #1626.
func findGroupGraphDirs(group string) ([]string, error) {
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
			Path string `json:"path"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse group config: %w", err)
	}

	var dirs []string
	for _, r := range cfg.Repos {
		if r.Path == "" {
			continue
		}
		// Use daemon.StateDirForRepo — the canonical store location (#1626).
		// It handles ARCHIGRAPH_DAEMON_ROOT isolation automatically.
		dirs = append(dirs, daemon.StateDirForRepo(r.Path))
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no repos registered in group %q", group)
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
		b.WriteString("> Run `archigraph index` and retry with a valid entity ID.\n\n")
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
		if len(seed.Properties) > 0 {
			b.WriteString("\n**Properties:**\n\n")
			b.WriteString("```\n")
			for k, v := range seed.Properties {
				b.WriteString(fmt.Sprintf("%s = %s\n", k, v))
			}
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
			for _, n := range neighbours {
				b.WriteString(fmt.Sprintf("    seed --> nb[\"%s\"]\n", n.Name))
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
		"reference-misc": "Any additional reference material not covered by other sections.",
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
