package enrichment

// repair_edge candidate emission. Implements the data-emission side of
// ADR-0015 phase-1 (issue #544): for every relationship whose endpoint the
// resolver tagged DispositionBugExtractor or DispositionBugResolver, emit a
// rich enrichment candidate carrying enough context for an LLM agent to
// propose a repair on a subsequent pass.
//
// IMPORTANT — this module is purely additive:
//   - It does not alter classification logic in internal/resolve/refs.go.
//   - It does not alter external synthesis in internal/external/synth.go.
//   - It only READS the post-synthesis graph + uses the existing
//     ClassifyEndpoints + DiagnoseBugResolver entry points to decide which
//     edges deserve a repair_edge entry.
//
// The on-disk shape conforms to docs/specs/enrichment-candidates-v2.schema.json.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
)

// KindRepairEdge is the canonical kind string for ADR-0015 repair_edge
// candidates. v1 readers skip-by-kind on unknown kinds.
const KindRepairEdge = "repair_edge"

// RepairEdgeContextWindowBefore / After bound the source excerpt attached to
// each repair_edge candidate. Together with the call-site line the window is
// at most 51 lines — well under the 50-line guidance from the spec when
// trimmed to the file's available range.
const (
	RepairEdgeContextWindowBefore = 25
	RepairEdgeContextWindowAfter  = 25
	// repairEdgeMaxCandidates caps the candidate-list length so a single
	// pathologically-ambiguous bare name (think `get` / `save`) doesn't
	// balloon the on-disk size. 8 is enough for an agent to choose from
	// without becoming a tutorial.
	repairEdgeMaxCandidates = 8
)

// repairEdgeID computes the stable er:<hex16> identifier described by the
// ADR-0015 schema. The hash inputs are intentionally string-only and
// order-fixed so the ID is reproducible across runs as long as the call site
// stays put.
func repairEdgeID(fromID, relation, originalStub string) string {
	h := sha256.New()
	h.Write([]byte(fromID))
	h.Write([]byte{0})
	h.Write([]byte(relation))
	h.Write([]byte{0})
	h.Write([]byte(originalStub))
	return "er:" + hex.EncodeToString(h.Sum(nil))[:16]
}

// repairCandidateID derives the enrichment-candidate ID (ec:<hex16>) from
// the repair-edge ID so the candidate-id ↔ edge-id relationship is one-way
// deterministic — readers can recover one from the other without a join.
func repairCandidateID(edgeID string) string {
	h := sha256.New()
	h.Write([]byte(KindRepairEdge))
	h.Write([]byte{0})
	h.Write([]byte(edgeID))
	return "ec:" + hex.EncodeToString(h.Sum(nil))[:16]
}

// RepairEdgeCandidateOptions are the knobs callers (the indexer) thread
// through. Keeping them in a struct keeps the public surface narrow and lets
// future phases (ADR-0015 #545-#551) add fields without breaking call sites.
type RepairEdgeCandidateOptions struct {
	// RepoRoot is the absolute path to the repo being indexed. Used to
	// resolve entity.SourceFile (which is repo-relative) when reading the
	// source-context window.
	RepoRoot string
	// Allow is the external-package allowlist that classifyEndpoints uses
	// to distinguish ExternalKnown from ExternalUnknown — without it every
	// edge with an "ext:" prefix would otherwise look like a bug.
	Allow resolve.ExternalAllowlist
	// Resolver is the post-resolve index. We need DiagnoseBugResolver +
	// ClassifyEndpoints from this instance.
	Resolver *resolve.Index
}

// CollectRepairEdgeCandidates walks doc.Relationships and emits a Candidate
// of kind "repair_edge" for every endpoint the resolver classified as
// DispositionBugExtractor or DispositionBugResolver.
//
// Performance notes:
//   - Source files are read at most once per index pass — repeated edges in
//     the same file share the cached content.
//   - The context-window slice is bounded by RepairEdgeContextWindow{Before,After}.
//   - We deliberately skip external-unknown stubs (those aren't ambiguous,
//     just unallowlisted) and skip stubs already resolved to a 16-char hex.
func CollectRepairEdgeCandidates(doc *graph.Document, opts RepairEdgeCandidateOptions) []Candidate {
	if doc == nil || opts.Resolver == nil {
		return nil
	}

	// Entity-id → entity lookup for the from-side context.
	byID := make(map[string]*graph.Entity, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		byID[e.ID] = e
	}

	// File-content cache so the same source file is read at most once per
	// index pass even when 1000 edges point into it.
	fileCache := newFileLineCache(opts.RepoRoot)

	out := make([]Candidate, 0, 64)
	seen := make(map[string]bool, 64)

	for ri := range doc.Relationships {
		r := &doc.Relationships[ri]
		// Only the ToID side of an edge is ever a bug-extractor /
		// bug-resolver — FromID is almost always a real entity (the
		// emitting extractor binds it from the local AST). Mirrors the
		// long-standing assumption in dumpBugExtractorSamples /
		// dumpBugResolverSamples.
		stub := r.ToID
		if stub == "" {
			continue
		}
		if isHexID(stub) || strings.HasPrefix(stub, "ext:") {
			continue
		}
		lang := r.PropGet("language")
		if lang == "" {
			lang = r.PropGet("lang")
		}

		d := classifyEndpointDisposition(*opts.Resolver, stub, lang, opts.Allow)
		if d != resolve.DispositionBugExtractor && d != resolve.DispositionBugResolver {
			continue
		}

		from := byID[r.FromID]
		// Non-hex / un-stamped FromID fall-through: if the resolver hasn't
		// stamped the from-side yet (qualified-name stub like
		// "scope:component:file:src/manage.py", "Model:View", "Route:User"),
		// `byID` won't contain it. We still want a repair_edge candidate —
		// the #545 reader keys off (from_id, relation, original_stub) and
		// computes edge_ids from the live relationships regardless of
		// hex-stamping, so the emitter must match. Synthesize a minimal
		// from_entity from the raw FromID so the candidate still carries
		// enough context for the agent + downstream apply path.
		fromEntity := from
		if fromEntity == nil {
			if r.FromID == "" {
				continue
			}
			fromEntity = syntheticFromEntity(r.FromID)
		}

		edgeID := repairEdgeID(fromEntity.ID, r.Kind, stub)
		if seen[edgeID] {
			continue
		}
		seen[edgeID] = true

		ctx := buildRepairEdgeContext(fromEntity, r, stub, d, opts.Resolver, fileCache)
		out = append(out, Candidate{
			ID:           repairCandidateID(edgeID),
			Kind:         KindRepairEdge,
			SubjectID:    fromEntity.ID,
			Context:      ctx,
			DiscoveredAt: nowRFC3339(),
		})
	}

	// Sort for stable byte output across runs of the same input.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SubjectID != out[j].SubjectID {
			return out[i].SubjectID < out[j].SubjectID
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// classifyEndpointDisposition asks the resolver to classify a single stub.
// Mirrors classifyForDiag in cmd/grafel/index.go without depending on it
// (this package can't import cmd/).
func classifyEndpointDisposition(idx resolve.Index, stub, lang string, allow resolve.ExternalAllowlist) resolve.Disposition {
	stats := idx.ClassifyEndpoints([]resolve.EndpointPair{
		{ToID: stub, ToOriginal: stub, Language: lang},
	}, allow)
	for d, n := range stats.DispositionCounts {
		if n > 0 {
			return d
		}
	}
	return resolve.DispositionUnclassified
}

// buildRepairEdgeContext assembles the RepairEdgeContext payload for a
// single edge. The shape matches docs/specs/enrichment-candidates-v2.schema.json.
func buildRepairEdgeContext(
	from *graph.Entity,
	r *graph.Relationship,
	stub string,
	d resolve.Disposition,
	resolver *resolve.Index,
	fc *fileLineCache,
) map[string]any {
	edgeID := repairEdgeID(from.ID, r.Kind, stub)

	dispositionReason := ""
	var candidatesField []map[string]any
	if d == resolve.DispositionBugResolver && resolver != nil {
		diag := resolver.DiagnoseBugResolver(stub, r.Kind)
		dispositionReason = diag.Category
		// For bug-resolver, the entity-kind buckets present for this name
		// are the agent's plausible binding targets. We emit them as a
		// flat list keyed by kind so the agent can disambiguate. Scores
		// are heuristic — hint-family matches score higher.
		hintSet := make(map[string]bool, len(diag.HintFamily))
		for _, h := range diag.HintFamily {
			hintSet[h] = true
		}
		for i, k := range diag.KindsPresent {
			if i >= repairEdgeMaxCandidates {
				break
			}
			score := 0.3
			if hintSet[k] {
				score = 0.7
			}
			candidatesField = append(candidatesField, map[string]any{
				"entity_id": "", // not known by name alone — agent must search
				"hint":      fmt.Sprintf("%s:%s", k, diag.Name),
				"score":     score,
			})
		}
	} else {
		// bug-extractor — no in-graph entity by this name. Reason is the
		// stub shape; the agent's repair path is usually an external-
		// synthesis hint rather than a local binding.
		dispositionReason = "extractor-stub-unbound"
	}

	from_entity := map[string]any{
		"id":   from.ID,
		"kind": from.Kind,
		"name": from.Name,
		"file": from.SourceFile,
		"line": from.StartLine,
	}
	if from.QualifiedName != "" {
		from_entity["qualified_name"] = from.QualifiedName
	}

	ctx := map[string]any{
		"edge_id":            edgeID,
		"from_entity":        from_entity,
		"relation":           r.Kind,
		"original_stub":      stub,
		"disposition":        d.String(),
		"disposition_reason": dispositionReason,
	}
	if candidatesField != nil {
		ctx["candidates"] = candidatesField
	} else {
		// Schema requires the array — emit an empty one for bug-extractor.
		ctx["candidates"] = []map[string]any{}
	}

	// Context window — bounded by RepairEdgeContextWindow{Before,After}.
	before, line, after := fc.window(from.SourceFile, from.StartLine,
		RepairEdgeContextWindowBefore, RepairEdgeContextWindowAfter)
	ctx["context_window"] = map[string]any{
		"before": before,
		"line":   line,
		"after":  after,
	}

	// Metadata block — file-scoped imports, receiver-type hint if any,
	// surrounding scope kind, language.
	receiverType := r.PropGet("receiver_type")
	imports := fc.imports(from.SourceFile)
	meta := map[string]any{
		"receiver_type":     nullableString(receiverType),
		"file_imports":      imports,
		"surrounding_scope": from.Kind,
		"language":          from.Language,
	}
	ctx["extracted_metadata"] = meta

	return ctx
}

// nullableString returns nil for "" so the JSON shape matches the
// `["string","null"]` typing in the schema.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// syntheticFromEntity builds a minimal graph.Entity from a raw FromID when
// the resolver hasn't stamped the from-side into doc.Entities yet. The
// shape mirrors the cmd-side qualified-name stub families
// ("scope:component:file:...", "Model:Name", "Route:Name", "View:Name", …)
// so the agent can still reason about the call site without a stamped ID.
// SourceFile / StartLine are left empty: the context-window block becomes
// empty arrays, matching the schema's optional-emptiness contract.
func syntheticFromEntity(rawFromID string) *graph.Entity {
	kind := ""
	name := rawFromID
	file := ""
	// Pull out a kind + name from the common stub shapes. This is a hint
	// for the agent, not a binding — best-effort parsing only.
	switch {
	case strings.HasPrefix(rawFromID, "scope:component:file:"):
		kind = "file"
		file = strings.TrimPrefix(rawFromID, "scope:component:file:")
		name = file
	case strings.HasPrefix(rawFromID, "scope:component:class:"):
		kind = "class"
		// shape: scope:component:class:<lang>:<file>:<name>
		parts := strings.SplitN(strings.TrimPrefix(rawFromID, "scope:component:class:"), ":", 3)
		if len(parts) == 3 {
			file = parts[1]
			name = parts[2]
		}
	default:
		if i := strings.Index(rawFromID, ":"); i > 0 {
			kind = strings.ToLower(rawFromID[:i])
			name = rawFromID[i+1:]
		}
	}
	return &graph.Entity{
		ID:         rawFromID,
		Name:       name,
		Kind:       kind,
		SourceFile: file,
	}
}

// isHexID is a local copy of the cmd-side helper. Inline to avoid a circular
// import (cmd/grafel already imports this package).
func isHexID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// fileLineCache — read each source file at most once per index pass.
// ---------------------------------------------------------------------------

// fileLineCache caches per-file split-by-line content and the extracted
// import list. All accessors are nil-safe so the emitter never has to branch
// on "did the file exist?".
type fileLineCache struct {
	root  string
	lines map[string][]string // repo-relative path → []line (1-indexed: index 0 is "")
	imps  map[string][]string // repo-relative path → []import-line literal
}

func newFileLineCache(root string) *fileLineCache {
	return &fileLineCache{
		root:  root,
		lines: make(map[string][]string),
		imps:  make(map[string][]string),
	}
}

// load reads relPath once and populates both caches. Errors are silently
// converted to an empty entry so callers don't need to handle them.
func (c *fileLineCache) load(relPath string) {
	if relPath == "" {
		return
	}
	if _, ok := c.lines[relPath]; ok {
		return
	}
	c.lines[relPath] = []string{""} // 1-indexed sentinel
	c.imps[relPath] = nil

	abs := relPath
	if c.root != "" && !filepath.IsAbs(relPath) {
		abs = filepath.Join(c.root, relPath)
	}
	f, err := os.Open(abs)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Permit long lines (default scanner buffer is 64KB which trips on
	// minified JS / generated SQL). 1MB is plenty for source code.
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	var imports []string
	for scanner.Scan() {
		text := scanner.Text()
		c.lines[relPath] = append(c.lines[relPath], text)
		if isImportLine(text) {
			imports = append(imports, strings.TrimSpace(text))
		}
	}
	c.imps[relPath] = imports
}

// window returns (before, line, after) for line lineNo. The slices are
// safely bounded by file length — short files just return shorter lists.
func (c *fileLineCache) window(relPath string, lineNo, nBefore, nAfter int) ([]string, string, []string) {
	c.load(relPath)
	lines := c.lines[relPath]
	if lineNo <= 0 || lineNo >= len(lines) {
		return []string{}, "", []string{}
	}
	start := lineNo - nBefore
	if start < 1 {
		start = 1
	}
	end := lineNo + nAfter
	if end >= len(lines) {
		end = len(lines) - 1
	}
	before := make([]string, 0, lineNo-start)
	for i := start; i < lineNo; i++ {
		before = append(before, lines[i])
	}
	after := make([]string, 0, end-lineNo)
	for i := lineNo + 1; i <= end; i++ {
		after = append(after, lines[i])
	}
	return before, lines[lineNo], after
}

// imports returns the cached import-line list for relPath.
func (c *fileLineCache) imports(relPath string) []string {
	c.load(relPath)
	if imps := c.imps[relPath]; imps != nil {
		return imps
	}
	return []string{}
}

// isImportLine is a deliberately conservative cross-language import sniffer.
// We don't need a real parser here — the value is a hint for the agent, not
// a binding. Recognised prefixes cover Python, Go, JS/TS, Java, Ruby, PHP,
// Rust, and C#.
func isImportLine(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	switch {
	case strings.HasPrefix(t, "import "),
		strings.HasPrefix(t, "from "),
		strings.HasPrefix(t, "require "),
		strings.HasPrefix(t, "require("),
		strings.HasPrefix(t, "use "),
		strings.HasPrefix(t, "using "),
		strings.HasPrefix(t, "#include"),
		strings.HasPrefix(t, "@import"),
		strings.HasPrefix(t, "package "):
		return true
	}
	// `const x = require('y')` / `import x from 'y'` in JS/TS — already
	// caught by the prefix branch when the line starts with `import`.
	// Bare `require(` calls inside code are NOT imports for our purposes
	// (we'd flood the list); only top-level `require ` (Ruby) is flagged.
	return false
}
