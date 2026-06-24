// lisp.go — Common Lisp ASDF / Quicklisp system-definition manifest parser
// (#5385, epic #5360).
//
// Common Lisp's de-facto build/dependency manifest is the ASDF *.asd system
// definition. A *.asd file contains one or more `(defsystem ...)` forms
// (optionally package-qualified, e.g. `asdf:defsystem` / `asdf/defsystem`); the
// system's declared dependencies are the `:depends-on (...)` list and its source
// members are the `:components (...)` list:
//
//	(defsystem "my-app"
//	  :version "1.2.0"
//	  :author "Jane Doe"
//	  :depends-on ("alexandria"
//	               "bordeaux-threads"
//	               (:version "cl-ppcre" "2.0.0")
//	               (:feature :sbcl "sb-posix"))
//	  :components ((:file "package")
//	               (:module "src"
//	                :components ((:file "core") (:file "util")))))
//
// Quicklisp — the dominant Common Lisp dependency manager — installs the
// systems named in `:depends-on` (Quicklisp resolves and fetches them from its
// remote dist; the .asd is the per-system manifest). There is no per-project
// Quicklisp lockfile in the tree (a Quicklisp dist pins versions REMOTELY, by
// dist release, not via an in-repo lock), so lockfile parsing is not_applicable.
//
// Honest scope (heuristic S-expression scan, no bundled grammar):
//   - Every `defsystem` form in the file is parsed; its system name (first
//     string/symbol after `defsystem`) anchors the deps but is not itself a dep.
//   - `:depends-on` entries are recovered in all three ASDF shapes: a bare string
//     ("alexandria"), a bare symbol (alexandria / :alexandria), and a dependency-
//     spec list whose dep name is the contained string/symbol — `(:version "x"
//     ...)`, `(:feature <feature-expr> "x")`, `(:require "x")`. Reader-conditional
//     `#+/#-` guarded deps are flattened (the guard is ignored; the dep is kept).
//   - The `:components` member list (top-level :file / :module names) is
//     summarized on the project anchor as a compact `asd_config` property — no new
//     entity kind, the same model as ipkg_config / cm_config / build_targets
//     (#5382/#5383/#5377).
//   - Version constraints are NOT generally available (only `(:version "x" "v")`
//     spec entries carry one); those are recorded, everything else is empty.
package manifest

import (
	"regexp"
	"strings"
)

// asdDefsystemRE matches the head of a `(defsystem ...)` form, optionally
// package-qualified (asdf:defsystem, asdf/defsystem, cl:defsystem). Group 1 is
// the system name token immediately following defsystem — a "double-quoted"
// string, a #:uninterned symbol, or a bare/keyword symbol. The name anchors the
// form but is itself excluded from the dependency set.
var asdDefsystemRE = regexp.MustCompile(
	`(?is)\(\s*(?:[a-z0-9+._/-]*:)?defsystem\s+(?:#?:?"([^"]+)"|#?:?([a-z0-9+._/-]+))`)

// asdDependsOnRE locates a `:depends-on` keyword. The list body that follows is
// balance-scanned by dependsOnBody (a regex cannot match balanced parens for the
// nested dependency-spec forms).
var asdDependsOnRE = regexp.MustCompile(`(?i):depends-on\b`)

// asdComponentsRE locates a `:components` keyword (top-level member list).
var asdComponentsRE = regexp.MustCompile(`(?i):components\b`)

// asdStringTokenRE matches a double-quoted token, e.g. "alexandria".
var asdStringTokenRE = regexp.MustCompile(`"([^"]+)"`)

// asdSymbolTokenRE matches a bare/keyword Lisp symbol used as a dependency name,
// e.g. alexandria, bordeaux-threads, :sb-posix. Excludes the ASDF spec keywords
// that introduce a dependency-spec list (handled separately).
var asdSymbolTokenRE = regexp.MustCompile(`(?i)^:?([a-z][a-z0-9+._/-]*)$`)

// asdSpecKeywords are the ASDF dependency-spec list heads; the dep NAME is the
// string/symbol contained in the spec, not the keyword itself.
var asdSpecKeywords = map[string]bool{
	"version": true, "feature": true, "require": true,
}

// asdFileModuleNameRE matches the name of a top-level (:file "x") / (:module
// "x" ...) / (:static-file "x") component for the asd_config anchor summary.
var asdComponentEntryRE = regexp.MustCompile(
	`(?is)\(\s*:(file|module|static-file|cl-source-file|system)\s+(?:"([^"]+)"|([a-z0-9+._/-]+))`)

// matchParen returns the index just past the close paren matching the open paren
// at source[open], or -1 if unbalanced. String literals are skipped so a `)`
// inside a "..." token is not mistaken for a structural close.
func matchParen(source string, open int) int {
	depth := 0
	inStr := false
	for i := open; i < len(source); i++ {
		c := source[i]
		if inStr {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// dependsOnBody returns the balanced list body that follows a `:depends-on`
// keyword starting at kwEnd (the index just past the keyword). It skips any
// whitespace then expects an opening paren; returns the inner text between the
// parens, or "" when no list is present (e.g. a `:depends-on` with no list).
func dependsOnBody(source string, kwEnd int) string {
	i := kwEnd
	for i < len(source) && (source[i] == ' ' || source[i] == '\t' || source[i] == '\n' || source[i] == '\r') {
		i++
	}
	if i >= len(source) || source[i] != '(' {
		return ""
	}
	end := matchParen(source, i)
	if end < 0 {
		return ""
	}
	return source[i+1 : end-1]
}

// parseDependsOnList extracts dependency names from a `:depends-on` list body.
// It handles the three ASDF entry shapes:
//
//	"alexandria"                       bare string
//	bordeaux-threads / :alexandria     bare symbol
//	(:version "cl-ppcre" "2.0.0")      version spec  → name=cl-ppcre, version=2.0.0
//	(:feature :sbcl "sb-posix")        feature spec  → name=sb-posix
//	(:require "uiop")                  require spec  → name=uiop
//
// Reader conditionals (#+/#-) are tokens we simply don't treat as deps. The
// caller dedupes; this returns one dep per recovered name.
func parseDependsOnList(body string) []dep {
	var out []dep
	seen := map[string]bool{}
	add := func(name, version string) {
		name = strings.TrimSpace(name)
		name = strings.TrimPrefix(name, ":")
		if name == "" || seen[strings.ToLower(name)] {
			return
		}
		seen[strings.ToLower(name)] = true
		out = append(out, dep{name: name, version: version, kind: "runtime"})
	}

	i := 0
	for i < len(body) {
		c := body[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '"':
			// Bare string dependency.
			if m := asdStringTokenRE.FindStringSubmatchIndex(body[i:]); m != nil && m[0] == 0 {
				add(body[i+m[2]:i+m[3]], "")
				i += m[1]
				continue
			}
			i++
		case c == '(':
			// A dependency-spec list — extract its inner name (+ version).
			end := matchParen(body, i)
			if end < 0 {
				i++
				continue
			}
			inner := body[i+1 : end-1]
			name, version := specName(inner)
			add(name, version)
			i = end
		case c == '#':
			// Reader conditional marker (#+feat / #-feat) — skip the marker and
			// its immediately-following feature expression token; the guarded dep
			// that follows is then read normally on the next iterations.
			i++
			if i < len(body) && (body[i] == '+' || body[i] == '-') {
				i++
			}
			// Skip the feature expression (a symbol or a balanced (...) form).
			for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r') {
				i++
			}
			if i < len(body) && body[i] == '(' {
				if e := matchParen(body, i); e > 0 {
					i = e
				} else {
					i++
				}
			} else {
				for i < len(body) && !isLispSep(body[i]) {
					i++
				}
			}
		default:
			// A bare symbol token.
			start := i
			for i < len(body) && !isLispSep(body[i]) {
				i++
			}
			tok := body[start:i]
			if m := asdSymbolTokenRE.FindStringSubmatch(tok); m != nil {
				add(m[1], "")
			}
		}
	}
	return out
}

// isLispSep reports whether c terminates a bare Lisp symbol token.
func isLispSep(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '(' || c == ')' || c == '"'
}

// specName extracts the dependency name (+ version, for :version specs) from a
// dependency-spec list inner body, e.g. `:version "cl-ppcre" "2.0.0"`,
// `:feature :sbcl "sb-posix"`, `:require "uiop"`. The name is the FIRST string
// (or non-keyword symbol) after the spec keyword; for :version the SECOND string
// is the version constraint.
func specName(inner string) (string, string) {
	inner = strings.TrimSpace(inner)
	// Determine the spec keyword (if any).
	fields := lispTokens(inner)
	if len(fields) == 0 {
		return "", ""
	}
	head := strings.ToLower(strings.TrimPrefix(fields[0], ":"))
	if asdSpecKeywords[head] {
		// Find the first string/non-keyword-symbol after the keyword as the name.
		rest := fields[1:]
		var name, version string
		for _, f := range rest {
			if isFeatureExpr(f) {
				continue
			}
			name = unquote(f)
			break
		}
		if head == "version" {
			// The token after the name is the version constraint.
			seenName := false
			for _, f := range rest {
				if isFeatureExpr(f) {
					continue
				}
				if !seenName {
					seenName = true
					continue
				}
				version = unquote(f)
				break
			}
		}
		return name, version
	}
	// Not a recognized spec keyword — treat the first token as the name (a bare
	// nested list naming a system).
	return unquote(fields[0]), ""
}

// lispTokens splits a spec body into top-level tokens (strings stay intact,
// nested (...) groups are kept whole).
func lispTokens(s string) []string {
	var out []string
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '"':
			if m := asdStringTokenRE.FindStringSubmatchIndex(s[i:]); m != nil && m[0] == 0 {
				out = append(out, s[i:i+m[1]])
				i += m[1]
				continue
			}
			i++
		case c == '(':
			end := matchParen(s, i)
			if end < 0 {
				out = append(out, s[i:])
				return out
			}
			out = append(out, s[i:end])
			i = end
		default:
			start := i
			for i < len(s) && !isLispSep(s[i]) {
				i++
			}
			out = append(out, s[start:i])
		}
	}
	return out
}

// isFeatureExpr reports whether a token is a :feature/keyword guard (e.g. :sbcl)
// or a nested feature (...) expression, which is NOT the dependency name.
func isFeatureExpr(tok string) bool {
	if tok == "" {
		return false
	}
	if tok[0] == '(' {
		return true
	}
	// A leading-colon keyword used as a feature flag (e.g. :sbcl, :unix). The
	// dependency name in :feature specs is a string, so any keyword in that
	// position is the feature guard, not the dep.
	return tok[0] == ':'
}

// unquote strips surrounding double quotes and a leading package-qualifier colon
// from a token, returning the bare name.
func unquote(tok string) string {
	tok = strings.TrimSpace(tok)
	if len(tok) >= 2 && tok[0] == '"' && tok[len(tok)-1] == '"' {
		return tok[1 : len(tok)-1]
	}
	return strings.TrimPrefix(tok, ":")
}

// parseAsd parses an ASDF *.asd system-definition manifest and returns the union
// of every `defsystem` form's `:depends-on` entries (deduped, first wins).
func parseAsd(source string) []dep {
	source = stripLispComments(source)
	var out []dep
	seen := map[string]bool{}

	// Scan each defsystem form; restrict :depends-on lookup to that form's body
	// so a multi-system .asd attributes deps correctly (best-effort: deps are
	// merged into one project anchor, which is the per-file manifest model).
	heads := asdDefsystemRE.FindAllStringIndex(source, -1)
	scanRange := func(seg string) {
		for _, m := range asdDependsOnRE.FindAllStringIndex(seg, -1) {
			body := dependsOnBody(seg, m[1])
			for _, d := range parseDependsOnList(body) {
				key := strings.ToLower(d.name)
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, d)
			}
		}
	}

	if len(heads) == 0 {
		// No defsystem — still attempt a whole-file :depends-on scan (defensive).
		scanRange(source)
		return out
	}
	for i, h := range heads {
		end := len(source)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		scanRange(source[h[0]:end])
	}
	return out
}

// lispLineCommentRE matches a `; ...` line comment (to end of line). ASDF files
// use `;`/`;;`/`;;;` line comments; we strip them so a commented-out dep is not
// mined. Block comments `#| ... |#` are handled separately.
var lispLineCommentRE = regexp.MustCompile(`;[^\n]*`)

// stripLispComments removes `#| ... |#` block comments and `; ...` line comments
// from a Lisp source so commented-out forms are not parsed. Double-quoted
// strings are preserved (a `;` inside a string is not a comment).
func stripLispComments(source string) string {
	// Remove block comments first (non-greedy, may be nested in practice but the
	// common case is non-nested; honest best-effort).
	source = regexp.MustCompile(`(?s)#\|.*?\|#`).ReplaceAllString(source, " ")

	var b strings.Builder
	inStr := false
	for i := 0; i < len(source); i++ {
		c := source[i]
		if inStr {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(source) {
				b.WriteByte(source[i+1])
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == ';' {
			// Skip to end of line.
			for i < len(source) && source[i] != '\n' {
				i++
			}
			if i < len(source) {
				b.WriteByte('\n')
			}
			continue
		}
		b.WriteByte(c)
	}
	_ = lispLineCommentRE
	return b.String()
}

// asdConfigProperty returns a compact, deterministic summary of an *.asd
// manifest's metadata — the first system name and its top-level component
// member names — surfaced on the project anchor as the "asd_config" property, or
// "" when the source declares none of them. Mirrors the ipkg_config / cm_config
// / build_targets anchor-property model (no new entity kind).
func asdConfigProperty(source string) string {
	source = stripLispComments(source)
	var parts []string

	if m := asdDefsystemRE.FindStringSubmatch(source); m != nil {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		if name != "" {
			parts = append(parts, "system="+strings.TrimPrefix(name, ":"))
		}
	}

	// Component member names (top-level :file/:module/:static-file/...). Locate
	// the first :components list body and mine its direct child component heads.
	if loc := asdComponentsRE.FindStringIndex(source); loc != nil {
		if body := dependsOnBody(source, loc[1]); body != "" {
			var comps []string
			seen := map[string]bool{}
			for _, m := range asdComponentEntryRE.FindAllStringSubmatch(body, -1) {
				name := m[2]
				if name == "" {
					name = m[3]
				}
				name = strings.TrimSpace(name)
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				comps = append(comps, name)
			}
			if len(comps) > 0 {
				parts = append(parts, "components="+strings.Join(comps, " "))
			}
		}
	}
	return strings.Join(parts, "; ")
}
