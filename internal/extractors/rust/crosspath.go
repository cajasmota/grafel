// crosspath.go — Rust cross-module / cross-crate CALLS qualifier resolution
// (issue #4373).
//
// Background. Rust reaches functions in other modules through a *path*
// qualifier on a scoped_identifier:
//
//	crate::services::order::place_order()   // absolute, crate root
//	self::sibling::helper()                 // current-module relative
//	super::parent::helper()                 // parent-module relative
//	ord::place_order()                      // `use ... as ord;` alias
//	OrderService::new()                     // Type::assoc_fn associated call
//
// The extractor (rustCallTarget) historically returned ONLY the rightmost
// identifier (`place_order`) for a scoped_identifier, dropping the whole path
// qualifier. A bare leaf resolves through the resolver's global byName index,
// which goes ambiguous the moment two modules define a same-named symbol
// (`place_order` in both services::order and services::invoice) — so the CALLS
// edge is dropped and the callee module looks falsely uncalled. This is the
// Rust analogue of the Go cross-package qualifier-drop fixed in #4332.
//
// Fix (mirrors #4332 structurally). The extractor maps the call's path
// qualifier — through the file's `use`/`mod` declarations, the crate root, and
// the self/super relative roots — to the *directory* the target module's file
// lives in (the same key `pkgDirOf(sourceFile)` the resolver's
// byPackageOperation / byPackageMember indexes are keyed on). It stamps that
// candidate directory (or directories — the mod.rs vs file.rs layout ambiguity
// yields two) plus the bare leaf and, for associated calls, the type scope.
// The resolver pass ResolveRustCrossModuleCalls (internal/resolve) binds the
// edge through those indexes. Conservative: blank / ambiguous → skip, never
// guess.
//
// Stamped Properties (consumed by ResolveRustCrossModuleCalls):
//   - rust_call_pkg_dirs : ";"-separated candidate package directories
//   - call_leaf          : the bare callee identifier
//   - rust_call_scope    : (associated calls only) the Type the leaf is a
//     member of, e.g. "OrderService" for OrderService::new()
package rust

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
)

// rustCrossCtx is the per-file context used to translate a call-site path
// qualifier into target package directories. Built once per file by
// buildRustCrossCtx and threaded through extractCallRelationships.
type rustCrossCtx struct {
	// crateSrc is the crate's source root directory (the dir holding lib.rs /
	// main.rs), e.g. "src" for "src/services/order.rs", or "crates/foo/src"
	// for a workspace member. Module paths rooted at `crate::` resolve
	// relative to this. Empty when the file is not under a recognisable src
	// root (then crate-absolute resolution is skipped — conservative).
	crateSrc string

	// selfModDir is the directory that `self::` resolves against: the module
	// directory of the current file. For "src/services/order.rs" the child
	// modules live in "src/services/order/…", so selfModDir is
	// "src/services/order". For a mod.rs / lib.rs / main.rs file the module
	// dir is simply the file's own directory.
	selfModDir string

	// superModDir is the directory `super::` resolves against — the parent of
	// selfModDir. Empty when selfModDir is the crate root.
	superModDir string

	// aliases maps a `use ... as NAME;` alias (or a plain `use a::b::c;`
	// trailing name) to the FULL crate-relative path of the item it names,
	// INCLUDING the trailing target segment. For `use crate::services::order
	// as ord;` → "ord" → ["services","order"] (the alias names a MODULE; used
	// as `ord::place_order()` the hosting module chain is exactly this). A
	// leading crate/self/super root is normalised to a crate-relative segment
	// list here so call-site resolution is uniform.
	aliases map[string][]string
}

// buildRustCrossCtx builds the per-file cross-module resolution context from
// the file path and its `use` declarations. root is the tree-sitter root node;
// src the file bytes; filePath the repo-relative source path.
func buildRustCrossCtx(root ts.Node, src []byte, filePath string) *rustCrossCtx {
	ctx := &rustCrossCtx{aliases: map[string][]string{}}
	ctx.crateSrc, ctx.selfModDir, ctx.superModDir = rustModuleDirs(filePath)
	if ctx.crateSrc == "" {
		// No recognisable crate src root: we can still resolve self::/super::
		// using selfModDir, but crate-absolute and aliased crate:: paths need
		// the root. selfModDir/superModDir are still populated below.
	}
	collectRustUseAliases(root, src, ctx)
	return ctx
}

// rustModuleDirs derives the crate src root, the current file's module
// directory (self::), and its parent (super::) from a repo-relative path.
//
//	src/services/order.rs       → crateSrc=src,  self=src/services/order,        super=src/services
//	src/services/order/mod.rs   → crateSrc=src,  self=src/services/order,        super=src/services
//	src/lib.rs / src/main.rs    → crateSrc=src,  self=src,                       super=""
//	crates/foo/src/a/b.rs       → crateSrc=crates/foo/src, self=crates/foo/src/a/b, super=crates/foo/src/a
func rustModuleDirs(filePath string) (crateSrc, selfDir, superDir string) {
	if filePath == "" {
		return "", "", ""
	}
	p := strings.TrimPrefix(filePath, "./")
	dir := pathDir(p)
	base := p
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		base = p[i+1:]
	}
	// Strip the ".rs" file extension to get the file's module leaf.
	leaf := strings.TrimSuffix(base, ".rs")

	// Locate the crate src root: the last path segment named "src".
	segs := strings.Split(dir, "/")
	srcIdx := -1
	for i := len(segs) - 1; i >= 0; i-- {
		if segs[i] == "src" {
			srcIdx = i
			break
		}
	}
	if srcIdx >= 0 {
		crateSrc = strings.Join(segs[:srcIdx+1], "/")
	}

	// The current file's module directory.
	switch leaf {
	case "mod", "lib", "main":
		// These files ARE their directory's module; child modules live in dir.
		selfDir = dir
	default:
		// A file `foo.rs` introduces module `foo`; its child modules live in
		// the sibling directory `<dir>/foo`.
		if dir == "" {
			selfDir = leaf
		} else {
			selfDir = dir + "/" + leaf
		}
	}
	superDir = pathDir(selfDir)
	if superDir == "." {
		superDir = ""
	}
	return crateSrc, selfDir, superDir
}

// pathDir returns the directory portion of a slash path, "" when there is no
// separator (root-level file).
func pathDir(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// collectRustUseAliases walks the file's `use_declaration` nodes and records
// alias → crate-relative-module-path-segments mappings into ctx.aliases.
//
// It handles:
//   - `use crate::services::order as ord;`   → ord  → [services order]
//   - `use crate::services::order::place_order;` (no alias) →
//     place_order → [services order]    (path to the item's MODULE)
//   - `use self::sibling::helper as h;`      → h    → [<selfMod…> sibling]
//   - `use super::parent::helper;`           → helper → [<superMod…> parent]
//
// Grouped uses (`use a::{b, c as d}`) are intentionally NOT expanded — they
// rarely qualify a call by alias and adding them risks mis-binding; the
// conservative skip leaves the bare-name fallback in place.
func collectRustUseAliases(root ts.Node, src []byte, ctx *rustCrossCtx) {
	for _, u := range findAllNodes(root, "use_declaration") {
		raw := strings.TrimSpace(string(src[u.StartByte():u.EndByte()]))
		raw = stripRustVisibility(raw)
		raw = strings.TrimPrefix(raw, "use ")
		raw = strings.TrimSuffix(raw, ";")
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.ContainsAny(raw, "{}*") {
			continue // grouped / glob use — skip (conservative)
		}

		alias := ""
		pathStr := raw
		if i := strings.Index(raw, " as "); i >= 0 {
			pathStr = strings.TrimSpace(raw[:i])
			alias = strings.TrimSpace(raw[i+4:])
		}

		segs := splitRustPath(pathStr)
		if len(segs) == 0 {
			continue
		}
		// Normalise the path root (crate/self/super) to crate-relative module
		// segments; the returned segments include the trailing leaf.
		modSegs, ok := ctx.normalizeToCrateRel(segs)
		if !ok || len(modSegs) == 0 {
			continue
		}
		leaf := modSegs[len(modSegs)-1]
		if alias == "" {
			alias = leaf
		}
		// Store the FULL crate-relative chain the alias names, INCLUDING its
		// trailing target segment. When the alias names a module
		// (`use crate::app::order as ord;` → [app order]) and is then used as
		// `ord::run`, the module chain hosting `run` is exactly [app order].
		// When the alias names a function (`use crate::app::order::run as r;`
		// → [app order run]) and is used bare as `r()` (an identifier call, not
		// scoped) this map is simply not consulted — only scoped `alias::…`
		// calls reach resolveCallPath's alias branch.
		cp := make([]string, len(modSegs))
		copy(cp, modSegs)
		ctx.aliases[alias] = cp
	}
}

// splitRustPath splits a `::`-separated path into trimmed segments, dropping
// any leading `::` (absolute external) and turbofish/generic noise. Segments
// containing characters that cannot be a module identifier abort the split.
func splitRustPath(p string) []string {
	p = strings.TrimPrefix(p, "::")
	parts := strings.Split(p, "::")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		// Strip a turbofish / generic argument list off the segment.
		if i := strings.IndexByte(s, '<'); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
		if s == "" || !isRustIdentLike(s) {
			return nil
		}
		out = append(out, s)
	}
	return out
}

// isRustIdentLike reports whether s is a plausible Rust path segment
// (identifier characters only). Rejects anything with operators/spaces.
func isRustIdentLike(s string) bool {
	for _, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// normalizeToCrateRel turns a raw segment list whose first element may be a
// `crate` / `self` / `super` root into a crate-relative segment list (rooted
// at the crate src dir). Returns ok=false when the root cannot be resolved
// (e.g. an external-crate path, or self/super without a known module dir).
//
// The returned segments are crate-src-relative path components — i.e. directly
// joinable under ctx.crateSrc — INCLUDING the trailing leaf segment.
func (ctx *rustCrossCtx) normalizeToCrateRel(segs []string) ([]string, bool) {
	if len(segs) == 0 {
		return nil, false
	}
	switch segs[0] {
	case "crate":
		if ctx.crateSrc == "" {
			return nil, false
		}
		return segs[1:], true
	case "self":
		rel, ok := dirToCrateRel(ctx.crateSrc, ctx.selfModDir)
		if !ok {
			return nil, false
		}
		return append(rel, segs[1:]...), true
	case "super":
		rel, ok := dirToCrateRel(ctx.crateSrc, ctx.superModDir)
		if !ok {
			return nil, false
		}
		return append(rel, segs[1:]...), true
	default:
		// Not a recognised intra-crate root: an external crate, an alias, or a
		// bare in-module path. We can't anchor it to the crate src here.
		return nil, false
	}
}

// dirToCrateRel returns the path segments of dir relative to crateSrc. ok is
// false when dir is empty or not under crateSrc.
func dirToCrateRel(crateSrc, dir string) ([]string, bool) {
	if crateSrc == "" || dir == "" {
		return nil, false
	}
	if dir == crateSrc {
		return []string{}, true
	}
	if !strings.HasPrefix(dir, crateSrc+"/") {
		return nil, false
	}
	rest := dir[len(crateSrc)+1:]
	if rest == "" {
		return []string{}, true
	}
	return strings.Split(rest, "/"), true
}

// resolveCallPath translates a call-site path's qualifier segments (everything
// up to and including the segment before the leaf) into candidate package
// directories — the keys the resolver's byPackageOperation / byPackageMember
// indexes use. It returns the candidate dirs and a scope type for associated
// `Type::method` calls (empty for free-function calls).
//
// segs is the FULL scoped path including the leaf, e.g.
// ["crate","services","order","place_order"] or ["OrderService","new"] or
// ["ord","place_order"] (alias) or ["self","sibling","helper"].
//
// Resolution strategy:
//   - crate:: / self:: / super::  → anchor to crate src, the qualifier (minus
//     leaf) names a module chain; emit both the mod.rs-layout dir and the
//     file.rs-layout dir as candidates.
//   - single-segment qualifier that is a known `use` alias → expand the alias
//     to its crate-relative module chain, then append the remaining qualifier
//     segments.
//   - a leading PascalCase segment with exactly one qualifier segment
//     (Type::method) → associated call: scope=Type, candidate dirs are the
//     CURRENT module dir candidates (the type is in-file or in-module). The
//     resolver also tries the crate-wide member index, so an unqualified
//     Type::method still binds when the type name is unique.
func (ctx *rustCrossCtx) resolveCallPath(segs []string) (dirs []string, scope string) {
	if len(segs) < 2 {
		return nil, ""
	}
	leaf := segs[len(segs)-1]
	qual := segs[:len(segs)-1] // qualifier chain (modules and/or a type)
	_ = leaf

	root := qual[0]
	switch root {
	case "crate", "self", "super":
		rel, ok := ctx.normalizeToCrateRel(qual)
		if !ok {
			return nil, ""
		}
		return ctx.moduleChainDirs(rel), ""
	}

	// Alias?  `ord::place_order` where `use crate::services::order as ord;`
	// recorded ord → [services order] (the FULL chain the alias names).
	if aliasMods, ok := ctx.aliases[root]; ok && len(aliasMods) > 0 {
		aliasLeaf := aliasMods[len(aliasMods)-1]
		// An aliased TYPE used as an associated call: `use crate::svc::a::
		// OrderService; OrderService::new()`. The alias names a type (its
		// final segment is PascalCase) and exactly one qualifier segment is
		// the alias — so the leaf is an associated method on that type. Bind
		// through byPackageMember[dir][Type][leaf] in the type's module dir.
		if len(qual) == 1 && isPascalCase(aliasLeaf) {
			modChain := aliasMods[:len(aliasMods)-1] // dirs hosting the type
			return ctx.moduleChainDirs(modChain), aliasLeaf
		}
		// Aliased MODULE used as `ord::fn` (or `ord::sub::fn`): the hosting
		// module chain is the alias's chain extended by the trailing qualifier
		// segments after the alias root.
		full := make([]string, 0, len(aliasMods)+len(qual)-1)
		full = append(full, aliasMods...)
		full = append(full, qual[1:]...)
		return ctx.moduleChainDirs(full), ""
	}

	// Type::method associated call (single qualifier segment, PascalCase) where
	// the type is NOT an aliased import — it is defined in the current module.
	if len(qual) == 1 && isPascalCase(root) {
		// Offer the current module's dir candidates; the resolver additionally
		// falls back to the unique crate-wide member index keyed on the type
		// name when the type lives in a sibling module.
		return ctx.selfDirCandidates(), root
	}

	// Unrecognised qualifier — leave the bare-name fallback in place.
	return nil, ""
}

// moduleChainDirs maps a crate-relative module chain (segments naming nested
// modules, WITHOUT a trailing leaf) to the candidate package directories that
// host that module's items, under both common layouts:
//
//	module chain [services order] under crateSrc "src" →
//	  file-layout : src/services/order.rs   → items keyed at pkgDir "src/services"
//	  mod-layout  : src/services/order/mod.rs → items keyed at pkgDir "src/services/order"
//
// An empty chain (crate root items) yields just the crate src dir.
func (ctx *rustCrossCtx) moduleChainDirs(chain []string) []string {
	if ctx.crateSrc == "" {
		return nil
	}
	if len(chain) == 0 {
		return []string{ctx.crateSrc}
	}
	full := ctx.crateSrc + "/" + strings.Join(chain, "/")
	parent := pathDir(full)
	// file-layout dir (parent) first — it is the more common single-file
	// module; mod-layout dir (full) second.
	out := []string{parent}
	if full != parent {
		out = append(out, full)
	}
	return dedupeStrings(out)
}

// selfDirCandidates returns the candidate dirs for items defined in the current
// file's own module (used for Type::method where the type is local).
func (ctx *rustCrossCtx) selfDirCandidates() []string {
	out := []string{}
	if d := pathDir(ctx.selfModDir); d != "" {
		out = append(out, d)
	}
	if ctx.selfModDir != "" {
		out = append(out, ctx.selfModDir)
	}
	// The file's own directory (where a `foo.rs` file's items are keyed).
	return dedupeStrings(out)
}

// isPascalCase reports whether s starts with an uppercase ASCII letter — the
// Rust convention distinguishing a Type from a module/function (snake_case).
func isPascalCase(s string) bool {
	return s != "" && s[0] >= 'A' && s[0] <= 'Z'
}

func dedupeStrings(in []string) []string {
	if len(in) <= 1 {
		return in
	}
	seen := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
