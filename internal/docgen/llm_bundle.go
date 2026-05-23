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
	"regexp"
	"strings"
	"time"

	"github.com/cajasmota/archigraph/internal/graph"
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
	// ClassManifest is a structured enumeration of the class's surface area:
	// methods, fields, bases, interfaces, and decorators/annotations. Populated
	// only when the seed entity is class-like (Class, Component, Controller,
	// Service, Model, View, etc.). Nil for non-class seeds. (#1861)
	ClassManifest *ClassManifest `json:"class_manifest,omitempty"`
	// ModuleReadme is the README content found in the same directory as the
	// module entity. Populated only for Module-kind seeds (#1880). Nil for
	// non-Module seeds.
	ModuleReadme *ModuleReadme `json:"module_readme,omitempty"`
	// ModuleConfigs is the list of sibling Config entities linked to this
	// module via DEPENDS_ON_CONFIG edges. Populated only for Module-kind seeds
	// (#1880). Nil/empty for non-Module seeds.
	ModuleConfigs []ModuleConfigEntry `json:"module_configs,omitempty"`
}

// ModuleReadme holds the README content embedded into a Module bundle (#1880).
type ModuleReadme struct {
	// File is the repo-relative path of the README (e.g. "README.md").
	File string `json:"file"`
	// Content is the first ModuleReadmeMaxLines lines of the README.
	Content string `json:"content"`
	// Language is the inferred markup language: "markdown", "rst", or "text".
	Language string `json:"language"`
}

// ModuleConfigEntry holds the extracted metadata from one sibling Config entity
// linked to a Module via a DEPENDS_ON_CONFIG edge (#1880).
type ModuleConfigEntry struct {
	// Name is the basename of the config file (e.g. "package.json").
	Name string `json:"name"`
	// Format is the format string stored on the Config entity (e.g. "json", "toml").
	Format string `json:"format,omitempty"`
	// Subtype is the config subtype (e.g. "node_project", "python_project").
	Subtype string `json:"subtype,omitempty"`
	// ProjectName is the project/package name extracted from the config file.
	ProjectName string `json:"project_name,omitempty"`
	// Dependencies is the comma-joined list of production dependencies.
	// Capped at ModuleConfigMaxKeys entries.
	Dependencies string `json:"dependencies,omitempty"`
	// Scripts is the comma-joined list of script/target names.
	Scripts string `json:"scripts,omitempty"`
	// KeysTopLevel is the comma-joined list of top-level config keys.
	// Capped at ModuleConfigMaxKeys entries. Included for generic configs.
	KeysTopLevel string `json:"keys_top_level,omitempty"`
}

// ModuleReadmeMaxLines is the cap on the number of lines read from a README
// file when embedding into the module bundle (#1880).
const ModuleReadmeMaxLines = 400

// ModuleConfigMaxConfigs is the maximum number of sibling Config entities
// embedded into a Module bundle (#1880).
const ModuleConfigMaxConfigs = 3

// ModuleConfigMaxKeys is the cap on the number of keys_top_level entries
// embedded per config entry (#1880).
const ModuleConfigMaxKeys = 50

// ClassManifest is a structured enumeration of a class entity's public
// surface area. It lets the LLM cite specific methods and fields by name
// without re-parsing the source_window. Populated by BuildBundle when the
// seed entity is class-like (#1861).
type ClassManifest struct {
	// Methods is the list of method/constructor entries found via CONTAINS edges.
	// Capped at ClassManifestMaxMethods; see MethodsTruncatedCount.
	Methods []ClassMethodEntry `json:"methods,omitempty"`
	// MethodsTruncatedCount is the number of methods omitted because the class
	// exceeded ClassManifestMaxMethods. Zero when no truncation occurred.
	MethodsTruncatedCount int `json:"methods_truncated_count,omitempty"`
	// Fields is the list of field/attribute entries found via CONTAINS edges.
	// Capped at ClassManifestMaxFields; see FieldsTruncatedCount.
	Fields []ClassFieldEntry `json:"fields,omitempty"`
	// FieldsTruncatedCount is the number of fields omitted because the class
	// exceeded ClassManifestMaxFields. Zero when no truncation occurred.
	FieldsTruncatedCount int `json:"fields_truncated_count,omitempty"`
	// Bases is the list of parent class names (EXTENDS edge targets).
	Bases []string `json:"bases,omitempty"`
	// Interfaces is the list of implemented interface names (IMPLEMENTS edge targets).
	Interfaces []string `json:"interfaces,omitempty"`
	// Decorators is the list of decorator/annotation names found on the class
	// entity (e.g. "@Component", "@Path", "@dataclass"). Parsed from the entity
	// Signature and from SCOPE.Pattern decorator neighbours. Deduped.
	Decorators []string `json:"decorators,omitempty"`
}

// ClassMethodEntry describes a single method or constructor of a class.
type ClassMethodEntry struct {
	// Name is the short method name (without the enclosing class prefix).
	Name string `json:"name"`
	// Signature is the full method signature as stored by the extractor
	// (e.g. "public String login(String username, String password)").
	Signature string `json:"signature,omitempty"`
	// Visibility is "public", "private", "protected", or "" (unknown).
	// Inferred from the Signature text on a best-effort basis.
	Visibility string `json:"visibility,omitempty"`
	// IsStatic is true when the entity carries Properties["is_static"]="true".
	IsStatic bool `json:"is_static,omitempty"`
	// Subtype is the extractor subtype: "method", "constructor", or "".
	Subtype string `json:"subtype,omitempty"`
	// StartLine is the first line of the method body (1-indexed).
	StartLine int `json:"start_line,omitempty"`
	// EndLine is the last line of the method body (1-indexed). May be 0 when
	// the extractor did not resolve the end line.
	EndLine int `json:"end_line,omitempty"`
}

// ClassFieldEntry describes a single field or attribute of a class.
type ClassFieldEntry struct {
	// Name is the short field name (without the enclosing class prefix).
	Name string `json:"name"`
	// TypeHint is the declared type of the field as inferred from the Signature
	// (e.g. "UserRepository", "str", "List<String>"). Empty when not available.
	TypeHint string `json:"type_hint,omitempty"`
	// DefaultValue is the literal default value when the extractor captured one.
	// Empty for computed or unknown defaults.
	DefaultValue string `json:"default_value,omitempty"`
	// Visibility is "public", "private", "protected", or "" (unknown).
	Visibility string `json:"visibility,omitempty"`
	// StartLine is the line where the field is declared (1-indexed).
	StartLine int `json:"start_line,omitempty"`
}

// ClassManifestMaxMethods is the cap on the number of method entries in a
// ClassManifest. Classes with more methods will have the excess counted in
// ClassManifest.MethodsTruncatedCount.
const ClassManifestMaxMethods = 100

// ClassManifestMaxFields is the cap on the number of field entries in a
// ClassManifest.
const ClassManifestMaxFields = 100

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

		// Populate source_window: strategy is determined by the entity's kind
		// profile (registry-driven via SectionProfile.SourceWindowStrategy, #1876).
		//
		// StrategyDefault (±20 lines): baseline; preserves original behaviour for
		// all non-Model kinds.
		//
		// StrategyWholeBody: emit the entire class body from start_line to
		// end_line (inclusive), capped at SourceWindowWholeBodyMaxLines.  Used for
		// Model entities where every field declaration is semantically meaningful.
		// A "truncated_at_line" comment is appended when the cap is reached.
		//
		// On error (file deleted, fsevents stall, etc.) we leave the field empty
		// and log a warning — a missing source window must not fail the bundle.
		const sourceWindowHalfLines = 20
		if entity.SourceFile != "" && entity.StartLine > 0 {
			absPath := filepath.Join(seedRepo, entity.SourceFile)

			// Resolve the section profile for this entity to pick the strategy.
			entityProfile := ResolveSectionProfile(entity.Kind, entity.Language)

			var startLine, endLine int
			var truncatedAt int // non-zero when the whole-body cap fires

			switch entityProfile.SourceWindowStrategy {
			case SourceWindowStrategyWholeBody:
				// Emit from start_line to end_line (whole class body).  Fall back
				// to the default window when end_line is missing or equals 0 (the
				// end_line=0 sentinel bug tracked in #1868 is not yet fixed).
				startLine = entity.StartLine
				if entity.EndLine > entity.StartLine {
					endLine = entity.EndLine
				} else {
					// end_line absent: fall back to default window so the output
					// is still useful rather than a 1-line stub.
					endLine = entity.StartLine + sourceWindowHalfLines
				}
				// Apply the safety cap (#1876 spec: 400 lines, log a warning).
				if endLine-startLine+1 > SourceWindowWholeBodyMaxLines {
					truncatedAt = startLine + SourceWindowWholeBodyMaxLines - 1
					endLine = truncatedAt
					fmt.Fprintf(os.Stderr,
						"docgen: source_window: Model entity %q body exceeds %d-line cap — "+
							"clipping at line %d (end_line=%d); set truncated_at_line annotation\n",
						entity.ID, SourceWindowWholeBodyMaxLines, truncatedAt, entity.EndLine)
				}
			default:
				// SourceWindowStrategyDefault: ±20 lines around start_line.
				startLine = entity.StartLine - sourceWindowHalfLines
				if startLine < 1 {
					startLine = 1
				}
				endLine = entity.EndLine + sourceWindowHalfLines
				if endLine < entity.StartLine+sourceWindowHalfLines {
					endLine = entity.StartLine + sourceWindowHalfLines
				}
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
				if truncatedAt > 0 {
					sw += fmt.Sprintf("\n# truncated_at_line: %d (body exceeds %d-line cap; full end_line=%d)\n",
						truncatedAt, SourceWindowWholeBodyMaxLines, entity.EndLine)
				}
				gc.SourceWindow = sw
			}
		}

		// Populate ClassManifest when the seed entity is class-like (#1861).
		// Walk the neighbours collected above to build a structured enumeration
		// of methods, fields, bases, interfaces, and decorators without requiring
		// the LLM to re-parse the source_window.
		if isClassLikeKind(entity.Kind) {
			gc.ClassManifest = buildClassManifest(entity, neighbours, neighbourKinds)
		}

		// Populate ModuleReadme and ModuleConfigs for Module-kind seeds (#1880).
		if isModuleKind(entity.Kind) {
			gc.ModuleReadme, gc.ModuleConfigs = buildModuleSupplements(entity, seedRepo, neighbours, neighbourKinds)
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
// ClassManifest helpers (#1861)
// ---------------------------------------------------------------------------

// decoratorTokenRE matches @Identifier patterns in a signature string.
// Used to extract class-level annotations / decorators.
var decoratorTokenRE = regexp.MustCompile(`@([A-Za-z_]\w*)`)

// isClassLikeKind returns true when the entity kind represents a class-like
// construct that should have a ClassManifest populated. The check is
// case-insensitive and handles both bare kind strings ("Class", "component")
// and SCOPE.* prefixed forms ("SCOPE.Component", "SCOPE.Class").
//
// Class-like kinds: Class, Component, Controller, Service, Model, View,
// UIComponent, and their SCOPE.* variants. Deliberately excludes Operation,
// Function, Module, Schema, and other leaf-or-file-level kinds.
func isClassLikeKind(kind string) bool {
	k := strings.ToLower(kind)
	// Strip "scope." prefix for uniform matching.
	k = strings.TrimPrefix(k, "scope.")
	switch {
	case k == "class",
		k == "component",
		k == "uicomponent",
		k == "controller",
		k == "service",
		k == "model",
		k == "view":
		return true
	}
	// Substring matches for compound kinds (e.g. "datamodel", "viewcontroller").
	for _, sub := range []string{"class", "component", "controller", "service", "model"} {
		if strings.Contains(k, sub) {
			return true
		}
	}
	return false
}

// shortName strips an enclosing class prefix "ClassName.memberName" → "memberName".
// If the name has no dot it is returned as-is.
func shortName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// inferVisibility reads the visibility modifier from a method/field signature
// on a best-effort basis. Returns "public", "private", "protected", or "".
func inferVisibility(sig string) string {
	lower := strings.ToLower(sig)
	switch {
	case strings.HasPrefix(lower, "private ") || strings.Contains(lower, " private "):
		return "private"
	case strings.HasPrefix(lower, "protected ") || strings.Contains(lower, " protected "):
		return "protected"
	case strings.HasPrefix(lower, "public ") || strings.Contains(lower, " public "):
		return "public"
	}
	return ""
}

// typeHintFromSignature extracts the declared type from a field signature.
// Field signatures are typically "TypeName fieldName" or just "fieldName: TypeName".
// Returns an empty string when no type can be inferred.
func typeHintFromSignature(sig string) string {
	sig = strings.TrimSpace(sig)
	if sig == "" {
		return ""
	}
	// TypeScript / Python style: "fieldName: TypeName"
	if idx := strings.Index(sig, ":"); idx >= 0 {
		return strings.TrimSpace(sig[idx+1:])
	}
	// Java / C# style: "TypeName fieldName" (space-separated, first token is type).
	// Strip modifiers first.
	for _, mod := range []string{"private ", "public ", "protected ", "static ", "final ", "readonly "} {
		sig = strings.ReplaceAll(sig, mod, "")
	}
	sig = strings.TrimSpace(sig)
	parts := strings.Fields(sig)
	if len(parts) >= 2 {
		// First token is the type, last is the name.
		return parts[0]
	}
	return ""
}

// buildClassManifest constructs a ClassManifest for the seed entity by
// inspecting the neighbours slice (already loaded by loadEntityContext) and
// the neighbourKinds slice (edge kinds, index-aligned with neighbours).
//
// It collects:
//   - Method entries: CONTAINS neighbours with Kind=="SCOPE.Operation"
//   - Field entries:  CONTAINS neighbours with Kind=="SCOPE.Schema" and Subtype=="field"
//   - Bases:          EXTENDS neighbours
//   - Interfaces:     IMPLEMENTS neighbours
//   - Decorators:     parsed from entity.Signature and from SCOPE.Pattern decorator neighbours
func buildClassManifest(entity *graph.Entity, neighbours []graph.Entity, neighbourKinds []string) *ClassManifest {
	if entity == nil {
		return nil
	}

	m := &ClassManifest{}

	// --- Decorators from class entity Signature ---
	if entity.Signature != "" {
		seen := map[string]bool{}
		for _, match := range decoratorTokenRE.FindAllString(entity.Signature, -1) {
			if !seen[match] {
				seen[match] = true
				m.Decorators = append(m.Decorators, match)
			}
		}
	}

	// Track whether we've added a decorator name (for dedup across sources).
	decoratorSeen := map[string]bool{}
	for _, d := range m.Decorators {
		decoratorSeen[d] = true
	}

	// Walk 1-hop neighbours.
	totalMethods := 0
	totalFields := 0
	for i, n := range neighbours {
		rel := ""
		if i < len(neighbourKinds) {
			rel = neighbourKinds[i]
		}

		switch rel {
		case "CONTAINS":
			lkind := strings.ToLower(n.Kind)
			isMethod := strings.Contains(lkind, "operation") &&
				(n.Subtype == "method" || n.Subtype == "constructor" || n.Subtype == "")
			isField := strings.Contains(lkind, "schema") && n.Subtype == "field"

			if isMethod {
				totalMethods++
				if totalMethods <= ClassManifestMaxMethods {
					entry := ClassMethodEntry{
						Name:      shortName(n.Name),
						Signature: n.Signature,
						Subtype:   n.Subtype,
						StartLine: n.StartLine,
						EndLine:   n.EndLine,
					}
					if n.Properties != nil && n.Properties["is_static"] == "true" {
						entry.IsStatic = true
					}
					entry.Visibility = inferVisibility(n.Signature)
					m.Methods = append(m.Methods, entry)
				}
			} else if isField {
				totalFields++
				if totalFields <= ClassManifestMaxFields {
					entry := ClassFieldEntry{
						Name:      shortName(n.Name),
						TypeHint:  typeHintFromSignature(n.Signature),
						StartLine: n.StartLine,
					}
					entry.Visibility = inferVisibility(n.Signature)
					m.Fields = append(m.Fields, entry)
				}
			}

		case "EXTENDS":
			m.Bases = append(m.Bases, shortName(n.Name))

		case "IMPLEMENTS":
			m.Interfaces = append(m.Interfaces, shortName(n.Name))
		}

		// Decorator pattern entities: SCOPE.Pattern with subtype "decorator"
		// that target this class (Properties["target_name"] matches class name).
		if strings.Contains(strings.ToLower(n.Kind), "pattern") &&
			strings.ToLower(n.Subtype) == "decorator" {
			if dn, ok := n.Properties["decorator_name"]; ok && dn != "" {
				token := "@" + dn
				if !decoratorSeen[token] {
					decoratorSeen[token] = true
					m.Decorators = append(m.Decorators, token)
				}
			}
		}
	}

	// Record truncation counts.
	if totalMethods > ClassManifestMaxMethods {
		m.MethodsTruncatedCount = totalMethods - ClassManifestMaxMethods
	}
	if totalFields > ClassManifestMaxFields {
		m.FieldsTruncatedCount = totalFields - ClassManifestMaxFields
	}

	// Return nil manifest when no data was collected (e.g. class with no
	// child entities in the graph and no signature annotations) — this keeps
	// the JSON output clean for entities whose extractors haven't been run yet.
	if len(m.Methods) == 0 && len(m.Fields) == 0 &&
		len(m.Bases) == 0 && len(m.Interfaces) == 0 && len(m.Decorators) == 0 {
		return nil
	}

	return m
}

// ---------------------------------------------------------------------------
// Module supplements helpers (#1880)
// ---------------------------------------------------------------------------

// readmeNames lists the README basenames we recognise, in priority order.
var readmeNames = []string{"README.md", "README.rst", "README.txt", "README"}

// readmeLanguage returns the markup language token for a README filename.
func readmeLanguage(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".md"):
		return "markdown"
	case strings.HasSuffix(lower, ".rst"):
		return "rst"
	default:
		return "text"
	}
}

// findReadmeInDir returns the repo-relative path of the first recognised README
// found in dir. Case-insensitive. Returns "" when none exists.
func findReadmeInDir(repoRoot, relDir string) string {
	absDir := filepath.Join(repoRoot, relDir)
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return ""
	}
	entryMap := make(map[string]string, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			entryMap[strings.ToLower(e.Name())] = e.Name()
		}
	}
	for _, candidate := range readmeNames {
		if actual, ok := entryMap[strings.ToLower(candidate)]; ok {
			return filepath.ToSlash(filepath.Join(relDir, actual))
		}
	}
	return ""
}

// readFirstNLines reads at most n lines from absPath. Returns "" on error.
func readFirstNLines(absPath string, n int) string {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// capKeysTopLevel truncates a comma-joined keys string to ModuleConfigMaxKeys,
// appending "+N more" when truncation occurs.
func capKeysTopLevel(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ",")
	if len(parts) <= ModuleConfigMaxKeys {
		return raw
	}
	more := len(parts) - ModuleConfigMaxKeys
	return fmt.Sprintf("%s,+%d more", strings.Join(parts[:ModuleConfigMaxKeys], ","), more)
}

// isModuleKind returns true when the entity kind is a Module kind (#1880).
func isModuleKind(kind string) bool {
	k := strings.ToLower(strings.TrimPrefix(kind, "SCOPE."))
	return k == "module" || strings.Contains(k, "module")
}

// buildModuleSupplements locates the README in the module directory and
// collects sibling Config entities from DEPENDS_ON_CONFIG edges (#1880).
func buildModuleSupplements(entity *graph.Entity, repoRoot string, neighbours []graph.Entity, neighbourKinds []string) (*ModuleReadme, []ModuleConfigEntry) {
	if entity == nil {
		return nil, nil
	}
	relDir := "."
	if entity.SourceFile != "" {
		d := filepath.ToSlash(filepath.Dir(entity.SourceFile))
		if d != "" && d != "." {
			relDir = d
		}
	}
	var moduleReadme *ModuleReadme
	if relReadme := findReadmeInDir(repoRoot, relDir); relReadme != "" {
		absReadme := filepath.Join(repoRoot, filepath.FromSlash(relReadme))
		if content := readFirstNLines(absReadme, ModuleReadmeMaxLines); content != "" {
			moduleReadme = &ModuleReadme{
				File:     relReadme,
				Content:  content,
				Language: readmeLanguage(filepath.Base(relReadme)),
			}
		}
	}
	var configs []ModuleConfigEntry
	for i, n := range neighbours {
		if len(configs) >= ModuleConfigMaxConfigs {
			break
		}
		rel := ""
		if i < len(neighbourKinds) {
			rel = neighbourKinds[i]
		}
		if rel != "DEPENDS_ON_CONFIG" {
			continue
		}
		if !strings.Contains(strings.ToLower(n.Kind), "config") {
			continue
		}
		configs = append(configs, configEntryFromEntity(&n))
	}
	if len(configs) == 0 {
		configs = nil
	}
	return moduleReadme, configs
}

// configEntryFromEntity extracts ModuleConfigEntry fields from a SCOPE.Config entity.
func configEntryFromEntity(n *graph.Entity) ModuleConfigEntry {
	props := n.Properties
	get := func(key string) string {
		if props == nil {
			return ""
		}
		return props[key]
	}
	entry := ModuleConfigEntry{
		Name:    n.Name,
		Format:  get("format"),
		Subtype: get("subtype"),
	}
	name := strings.ToLower(n.Name)
	switch {
	case name == "pyproject.toml":
		entry.ProjectName = get("project_name")
		entry.Dependencies = get("dependencies")
		entry.Scripts = get("scripts")
		entry.KeysTopLevel = capKeysTopLevel(get("keys_top_level"))
	case name == "package.json":
		entry.ProjectName = get("project_name")
		entry.Dependencies = get("dependencies")
		entry.Scripts = get("scripts")
	case name == "pom.xml":
		entry.ProjectName = get("project_name")
		entry.Dependencies = get("dependencies")
	case name == "go.mod":
		entry.ProjectName = get("project_name")
		entry.Dependencies = get("dependencies")
	default:
		entry.ProjectName = get("project_name")
		entry.Dependencies = get("dependencies")
		entry.Scripts = get("scripts")
		entry.KeysTopLevel = capKeysTopLevel(get("keys_top_level"))
	}
	return entry
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
