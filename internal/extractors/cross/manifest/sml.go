// sml.go — Standard ML build-manifest parsers (#5383, epic #5360).
//
// Standard ML has no real package-manager ecosystem (academic language), but it
// has two widely-used BUILD MANIFEST formats that enumerate the source files and
// library dependencies of a compilation unit. Both are parsed here as
// suffix-dispatched manifests (mirroring the OCaml *.opam / Idris *.ipkg
// precedent, #5374/#5382):
//
//	*.cm — SML/NJ Compilation Manager group/library description. A group is a
//	  `Group is ... end` (or `Library ... is ... end`) body listing member
//	  entries, one per line: source files (foo.sml / foo.sig / foo.fun) and
//	  library references — anchored stdlib refs ($/basis.cm, $smlnj/...),
//	  pathname-anchored refs ($(ANCHOR)/lib.cm), and plain relative .cm includes
//	  (util/util.cm). An optional `(<exports>)` clause may precede `is`:
//
//	    Library
//	      structure Foo
//	      signature BAR
//	    is
//	      $/basis.cm
//	      $/smlnj-lib.cm
//	      foo.sml
//	      bar/bar.sig
//	      util/util.cm
//	    end
//
//	*.mlb — MLton ML Basis file. A flat, ordered list of paths plus `basis`/
//	  `local`/`open`/`ann` declarations. Library dependencies are other `.mlb`
//	  includes — most importantly the MLton stdlib basis `$(SML_LIB)/basis/
//	  basis.mlb`; source members are .sml / .sig / .fun files:
//
//	    $(SML_LIB)/basis/basis.mlb
//	    $(SML_LIB)/smlnj-lib/Util/smlnj-lib.mlb
//	    local
//	      util/util.mlb
//	      foo.sml
//	    in
//	      bar.sml
//	    end
//
// Dependency surface — library references (.cm / .mlb includes, including the
// anchored stdlib basis):
//
//	Every library reference becomes a DEPENDS_ON / DEPENDS_ON_PACKAGE edge with
//	an EMPTY version (CM/MLB carry no version-constraint syntax — the compiler
//	toolchain resolves the closure; honest). The toolchain stdlib floor
//	($/basis.cm, $smlnj/..., $(SML_LIB)/.../basis.mlb) is KEPT as a real edge,
//	mirroring the cabal `base` / nimble `nim` / luarocks `lua` / idris `base`
//	interpreter-floor treatment (#5373/#5367/#5382). The package manager is
//	`smlnj_cm` (CM, SML/NJ) or `mlton_mlb` (MLB, MLton) so the build toolchain is
//	queryable from the dep records.
//
// Manifest metadata — surfaced on the project anchor (no new entity kind, same
// model as the Idris ipkg_config / Zig build_targets props):
//
//	sources — the space-joined member source-file list (.sml/.sig/.fun)
//	export  — (CM only) the group/library export kind: "group" or "library"
//
// joined into a compact, deterministic `cm_config` / `mlb_config` property so the
// source membership + group kind is queryable without a new entity kind.
//
// Honest scope: only the DECLARED build surface is recovered. There are no
// versions (CM/MLB have no constraint syntax) and no lockfile format (the
// toolchain resolves and pins the closure at build time, not a per-unit
// manifest), so lockfile_parsing is not_applicable. CM conditional-compilation
// directives (#if/#elif preprocessor) and MLB `ann "..."` annotation strings are
// not interpreted (every branch's members are mined; annotations ignored).
package manifest

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// smlLibRefSuffix reports whether a member token is a library reference (another
// CM group / MLB basis include) rather than a source file.
func smlLibRefSuffix(tok string) bool {
	low := strings.ToLower(tok)
	return strings.HasSuffix(low, ".cm") || strings.HasSuffix(low, ".mlb")
}

// smlSourceSuffix reports whether a member token is an SML source file.
func smlSourceSuffix(tok string) bool {
	low := strings.ToLower(tok)
	return strings.HasSuffix(low, ".sml") ||
		strings.HasSuffix(low, ".sig") ||
		strings.HasSuffix(low, ".fun") ||
		strings.HasSuffix(low, ".ml")
}

// smlDepName normalises a library-reference path to a stable dependency name.
// The basename (last path segment) is used so $(SML_LIB)/basis/basis.mlb and a
// sibling-relative basis.mlb converge, while anchored stdlib refs keep their
// leading anchor token visible (e.g. "$/basis.cm" stays "$/basis.cm") so the
// toolchain floor is recognisable.
func smlDepName(ref string) string {
	ref = strings.TrimSpace(ref)
	// Keep a bare SML/NJ anchor reference verbatim ("$/basis.cm", "$smlnj/x.cm").
	if strings.HasPrefix(ref, "$/") || strings.HasPrefix(ref, "$smlnj") {
		return ref
	}
	// $(SML_LIB)/basis/basis.mlb → basis.mlb (the stable, convergent name).
	return filepath.Base(ref)
}

// ---------------------------------------------------------------------------
// Parser: *.cm (SML/NJ Compilation Manager)
// ---------------------------------------------------------------------------

// cmCommentRE strips SML `(* ... *)` block comments (CM uses SML comment
// syntax). Non-greedy, dot-matches-newline for multi-line comments.
var cmCommentRE = regexp.MustCompile(`(?s)\(\*.*?\*\)`)

// cmGroupHeadRE matches the `Group`/`Library` head keyword (case-insensitive) so
// the export kind can be recorded. Group 1 is the keyword.
var cmGroupHeadRE = regexp.MustCompile(`(?im)^\s*(group|library)\b`)

// cmMemberBody returns the member-list body of a CM file: the text between the
// `is` keyword and the closing `end`. The optional `( <exports> )` clause and
// the head keyword precede `is`; members follow it.
func cmMemberBody(source string) string {
	// Locate the `is` keyword that opens the member list. It appears on its own
	// or at the end of the export clause; match the first whole-word `is`.
	loc := regexp.MustCompile(`(?im)\bis\b`).FindStringIndex(source)
	if loc == nil {
		return source
	}
	body := source[loc[1]:]
	// Trim a trailing `end` (with optional trailing `;`/whitespace).
	if m := regexp.MustCompile(`(?is)\bend\b\s*;?\s*$`).FindStringIndex(body); m != nil {
		body = body[:m[0]]
	}
	return body
}

// parseCM parses an SML/NJ `*.cm` group/library file and returns its library
// dependencies (.cm includes, including anchored stdlib refs). Source-file
// members are not deps (they are the unit's own sources, surfaced on the anchor
// via cmConfigProperty). First reference of a name wins on duplicates.
func parseCM(source string) []dep {
	source = cmCommentRE.ReplaceAllString(source, " ")
	body := cmMemberBody(source)

	var out []dep
	seen := map[string]bool{}
	for _, tok := range cmMemberTokens(body) {
		if !smlLibRefSuffix(tok) {
			continue
		}
		name := smlDepName(tok)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, dep{name: name, version: "", kind: "runtime"})
	}
	return out
}

// cmMemberTokens splits a CM member-list body into member path tokens, one per
// whitespace-separated token, dropping CM tool annotations like `(tool:...)` and
// `:tool` suffixes that are not paths.
func cmMemberTokens(body string) []string {
	var out []string
	for _, f := range strings.Fields(body) {
		f = strings.TrimSpace(f)
		// Drop a `(...)` tool-class annotation token.
		if strings.HasPrefix(f, "(") {
			continue
		}
		// Drop a `: toolname` class suffix appended to a member (rare); keep the
		// path part before any whitespace (already split) — a bare `:` token is
		// dropped.
		if f == ":" || f == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// cmConfigProperty returns a compact, deterministic summary of a CM file's
// build metadata (export kind + source-file member list), surfaced on the
// project anchor as the "cm_config" property, or "" when none is present.
func cmConfigProperty(source string) string {
	source = cmCommentRE.ReplaceAllString(source, " ")
	var parts []string

	if m := cmGroupHeadRE.FindStringSubmatch(source); m != nil {
		parts = append(parts, "export="+strings.ToLower(m[1]))
	}
	var sources []string
	seen := map[string]bool{}
	for _, tok := range cmMemberTokens(cmMemberBody(source)) {
		if smlSourceSuffix(tok) && !seen[tok] {
			seen[tok] = true
			sources = append(sources, tok)
		}
	}
	if len(sources) > 0 {
		parts = append(parts, "sources="+strings.Join(sources, " "))
	}
	return strings.Join(parts, "; ")
}

// ---------------------------------------------------------------------------
// Parser: *.mlb (MLton ML Basis)
// ---------------------------------------------------------------------------

// mlbCommentRE strips MLB `(* ... *)` block comments (same syntax as SML).
var mlbCommentRE = regexp.MustCompile(`(?s)\(\*.*?\*\)`)

// mlbKeywords are MLB declaration keywords that are NOT path members; a token
// equal to one of these (case-insensitive) is skipped during the path scan.
var mlbKeywords = map[string]bool{
	"basis": true, "local": true, "in": true, "end": true, "open": true,
	"let": true, "ann": true, "and": true, "functor": true, "signature": true,
	"structure": true, "=": true,
}

// mlbPathTokens scans an MLB body for path tokens — the whitespace-separated
// tokens that look like file paths (carry a known SML/basis suffix). Declaration
// keywords, `ann "..."` annotation strings, and `basis B = ...`/`open B` basis
// identifiers are skipped (only suffix-bearing path members are kept).
func mlbPathTokens(body string) []string {
	var out []string
	for _, f := range strings.Fields(body) {
		f = strings.Trim(f, ";")
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		// Skip quoted annotation strings ("redundantMatch warn" etc.).
		if strings.HasPrefix(f, `"`) {
			continue
		}
		if mlbKeywords[strings.ToLower(f)] {
			continue
		}
		if smlLibRefSuffix(f) || smlSourceSuffix(f) {
			out = append(out, f)
		}
	}
	return out
}

// parseMLB parses a MLton `*.mlb` ML Basis file and returns its library
// dependencies — other `.mlb` includes, including the anchored MLton stdlib
// basis ($(SML_LIB)/basis/basis.mlb). Source-file members (.sml/.sig/.fun) are
// the unit's own sources (surfaced on the anchor via mlbConfigProperty), not
// deps. First reference of a name wins on duplicates.
func parseMLB(source string) []dep {
	source = mlbCommentRE.ReplaceAllString(source, " ")

	var out []dep
	seen := map[string]bool{}
	for _, tok := range mlbPathTokens(source) {
		if !smlLibRefSuffix(tok) {
			continue
		}
		name := smlDepName(tok)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, dep{name: name, version: "", kind: "runtime"})
	}
	return out
}

// mlbConfigProperty returns a compact, deterministic summary of a MLB file's
// source-file member list, surfaced on the project anchor as the "mlb_config"
// property, or "" when the file declares no source members.
func mlbConfigProperty(source string) string {
	source = mlbCommentRE.ReplaceAllString(source, " ")
	var sources []string
	seen := map[string]bool{}
	for _, tok := range mlbPathTokens(source) {
		if smlSourceSuffix(tok) && !seen[tok] {
			seen[tok] = true
			sources = append(sources, tok)
		}
	}
	if len(sources) == 0 {
		return ""
	}
	return "sources=" + strings.Join(sources, " ")
}
