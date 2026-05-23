// Package docgen — LLM-mode prompt bundle schema and constructor (issue #1813).
//
// This file defines the JSON schema for the emit-and-orchestrate LLM path:
//
//  1. Daemon emits an LLMPromptBundle JSON per seed entity.
//  2. External orchestrator (Claude Code skill) reads the bundle, calls an LLM
//     for each section, and writes an LLMRunResult back.
//  3. Daemon --llm-mode=apply re-runs contracts and builds the score.
//
// BuildBundle constructs a bundle for Tier 0 or Tier 1 inputs WITHOUT making
// any LLM calls or network requests. It reuses loadEntityContext from tier0.go.
//
// prompt_hash design:
//
//	Per-section hash = sha256(version + "\x00" + section + "\x00" +
//	                          entity_id + "\x00" + graph_node_hash + "\x00" +
//	                          guidance_text)
//	Bundle hash = sha256(concat of all per-section hashes in KnownSections order)
//
// The hash is a stable cache key: the same graph state + guidance text always
// produces the same hash, so the orchestrator can skip LLM calls for sections
// it has already filled.
package docgen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/mcp"
)

// LLMBundleVersion is the schema version embedded in every bundle.
const LLMBundleVersion = "1"

// ---------------------------------------------------------------------------
// Schema structs
// ---------------------------------------------------------------------------

// LLMPromptBundle is the top-level envelope emitted by the daemon for one seed
// entity. Every field that an LLM or orchestrator needs is self-contained here:
// graph context, per-section stubs, guidance text, and contract budgets.
type LLMPromptBundle struct {
	// Version is the schema version ("1"). Bumped on breaking changes.
	Version string `json:"version"`
	// Tier is 0 (section) or 1 (full page).
	Tier int `json:"tier"`
	// Group is the archigraph group name.
	Group string `json:"group"`
	// SeedEntityID is the entity ID used to build this bundle.
	SeedEntityID string `json:"seed_entity_id"`
	// PageID is the page identifier for Tier 1 bundles. Empty for Tier 0.
	PageID string `json:"page_id,omitempty"`
	// Sections holds one prompt entry per section rendered.
	Sections []LLMSectionPrompt `json:"sections"`
	// GraphContext carries the resolved entity + neighbour data an LLM needs.
	GraphContext LLMGraphContext `json:"graph_context"`
	// PromptHash is the bundle-level cache key (sha256 of per-section hashes).
	PromptHash string `json:"prompt_hash"`
	// GeneratedAt is the RFC3339 timestamp when the bundle was built.
	GeneratedAt string `json:"generated_at"`
}

// LLMSectionPrompt is the per-section prompt payload. It carries both the
// deterministic stub (for grounding) and the guidance text for the LLM.
type LLMSectionPrompt struct {
	// Section is the section name from KnownSections (e.g. "capabilities").
	Section string `json:"section"`
	// AnchorID is the HTML anchor slug for Tier 1 cross-section linking.
	AnchorID string `json:"anchor_id"`
	// StubMarkdown is the deterministic Tier 0/1 output for this section.
	// When CacheHit is true this field is replaced with the cached LLM markdown
	// so the orchestrator can skip the LLM call for this section.
	StubMarkdown string `json:"stub_markdown"`
	// Guidance is the section-specific prompt text for the LLM.
	Guidance string `json:"guidance"`
	// MaxWords is the contract word-count budget for the LLM's prose output.
	MaxWords int `json:"max_words"`
	// MaxMermaid is the contract mermaid-block budget for the LLM's output.
	MaxMermaid int `json:"max_mermaid"`
	// NeighbourIDs is the list of 1-hop neighbour entity IDs for context.
	NeighbourIDs []string `json:"neighbour_ids"`
	// PromptHash is the per-section cache key (sha256). Additive field; omitted
	// when empty so older orchestrators that don't know the field ignore it.
	PromptHash string `json:"prompt_hash,omitempty"`
	// CacheHit is true when a cached LLM result was found for this section's
	// PromptHash. When true, StubMarkdown is populated with the cached LLM
	// markdown and the orchestrator should skip the LLM call for this section.
	// Additive field: orchestrators that don't know it treat it as falsy/absent.
	CacheHit bool `json:"cache_hit,omitempty"`
}

// LLMGraphContext carries the resolved entity metadata and neighbour summaries
// that an LLM needs without having to re-read the graph.
type LLMGraphContext struct {
	// EntityName is the short name of the seed entity.
	EntityName string `json:"entity_name"`
	// EntityKind is the kind string (e.g. "function", "class", "module").
	EntityKind string `json:"entity_kind"`
	// QualifiedName is the fully-qualified name if available.
	QualifiedName string `json:"qualified_name"`
	// Repo is the repository root path as stored in the graph document.
	Repo string `json:"repo"`
	// SourceFile is the source file path for the seed entity.
	SourceFile string `json:"source_file"`
	// NeighbourBriefs is a compact summary of each 1-hop neighbour.
	NeighbourBriefs []NeighbourBrief `json:"neighbour_briefs"`
	// SourceWindow is an optional excerpt of source lines around the entity
	// (populated by future --source-window flag; empty in foundation ticket).
	SourceWindow string `json:"source_window,omitempty"`
}

// NeighbourBrief is a compact description of a single 1-hop neighbour entity.
type NeighbourBrief struct {
	// EntityID is the graph entity ID.
	EntityID string `json:"entity_id"`
	// Name is the short name.
	Name string `json:"name"`
	// Kind is the entity kind.
	Kind string `json:"kind"`
	// Relationship is the typed edge kind from the seed to this neighbour as
	// stored on the graph relationship — see NeighbourRelationship* constants
	// for the canonical set. The value is preserved verbatim from the graph
	// (#1879) so docgen can answer questions like "upstream callers" or
	// "downstream callees" without inference. Falls back to
	// NeighbourRelationshipRelated only when the graph lacks an explicit kind.
	Relationship string `json:"relationship"`
}

// Canonical NeighbourBrief.Relationship values. The graph may emit other
// kinds — these constants name the well-known set that docgen section
// templates rely on (#1879, #1881, #1877). Consumers should treat the field
// as an open string and switch on these constants for known cases.
const (
	NeighbourRelationshipCalls      = "CALLS"
	NeighbourRelationshipImports    = "IMPORTS"
	NeighbourRelationshipReferences = "REFERENCES"
	NeighbourRelationshipContains   = "CONTAINS"
	NeighbourRelationshipDependsOn  = "DEPENDS_ON"
	NeighbourRelationshipFKTo       = "FK_TO"
	NeighbourRelationshipRenders    = "RENDERS"
	NeighbourRelationshipTests      = "TESTS"
	NeighbourRelationshipImplements = "IMPLEMENTS"
	// NeighbourRelationshipRelated is the fallback used only when an edge has
	// no explicit kind on the graph. Should be rare for a well-formed graph.
	NeighbourRelationshipRelated = "RELATED"
)

// LLMSectionResult is the per-section output written by the external
// orchestrator after calling an LLM. The daemon reads a slice of these (wrapped
// in LLMRunResult) during --llm-mode=apply.
type LLMSectionResult struct {
	// Section is the KnownSections name this result is for.
	Section string `json:"section"`
	// Markdown is the LLM-generated prose for this section.
	Markdown string `json:"markdown"`
	// MermaidCount is the number of mermaid blocks in Markdown (for contract checks).
	MermaidCount int `json:"mermaid_count"`
	// WordCount is the word count of Markdown.
	WordCount int `json:"word_count"`
	// LinkRefs holds relative links found in Markdown for cross-page validation.
	LinkRefs []string `json:"link_refs"`
}

// LLMRunResult is the envelope written by the orchestrator after all sections
// are filled. Its PromptHash must match the bundle it was produced from so the
// daemon can detect stale or mismatched results.
type LLMRunResult struct {
	// Version is the schema version (must match the bundle's version).
	Version string `json:"version"`
	// PromptHash must equal LLMPromptBundle.PromptHash.
	PromptHash string `json:"prompt_hash"`
	// Tier is 0 or 1 (matches the bundle).
	Tier int `json:"tier"`
	// Group is the archigraph group name.
	Group string `json:"group"`
	// SeedEntityID is the seed entity ID (matches the bundle).
	SeedEntityID string `json:"seed_entity_id"`
	// SectionResults holds one result per filled section.
	SectionResults []LLMSectionResult `json:"section_results"`
	// FilledAt is the RFC3339 timestamp when the orchestrator finished.
	FilledAt string `json:"filled_at"`
}

// ---------------------------------------------------------------------------
// Default section guidance
// ---------------------------------------------------------------------------

// defaultSectionGuidance maps each KnownSection to a 1-2 sentence LLM prompt
// stub explaining what the LLM should produce for that section. Override via
// config in future tickets.
var defaultSectionGuidance = map[string]string{
	"overview": "Write a 2–3 sentence description of what this entity does and why it exists. " +
		"Highlight whether it is a god node, articulation point, or performance-critical path.",
	"capabilities": "Enumerate the product capabilities this entity implements, grouped by business outcome. " +
		"Each capability should be one bullet referencing the relevant functions or methods.",
	"flows": "Trace the primary request or event flow through this entity using a mermaid sequence or flowchart. " +
		"Reference upstream callers and downstream callees by name.",
	"patterns": "Identify the structural design patterns present (adapter, gateway, orchestrator, saga, etc.). " +
		"Cite specific neighbour relationships as evidence for each pattern identified.",
	"api": "Document the full public API surface: exported functions, HTTP endpoints, event topics, or CLI flags. " +
		"Include method signatures and a one-line usage note for each.",
	"reference-config": "List every configuration key this entity reads or writes, with type, default value, and effect. " +
		"Source from Properties, Metadata, and environment variable names visible in the source.",
	"reference-dependencies": "List direct dependencies separated into production and test/dev. " +
		"For external dependencies include the import path; for internal ones include the entity ID.",
	"reference-deployment": "Describe deployment concerns: required environment variables, exposed ports, scaling constraints, and health-check endpoints. " +
		"Source from graph metadata and the Properties map.",
	"reference-scripts": "List all Makefile targets, npm/go scripts, or shell commands associated with this entity and explain what each does.",
	"reference-misc": "Capture any additional reference material not covered by the other sections: ADR links, architecture diagrams, or known limitations.",
	"module-readme": "Write a README-style introduction for the module containing this entity: purpose, key entities, quickstart build/test/run commands, and link to the main documentation page.",
	"glossary": "Define each domain-specific term that appears in this entity's name, signature, or immediate neighbourhood. " +
		"One term per table row with a 1-sentence definition.",
	"how-to-local-dev": "Provide a numbered step-by-step local development guide for working with this entity: clone, configure env, build, run tests, and observe output.",
}

// sectionMaxWords returns the default word-count contract budget for a section.
func sectionMaxWords(section string) int {
	switch section {
	case "overview":
		return 150
	case "capabilities":
		return 400
	case "flows":
		return 300
	case "patterns":
		return 250
	case "api":
		return 500
	case "reference-config", "reference-dependencies", "reference-deployment",
		"reference-scripts", "reference-misc":
		return 300
	case "module-readme":
		return 400
	case "glossary":
		return 200
	case "how-to-local-dev":
		return 350
	default:
		return 300
	}
}

// sectionMaxMermaid returns the default mermaid-block contract budget for a section.
func sectionMaxMermaid(section string) int {
	switch section {
	case "flows":
		return 2
	case "overview", "capabilities", "module-readme":
		return 1
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Hashing helpers
// ---------------------------------------------------------------------------

// graphNodeHash produces a stable hash for a seed entity and its neighbour
// IDs. This is the "graph_node_hash" component of the per-section hash: if the
// graph changes, the hash changes, invalidating the LLM cache for that section.
func graphNodeHash(entityID string, neighbourIDs []string) string {
	h := sha256.New()
	h.Write([]byte(entityID))
	// Sort-free: KnownSections iteration order is deterministic; neighbourIDs
	// here reflect the order returned by loadEntityContext which is consistent
	// for the same graph state.
	for _, nid := range neighbourIDs {
		h.Write([]byte{0})
		h.Write([]byte(nid))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// sectionPromptHash computes the per-section hash component.
//
//	sha256(version + "\x00" + section + "\x00" + entity_id + "\x00" +
//	       graph_node_hash + "\x00" + guidance_text)
func sectionPromptHash(version, section, entityID, nodeHash, guidance string) string {
	h := sha256.New()
	parts := []string{version, section, entityID, nodeHash, guidance}
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// bundlePromptHash rolls up per-section hashes into the bundle-level hash.
// Sections are processed in KnownSections order for determinism.
func bundlePromptHash(sectionHashes map[string]string) string {
	h := sha256.New()
	for _, sec := range KnownSections {
		if sh, ok := sectionHashes[sec]; ok {
			h.Write([]byte(sh))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ---------------------------------------------------------------------------
// BuildBundle — public constructor
// ---------------------------------------------------------------------------

// BuildBundleOpts extends RunOpts with LLM-bundle-specific fields.
type BuildBundleOpts struct {
	// RunOpts provides Group, SeedEntityID, Section, OutputDir.
	// For a Tier 1 bundle, Section may be empty (all sections are included).
	RunOpts
	// PageID is the page identifier for Tier 1 bundles.
	PageID string
	// Tier is 0 (single-section bundle) or 1 (full-page bundle).
	// Defaults to 0 when 0 and RunOpts.Section is set.
	Tier int
	// CacheDir overrides the default cache directory for this bundle.
	// Defaults to DefaultCacheDir(group) when empty.
	CacheDir string
	// NoCache disables both cache reads and writes when true.
	// Useful for benchmark / quality-check runs that must not use cached data.
	NoCache bool
}

// BuildBundle constructs and returns an LLMPromptBundle for the given opts
// WITHOUT calling any LLM or making any network request.
//
// For Tier 0: opts.RunOpts.Section must be one of KnownSections.
// For Tier 1: all sections appropriate for the entity kind are included;
//
//	opts.RunOpts.Section is ignored.
//
// The bundle is ready to marshal to JSON and pass to an external orchestrator.
func BuildBundle(_ context.Context, opts BuildBundleOpts) (*LLMPromptBundle, error) {
	tier := opts.Tier

	// Tier 0: validate section.
	if tier == 0 {
		if err := validateSection(opts.Section); err != nil {
			return nil, err
		}
	}

	// Load entity context — reuses tier0.go loadEntityContext.
	// neighbourKinds carries the typed edge kind for each neighbour so that
	// NeighbourBrief.Relationship surfaces the actual graph relationship
	// (CALLS, IMPORTS, CONTAINS, REFERENCES, DEPENDS_ON, FK_TO, ...) instead
	// of a flat "RELATED" placeholder (#1879).
	_, entity, neighbours, neighbourKinds, seedRepo, err := loadEntityContext(opts.Group, opts.SeedEntityID)
	if err != nil {
		return nil, err
	}

	// Resolved entity ID (may differ from input when prefix-matched).
	resolvedID := opts.SeedEntityID
	if entity != nil {
		resolvedID = entity.ID
	}

	// Collect neighbour IDs and build briefs. Relationship is sourced from
	// the index-aligned neighbourKinds slice produced by loadEntityContext;
	// when a kind is somehow missing (defensive: should not happen for a
	// well-formed graph) we fall back to "RELATED" to preserve a valid
	// non-empty enum-shaped value for downstream consumers (#1879).
	var neighbourIDs []string
	var briefs []NeighbourBrief
	for i, n := range neighbours {
		neighbourIDs = append(neighbourIDs, n.ID)
		rel := "RELATED"
		if i < len(neighbourKinds) && neighbourKinds[i] != "" {
			rel = neighbourKinds[i]
		}
		briefs = append(briefs, NeighbourBrief{
			EntityID:     n.ID,
			Name:         n.Name,
			Kind:         n.Kind,
			Relationship: rel,
		})
	}

	// Build graph context.
	gc := LLMGraphContext{
		NeighbourBriefs: briefs,
	}
	if entity != nil {
		gc.EntityName = entity.Name
		gc.EntityKind = entity.Kind
		gc.QualifiedName = entity.QualifiedName
		gc.Repo = seedRepo
		gc.SourceFile = entity.SourceFile

		// Populate source_window: read N lines around the entity's start_line.
		// Uses the cross-platform readSourceWindow (build-tag split per #1780).
		// On error (file deleted, fsevents stall, etc.) we leave the field empty
		// and log a warning — a missing source window must not fail the bundle.
		const sourceWindowHalfLines = 20
		if entity.SourceFile != "" && entity.StartLine > 0 {
			absPath := filepath.Join(seedRepo, entity.SourceFile)
			startLine := entity.StartLine - sourceWindowHalfLines
			if startLine < 1 {
				startLine = 1
			}
			endLine := entity.EndLine + sourceWindowHalfLines
			if endLine < entity.StartLine+sourceWindowHalfLines {
				endLine = entity.StartLine + sourceWindowHalfLines
			}
			if sw, swErr := mcp.ReadSourceWindow(absPath, startLine, endLine); swErr != nil {
				// Non-fatal: log and continue — the rest of the bundle is valid.
				// Include the resolved absolute path, the original entity SourceFile,
				// the repo root, and the current working directory so that future
				// debugging is easy (#1834).
				cwd, _ := os.Getwd()
				fmt.Fprintf(os.Stderr,
					"docgen: source_window: cannot read source file for entity %q:\n"+
						"  resolved path : %s\n"+
						"  entity source : %s\n"+
						"  repo root     : %s\n"+
						"  cwd           : %s\n"+
						"  error         : %v\n",
					entity.ID, absPath, entity.SourceFile, seedRepo, cwd, swErr)
			} else {
				gc.SourceWindow = sw
			}
		}
	}

	// Determine section list and profile (profile carries per-kind guidance overrides).
	var sections []string
	var profile SectionProfile
	if tier == 0 {
		sections = []string{opts.Section}
		// For Tier 0, resolve the profile based on entity kind so guidance
		// overrides apply even to single-section bundles.
		entityKind := ""
		if entity != nil {
			entityKind = entity.Kind
		}
		profile = ResolveSectionProfile(entityKind, "")
	} else {
		kind := ""
		if entity != nil {
			kind = entity.Kind
		}
		profile = ResolveSectionProfile(kind, "")
		sections = profile.Sections
	}

	// Pre-compute node hash (shared across all sections for this bundle).
	nodeHash := graphNodeHash(resolvedID, neighbourIDs)

	// Build per-section prompts and collect hashes.
	sectionHashes := make(map[string]string, len(sections))
	sectionSet := make(map[string]bool, len(sections))
	for _, s := range sections {
		sectionSet[s] = true
	}

	// Resolve cache directory (nil when NoCache is set).
	cacheDir := ""
	if !opts.NoCache {
		cacheDir = opts.CacheDir
		if cacheDir == "" {
			// Compute default — ignore error; if we can't determine home we
			// simply run without cache (cache miss is always safe).
			if cd, cdErr := DefaultCacheDir(opts.Group); cdErr == nil {
				cacheDir = cd
			}
		}
	}

	var sectionPrompts []LLMSectionPrompt
	// Emit in KnownSections order for determinism.
	for _, sec := range KnownSections {
		if !sectionSet[sec] {
			continue
		}

		// ResolveGuidance checks profile.GuidanceOverrides before falling back
		// to defaultSectionGuidance, so kind-specific prompt text takes effect
		// without touching the shared defaults (#1875).
		guidance := ResolveGuidance(profile, sec)

		// Build deterministic stub using tier0 renderSection.
		stub := renderSection(sec, entity, neighbours)

		// Collect neighbour IDs for this section (same for all sections in Tier 0/1).
		nbIDs := make([]string, len(neighbourIDs))
		copy(nbIDs, neighbourIDs)

		sh := sectionPromptHash(LLMBundleVersion, sec, resolvedID, nodeHash, guidance)
		sectionHashes[sec] = sh

		sp := LLMSectionPrompt{
			Section:      sec,
			AnchorID:     sectionSlug(sec),
			StubMarkdown: stub,
			Guidance:     guidance,
			MaxWords:     sectionMaxWords(sec),
			MaxMermaid:   sectionMaxMermaid(sec),
			NeighbourIDs: nbIDs,
			PromptHash:   sh,
		}

		// Cache read: if a cached result exists, stamp cache_hit=true and
		// replace StubMarkdown with the cached LLM markdown so the orchestrator
		// can skip the LLM call for this section.
		if cacheDir != "" {
			if ce, readErr := ReadCache(cacheDir, sh); readErr == nil && ce != nil {
				sp.CacheHit = true
				sp.StubMarkdown = ce.Markdown
			}
			// ReadCache errors (permissions, corrupt JSON) are silently ignored:
			// a cache miss is always safe — we just re-run the LLM for this section.
		}

		sectionPrompts = append(sectionPrompts, sp)
	}

	// Compute bundle-level prompt hash.
	bHash := bundlePromptHash(sectionHashes)

	// PageID.
	pageID := opts.PageID
	if pageID == "" && tier == 1 {
		pageID = sanitizeFilename(resolvedID)
	}

	bundle := &LLMPromptBundle{
		Version:      LLMBundleVersion,
		Tier:         tier,
		Group:        opts.Group,
		SeedEntityID: resolvedID,
		PageID:       pageID,
		Sections:     sectionPrompts,
		GraphContext: gc,
		PromptHash:   bHash,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	return bundle, nil
}

// ---------------------------------------------------------------------------
// Utility: word-count for LLMSectionResult (used by orchestrator write-back)
// ---------------------------------------------------------------------------

// CountResultWords counts words in an LLMSectionResult's Markdown field.
func CountResultWords(r LLMSectionResult) int {
	return len(strings.Fields(r.Markdown))
}

// CountResultMermaid counts mermaid blocks in an LLMSectionResult's Markdown.
func CountResultMermaid(r LLMSectionResult) int {
	return strings.Count(r.Markdown, "```mermaid")
}

// BundleHashValid checks that an LLMRunResult's PromptHash matches the
// originating bundle. Returns a non-nil error when they diverge.
func BundleHashValid(bundle *LLMPromptBundle, result *LLMRunResult) error {
	if bundle.PromptHash != result.PromptHash {
		return fmt.Errorf("prompt_hash mismatch: bundle=%q result=%q — result was produced from a different bundle version",
			bundle.PromptHash, result.PromptHash)
	}
	return nil
}
