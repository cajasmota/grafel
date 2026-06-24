// ocaml.go — OCaml opam / Dune package & build manifest parsers (#5374,
// epic #5360).
//
// The OCaml ecosystem declares dependencies across two manifest shapes, both
// parsed here:
//
//	*.opam — the opam package manifest (the canonical OCaml package
//	  description). Runtime dependencies are declared in a `depends:` field whose
//	  value is a bracketed list of quoted package names, each optionally followed
//	  by a `{ ... }` version-and-filter formula:
//
//	    opam-version: "2.0"
//	    name: "my-service"
//	    depends: [
//	      "ocaml" {>= "4.14"}
//	      "dune" {>= "3.0"}
//	      "dream"
//	      "caqti" {>= "1.9.0"}
//	      "alcotest" {with-test}
//	    ]
//	    depopts: [ "lwt_ssl" ]
//
//	  A dependency carrying a `{with-test}` (or `{with-doc}`) filter is a
//	  dev/test-only dependency and is flagged is_dev. `depopts:` lists optional
//	  dependencies (kind="optional"). The `ocaml` compiler-version floor is KEPT
//	  as a real edge (mirrors the nimble `nim` / luarocks `lua` interpreter-floor
//	  treatment, #5365/#5367). The version inside the first `{ ... }` formula
//	  (the constraint preceding any `&`/filter terms) is recorded as the version.
//
//	dune-project — the Dune project file. When the project uses Dune's
//	  generate_opam_files workflow, packages are declared inline with `(package
//	  (name ...) (depends pkg1 pkg2 (pkg3 (>= 1.0)) ...))` stanzas; the
//	  `(depends ...)` list is the dependency surface. A bare `(lang dune 3.0)`
//	  project with no inline `(package ...)` declares no deps here (the *.opam
//	  file carries them) and is a no-op.
//
// Honest scope: only the DECLARED dependency surface is recovered. opam version
// FORMULAS are reduced to the first version literal (conjunction/disjunction of
// constraints and non-version filter terms like `{build}` are not threaded);
// opam `pin-depends:`, `conflicts:` and `available:` os-filters are not modelled;
// Dune `(libraries ...)` stanzas in plain `dune` files name INTERNAL+external
// libraries indistinguishably and are intentionally NOT mined as package deps
// (that surface belongs to the source-graph IMPORTS, not the manifest dep
// graph). opam has no separate lockfile in wide use, so only manifest_parsing is
// provided.
package manifest

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Parser: *.opam
// ---------------------------------------------------------------------------

// opamDependsFieldRE matches the start of a `depends:` / `depopts:` field and
// captures the field key so the value list (which follows in `[ ... ]`) can be
// classified runtime vs optional. The field must start at a line boundary so a
// `depends` substring inside a string/comment never opens a match.
var opamDependsFieldRE = regexp.MustCompile(`(?m)^(depends|depopts)\s*:\s*\[`)

// opamDepEntryRE matches one dependency entry inside a depends list: a quoted
// package name optionally followed by a `{ ... }` version/filter formula.
//
//	"dune" {>= "3.0"}        → name=dune,     formula=">= "3.0""
//	"dream"                  → name=dream,    formula=""
//	"alcotest" {with-test}   → name=alcotest, formula="with-test"
//
// Group 1 is the package name; group 2 is the formula body (without braces) or
// empty. opam package names are letters, digits, `_`, `-` and `+`.
var opamDepEntryRE = regexp.MustCompile(
	`"([A-Za-z0-9][A-Za-z0-9_+-]*)"\s*(?:\{([^}]*)\})?`)

// opamFormulaVersionRE pulls the first quoted version literal out of a formula
// body (`>= "4.14"` → `4.14`), prefixed with the comparison operator if present.
var opamFormulaVersionRE = regexp.MustCompile(`([<>=~!]*=?)\s*"([^"]+)"`)

// parseOpam parses a `*.opam` manifest and returns its declared dependencies.
// `depends:` entries are runtime (a `{with-test}`/`{with-doc}` filter flags them
// is_dev); `depopts:` entries are optional (kind="optional"). First declaration
// of a name wins.
func parseOpam(source string) []dep {
	var out []dep
	seen := map[string]bool{}

	for _, field := range opamDependsFieldRE.FindAllStringSubmatchIndex(source, -1) {
		// field[2:4] is the field-key capture; field[1] is the offset just
		// after the opening `[`.
		key := source[field[2]:field[3]]
		body := opamListBody(source, field[1])
		optional := key == "depopts"
		for _, dm := range opamDepEntryRE.FindAllStringSubmatch(body, -1) {
			name := dm[1]
			if name == "" || seen[name] {
				continue
			}
			formula := dm[2]
			isDev := strings.Contains(formula, "with-test") || strings.Contains(formula, "with-doc")
			kind := "runtime"
			switch {
			case optional:
				kind = "optional"
			case isDev:
				kind = "dev"
			}
			seen[name] = true
			out = append(out, dep{
				name:    name,
				version: opamFormulaVersion(formula),
				isDev:   isDev,
				kind:    kind,
			})
		}
	}
	return out
}

// opamListBody returns the text of a bracketed list starting just after its
// opening `[` (at startAfter), up to the matching `]`, honouring nested
// brackets (a version formula `{ ... }` never contains `]`, but defensive
// bracket-depth tracking keeps the scan correct for nested `[ ... ]` filters).
func opamListBody(source string, startAfter int) string {
	depth := 1
	for i := startAfter; i < len(source); i++ {
		switch source[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return source[startAfter:i]
			}
		}
	}
	return source[startAfter:]
}

// opamFormulaVersion extracts the first version literal from an opam version
// formula body (the part before any `&`/filter conjunction). A `with-test`-only
// filter has no version and yields "".
func opamFormulaVersion(formula string) string {
	m := opamFormulaVersionRE.FindStringSubmatch(formula)
	if m == nil {
		return ""
	}
	op := strings.TrimSpace(m[1])
	ver := strings.TrimSpace(m[2])
	if op != "" {
		return op + " " + ver
	}
	return ver
}

// ---------------------------------------------------------------------------
// Parser: dune-project
// ---------------------------------------------------------------------------

// duneProjectHasDepends is a fast pre-filter: a dune-project with no `(depends`
// sexp declares no inline package deps (they live in the *.opam file) and is a
// no-op for the manifest dep-graph.
var duneProjectHasDepends = "(depends"

// duneNonDepAtoms are leading atoms inside a `(depends ...)` body that are NOT
// package names (constraint operators / filter keywords that can appear as the
// head of a constrained sub-list or filter).
var duneNonDepAtoms = map[string]bool{
	">=": true, "<=": true, "=": true, ">": true, "<": true, "<>": true,
	"and": true, "or": true, ":with-test": true, ":with-doc": true,
	":build": true, ":dev": true, ":post": true, ":pinned": true,
}

// parseDuneProject parses a `dune-project` file and returns the declared
// dependencies of any inline `(package (name ...) (depends ...))` stanza (the
// generate_opam_files workflow). A `with-test`/`with-doc`-filtered dep is
// flagged is_dev. A dune-project with no inline `(depends ...)` is a no-op.
func parseDuneProject(source string) []dep {
	if !strings.Contains(source, duneProjectHasDepends) {
		return nil
	}
	var out []dep
	seen := map[string]bool{}

	for _, body := range duneDependsBodies(source) {
		for _, entry := range duneDependsEntries(body) {
			name := entry.name
			if name == "" || seen[name] || duneNonDepAtoms[name] {
				continue
			}
			seen[name] = true
			kind := "runtime"
			if entry.isDev {
				kind = "dev"
			}
			out = append(out, dep{
				name:    name,
				version: entry.version,
				isDev:   entry.isDev,
				kind:    kind,
			})
		}
	}
	return out
}

// duneDependsBodies returns the body text of every `(depends ...)` sexp in a
// dune-project, balanced on parens.
func duneDependsBodies(source string) []string {
	var bodies []string
	const marker = "(depends"
	for {
		idx := strings.Index(source, marker)
		if idx < 0 {
			break
		}
		// Walk from the opening paren to its matching close.
		depth := 0
		end := -1
		for i := idx; i < len(source); i++ {
			switch source[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			bodies = append(bodies, source[idx+len(marker):])
			break
		}
		bodies = append(bodies, source[idx+len(marker):end])
		source = source[end+1:]
	}
	return bodies
}

// duneDepEntry is one parsed (depends ...) entry.
type duneDepEntry struct {
	name    string
	version string
	isDev   bool
}

// duneDependsEntries mines one `(depends ...)` body for dependency entries.
// A bare atom (`dream`) is a dep with no version; a constrained sub-list
// (`(caqti (>= 1.9.0))`) yields the leading atom as the name and the version
// literal as the constraint; a `:with-test`/`:with-doc` filter term inside a
// sub-list marks the owning dep is_dev.
func duneDependsEntries(body string) []duneDepEntry {
	var entries []duneDepEntry
	i := 0
	for i < len(body) {
		c := body[i]
		switch {
		case c == '(':
			// Constrained sub-list: read to matching close.
			depth := 0
			end := i
			for j := i; j < len(body); j++ {
				if body[j] == '(' {
					depth++
				} else if body[j] == ')' {
					depth--
					if depth == 0 {
						end = j
						break
					}
				}
			}
			sub := body[i+1 : end]
			entries = append(entries, parseDuneConstrainedDep(sub))
			i = end + 1
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		default:
			// Bare atom.
			start := i
			for i < len(body) && !isDuneAtomBreak(body[i]) {
				i++
			}
			atom := body[start:i]
			entries = append(entries, duneDepEntry{name: atom})
		}
	}
	return entries
}

func isDuneAtomBreak(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '(' || c == ')'
}

// parseDuneConstrainedDep parses a constrained dependency sub-list body, e.g.
// `caqti (>= 1.9.0)` or `alcotest :with-test`.
func parseDuneConstrainedDep(sub string) duneDepEntry {
	fields := strings.Fields(sub)
	if len(fields) == 0 {
		return duneDepEntry{}
	}
	e := duneDepEntry{name: fields[0]}
	if strings.Contains(sub, ":with-test") || strings.Contains(sub, ":with-doc") {
		e.isDev = true
	}
	// Pull a version literal from a `(>= 1.9.0)` style constraint.
	if m := duneVersionRE.FindStringSubmatch(sub); m != nil {
		op := strings.TrimSpace(m[1])
		ver := strings.TrimSpace(m[2])
		if op != "" {
			e.version = op + " " + ver
		} else {
			e.version = ver
		}
	}
	return e
}

// duneVersionRE pulls an operator + version literal out of a dune constraint
// sub-list (`(>= 1.9.0)` → op=">=", ver="1.9.0").
var duneVersionRE = regexp.MustCompile(`([<>=~!]*=?)\s*([0-9][A-Za-z0-9_.+-]*)`)
