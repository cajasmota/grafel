// Generated-docs storage location (#1624).
//
// The `generate-docs` skill used to write its markdown into each repo's
// `<repo>/docs/` directory. That created commit noise in source repos and
// violated the #1626 principle that grafel never writes into the working
// tree of a registered repo.
//
// As of #1624 generated docs live under an grafel-managed location:
//
//	$GRAFEL_HOME (or ~/.grafel)/docs/<group>/<repoSlug>/...   (technical tier, per-repo)
//	$GRAFEL_HOME (or ~/.grafel)/docs/<group>/business/...     (business tier, group-synthesised)
//
// `<repoSlug>` matches the repo slug used by the registry / group config so the
// dashboard layer can address a repo's docs without recomputing path hashes.
//
// A one-time best-effort migration relocates any pre-#1624 `<repo>/docs/` set
// produced by the skill into the store layout. See MigrateInRepoDocs below.
package daemon

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cajasmota/grafel/internal/registry"
)

// DocsDir returns the root of the daemon's external docs store —
// `$GRAFEL_HOME (or ~/.grafel)/docs`. New code MUST use this helper
// (or one of the per-group / per-repo wrappers) instead of joining paths to
// the repo working tree.
func DocsDir() string {
	return filepath.Join(homeDir(), "docs")
}

// GroupDocsDir returns the docs root for a single group:
// `~/.grafel/docs/<group>`. The returned path is NOT created.
func GroupDocsDir(group string) string {
	if group == "" {
		return ""
	}
	return filepath.Join(DocsDir(), group)
}

// RepoDocsDir returns the technical-tier per-repo docs directory:
// `~/.grafel/docs/<group>/<repoSlug>`.
func RepoDocsDir(group, repoSlug string) string {
	if group == "" || repoSlug == "" {
		return ""
	}
	return filepath.Join(DocsDir(), group, repoSlug)
}

// BusinessDocsDir returns the business-tier docs directory for a group:
// `~/.grafel/docs/<group>/business`. The business tier is group-
// synthesised, not per-repo.
func BusinessDocsDir(group string) string {
	if group == "" {
		return ""
	}
	return filepath.Join(DocsDir(), group, "business")
}

// RepoDocsDir/BusinessDocsDir are READ-safe derivations: they are also used
// to resolve the docs location of an already-registered group (docgen, the
// docs handlers, export), including one whose name predates
// registry.ValidateGroupName or is otherwise grandfathered-invalid. They
// intentionally do NOT validate group — see registry.ValidateGroupName's doc
// comment for why gating reads would be worse than the bug it prevents.
//
// RepoDocsDirForNew and BusinessDocsDirForNew are the write-side
// counterparts: they validate group before deriving the path.
//
// #6186 R1 (found on review): the import/restore handler in
// internal/dashboard/handlers_v2_graph_export.go derives a destination
// directory from RepoDocsDir/BusinessDocsDir using a group name taken from
// an uploaded archive's manifest.json (or a request query override) and then
// os.MkdirAll + unzips into it — with no validation anywhere on that path,
// because it was never on the hand-enumerated list of AddGroup callers the
// F6 fix walked. filepath.Join collapses "..", so an archive whose manifest
// group is "../../../../tmp/pwn" wrote into that directory before the
// (too-late) registry.AddGroup rejection at the end of the handler ever ran.
//
// Use the ForNew variants (not RepoDocsDir/BusinessDocsDir) at any call site
// that is about to CREATE or POPULATE a docs directory for a name that is
// not already a trusted, registered group — the validated path is
// structurally distinct from the read path, rather than relying on the
// caller to remember a check.
func RepoDocsDirForNew(group, repoSlug string) (string, error) {
	if err := registry.ValidateGroupName(group); err != nil {
		return "", err
	}
	if repoSlug == "" {
		return "", fmt.Errorf("repo slug required")
	}
	return RepoDocsDir(group, repoSlug), nil
}

// BusinessDocsDirForNew is RepoDocsDirForNew's counterpart for the group-level
// business docs directory. See RepoDocsDirForNew for the rationale.
func BusinessDocsDirForNew(group string) (string, error) {
	if err := registry.ValidateGroupName(group); err != nil {
		return "", err
	}
	return BusinessDocsDir(group), nil
}

// LegacyInRepoDocsDir returns the historical `<repo>/docs/` location.
// Used by the migration path; new code MUST use RepoDocsDir / BusinessDocsDir.
func LegacyInRepoDocsDir(repoPath string) string {
	if repoPath == "" {
		return ""
	}
	return filepath.Join(repoPath, "docs")
}

// looksLikeSkillGeneratedDocs reports whether dir holds output of the
// generate-docs skill (vs a pre-existing repo `docs/` with hand-authored
// markdown). The skill always writes at least one of:
//
//   - overview.md
//   - a non-empty modules/ subdirectory
//
// We only migrate dirs that match this signature so a repo's existing docs/
// (e.g. a Docusaurus site, hand-authored READMEs) are left untouched.
func looksLikeSkillGeneratedDocs(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "overview.md")); err == nil {
		return true
	}
	if info, err := os.Stat(filepath.Join(dir, "modules")); err == nil && info.IsDir() {
		// modules dir must contain something to count.
		entries, _ := os.ReadDir(filepath.Join(dir, "modules"))
		if len(entries) > 0 {
			return true
		}
	}
	return false
}

// MigrateInRepoDocs relocates a pre-#1624 `<repo>/docs/` directory produced
// by the generate-docs skill into the new store layout
// `~/.grafel/docs/<group>/<repoSlug>/`. It is a no-op when:
//
//   - the legacy dir does not exist,
//   - the legacy dir does not look like skill-generated output, or
//   - store docs already exist for this (group, repoSlug) — we never
//     overwrite freshly generated docs with stale repo-side state.
//
// Returns (migrated, error). Migration is move-based (rename, falling back to
// copy+remove across filesystems). Any per-file failure is returned so the
// caller can log it; partial success still leaves usable docs in the store.
func MigrateInRepoDocs(group, repoSlug, repoPath string) (bool, error) {
	if group == "" || repoSlug == "" || repoPath == "" {
		return false, nil
	}
	legacy := LegacyInRepoDocsDir(repoPath)
	info, err := os.Stat(legacy)
	if err != nil || !info.IsDir() {
		return false, nil
	}
	if !looksLikeSkillGeneratedDocs(legacy) {
		return false, nil
	}
	dest := RepoDocsDir(group, repoSlug)
	// If the destination already has content, treat as already-migrated.
	if entries, statErr := os.ReadDir(dest); statErr == nil && len(entries) > 0 {
		return false, nil
	}
	if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
		return false, mkErr
	}
	// movePath handles cross-device rename via copy+remove.
	if err := movePath(legacy, dest); err != nil {
		return false, err
	}
	return true, nil
}
