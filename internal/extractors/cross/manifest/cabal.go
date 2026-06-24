// cabal.go — Haskell Cabal / Stack / hpack package manifest parsers (#5373,
// epic #5360).
//
// The Haskell build ecosystem has three manifest shapes, all parsed here:
//
//	*.cabal — the Cabal package description. Dependencies are declared with the
//	  `build-depends:` field inside library / executable / test-suite stanzas. The
//	  field value is a comma-separated list of `<name> <version-constraint>`
//	  entries that may span many lines:
//
//	    library
//	      build-depends:  base >=4.14 && <5
//	                    , text
//	                    , aeson >= 2.0
//	      hs-source-dirs: src
//
//	    test-suite spec
//	      build-depends: base, hspec >= 2.7, mylib
//
//	  A dependency entry's leading token (letters/digits/`-`) is the package
//	  name; the remainder is the version constraint. `base` (the GHC base library
//	  floor) is KEPT — it is a real edge in the dependency graph, mirroring the
//	  LuaRocks `lua` / nimble `nim` interpreter-floor treatment (#5365/#5367).
//	  Dependencies in a `test-suite` / `benchmark` stanza are flagged is_dev.
//
//	package.yaml — the hpack manifest (a higher-level YAML that hpack compiles to
//	  a .cabal). Dependencies are a YAML list under a top-level `dependencies:`
//	  key (runtime) and/or under per-component `tests:`/`benchmarks:` sections:
//
//	    dependencies:
//	      - base >= 4.14 && < 5
//	      - text
//	      - aeson
//
//	    tests:
//	      spec:
//	        dependencies:
//	          - hspec
//
//	stack.yaml — the Stack project/build config. Its `extra-deps:` list pins
//	  packages NOT on the chosen resolver/snapshot (the snapshot itself provides
//	  the bulk of the dependency set and is not enumerated here):
//
//	    resolver: lts-21.0
//	    extra-deps:
//	      - acme-missiles-0.3
//	      - github: foo/bar
//	        commit: abc123
//
//	  An `extra-deps` entry of the `<name>-<version>` form is split into name +
//	  version (the trailing `-<version>` semver suffix is the pinned version);
//	  source-pinned entries (git `github:`/`commit:` maps) record the repo name
//	  with an empty version and kind="locked" (an exactly-pinned source dep).
//
// Honest scope: only the declared/manifest dependency surface is recovered.
// Cabal conditional `if flag(...)` stanzas are flattened (all build-depends
// across conditionals are emitted); Stack snapshot-provided packages are not
// enumerated (the resolver is a remote set, not a local manifest).
package manifest

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Parser: *.cabal
// ---------------------------------------------------------------------------

// cabalStanzaHeadRE matches a stanza header that starts a component block. Only
// the kind matters for dev-classification; the optional name is ignored.
var cabalStanzaHeadRE = regexp.MustCompile(
	`(?mi)^(library|executable|test-suite|benchmark|foreign-library)\b`)

// cabalBuildDependsRE captures the value body of a `build-depends:` field. The
// value runs from the colon to the first subsequent line that begins at the
// SAME-or-shallower indentation as the field name with a non-comma, non-blank
// field key (i.e. the next field/stanza). Because Cabal field continuations are
// indented MORE than the field key, the body is the field key's line plus every
// following line that is either blank, a comment, or more-indented.
var cabalBuildDependsRE = regexp.MustCompile(
	`(?mi)^[ \t]*build-depends[ \t]*:`)

// cabalDepEntryRE splits one dependency entry into name + version constraint.
//
//	"base >=4.14 && <5"  → name=base,  version=">=4.14 && <5"
//	"text"               → name=text,  version=""
//	"aeson >= 2.0"       → name=aeson, version=">= 2.0"
//
// Cabal package names are letters/digits separated by `-` (e.g. `bytestring`,
// `optparse-applicative`). The version constraint is whatever follows the first
// run of whitespace.
var cabalDepEntryRE = regexp.MustCompile(
	`^([A-Za-z0-9][A-Za-z0-9_-]*)\s*(.*)$`)

// parseCabal parses a `*.cabal` manifest. Dependencies are mined from every
// `build-depends:` field; an entry inside a `test-suite`/`benchmark` stanza is
// flagged is_dev. First declaration of a name wins (runtime beats a later dev
// declaration of the same name).
func parseCabal(source string) []dep {
	lines := strings.Split(source, "\n")
	var out []dep
	seen := map[string]bool{}

	// Track the kind of the current stanza so build-depends can be classified.
	currentDev := false

	emit := func(body string, isDev bool) {
		// The body is a comma-separated list (possibly across lines). Split on
		// commas and newlines, then parse each entry.
		fields := strings.FieldsFunc(body, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r'
		})
		for _, f := range fields {
			f = strings.TrimSpace(f)
			if f == "" || strings.HasPrefix(f, "--") {
				continue
			}
			m := cabalDepEntryRE.FindStringSubmatch(f)
			if m == nil {
				continue
			}
			name := m[1]
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			kind := "runtime"
			if isDev {
				kind = "dev"
			}
			out = append(out, dep{
				name:    name,
				version: strings.TrimSpace(m[2]),
				isDev:   isDev,
				kind:    kind,
			})
		}
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if h := cabalStanzaHeadRE.FindStringSubmatch(line); h != nil {
			kind := strings.ToLower(h[1])
			currentDev = kind == "test-suite" || kind == "benchmark"
			continue
		}
		if cabalBuildDependsRE.MatchString(line) {
			// Field key indentation defines the continuation boundary.
			fieldIndent := indentWidth(line)
			// Body starts after the first colon on this line.
			body := line[strings.IndexByte(line, ':')+1:]
			// Gather more-indented / blank / comment continuation lines.
			j := i + 1
			for j < len(lines) {
				cont := lines[j]
				if strings.TrimSpace(cont) == "" {
					body += "\n"
					j++
					continue
				}
				if indentWidth(cont) > fieldIndent {
					body += "\n" + cont
					j++
					continue
				}
				break
			}
			emit(body, currentDev)
			i = j - 1
		}
	}
	return out
}

// indentWidth returns the number of leading whitespace columns (tabs count as 1)
// of a line.
func indentWidth(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' || r == '\t' {
			n++
			continue
		}
		break
	}
	return n
}

// ---------------------------------------------------------------------------
// Parser: package.yaml (hpack)
// ---------------------------------------------------------------------------

// hpackDepListItemRE matches one YAML list item in a `dependencies:` block:
//
//   - base >= 4.14 && < 5   → name=base
//   - text                  → name=text
//
// Group 1 is the package name; group 2 is the trailing version constraint.
var hpackDepListItemRE = regexp.MustCompile(
	`^[ \t]*-\s*([A-Za-z0-9][A-Za-z0-9_-]*)\s*(.*)$`)

// hpackDepsKeyRE matches a `dependencies:` YAML key (runtime deps). hpack also
// allows per-component (tests:/benchmarks:) dependency blocks; those are
// detected by an enclosing dev section tracked in parseHpackPackageYaml.
var hpackDepsKeyRE = regexp.MustCompile(`(?m)^([ \t]*)dependencies\s*:`)

// hpackDevSectionRE matches the start of a tests:/benchmarks: section so deps
// declared underneath are classified is_dev.
var hpackDevSectionRE = regexp.MustCompile(`(?m)^([ \t]*)(tests|benchmarks)\s*:`)

// parsePackageYaml parses an hpack `package.yaml` manifest. A `dependencies:`
// list under a tests:/benchmarks: section is flagged is_dev; a top-level
// `dependencies:` list is runtime.
func parsePackageYaml(source string) []dep {
	lines := strings.Split(source, "\n")
	var out []dep
	seen := map[string]bool{}

	// devIndent is the indentation of the active tests:/benchmarks: section, or
	// -1 when not inside one. A line at <= devIndent ends the dev section.
	devIndent := -1

	emit := func(start int, keyIndent int, isDev bool) int {
		j := start + 1
		for j < len(lines) {
			cur := lines[j]
			if strings.TrimSpace(cur) == "" {
				j++
				continue
			}
			// A dependency list item must be MORE indented than the key.
			if indentWidth(cur) <= keyIndent {
				break
			}
			m := hpackDepListItemRE.FindStringSubmatch(cur)
			if m == nil {
				// Non-list-item line at deeper indent ends the inline list.
				break
			}
			name := m[1]
			if name != "" && !seen[name] {
				seen[name] = true
				kind := "runtime"
				if isDev {
					kind = "dev"
				}
				out = append(out, dep{
					name:    name,
					version: strings.TrimSpace(m[2]),
					isDev:   isDev,
					kind:    kind,
				})
			}
			j++
		}
		return j - 1
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		ind := indentWidth(line)
		// Leaving a dev section.
		if devIndent >= 0 && ind <= devIndent && strings.TrimSpace(line) != "" {
			if !hpackDevSectionRE.MatchString(line) {
				devIndent = -1
			}
		}
		if m := hpackDevSectionRE.FindStringSubmatch(line); m != nil {
			devIndent = len(m[1])
			continue
		}
		if m := hpackDepsKeyRE.FindStringSubmatch(line); m != nil {
			keyIndent := len(m[1])
			isDev := devIndent >= 0 && keyIndent > devIndent
			i = emit(i, keyIndent, isDev)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Parser: stack.yaml
// ---------------------------------------------------------------------------

// stackExtraDepsKeyRE matches the `extra-deps:` YAML key.
var stackExtraDepsKeyRE = regexp.MustCompile(`(?m)^([ \t]*)extra-deps\s*:`)

// stackVersionedDepRE matches an `extra-deps` list item of the `<name>-<version>`
// form, splitting the trailing semver suffix off as the pinned version:
//
//   - acme-missiles-0.3   → name=acme-missiles, version=0.3
//   - text-2.0.1          → name=text,          version=2.0.1
//
// The name is the longest leading run of `-`-separated identifier segments
// before the final `-<numeric-version>` segment.
var stackVersionedDepRE = regexp.MustCompile(
	`^[ \t]*-\s*([A-Za-z0-9][A-Za-z0-9_-]*?)-([0-9][A-Za-z0-9.]*)\s*$`)

// stackBareDepRE matches an `extra-deps` list item with no version suffix
// (a bare package name) — rare but valid.
var stackBareDepRE = regexp.MustCompile(
	`^[ \t]*-\s*([A-Za-z0-9][A-Za-z0-9_-]*)\s*$`)

// stackGitRepoRE matches a source-pinned git extra-dep map entry's repo:
//
//   - github: foo/bar   → name=bar
//   - git: https://example.com/foo/baz.git → name=baz
var stackGitRepoRE = regexp.MustCompile(
	`^[ \t]*-?\s*(?:github|git)\s*:\s*(?:\S*/)?([A-Za-z0-9._-]+?)(?:\.git)?/?\s*$`)

// parseStackYaml parses a `stack.yaml` and returns its `extra-deps` entries as
// pinned (kind="locked") dependencies. The resolver/snapshot set is NOT
// enumerated (it is a remote package set, not a local manifest).
func parseStackYaml(source string) []dep {
	lines := strings.Split(source, "\n")
	var out []dep
	seen := map[string]bool{}

	add := func(name, version string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, dep{name: name, version: version, kind: "locked"})
	}

	for i := 0; i < len(lines); i++ {
		if m := stackExtraDepsKeyRE.FindStringSubmatch(lines[i]); m != nil {
			keyIndent := len(m[1])
			for j := i + 1; j < len(lines); j++ {
				cur := lines[j]
				if strings.TrimSpace(cur) == "" {
					continue
				}
				// A list entry must be more-indented than the key; a same-or-
				// shallower key ends the block.
				if indentWidth(cur) <= keyIndent && !strings.HasPrefix(strings.TrimSpace(cur), "-") {
					i = j - 1
					break
				}
				if gm := stackGitRepoRE.FindStringSubmatch(cur); gm != nil {
					add(gm[1], "")
				} else if vm := stackVersionedDepRE.FindStringSubmatch(cur); vm != nil {
					add(vm[1], vm[2])
				} else if bm := stackBareDepRE.FindStringSubmatch(cur); bm != nil {
					add(bm[1], "")
				}
				i = j
			}
		}
	}
	return out
}
