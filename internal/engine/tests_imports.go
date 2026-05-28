// TESTS edge extraction via test-module imports — #2812.
//
// # Problem
//
// Django/pytest projects exercise production code through symbols imported at
// the top of the test module, e.g.:
//
//	from core.helper.schedule_import_helper import parse_csv_file, resolve_device
//	from core.models import Building, Device
//	...
//	class ResolveDeviceTest(TestCase):
//	    def test_matches_by_name_exact(self):
//	        device, errors = resolve_device("ELV-300", self.group.id)
//
// The per-file testmap extractor (internal/extractors/cross/testmap) resolves a
// bare identifier inside the test body against the *globally-unique* name index
// only, so it misses:
//
//   - production symbols whose bare name is ambiguous across the graph
//     (e.g. Django models like `Building`, which also appear as Constraint /
//     Component entities under several files), and
//   - the broad set of imported helpers a single test exercises (the resolver
//     collapses to the highest-confidence call and a noisy naming-convention
//     fallback that walks up to the wrong ViewSet).
//
// On upvate-core this left TESTS coverage at ~0.6%: every TESTS edge originated
// from a SCOPE.Pattern coverage wrapper (not the test-function entity itself)
// and pointed at a handful of mis-resolved targets, so archigraph_test_coverage
// reported almost no covered production entities.
//
// # Fix
//
// ApplyTestsViaImports is a repo-wide, append-only pass that runs AFTER the
// per-file extractor passes (alongside ApplyTestsMultiHopViaHTTP). For every
// Python test file it:
//
//  1. Parses `from <module> import a, b, c` / `import <module>` statements and
//     builds symbol → fully-qualified-name candidates, resolving re-exports
//     (e.g. `from core.models import Device` → `core.models.device.Device`) by
//     consulting the entity records' qualified names.
//  2. Identifies every test function — top-level `def test_*` and TestCase
//     methods `def test_*` / `def setUp` inside test classes — and captures its
//     body text.
//  3. For each imported symbol whose name appears in a test function body, emits
//     a TESTS edge from that test function to the production entity the symbol
//     resolves to.
//
// The FromID is the test-function structural ref
// (`scope:operation:<test_file>#<method_or_func_name>`) so the resolver binds
// it to the actual test-function Operation entity — making the test function
// count as "linked" in coverage. The ToID is the production symbol's
// fully-qualified name in `scope:operation:?#<qname>` form, which the resolver
// binds via its byQualifiedName index (robust against bare-name ambiguity).
//
// The pass is append-only: it never modifies or removes any entity or
// relationship. It returns only the newly synthesised TESTS edges.
//
// Refs #2812.
package engine

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Regexes
// ---------------------------------------------------------------------------

// pyFromImportRe matches `from <module> import <names>` on a single logical
// line (parenthesised multi-line imports are normalised before matching, see
// flattenContinuedImports). Group 1 = module path, group 2 = imported names.
var pyFromImportRe = regexp.MustCompile(
	`(?m)^[ \t]*from\s+([\w.]+)\s+import\s+(.+)$`,
)

// testImportDefRe matches a Python function/method header and captures its
// leading indentation (group 1) and name (group 2). Used to locate test
// functions for indentation-scoped body capture.
var testImportDefRe = regexp.MustCompile(
	`(?m)^([ \t]*)(?:async\s+)?def\s+(\w+)\s*\(`,
)

// testImportIdentRe matches a bare Python identifier for word-boundary body
// scanning.
var testImportIdentRe = regexp.MustCompile(`[A-Za-z_]\w*`)

// ---------------------------------------------------------------------------
// Imported-symbol resolution index
// ---------------------------------------------------------------------------

// importedSymbolIndex maps qualified names and bare names of production
// entities to a representative qualified name the resolver can bind via its
// byQualifiedName index.
type importedSymbolIndex struct {
	// exactQN is the set of qualified names that exist in the graph.
	exactQN map[string]bool
	// byNameQNs maps a bare entity name → the set of qualified names that own
	// it. Used to resolve re-exported imports (e.g. core.models import Device).
	byNameQNs map[string][]string
}

// buildImportedSymbolIndex scans entity records and records every dotted
// qualified name plus a bare-name → qualified-name multimap. Only entities with
// a dotted qualified name (which Python operations/models carry) are indexed;
// the structural-stub qualified names emitted by some cross-language passes
// (e.g. "scope:ormmodel:...#Device") are skipped because they are not
// resolvable via the byQualifiedName dotted-name path.
func buildImportedSymbolIndex(records []types.EntityRecord) importedSymbolIndex {
	idx := importedSymbolIndex{
		exactQN:   make(map[string]bool),
		byNameQNs: make(map[string][]string),
	}
	for i := range records {
		e := &records[i]
		qn := e.QualifiedName
		// Only dotted qualified names participate (Python module.path.Symbol).
		// Skip structural stubs ("scope:...") and bare names.
		if qn == "" || strings.Contains(qn, ":") || !strings.Contains(qn, ".") {
			continue
		}
		if e.Name == "" {
			continue
		}
		// The qualified name must END with the entity's bare name so that
		// module.path == strings.TrimSuffix(qn, "."+name) holds.
		if !strings.HasSuffix(qn, "."+e.Name) {
			continue
		}
		idx.exactQN[qn] = true
		idx.byNameQNs[e.Name] = append(idx.byNameQNs[e.Name], qn)
	}
	return idx
}

// resolveImport returns the qualified name that the resolver should bind for an
// `from <module> import <symbol>` statement. It tries, in order:
//
//  1. exact <module>.<symbol> (direct import of a symbol defined in module),
//  2. a unique qualified name whose owner module is a sub-package of <module>
//     and whose bare name is <symbol> (re-export through a package __init__,
//     e.g. core.models import Device → core.models.device.Device),
//  3. a globally-unique qualified name for the bare <symbol>.
//
// Returns ("", false) when no unambiguous production entity is found.
func (idx importedSymbolIndex) resolveImport(module, symbol string) (string, bool) {
	// Tier 1: exact module.symbol.
	exact := module + "." + symbol
	if idx.exactQN[exact] {
		return exact, true
	}

	owners := idx.byNameQNs[symbol]
	if len(owners) == 0 {
		return "", false
	}

	// Tier 2: re-export — a single qualified name under the module package.
	prefix := module + "."
	var subMatch string
	subCount := 0
	for _, qn := range owners {
		if strings.HasPrefix(qn, prefix) {
			if subMatch != qn {
				subCount++
			}
			subMatch = qn
		}
	}
	if subCount == 1 {
		return subMatch, true
	}

	// Tier 3: globally-unique bare name (all owners agree on a single qname).
	first := owners[0]
	for _, qn := range owners[1:] {
		if qn != first {
			return "", false
		}
	}
	return first, true
}

// ---------------------------------------------------------------------------
// Import parsing
// ---------------------------------------------------------------------------

// flattenContinuedImports collapses parenthesised multi-line `from x import (
// a, b, c )` blocks onto a single logical line so the single-line regex can
// match the full name list. Backslash continuations are also joined.
func flattenContinuedImports(src string) string {
	// Join backslash-newline continuations first.
	src = strings.ReplaceAll(src, "\\\n", " ")
	// Collapse parenthesised import lists: replace newlines that occur between
	// a "import (" and the matching ")" with spaces. We do a lightweight scan
	// rather than a full parser.
	var b strings.Builder
	b.Grow(len(src))
	depth := 0
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch c {
		case '(':
			depth++
			b.WriteByte(c)
		case ')':
			if depth > 0 {
				depth--
			}
			b.WriteByte(c)
		case '\n':
			if depth > 0 {
				b.WriteByte(' ')
			} else {
				b.WriteByte(c)
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// importedSymbol records a symbol imported into the test module and the
// production qualified name it resolves to.
type importedSymbol struct {
	localName string // name bound in the test module (alias-aware)
	qname     string // resolved production qualified name
}

// parseTestImports extracts the resolvable imported symbols from a (flattened)
// Python source string. Only symbols that resolve to a production entity are
// returned. `import x.y.z` statements are skipped — they bind the top-level
// package name, which is too coarse to attribute to a single entity.
func parseTestImports(flatSrc string, idx importedSymbolIndex) []importedSymbol {
	var out []importedSymbol
	seen := map[string]bool{}

	for _, m := range pyFromImportRe.FindAllStringSubmatch(flatSrc, -1) {
		module := m[1]
		names := m[2]
		// Strip a trailing comment.
		if hash := strings.IndexByte(names, '#'); hash >= 0 {
			names = names[:hash]
		}
		names = strings.Trim(strings.TrimSpace(names), "()")
		for _, part := range strings.Split(names, ",") {
			part = strings.TrimSpace(part)
			if part == "" || part == "*" {
				continue
			}
			// Handle `symbol as alias`.
			local := part
			symbol := part
			if asIdx := strings.Index(part, " as "); asIdx >= 0 {
				symbol = strings.TrimSpace(part[:asIdx])
				local = strings.TrimSpace(part[asIdx+4:])
			}
			if symbol == "" || local == "" {
				continue
			}
			qn, ok := idx.resolveImport(module, symbol)
			if !ok {
				continue
			}
			key := local + "|" + qn
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, importedSymbol{localName: local, qname: qn})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Test-function discovery (top-level funcs + TestCase methods)
// ---------------------------------------------------------------------------

// testFuncSite is a discovered test function and the body text used to scan for
// imported-symbol references.
type testFuncSite struct {
	name string // bare def name (method or function) used in the FromID ref
	body string
}

// isTestFuncName reports whether a def name denotes a test function. Both
// top-level `test_*` and TestCase fixture methods (`setUp`, `setUpClass`) are
// counted: fixtures construct the models/factories under exercise, so linking
// them keeps the model-construction edges attributable to a real test entity.
func isTestFuncName(name string) bool {
	return strings.HasPrefix(name, "test_") || name == "setUp" || name == "setUpClass"
}

// discoverPyTestFuncs returns every test function in the (raw, not flattened)
// source together with its indentation-scoped body. The Python extractor stores
// TestCase methods under their bare method name in the member index, so the
// bare def name is all the FromID ref needs.
func discoverPyTestFuncs(src string) []testFuncSite {
	matches := testImportDefRe.FindAllStringSubmatchIndex(src, -1)
	var out []testFuncSite
	for _, m := range matches {
		indent := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		if !isTestFuncName(name) {
			continue
		}
		body := captureIndentedBlock(src, m[0], len(indent))
		out = append(out, testFuncSite{name: name, body: body})
	}
	return out
}

// captureIndentedBlock returns the lines after the header at headerStart whose
// indentation strictly exceeds headerIndent (blank lines do not terminate a
// Python block). Mirrors the testmap extractor's body capture.
func captureIndentedBlock(src string, headerStart, headerIndent int) string {
	nlIdx := strings.IndexByte(src[headerStart:], '\n')
	if nlIdx < 0 {
		return ""
	}
	bodyStart := headerStart + nlIdx + 1
	lines := strings.Split(src[bodyStart:], "\n")
	var b []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			b = append(b, line)
			continue
		}
		if pyLeadingIndent(line) > headerIndent {
			b = append(b, line)
			continue
		}
		break
	}
	return strings.Join(b, "\n")
}

// pyLeadingIndent counts the leading-whitespace columns of a line (tabs = 8).
func pyLeadingIndent(s string) int {
	w := 0
	for _, r := range s {
		switch r {
		case ' ':
			w++
		case '\t':
			w += 8
		default:
			return w
		}
	}
	return w
}

// ---------------------------------------------------------------------------
// Main pass
// ---------------------------------------------------------------------------

// ApplyTestsViaImports synthesises TESTS edges from Python test functions to the
// production entities they exercise via module imports.
//
// Parameters:
//
//	paths      — repo-relative paths of every file in the index.
//	fileReader — returns raw source bytes for a repo-relative path.
//	records    — all entity records collected so far (pass1+pass2+pass3); used
//	             to resolve imported symbols to qualified names.
//
// Returns only the newly synthesised TESTS RelationshipRecords. The caller
// appends them to its accumulated relationship slice so they flow through the
// resolver (which binds the stub FromID/ToID to entity IDs).
func ApplyTestsViaImports(
	paths []string,
	fileReader NestedURLConfFileReader,
	records []types.EntityRecord,
) []types.RelationshipRecord {
	if fileReader == nil || len(records) == 0 {
		return nil
	}

	idx := buildImportedSymbolIndex(records)
	if len(idx.exactQN) == 0 {
		return nil
	}

	var out []types.RelationshipRecord
	seen := map[string]bool{} // dedup (testFile, testFunc, qname)

	for _, p := range paths {
		if !isPyTestFilePath(p) {
			continue
		}
		content := fileReader(p)
		if len(content) == 0 {
			continue
		}
		src := string(content)

		imports := parseTestImports(flattenContinuedImports(src), idx)
		if len(imports) == 0 {
			continue
		}

		funcs := discoverPyTestFuncs(src)
		if len(funcs) == 0 {
			continue
		}

		for _, tf := range funcs {
			bodyIdents := identSet(tf.body)
			if len(bodyIdents) == 0 {
				continue
			}
			for _, imp := range imports {
				if !bodyIdents[imp.localName] {
					continue
				}
				key := p + "|" + tf.name + "|" + imp.qname
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, types.RelationshipRecord{
					FromID: "scope:operation:" + p + "#" + tf.name,
					ToID:   "scope:operation:?#" + imp.qname,
					Kind:   "TESTS",
					Properties: map[string]string{
						"via":            "import",
						"test_file":      p,
						"test_function":  tf.name,
						"tested":         imp.qname,
						"pattern_type":   "tests_via_import",
						"confidence":     "high",
						"test_framework": "pytest",
					},
				})
			}
		}
	}
	return out
}

// identSet returns the set of bare identifiers present in a body of text.
func identSet(body string) map[string]bool {
	out := map[string]bool{}
	for _, id := range testImportIdentRe.FindAllString(body, -1) {
		out[id] = true
	}
	return out
}

// isPyTestFilePath reports whether p is a Python test file by naming/path
// convention. Mirrors isTestFilePath but constrained to .py.
func isPyTestFilePath(p string) bool {
	lower := strings.ToLower(filepath.ToSlash(p))
	if !strings.HasSuffix(lower, ".py") {
		return false
	}
	slashed := "/" + lower
	if strings.Contains(slashed, "/tests/") || strings.Contains(slashed, "/test/") {
		return true
	}
	base := lower
	if idx := strings.LastIndexByte(lower, '/'); idx >= 0 {
		base = lower[idx+1:]
	}
	return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
}
