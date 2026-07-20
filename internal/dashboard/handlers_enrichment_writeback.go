package dashboard

// handlers_enrichment_writeback.go — Enrichment write-back endpoint (#1304).
//
//	POST /api/enrichments/{group}/write?subject_id=X
//
// After an agent generates a description for entity X, this endpoint persists
// it to three durable targets:
//
//  1. In-memory graph property — entity.Properties["description"] is set so
//     that the next qualifiesForEnrichment check returns false for this entity.
//     The updated graph.json is written atomically to disk so the change
//     survives daemon restarts.
//
//  2. YAML-frontmatter Markdown file — written to
//     <repo>/docs/enrichments/<kind>/<entity_id>.md following the convention
//     documented in skills/grafel-tech-docs/SKILL.md. This is the file that
//     dashboard panels and MCP tools read for rich descriptions.
//
//  3. Audit log — the enrichment action is recorded via s.auditor.OK so it
//     appears in GET /api/audit and streams to SSE subscribers.
//
// Design notes:
//   - Atomic write (tmp + rename) for both the Markdown doc and graph.json.
//   - Idempotent: a second call for the same entity_id + kind overwrites the
//     existing doc and property value.
//   - Validation: description must be ≥ 10 chars and must not look like
//     placeholder text ("TODO", "FIXME", "describe this", "N/A", …).
//   - Cache invalidation: the group entry in GraphCache is dropped so the
//     next GET against this group re-loads the updated graph.json.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire shapes
// ─────────────────────────────────────────────────────────────────────────────

// enrichmentWritebackRequest is the JSON body for
// POST /api/enrichments/{group}/write.
type enrichmentWritebackRequest struct {
	// Description is the agent-generated natural-language description.
	// Must be ≥ 10 characters and must not be placeholder text.
	Description string `json:"description"`

	// Kind is the enrichment kind used to form the frontmatter and the doc
	// path. Defaults to "describe_entity" when empty.
	// Typical values: "http_endpoint", "process_flow", "message_topic",
	// "describe_entity".
	Kind string `json:"kind,omitempty"`

	// ModelUsed is the Claude model ID that produced the description
	// (e.g. "claude-haiku-4-5"). Stored in the frontmatter for auditability.
	ModelUsed string `json:"model_used,omitempty"`

	// TokensUsed is the total token count consumed for this enrichment.
	// Stored in the frontmatter for cost attribution.
	TokensUsed int `json:"tokens_used,omitempty"`
}

// enrichmentWritebackResponse is returned on success.
type enrichmentWritebackResponse struct {
	// SubjectID echoes the entity ID that was enriched.
	SubjectID string `json:"subject_id"`
	// DocPath is the absolute path to the written Markdown file.
	DocPath string `json:"doc_path"`
	// GraphPath is the absolute path to the updated graph.json.
	GraphPath string `json:"graph_path"`
	// EnrichedAt is the RFC3339 timestamp of the write.
	EnrichedAt string `json:"enriched_at"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Validation helpers
// ─────────────────────────────────────────────────────────────────────────────

// minDescriptionLen is the minimum acceptable description length in characters.
const minDescriptionLen = 10

// placeholderRE matches common placeholder strings that indicate the agent
// produced garbage output. Checked case-insensitively.
var placeholderRE = regexp.MustCompile(
	`(?i)\b(todo|fixme|describe\s+this|n/a|placeholder|tbd|coming\s+soon|not\s+implemented)\b`,
)

// validateDescription returns a non-empty error string when description should
// be rejected.
func validateDescription(desc string) string {
	trimmed := strings.TrimSpace(desc)
	if len([]rune(trimmed)) < minDescriptionLen {
		return fmt.Sprintf("description too short: got %d chars, need ≥ %d", len([]rune(trimmed)), minDescriptionLen)
	}
	if placeholderRE.MatchString(trimmed) {
		return "description looks like placeholder text; supply a real description"
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler
// ─────────────────────────────────────────────────────────────────────────────

// handleEnrichmentWriteback — POST /api/enrichments/{group}/write
//
// Query params:
//
//	subject_id — required; the entity ID to enrich (e.g. "ep-abc123")
//
// Request body: enrichmentWritebackRequest (JSON).
//
// On success returns 200 with an enrichmentWritebackResponse.
// Returns 400 for validation failures, 404 when the entity is not found in
// the group, and 500 for I/O errors.
func (s *Server) handleEnrichmentWriteback(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if group == "" {
		writeErr(w, http.StatusBadRequest, "group required")
		return
	}

	subjectID := r.URL.Query().Get("subject_id")
	if subjectID == "" {
		writeErr(w, http.StatusBadRequest, "subject_id query parameter required")
		return
	}

	var req enrichmentWritebackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	// Default kind.
	if req.Kind == "" {
		req.Kind = "describe_entity"
	}

	// Validate description.
	if msg := validateDescription(req.Description); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}

	// Load group to find the entity.
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	// Locate the entity across all repos in the group.
	type foundEntity struct {
		repoPath string
		entity   *graph.Entity
		doc      *graph.Document
	}
	var found *foundEntity
	for _, repo := range grp.Repos {
		if repo == nil || repo.Path == "" || repo.Doc == nil {
			continue
		}
		for i := range repo.Doc.Entities {
			if repo.Doc.Entities[i].ID == subjectID {
				found = &foundEntity{
					repoPath: repo.Path,
					entity:   &repo.Doc.Entities[i],
					doc:      repo.Doc,
				}
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		writeErr(w, http.StatusNotFound, "entity not found in group: "+subjectID)
		return
	}

	now := time.Now().UTC()
	enrichedAt := now.Format(time.RFC3339)

	// ─── Target 1: update in-memory graph property ────────────────────────
	if found.entity.PropLen() == 0 {
		found.entity.PropsReplace(make(map[string]string))
	}
	found.entity.PropSet("description", req.Description)

	// Persist the updated graph to disk — both graph.fb (canonical binary,
	// preferred by LoadGraphFromDir / MCP) and graph.json (backward-compatible
	// JSON, read by debug tools and external integrations). Writing only one
	// format leaves the other stale, causing ghost entity IDs for direct
	// graph.json readers (fixes #1702).
	stateDir := daemon.StateDirForRepo(found.repoPath)
	fbPath := filepath.Join(stateDir, "graph.fb")
	graphPath := daemon.GraphPathForRepo(found.repoPath)

	if err := fbwriter.WriteAtomic(fbPath, found.doc); err != nil {
		s.auditor.Err("enrichment_writeback", group, map[string]any{
			"subject_id": subjectID,
			"kind":       req.Kind,
		}, "write graph.fb: "+err.Error())
		writeErr(w, http.StatusInternalServerError, "persist graph.fb: "+err.Error())
		return
	}
	if err := graph.WriteAtomic(graphPath, found.doc, false); err != nil {
		s.auditor.Err("enrichment_writeback", group, map[string]any{
			"subject_id": subjectID,
			"kind":       req.Kind,
		}, "write graph.json: "+err.Error())
		writeErr(w, http.StatusInternalServerError, "persist graph.json: "+err.Error())
		return
	}
	// Stamp identical mtime on both files so the on-disk pair is never
	// mistaken for a partial write (#1626 pattern).
	writeNow := time.Now()
	_ = os.Chtimes(fbPath, writeNow, writeNow)
	_ = os.Chtimes(graphPath, writeNow, writeNow)

	// Invalidate the cache so the next GET re-reads the updated graph.
	s.graphs.Invalidate(group)

	// ─── Target 2: write YAML-frontmatter Markdown doc ────────────────────
	// Path: <repo>/docs/enrichments/<kind>/<entity_id>.md
	// This is the convention from skills/grafel-tech-docs/SKILL.md.
	docPath := filepath.Join(
		found.repoPath, "docs", "enrichments", req.Kind,
		sanitizePathSegment(subjectID)+".md",
	)
	if err := writeEnrichmentDoc(docPath, subjectID, req, enrichedAt, found.entity); err != nil {
		s.auditor.Err("enrichment_writeback", group, map[string]any{
			"subject_id": subjectID,
			"kind":       req.Kind,
		}, "write doc file: "+err.Error())
		writeErr(w, http.StatusInternalServerError, "write enrichment doc: "+err.Error())
		return
	}

	// ─── Target 3: audit ─────────────────────────────────────────────────
	s.auditor.OK("enrichment_writeback", group, map[string]any{
		"subject_id":  subjectID,
		"kind":        req.Kind,
		"model_used":  req.ModelUsed,
		"tokens_used": req.TokensUsed,
		"doc_path":    docPath,
		"graph_path":  graphPath,
		"enriched_at": enrichedAt,
	})

	writeJSON(w, http.StatusOK, enrichmentWritebackResponse{
		SubjectID:  subjectID,
		DocPath:    docPath,
		GraphPath:  graphPath,
		EnrichedAt: enrichedAt,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Markdown doc writer
// ─────────────────────────────────────────────────────────────────────────────

// writeEnrichmentDoc writes a YAML-frontmatter Markdown file to docPath.
// The file is written atomically (tmp + rename) to prevent partial writes.
// When the file already exists it is overwritten (idempotent).
func writeEnrichmentDoc(
	docPath string,
	entityID string,
	req enrichmentWritebackRequest,
	enrichedAt string,
	entity *graph.Entity,
) error {
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	content := buildEnrichmentDocContent(entityID, req, enrichedAt, entity)

	tmp := docPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, docPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// buildEnrichmentDocContent assembles the full Markdown content (YAML
// frontmatter + prose body) for an enriched entity doc.
func buildEnrichmentDocContent(
	entityID string,
	req enrichmentWritebackRequest,
	enrichedAt string,
	entity *graph.Entity,
) string {
	var sb strings.Builder

	// YAML frontmatter — compatible with EnrichmentFrontmatter parser.
	sb.WriteString("---\n")
	sb.WriteString("entity_id: " + yamlScalar(entityID) + "\n")
	sb.WriteString("kind: " + yamlScalar(req.Kind) + "\n")
	if entity != nil {
		if entity.Name != "" {
			sb.WriteString("name: " + yamlScalar(entity.Name) + "\n")
		}
		if entity.Language != "" {
			sb.WriteString("language: " + yamlScalar(entity.Language) + "\n")
		}
		if entity.SourceFile != "" {
			sb.WriteString("source_file: " + yamlScalar(entity.SourceFile) + "\n")
		}
	}
	sb.WriteString("summary: " + yamlScalar(strings.TrimSpace(req.Description)) + "\n")
	sb.WriteString("enriched_at: " + yamlScalar(enrichedAt) + "\n")
	if req.ModelUsed != "" {
		sb.WriteString("model_used: " + yamlScalar(req.ModelUsed) + "\n")
	}
	if req.TokensUsed > 0 {
		sb.WriteString(fmt.Sprintf("tokens_used: %d\n", req.TokensUsed))
	}
	sb.WriteString("---\n\n")

	// Prose body.
	if entity != nil && entity.Name != "" {
		sb.WriteString("# " + entity.Name + "\n\n")
	}
	sb.WriteString(strings.TrimSpace(req.Description) + "\n")

	return sb.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// Utility helpers
// ─────────────────────────────────────────────────────────────────────────────

// sanitizePathSegment replaces characters that are illegal or problematic in
// file-system path segments with a hyphen. Safe for entity IDs which are
// typically 16-char hex strings, but also handles arbitrary IDs.
var unsafeSegmentRE = regexp.MustCompile(`[^a-zA-Z0-9._\-]`)

func sanitizePathSegment(s string) string {
	return unsafeSegmentRE.ReplaceAllString(s, "-")
}

// yamlScalar wraps a string in single quotes when it contains characters that
// would require quoting in YAML. For the simple scalar values used in the
// frontmatter this is sufficient. Embedded single quotes inside the value
// are escaped per YAML rules by doubling them.
//
// Note on '#': YAML only treats '#' as a comment marker when preceded by
// whitespace. "has#hash" is a valid plain scalar but "foo #bar" is not.
// We therefore quote on " #", not on bare '#'.
func yamlScalar(s string) string {
	needsQuote := s == "" ||
		strings.TrimSpace(s) != s ||
		strings.ContainsAny(s, ":{}[]|>&*!,'\"") ||
		strings.Contains(s, " #")
	if needsQuote {
		escaped := strings.ReplaceAll(s, "'", "''")
		return "'" + escaped + "'"
	}
	return s
}
