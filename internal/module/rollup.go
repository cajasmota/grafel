// Package module derives a stable "module" label for every entity from its
// source_file path relative to the repository root.
//
// The derivation is a deterministic path rollup — same input always produces
// the same module string.  No clustering or randomness is involved.
//
// # Algorithm
//
//  1. Strip the repo root prefix from source_file to get a repo-relative path.
//  2. Scan the path bottom-up for package-boundary markers:
//     - Go: presence of a go.mod sibling OR same directory as the file (package
//     declarations are per-directory in Go).
//     - Python: __init__.py in the same directory.
//     - JS/TS: package.json OR index.ts / index.js / index.tsx in the same dir.
//  3. If a marker boundary is found at depth D, use the first D path segments
//     as the module label (capped at MaxDepth).
//  4. Fall back to the first DefaultDepth path segments when no marker is found.
//  5. Files at the repo root (no directory component) → label "_root".
//
// # Thread safety
//
// Derive is stateless and safe for concurrent use.
package module

import (
	"path"
	"strings"
)

const (
	// DefaultDepth is the fallback number of path segments used when no
	// package-boundary marker is detected.  Two segments gives labels like
	// "core/views" or "src/features" rather than either the top-level dir
	// alone or the full file path.
	DefaultDepth = 2

	// MaxDepth caps the label depth even when a marker is detected deeper
	// in the tree, preventing excessively granular labels on deeply nested
	// packages.
	MaxDepth = 3

	// RootLabel is the module label used for files that live directly at
	// the repository root (e.g. "main.go", "setup.py").
	RootLabel = "_root"
)

// markerFileNames is the set of file names that indicate a package boundary
// directory when present.  Exported as MarkerFileNames for test access.
var MarkerFileNames = map[string]bool{
	"package.json": true,
	"index.ts":     true,
	"index.js":     true,
	"index.tsx":    true,
	"index.jsx":    true,
	"__init__.py":  true,
	"go.mod":       true,
	"Cargo.toml":   true,
	"pom.xml":      true,
	"build.gradle": true,
}

// MarkerSet is a pre-scanned set of repo-relative directory paths that contain
// at least one package-boundary marker file.  Built once per index run from
// the walked file list via BuildMarkerSet.  Nil is safe — Derive falls back to
// depth-N without marker awareness when ms is nil.
type MarkerSet map[string]struct{}

// BuildMarkerSet scans a list of repo-relative file paths and returns a
// MarkerSet of every directory that contains at least one boundary-marker file.
//
// repoRelPaths should use forward slashes and be relative to the repo root —
// the same slice produced by walk.WalkRepo.  Call this once per index run and
// reuse the result across all Derive calls.
func BuildMarkerSet(repoRelPaths []string) MarkerSet {
	ms := make(MarkerSet)
	for _, p := range repoRelPaths {
		base := path.Base(p)
		if !MarkerFileNames[base] {
			continue
		}
		dir := path.Dir(p)
		if dir == "." {
			dir = "" // repo root sentinel
		}
		ms[dir] = struct{}{}
	}
	return ms
}

// Derive returns the module label for an entity given its repo-relative source
// file path.
//
// sourceFile must be slash-separated and relative to the repo root (the value
// stored in graph.Entity.SourceFile after the repo-root prefix is stripped).
// ms may be nil; when non-nil it is consulted for package-boundary hints.
//
// The function is pure and deterministic: identical inputs always return the
// same label.
func Derive(sourceFile string, ms MarkerSet) string {
	// Normalise: convert backslashes and strip leading "./".
	sourceFile = strings.ReplaceAll(sourceFile, "\\", "/")
	sourceFile = strings.TrimPrefix(sourceFile, "./")

	dir := path.Dir(sourceFile)
	if dir == "." {
		// File lives at the repo root.
		return RootLabel
	}

	segments := splitSegments(dir)
	if len(segments) == 0 {
		return RootLabel
	}

	// Walk ancestor paths from shallowest to deepest (up to MaxDepth),
	// recording the deepest marker boundary found.  Walking shallow-first
	// ensures a top-level marker doesn't collapse sub-packages; we keep
	// updating bestDepth so the deepest marker within MaxDepth wins.
	bestDepth := 0
	if ms != nil {
		for d := 1; d <= len(segments) && d <= MaxDepth; d++ {
			ancestor := strings.Join(segments[:d], "/")
			if _, ok := ms[ancestor]; ok {
				bestDepth = d
			}
		}
		// Also check the repo root (empty string) — this handles the case
		// where the root itself has a marker (single-module repos).
		if _, ok := ms[""]; ok && bestDepth == 0 {
			// Root marker found: use DefaultDepth so we still get
			// meaningful labels rather than collapsing everything to "_root".
			bestDepth = 0 // intentionally leave 0 → fall through to DefaultDepth
		}
	}

	depth := DefaultDepth
	if bestDepth > 0 {
		depth = bestDepth
	}
	// Hard cap and clamp to available segments.
	if depth > MaxDepth {
		depth = MaxDepth
	}
	if depth > len(segments) {
		depth = len(segments)
	}

	return strings.Join(segments[:depth], "/")
}

// EnsureModule stamps the "module" key into props if it is absent, computing
// the value from sourceFile and ms.  If props is nil a new map is allocated
// and returned.  If "module" is already set, props is returned unchanged.
//
//	props = module.EnsureModule(props, sourceFile, ms)
func EnsureModule(props map[string]string, sourceFile string, ms MarkerSet) map[string]string {
	if props == nil {
		return map[string]string{"module": Derive(sourceFile, ms)}
	}
	if _, ok := props["module"]; !ok {
		props["module"] = Derive(sourceFile, ms)
	}
	return props
}

// splitSegments splits a slash-separated path into non-empty segments,
// dropping "." components.
func splitSegments(p string) []string {
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, s := range parts {
		if s != "" && s != "." {
			out = append(out, s)
		}
	}
	return out
}
