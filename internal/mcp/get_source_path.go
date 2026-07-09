package mcp

// get_source_path.go — filesystem-path resolution for entity source files
// (#5682).
//
// THE BUG this closes: get_source (and the inspect/call-context read paths)
// build a source file's absolute path by joining the recorded source_file onto
// the GROUP/REPO root (LoadedRepo.Path):
//
//	abs = filepath.Join(lr.Path, e.SourceFile)
//
// In a monorepo with nested build units (per-module go.mod / package.json /
// pom.xml / *.csproj) the indexer records source_file RELATIVE TO ITS
// NESTED-MODULE ROOT, not to the group root. So a file physically at
// <root>/services/billing/pkg/handler.go is recorded as "pkg/handler.go", and
// the naive join stat's <root>/pkg/handler.go — which does not exist. On a repo
// with ~300 nested modules this made get_source unusable for most files.
//
// THE FIX (read-side, lowest risk): resolveEntitySourcePath preserves the exact
// happy-path behaviour for files that live at the group root — the same join,
// plus a SINGLE os.Stat — and only when that path does not exist does it fall
// back to an IN-REPO unique-suffix lookup that recovers the real path.
//
// The resolved entity already belongs to lr (resolveSourceEntity picks the repo
// whose Document contains it), so its source file lives in lr's tree. We
// therefore resolve ONLY within lr and never consult sibling repos: common
// relative paths ("cmd/main.go", "pkg/handler.go") collide across repos in a
// monorepo group, so returning the first sibling repo whose root + source_file
// stats would silently return the WRONG file from another repo. In-repo-only is
// both correct and cheaper (#5682 review D2).
//
// Cost contract:
//   - Happy path (file at the group root): exactly one os.Stat over the
//     pre-#5682 code; the identical path is returned.
//   - Nested-module lookups: one os.Stat (miss) then an O(1) map lookup against
//     a per-repo suffix index that is built by a SINGLE filesystem walk, cached
//     on LoadedRepo, and re-armed on reload (getSuffixIndex). The tree is walked
//     at most once per repo regardless of how many get_source / inspect lookups
//     occur — no per-call walk, no inspect-loop amplification (#5682 review D1).
//   - Genuine miss (deleted file): join → O(1) index lookup → "" → the same
//     clear lstat error, cheap after the first index build (#5682 review N2).
//
// Why not Properties["module"]? The "module" property is a DERIVED LABEL
// (internal/module.Derive: a depth-capped path prefix of the source_file), not
// a record of the nested-module root directory, so it cannot reconstruct the
// stripped prefix. The suffix index is the cheapest strategy that is actually
// correct for the general nested-build-unit case.

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// suffixWalkFileBudget caps the number of regular files the suffix-index walk
// will record before giving up, so a pathological tree can never turn the
// index build into an unbounded filesystem scan. When the cap is hit the index
// is marked partial and lookups conservatively return "" (uniqueness cannot be
// proven over the unscanned remainder — #5682 review N1).
const suffixWalkFileBudget = 200000

// resolveEntitySourcePath returns the best absolute filesystem path for an
// entity's recorded source_file, given the entity's resolved repo lr.
//
// Behaviour, in order:
//
//  1. An already-absolute source_file is returned unchanged.
//  2. join(lr.Path, source_file) — the pre-#5682 happy path. Returned as-is
//     when it exists (single os.Stat) OR when there is no repo root to fall
//     back to (lr == nil / lr.Path == ""), preserving today's behaviour and
//     error messages exactly.
//  3. In-repo unique-suffix resolution via the per-repo suffix index, for a
//     nested-module-relative source_file (#5682).
//
// On total failure the primary join is returned so the caller emits the same
// clear lstat error as before.
func resolveEntitySourcePath(lr *LoadedRepo, sourceFile string) string {
	if sourceFile == "" {
		return sourceFile
	}
	if filepath.IsAbs(sourceFile) {
		return sourceFile
	}

	// No repo root to join against — nothing this helper can improve; return the
	// recorded path verbatim (identical to the pre-#5682 `abs := e.SourceFile`).
	if lr == nil || lr.Path == "" {
		return sourceFile
	}

	primary := filepath.Join(lr.Path, sourceFile)
	if _, err := os.Stat(primary); err == nil {
		// Happy path: file lives at the group root exactly as recorded. Identical
		// to pre-#5682 apart from this single Stat.
		return primary
	}

	// In-repo unique-suffix resolution — recovers a nested-module-relative path
	// (the primary #5682 case) via the precomputed suffix index.
	if hit := lr.resolveSourceBySuffix(sourceFile); hit != "" {
		return hit
	}

	// Nothing found — return the primary join so the caller reports the same
	// clear "lstat <path>: no such file or directory" error as before.
	return primary
}

// resolveSourceBySuffix resolves a repo-relative-ish source_file to an absolute
// path within lr by finding the single indexed file whose path ends with
// source_file at a path-segment boundary. Returns "" when there is no match,
// more than one match (ambiguous — never guess), or the suffix index is partial
// (budget-exhausted, so uniqueness is unprovable — #5682 review N1).
func (lr *LoadedRepo) resolveSourceBySuffix(sourceFile string) string {
	idx, partial := lr.getSuffixIndex()
	if partial || len(idx) == 0 {
		return ""
	}
	sf := strings.TrimPrefix(filepath.ToSlash(sourceFile), "/")
	if sf == "" {
		return ""
	}
	cands := idx[path.Base(sf)]
	if len(cands) == 0 {
		return ""
	}
	// Anchor on a leading separator so "pkg/handler.go" matches
	// ".../billing/pkg/handler.go" but NOT ".../mypkg/handler.go".
	suffix := "/" + sf
	var match string
	count := 0
	for _, rel := range cands {
		if rel == sf || strings.HasSuffix("/"+rel, suffix) {
			match = rel
			count++
			if count > 1 {
				return "" // ambiguous — refuse to guess
			}
		}
	}
	if count == 1 {
		return filepath.Join(lr.Path, filepath.FromSlash(match))
	}
	return ""
}

// buildSuffixIndex walks root (skipping vendor / node_modules / .git and
// capping the number of files recorded) and returns a basename -> repo-relative
// (slash) paths index plus a partial flag that is true when the file budget was
// exhausted. It is a package var so tests can wrap it with a call counter to
// prove the tree is walked at most once per repo (#5682 review D1).
var buildSuffixIndex = func(root string) (map[string][]string, bool) {
	idx := map[string][]string{}
	if root == "" {
		return idx, false
	}
	files := 0
	partial := false
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Unreadable dir/file — skip it, keep walking the rest.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "node_modules", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		files++
		if files > suffixWalkFileBudget {
			partial = true
			return filepath.SkipAll
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		idx[path.Base(rel)] = append(idx[path.Base(rel)], rel)
		return nil
	})
	return idx, partial
}
