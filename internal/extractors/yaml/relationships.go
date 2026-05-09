package yaml

// Relationship helpers for the YAML extractor (Issue #386).
//
// The YAML extractor emits two relationship kinds:
//
//   - CONTAINS: structural parent → child edges. Each flavor knows its
//     own structural hierarchy:
//       GitHub Actions  workflow → job → step | action
//       GitLab CI       file → job
//       Docker Compose  file → service → port; file → volume
//       Kubernetes      file → resource → container | init_container
//       Ansible         play → task | role
//     File-rooted edges use file.Path as FromID; nested edges use the
//     parent's canonical ref (matching the child's QualifiedName scheme).
//
//   - IMPORTS: cross-references that look like dependencies in the
//     container model:
//       GitHub Actions  workflow file → `uses:` action ref (e.g.
//                       actions/checkout@v4) — one IMPORTS per unique action.
//       Docker Compose  service → service named in `depends_on:`.
//
// Every relationship is later tagged Properties["language"]="yaml" via
// extractor.TagRelationshipsLanguage in extractByFlavor.

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// composeVolumeSource extracts the source segment from a compose volume
// short-syntax entry. Compose volumes accept:
//
//	"<src>:<dst>"            → returns "<src>"
//	"<src>:<dst>:<mode>"     → returns "<src>"
//	"<dst>"                  → returns "" (no source — volume targets only)
//
// Long-syntax map entries (`{ source: ..., target: ... }`) come through
// getSequenceItems as the empty string today (we don't unpack inline maps in
// short-syntax helpers); this helper returns "" for those — caller skips.
//
// Issue #424.
func composeVolumeSource(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	// Strip surrounding quotes if the YAML scalar was quoted (the parser
	// returns the raw text including the quote characters). Single and
	// double quotes are both valid YAML; no escape unwrapping is needed
	// because the path content is round-tripped verbatim.
	if len(entry) >= 2 {
		first, last := entry[0], entry[len(entry)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			entry = entry[1 : len(entry)-1]
		}
	}
	// Long-syntax inline maps (`{ source: ..., target: ... }`) start with
	// `{` and never look like a host path source. Reject only the inline-
	// map shape; bare `${VAR}` env-substitutions inside a scalar are valid
	// host-path sources.
	if strings.HasPrefix(entry, "{") {
		return ""
	}
	idx := strings.IndexByte(entry, ':')
	if idx < 0 {
		// Single-element entry — by spec this is a target (anonymous volume),
		// no source to route.
		return ""
	}
	// Windows-style absolute paths ("C:\\..." or "C:/...") share the colon
	// with compose's separator. Detect: a single drive letter followed by `:`
	// followed by a path separator means the colon is part of the source.
	if idx == 1 {
		c := entry[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			if len(entry) > 2 && (entry[2] == '/' || entry[2] == '\\') {
				// Find the next colon (separator before the destination).
				if next := strings.IndexByte(entry[2:], ':'); next >= 0 {
					return entry[:2+next]
				}
				return entry
			}
		}
	}
	return entry[:idx]
}

// looksLikeHostPath returns true when src is a compose-style host filesystem
// reference: relative path (`./...`, `../...`), absolute Unix path (`/...`),
// home-anchored path (`~`, `~/...`), Windows drive path (`C:\` / `C:/`), or
// an env-substitution form (`${VAR}`, `$VAR`). Bare-word sources (e.g.
// `postgres_data`) are NOT host paths — they reference a top-level named
// volume key and are already routed by the existing CONTAINS edge.
//
// Issue #424.
func looksLikeHostPath(src string) bool {
	if src == "" {
		return false
	}
	switch {
	case strings.HasPrefix(src, "./"), strings.HasPrefix(src, "../"):
		return true
	case strings.HasPrefix(src, "/"):
		return true
	case src == "~" || strings.HasPrefix(src, "~/"):
		return true
	case strings.HasPrefix(src, "${"), strings.HasPrefix(src, "$"):
		// `$VAR` — must look like a shell var reference, not a literal `$tag`.
		return true
	}
	// Windows drive letter: `C:\` or `C:/`.
	if len(src) >= 3 && src[1] == ':' && (src[2] == '/' || src[2] == '\\') {
		c := src[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return true
		}
	}
	return false
}

// containsRel builds a CONTAINS RelationshipRecord (Properties left nil; the
// resolver-tag pass fills language).
func containsRel(fromID, toID string) types.RelationshipRecord {
	return types.RelationshipRecord{FromID: fromID, ToID: toID, Kind: "CONTAINS"}
}

// importsRel builds an IMPORTS RelationshipRecord with the given importKind
// recorded under Properties["import_kind"] for downstream resolver dispatch.
func importsRel(fromID, toID, importKind string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: fromID,
		ToID:   toID,
		Kind:   "IMPORTS",
		Properties: map[string]string{
			"import_kind":   importKind,
			"source_module": toID,
		},
	}
}

// findEntityIndex returns the index of the first entity whose QualifiedName
// matches ref, or -1.
func findEntityIndex(entities []types.EntityRecord, qualifiedName string) int {
	for i := range entities {
		if entities[i].QualifiedName == qualifiedName {
			return i
		}
	}
	return -1
}
