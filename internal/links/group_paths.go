// group_paths.go — the single derivation for every group-scoped path grafel
// writes or reads under the grafel home: <group>-links.json and its three
// PathsFor siblings, the eight downstream-pass sidecars
// (<group>-links-<pass>.json — effects, data-flow, reachability, taint,
// module-cycles, pure-functions, def-use, template-patterns), the
// <group>-memory saved-finding directory, and the <group>-patterns
// directory.
//
// # Why this file exists (#6178 round 3)
//
// Three rounds of this issue found the same bug at a new suffix each time:
// round 1 fixed internal/cli/links.go's two call sites and links.PathsFor's
// own fallback; round 2 found four more readers (mcp, dashboard, docgen,
// cli) that independently re-derived "<home>/groups/<group>-links.json"
// with a hand-rolled os.UserHomeDir() join instead of calling PathsFor;
// round 3 found the SAME shape on eleven more sidecars, because every
// downstream pass's own sidecar reader re-derived its path by hand too,
// each in its own file, several with their own "prefer os.Getenv(HOME) for
// Windows tests" variant of the same join.
//
// Grep-and-patch does not end this: a twelfth sidecar next year re-opens it
// exactly the same way. The fix is structural, not enumerative — there is
// now exactly ONE function, GroupHome, that resolves the grafel home, and
// exactly one function per sidecar FAMILY (groupsDir, PassSidecarPath,
// MemoryDir, PatternsDir) that joins onto it. Every writer and reader in
// the tree — internal/links' own passes, internal/mcp, internal/dashboard,
// internal/docgen, internal/cli, cmd/grafel — now calls one of these
// instead of rebuilding the join. A new sidecar that calls PassSidecarPath
// cannot fall outside GRAFEL_HOME; one that hand-rolls the join again is
// exactly what internal/registry's home-sweep guard test
// (TestNoHandRolledGrafelHomePaths) exists to catch, repo-wide, at build
// time rather than at the next review round.
package links

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/registry"
)

// GroupHome resolves grafelHome ("" → the GRAFEL_HOME-aware default from
// registry.HomeDir()) to a concrete grafel-home directory. This is the ONE
// place in the tree that decides "which grafel home" for group-scoped
// state; every function in this file, and every caller elsewhere in the
// tree that used to hand-roll os.UserHomeDir()+".grafel", now goes through
// it (directly or via groupsDir/PassSidecarPath/MemoryDir/PatternsDir).
func GroupHome(grafelHome string) (string, error) {
	if grafelHome != "" {
		return grafelHome, nil
	}
	return registry.HomeDir()
}

// groupsDir returns "<grafelHome>/groups" and validates group is non-empty
// — every group-scoped file lives directly under this directory, keyed by
// a "<group>-<suffix>" basename.
func groupsDir(grafelHome, group string) (string, error) {
	if group == "" {
		return "", errors.New("group name required")
	}
	home, err := GroupHome(grafelHome)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "groups"), nil
}

// PassSidecarPath returns the canonical path for a downstream-pass sidecar
// named "<group>-links-<suffix>.json" — the family effect_propagation.go,
// dataflow_pass.go, reachability.go, taint_flow.go, module_cycle_pass.go,
// pure_function_pass.go, def_use_pass.go, and template_pattern_pass.go all
// write via `strings.TrimSuffix(paths.Links, ".json") + "-<suffix>.json"`
// (paths from PathsFor). Every reader of one of these sidecars — the
// Phase 3 MCP tools, the effects/dead-code/stub-detector MCP tools, and
// the dashboard's downstream-DAG and data-flow/taint handlers — must call
// this instead of re-deriving the join, so a reader can never disagree
// with the writer about which grafel home the sidecar lives under.
func PassSidecarPath(grafelHome, group, suffix string) (string, error) {
	paths, err := PathsFor(grafelHome, group)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(paths.Links, ".json") + "-" + suffix + ".json", nil
}

// MemoryDir returns the canonical "<group>-memory" directory — where
// save_finding output lands (internal/mcp's grp.MemoryDir / the
// registry-configured override takes precedence at the call site; this is
// only the DEFAULT used when no override is configured).
func MemoryDir(grafelHome, group string) (string, error) {
	dir, err := groupsDir(grafelHome, group)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, group+"-memory"), nil
}

// PatternsDir returns the canonical "<group>-patterns" directory — where
// agentpatterns stores patterns.json (internal/mcp/patterns.go,
// internal/dashboard/handlers_patterns.go, cmd/grafel's daemon pattern-GC
// sweep). Same override precedence note as MemoryDir: a registry-configured
// MemoryDir/patterns dir wins at the call site; this is the default.
func PatternsDir(grafelHome, group string) (string, error) {
	dir, err := groupsDir(grafelHome, group)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, group+"-patterns"), nil
}

// RealHomeOrphanWarning returns a one-line, print-ready warning when data
// that predates a GRAFEL_HOME override may be stranded at the real home
// instead of showing up at resolvedPath. It exists for the two group-scoped
// stores #6178 round 3 established are NOT safe to leave silently
// un-migrated: save_finding output (MemoryDir) and user-accepted cross-repo
// link candidates, which live inside <group>-links.json itself (appended by
// internal/mcp's appendLink) and so are NOT regenerated by re-running a
// link pass the way every pass-derived sidecar is — that's exactly why the
// #6178 round 1/2 "no migration needed, just re-run the pass" argument does
// NOT extend to these two.
//
// "" means: GRAFEL_HOME is unset (no divergence is possible — resolvedPath
// already IS the legacy path), the legacy path itself has no data, or
// resolvedPath already has data (nothing to warn about). kind is a short
// human label for the message ("saved findings", "cross-repo links").
// legacySuffix is the "<group>-..." basename this sidecar uses directly
// under "groups/" (no leading slash), e.g. "myteam-memory" or
// "myteam-links.json".
//
// This performs at most two os.Stat/os.ReadDir calls and does no writing —
// callers own deciding where the message goes (stderr, an API response
// field, a log line) and how often to call it; it is cheap enough to call
// on every request that reads the resolvedPath.
func RealHomeOrphanWarning(kind, resolvedPath, legacySuffix string) string {
	if os.Getenv("GRAFEL_HOME") == "" {
		return ""
	}
	realHome, err := os.UserHomeDir()
	if err != nil || realHome == "" {
		return ""
	}
	legacyPath := filepath.Join(realHome, ".grafel", "groups", legacySuffix)
	if legacyPath == resolvedPath {
		return ""
	}
	if !pathHasData(legacyPath) {
		return ""
	}
	if pathHasData(resolvedPath) {
		return ""
	}
	return fmt.Sprintf(
		"grafel: warning: no %s found under GRAFEL_HOME (%s) but pre-existing data exists at "+
			"%s (the pre-#6178 default location). This is NOT migrated automatically, and "+
			"re-running the link pass will NOT recover it either — this is hand-curated data, "+
			"not pass output. Copy it manually if you still need it, or delete it once you have "+
			"verified you don't.",
		kind, resolvedPath, legacyPath,
	)
}

// pathHasData reports whether path exists and holds something worth warning
// about: a non-empty file, or a directory containing at least one entry.
func pathHasData(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		entries, rerr := os.ReadDir(path)
		return rerr == nil && len(entries) > 0
	}
	return info.Size() > 0
}
