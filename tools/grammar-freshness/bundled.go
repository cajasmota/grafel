package main

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Pin is one grammar's ACTUAL bundled version, derived from the repo itself.
//
// Historically (#6749) this was a single constant — the pinned_date of a
// `smacker/go-tree-sitter` binding that has not been a dependency of this repo
// for a long time — reused for every grammar. That made the freshness report's
// "bundled" side independent of anything the repo does, so its verdict could
// never change. Bundled versions are now read per grammar from the two places
// the truth actually lives:
//
//  1. go.mod, for the ~21 grammars that come from their own module.
//  2. The vendored-source header in internal/treesitter/ts/grammars/<lang>/<lang>.go,
//     for the grammars whose C sources are checked in.
//
// A Pin carries a Release (a semver-ish tag) OR a Date (a commit date, for
// pseudo-versions and branch pins) — never an invented value. A grammar whose
// pin cannot be resolved is reported UNKNOWN, never defaulted.
type Pin struct {
	// Source is the upstream owner/repo slug, matching grammars.lock's "source".
	Source string
	// Release is the pinned release tag ("v0.23.6", "0.3.8"), or "" when the pin
	// is a commit rather than a release.
	Release string
	// Date is the pinned commit date (YYYY-MM-DD), or "" when the pin is a bare
	// release tag with no date recorded.
	Date string
	// Origin says where the pin was read from, so a wrong row can be traced.
	Origin string
	// RawRef is the provenance text a vendored header recorded, verbatim. It
	// exists so a diagnostic can quote what the file actually says instead of
	// asserting the file records nothing — the failure class of #6749 itself.
	RawRef string
}

// String renders the pin for the BUNDLED column.
func (p Pin) String() string {
	switch {
	case p.Release != "" && p.Date != "":
		return p.Release + " @ " + p.Date
	case p.Release != "":
		return p.Release
	case p.Date != "":
		return p.Date
	default:
		return "?"
	}
}

// resolved reports whether the pin carries any real version information.
func (p Pin) resolved() bool { return p.Release != "" || p.Date != "" }

// Pins is the resolved bundled-version set, keyed by upstream owner/repo slug.
type Pins struct {
	bySource map[string]Pin
}

// Get returns the pin for an upstream slug.
func (p Pins) Get(source string) (Pin, bool) {
	pin, ok := p.bySource[source]
	return pin, ok
}

// sortedKeys returns a map's keys in a deterministic order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Sources lists every pinned slug.
func (p Pins) Sources() []string { return sortedKeys(p.bySource) }

// loadPins resolves every grammar's bundled version from go.mod plus the
// vendored grammar packages.
//
// The two sources PARTITION the grammars; there is no precedence rule, because
// there is nothing for one to win. Every language is reached through exactly one
// internal/treesitter/ts/grammars/<lang> package, and that package either
// imports a go.mod grammar binding (bash, c, java, ...) or cgo-compiles vendored
// C (kotlin, hcl, sql, groovy, proto, dockerfile) — never both. On the real repo
// the two maps are disjoint, asserted by TestLoadPins_SourcesPartitionGrammars.
//
// An earlier revision of this comment claimed "go.mod wins where both exist,
// because it is what compiles". That tie-break was unobservable AND backwards:
// if a grammar were vendored, the vendored C is what links, so preferring the
// go.mod version would report a version the binary does not contain. Rather than
// silently pick a side, an overlap is a hard error naming the grammar — it means
// a migration is half-done and a human must say which source is real.
func loadPins(goModPath, vendorRoot string) (Pins, error) {
	vendored, err := parseVendoredPins(vendorRoot)
	if err != nil {
		return Pins{}, err
	}
	mod, err := parseGoModPins(goModPath)
	if err != nil {
		return Pins{}, err
	}
	merged := make(map[string]Pin, len(vendored)+len(mod))
	for _, k := range sortedKeys(vendored) {
		if m, both := mod[k]; both {
			return Pins{}, fmt.Errorf(
				"grammar %s is pinned twice — %s says %s and %s says %s; "+
					"a grammar is either a go.mod module or vendored C, never both, "+
					"so one of these is stale and no tie-break can be trusted",
				k, mod[k].Origin, m.String(), vendored[k].Origin, vendored[k].String())
		}
		merged[k] = vendored[k]
	}
	for k, v := range mod {
		merged[k] = v
	}
	if len(merged) == 0 {
		return Pins{}, fmt.Errorf("no grammar pins found in %s or %s", goModPath, vendorRoot)
	}
	return Pins{bySource: merged}, nil
}

// pseudoVersion matches a Go pseudo-version's embedded UTC timestamp, e.g.
// v0.0.0-20260601004120-31d17fe7e818 -> 20260601.
var pseudoVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.]+)?-(\d{8})\d{6}-[0-9a-f]{12}$`)

// parseGoModPins reads go.mod and returns one Pin per grammar module, keyed by
// upstream slug. Only modules whose final path element starts with
// "tree-sitter-" are grammars: that deliberately excludes the runtime binding
// github.com/tree-sitter/go-tree-sitter, which is not a grammar.
//
// Replace directives are applied, so a module replaced by a fork is reported
// under the fork's slug — which is what grammars.lock names as the source.
func parseGoModPins(goModPath string) (map[string]Pin, error) {
	f, err := os.Open(goModPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	requires := map[string]string{} // module path -> version
	replaces := map[string]string{} // module path -> "newpath newversion"

	inRequire, inReplace := false, false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(stripLineComment(sc.Text()))
		if line == "" {
			continue
		}
		switch {
		case line == ")":
			inRequire, inReplace = false, false
			continue
		case strings.HasPrefix(line, "require ("):
			inRequire = true
			continue
		case strings.HasPrefix(line, "replace ("):
			inReplace = true
			continue
		case strings.HasPrefix(line, "require "):
			addRequire(requires, strings.TrimPrefix(line, "require "))
			continue
		case strings.HasPrefix(line, "replace "):
			addReplace(replaces, strings.TrimPrefix(line, "replace "))
			continue
		}
		if inRequire {
			addRequire(requires, line)
		} else if inReplace {
			addReplace(replaces, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", goModPath, err)
	}

	pins := map[string]Pin{}
	for mod, ver := range requires {
		if !isGrammarModule(mod) {
			continue
		}
		if rep, ok := replaces[mod]; ok {
			parts := strings.Fields(rep)
			if len(parts) == 2 {
				mod, ver = parts[0], parts[1]
			} else if len(parts) == 1 {
				mod = parts[0]
			}
			if !isGrammarModule(mod) {
				continue
			}
		}
		pin := Pin{Source: moduleSlug(mod), Origin: filepath.Base(goModPath)}
		if m := pseudoVersion.FindStringSubmatch(ver); m != nil {
			pin.Date = m[1][0:4] + "-" + m[1][4:6] + "-" + m[1][6:8]
		} else {
			pin.Release = ver
		}
		pins[pin.Source] = pin
	}
	return pins, nil
}

// addRequire records "modulepath v1.2.3" from a require line.
func addRequire(into map[string]string, s string) {
	fields := strings.Fields(s)
	if len(fields) >= 2 {
		into[fields[0]] = fields[1]
	}
}

// addReplace records "old [oldver] => new [newver]" from a replace line.
func addReplace(into map[string]string, s string) {
	lhs, rhs, ok := strings.Cut(s, "=>")
	if !ok {
		return
	}
	lf := strings.Fields(lhs)
	if len(lf) == 0 {
		return
	}
	into[lf[0]] = strings.TrimSpace(rhs)
}

// stripLineComment drops a trailing "// indirect"-style comment. It is only ever
// applied to go.mod lines, which cannot contain "//" inside a token.
func stripLineComment(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		return s[:i]
	}
	return s
}

// isGrammarModule reports whether a module path names a tree-sitter GRAMMAR (as
// opposed to the runtime binding, whose base is "go-tree-sitter").
func isGrammarModule(mod string) bool {
	return strings.HasPrefix(path.Base(mod), "tree-sitter-")
}

// moduleSlug converts a module path to the owner/repo slug grammars.lock uses.
func moduleSlug(mod string) string {
	return strings.TrimPrefix(mod, "github.com/")
}

// vendoredSource / vendoredRef match the license-audit provenance header the
// vendored grammar packages carry, e.g.
//
//	source: github.com/fwcd/tree-sitter-kotlin
//	ref:    e1a2d5ad... (0.3.8, 2024-08-03)
var (
	// The source line may carry trailing prose ("(fork)", "— vendored"); the
	// slug is the first field. Anchoring to end-of-line dropped the WHOLE
	// grammar over a trailing note, ref line and all.
	vendoredSource = regexp.MustCompile(`^//\s*source:\s*(\S+)`)
	vendoredRef    = regexp.MustCompile(`^//\s*ref:\s*\S+\s*\(([^)]*)\)`)
	// fullDate accepts a dash- or dot-separated calendar date; normaliseDate
	// converts it to the canonical dashed form.
	fullDate   = regexp.MustCompile(`^(\d{4})[-.](\d{1,2})[-.](\d{1,2})$`)
	releaseTag = regexp.MustCompile(`^v?\d+\.\d+(\.\d+)?`)
)

// dateShaped reports whether s is a calendar date rather than a version.
//
// This guard is load-bearing. A dotted date like "2024.05.09" satisfies every
// "looks like a version" test, parses as [2024,5,9], and therefore compares
// GREATER than every real tree-sitter tag (v0.2x, v1.x) — so a grammar that is
// genuinely behind would be reported CURRENT. That is #6749's exact direction:
// an alarm that cannot report staleness, only harder to notice. A date is never
// a release identifier here.
func dateShaped(s string) bool {
	return fullDate.MatchString(strings.TrimSpace(s))
}

// normaliseDate renders a dash- or dot-separated date as YYYY-MM-DD, or ""
// if s is not a date.
func normaliseDate(s string) string {
	m := fullDate.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return ""
	}
	y, mo, d := m[1], m[2], m[3]
	if len(mo) == 1 {
		mo = "0" + mo
	}
	if len(d) == 1 {
		d = "0" + d
	}
	return y + "-" + mo + "-" + d
}

// parseVendoredPins reads the provenance headers of the vendored grammar
// packages under root. A grammar with no readable header simply yields no pin —
// which surfaces as UNKNOWN, never as a guessed version.
func parseVendoredPins(root string) (map[string]Pin, error) {
	pins := map[string]Pin{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := filepath.Glob(filepath.Join(root, e.Name(), "*.go"))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			pin, ok, err := parseVendoredFile(f)
			if err != nil {
				return nil, err
			}
			if ok {
				pins[pin.Source] = pin
			}
		}
	}
	return pins, nil
}

func parseVendoredFile(path string) (Pin, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Pin{}, false, err
	}
	var pin Pin
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if m := vendoredSource.FindStringSubmatch(line); m != nil {
			pin.Source = moduleSlug(m[1])
			continue
		}
		if m := vendoredRef.FindStringSubmatch(line); m != nil {
			pin.RawRef = strings.TrimSpace(m[1])
			// Split on commas AND whitespace, so "(tag v0.2.0, date 2024-05-09)"
			// and "(v0.2.0 2024-05-09)" are read as well as "(0.3.8, 2024-08-03)".
			// Each word is classified independently; unrecognised words (labels
			// like "tag", branch names like "HEAD") are simply not used.
			for _, tok := range strings.FieldsFunc(m[1], func(r rune) bool {
				return r == ',' || r == ' ' || r == '\t'
			}) {
				switch {
				case dateShaped(tok):
					pin.Date = normaliseDate(tok)
				case releaseTag.MatchString(tok):
					pin.Release = tok
				}
			}
		}
		if strings.HasPrefix(line, "package ") {
			break // provenance lives in the package doc comment only
		}
	}
	if pin.Source == "" {
		return Pin{}, false, nil
	}
	pin.Origin = path
	return pin, true, nil
}

// compareRelease compares two release identifiers numerically, ignoring a
// leading "v". It returns ok=false when either side is not a release
// identifier at all (a branch name like "master", or an ad-hoc tag like
// "initial") — the caller must then fall back to dates or report UNKNOWN,
// never guess.
func compareRelease(a, b string) (int, bool) {
	av, aok := releaseParts(a)
	bv, bok := releaseParts(b)
	if !aok || !bok {
		return 0, false
	}
	for i := 0; i < len(av) || i < len(bv); i++ {
		x, y := 0, 0
		if i < len(av) {
			x = av[i]
		}
		if i < len(bv) {
			y = bv[i]
		}
		if x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// releaseParts splits "v0.23.6" / "0.3.8" into numeric components. Anything
// that is not a dotted numeric version is rejected.
func releaseParts(s string) ([]int, bool) {
	s = strings.TrimSpace(s)
	// A calendar date is not a release. Rejected BEFORE the suffix trim below,
	// which would otherwise reduce "2024-05-09" to the bare year "2024" and
	// rank it above every real tag.
	if dateShaped(s) {
		return nil, false
	}
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return nil, false
	}
	// Drop any pre-release / build suffix, so v0.25.0-rc1 compares EQUAL to
	// v0.25.0. This under-reports rather than over-reports: a pin sitting on a
	// release candidate of the latest tag reads CURRENT, never STALE. Pinned by
	// TestCompareRelease's prerelease cases.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
