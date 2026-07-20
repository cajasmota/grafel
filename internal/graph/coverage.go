// Package graph — coverage.go implements the test-coverage graph algorithm.
//
// ComputeCoverage walks all TESTS and CALLS relationships in a graph.Document
// to build a CoverageReport that answers three questions:
//
//  1. Which production entities have at least one incoming TESTS edge?
//     (covered = true)
//  2. Which production entities are untested (no TESTS inbound)?
//     (untested list, severity-ranked)
//  3. What is the coverage percentage per directory / module?
//
// # Definitions
//
// "Test entity" — an entity whose SourceFile matches a test-file pattern
// (_test.go, .test.ts, .spec.ts, test_*.py, *_test.py, *_spec.rb,
// *Test.java, *Tests.java, *Spec.java, *Test.kt).
//
// "Production entity" — any non-test entity whose kind is in the coverage
// scope: Function, Method, Class, Interface, Struct.
//
// "TESTS edge" — a graph relationship with Kind == "TESTS".  The testmap
// extractor (internal/extractors/cross/testmap) emits these edges directly.
// When none are present, ComputeCoverage falls back to a synthetic pass:
// for every test entity it walks its CALLS edges and emits a virtual TESTS
// link for each call target that is a production entity.
//
// # Severity rules for untested entities
//
//	"high"   — http_endpoint_definition or http_endpoint without TESTS
//	"medium" — exported Function / Method (capitalised name, no leading _)
//	"low"    — everything else in scope
//
// OTel span: graph.ComputeCoverage
// Issue #1323.
package graph

import (
	"path/filepath"
	"sort"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Public types
// ─────────────────────────────────────────────────────────────────────────────

// CoverageReport is the output of ComputeCoverage.
type CoverageReport struct {
	// TotalProduction is the count of in-scope production entities.
	TotalProduction int `json:"total_production"`
	// CoveredProduction is the count of production entities with ≥1 TESTS edge.
	CoveredProduction int `json:"covered_production"`
	// CoveragePct is CoveredProduction/TotalProduction*100 (0 when no
	// production entities exist).
	//
	// NOTE (#4662): this is the REACH-coverage % — production entities a test
	// actually executes (CALLS the handler / executing TESTS edge / endpoint
	// credited via a reached handler). It deliberately does NOT include
	// contract-covered-only endpoints (see ContractCoveredOnly), so the headline
	// figure stays honest about what is structurally exercised by a test.
	CoveragePct float64 `json:"coverage_pct"`

	// ContractCoveredOnly is the count of production endpoints that are NOT
	// reach-covered but ARE contract-covered: an OFFLINE contract spec references
	// the endpoint's route (asserts its shape) without any test executing the
	// handler (#4662). These read as "contract-covered" rather than "untested".
	//
	// This is a SEPARATE, secondary band — it is never added into
	// CoveredProduction / CoveragePct (which remain pure reach-coverage).
	ContractCoveredOnly int `json:"contract_covered_only"`
	// ContractCoveredPct is (CoveredProduction+ContractCoveredOnly)/TotalProduction
	// *100 — the union band the UI renders behind the reach-coverage bar so the
	// gap between "executed by a test" and "shape-asserted offline" is visible.
	ContractCoveredPct float64 `json:"contract_covered_pct"`

	// TotalTests is the number of test entities found.
	TotalTests int `json:"total_tests"`
	// TotalTestsEdges is the number of TESTS relationship edges (real or
	// synthetic) that contributed to coverage.
	TotalTestsEdges int `json:"total_tests_edges"`

	// UncoveredEntities lists production entities without any TESTS edge,
	// sorted by severity (high → medium → low) then by file+name.
	UncoveredEntities []UncoveredEntity `json:"uncovered_entities"`

	// ByDirectory contains per-directory coverage statistics, sorted by
	// directory path. Only directories with ≥1 production entity are included.
	ByDirectory []DirCoverage `json:"by_directory"`

	// ByFile contains per-file coverage statistics, sorted by file path. Only
	// files with ≥1 production entity are included. Directory rollups in
	// ByDirectory are sums of the files sharing their directory, so the
	// frontend can nest files under their directory using the path segments.
	ByFile []FileCoverage `json:"by_file"`

	// ByModule contains per-module (Properties["module"]) coverage statistics.
	// Only populated when entities carry the "module" property.
	ByModule []ModuleCoverage `json:"by_module"`

	// EntitiesScanned is the total number of entities examined.
	EntitiesScanned int `json:"entities_scanned"`
	// RelationshipsScanned is the total number of relationships examined.
	RelationshipsScanned int `json:"relationships_scanned"`
}

// UncoveredEntity is one production entity that has no TESTS inbound.
type UncoveredEntity struct {
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	SourceFile string `json:"source_file"`
	StartLine  int    `json:"start_line"`
	Language   string `json:"language"`
	Module     string `json:"module,omitempty"`
	// Repo is the slug of the owning repository. ComputeCoverage runs on a
	// single document and leaves this empty; the group-level aggregator stamps
	// it so the UI can resolve source through the correct repo root in a
	// multi-repo group (#4551). Without it, a source-peek ref like
	// "src/app.controller.ts:9" cannot be mapped to the repo it lives in.
	Repo string `json:"repo,omitempty"`
	// Severity is "high" | "medium" | "low".
	Severity string `json:"severity"`
	// State is the coverage state of this entity (#4662). For entities in this
	// (not-reach-covered) list it is either:
	//
	//	"contract-only" — an offline contract spec asserts the endpoint's shape
	//	                  but no test executes the handler. NOT dangerously
	//	                  untested; severity is downgraded accordingly.
	//	"uncovered"     — neither reach-covered nor contract-covered.
	//
	// (Reach-covered entities never appear in this list, so the value is never
	// "reach" here; the three-state vocabulary is reach | contract-only |
	// uncovered, and "reach" lives on the covered side of the report.)
	State string `json:"state"`
}

// Coverage-state string constants (#4662). The full three-state vocabulary is
// reach | contract-only | uncovered; only the latter two appear in
// UncoveredEntity.State (reach-covered entities are not listed as uncovered).
const (
	CoverageStateReach        = "reach"
	CoverageStateContractOnly = "contract-only"
	CoverageStateUncovered    = "uncovered"
)

// DirCoverage is per-directory coverage statistics.
type DirCoverage struct {
	Dir     string `json:"dir"`
	Total   int    `json:"total"`
	Covered int    `json:"covered"`
	// ContractOnly is the count of entities in this directory that are
	// contract-covered-only (shape-asserted offline, not reach-covered) (#4662).
	ContractOnly int     `json:"contract_only"`
	CoveragePct  float64 `json:"coverage_pct"`
}

// FileCoverage is per-file coverage statistics. File is the full source path
// (forward-slash normalised); Dir is its parent directory (matching the keys
// in ByDirectory) so the frontend can nest files under directories.
type FileCoverage struct {
	File    string `json:"file"`
	Dir     string `json:"dir"`
	Total   int    `json:"total"`
	Covered int    `json:"covered"`
	// ContractOnly is the count of entities in this file that are
	// contract-covered-only (shape-asserted offline, not reach-covered) (#4662).
	ContractOnly int     `json:"contract_only"`
	CoveragePct  float64 `json:"coverage_pct"`
}

// ModuleCoverage is per-module coverage statistics.
type ModuleCoverage struct {
	Module      string  `json:"module"`
	Total       int     `json:"total"`
	Covered     int     `json:"covered"`
	CoveragePct float64 `json:"coverage_pct"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Kind / file helpers
// ─────────────────────────────────────────────────────────────────────────────

// coverageEntityKinds is the set of entity kinds that count as "testable
// production entities" for coverage purposes — behaviour-bearing code that a
// unit/integration test could reasonably exercise.
//
// "SCOPE.Operation" is the canonical kind emitted by every language extractor
// for functions and methods (Go, Python, JS/TS, Java, Rust, Ruby, …). The
// bare "Function" / "Method" keys are kept for forward compatibility with any
// third-party or future extractors that emit those kinds directly.
//
// NOTE on Interface (#4510): a bare `Interface` declaration is a type-only
// contract with no executable body, so it is NOT testable production code and
// is deliberately excluded from this set. Concrete behaviour-bearing kinds
// (Class, Struct, Function, Method, http endpoints) remain in scope.
var coverageEntityKinds = map[string]bool{
	"SCOPE.Operation":          true, // canonical: all language extractors
	"Function":                 true, // compat / future
	"Method":                   true, // compat / future
	"Class":                    true,
	"Struct":                   true,
	"http_endpoint":            true,
	"http_endpoint_definition": true,
}

// testEntityKinds is the set of entity kinds that count as "test entities"
// for coverage purposes. Only callable kinds (SCOPE.Operation, Function,
// Method) are counted — not every entity that happens to live in a test file
// (imports, constants, class declarations, SCOPE.Pattern wrappers, etc.).
//
// The previous behaviour of counting every entity from a test file inflated
// TotalTests because the Python / JS / Go extractors emit an entity per
// symbol, so a 3-file test suite could report 500+ "test entities" when only
// ~10 test functions exist (issue #1410).
//
// "SCOPE.Operation" is the canonical kind emitted by every language extractor;
// the bare "Function" / "Method" keys are kept for forward compatibility.
var testEntityKinds = map[string]bool{
	"SCOPE.Operation": true, // canonical: all language extractors
	"Function":        true, // compat / future
	"Method":          true, // compat / future
}

// contractSpecPathSegments are slash-normalised path segments that mark a test
// file as an OFFLINE CONTRACT spec — a spec that asserts an endpoint's
// request/response SHAPE against an oracle/fixture but does NOT invoke the
// handler (no CALLS/execution edge). These are the dominant test mechanism on
// contract-first backends (acme-v3: test/contract/*.contract.spec.ts) and the
// reason a large slice of endpoints read "untested" under pure reach-coverage
// even though their shape is asserted (#4662, follow-on to #4671).
var contractSpecPathSegments = []string{
	"/contract/",  // test/contract/… (NestJS, generic)
	"/contracts/", // pluralised variant
	"/pact/",      // Pact consumer/provider contract tests
	"/pacts/",     // pluralised variant
}

// contractSpecNameInfixes are case-insensitive base-name infixes that mark a
// contract spec regardless of directory (`*.contract.spec.ts`, `*.pact.test.ts`,
// `foo.contract.test.js`, …). Matched on the lower-cased base name.
var contractSpecNameInfixes = []string{
	".contract.spec.", ".contract.test.",
	".pact.spec.", ".pact.test.",
	".contract.", // e.g. foo.contract.ts when it is itself a spec dir resident
}

// isContractSpecFile reports whether path is an OFFLINE contract spec file — a
// test file (it must already satisfy isTestFile for the caller's purposes) that
// asserts endpoint shapes against an oracle and does not execute the handler.
//
// Detection is path-/name-driven and framework-agnostic: a `/contract/`-style
// directory segment OR a `*.contract.spec.*` / `*.pact.*` base-name infix. It is
// intentionally conservative — a plain `foo.spec.ts` is NOT a contract spec; the
// "contract"/"pact" marker must be explicit, so we never mistake an executing
// unit/e2e spec for an offline contract spec (#4662).
func isContractSpecFile(path string) bool {
	if path == "" {
		return false
	}
	slashed := "/" + filepath.ToSlash(strings.ToLower(path))
	for _, seg := range contractSpecPathSegments {
		if strings.Contains(slashed, seg) {
			return true
		}
	}
	base := strings.ToLower(filepath.Base(path))
	for _, infix := range contractSpecNameInfixes {
		if strings.Contains(base, infix) {
			return true
		}
	}
	return false
}

// isTestFile returns true when the source file path matches a recognised test
// file convention.
func isTestFile(path string) bool {
	// Path-segment check first: /test/, /tests/, /__tests__/, /spec/
	// Prefix the path with "/" so that a leading path segment like "spec/foo.rb"
	// also matches "/spec/".
	slashed := "/" + filepath.ToSlash(strings.ToLower(path))
	for _, seg := range []string{"/test/", "/tests/", "/__tests__/", "/spec/"} {
		if strings.Contains(slashed, seg) {
			return true
		}
	}

	base := filepath.Base(path)
	lower := strings.ToLower(base)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(lower, ext)

	switch ext {
	case ".go":
		return strings.HasSuffix(stem, "_test")
	case ".py":
		return strings.HasPrefix(stem, "test_") || strings.HasSuffix(stem, "_test")
	case ".ts", ".tsx", ".js", ".jsx":
		return strings.HasSuffix(stem, ".test") ||
			strings.HasSuffix(stem, ".spec") ||
			strings.Contains(lower, ".test.") ||
			strings.Contains(lower, ".spec.")
	case ".rb":
		return strings.HasSuffix(stem, "_spec")
	case ".java":
		return strings.HasSuffix(stem, "test") ||
			strings.HasSuffix(stem, "tests") ||
			strings.HasSuffix(stem, "spec")
	case ".kt":
		return strings.HasSuffix(stem, "test") ||
			strings.HasSuffix(stem, "tests") ||
			strings.HasSuffix(stem, "spec")
	case ".cs":
		return strings.HasSuffix(stem, "test") ||
			strings.HasSuffix(stem, "tests") ||
			strings.HasSuffix(stem, "spec")
	}
	// No match found for the file's naming convention.
	return false
}

// nonTestablePathSegments are normalised path segments that mark a file as
// NON-testable production code. These hold operational glue, schema mutations,
// generated artefacts, and build/config plumbing — none of which a unit or
// integration test meaningfully covers, yet all of which inflated the coverage
// denominator (#4510: `scripts` alone contributed 0/86 on acme-v3).
//
// Matching is on slash-normalised, lower-cased "/seg/" substrings so the
// predicate is language- and layout-agnostic (Go, Python, JS/TS, Java, …).
var nonTestablePathSegments = []string{
	"/scripts/",       // one-off operational/CLI scripts
	"/script/",        // singular variant
	"/migrations/",    // DB schema mutations (Django, Alembic, TypeORM, Rails, …)
	"/migration/",     // singular variant
	"/__generated__/", // codegen output (GraphQL, protobuf, …)
	"/generated/",     // codegen output
	"/.generated/",    // codegen output
	"/gen/",           // codegen output (Go, protobuf, …)
	"/node_modules/",  // vendored deps
	"/vendor/",        // vendored deps (Go, PHP)
	"/dist/",          // build output
	"/build/",         // build output
}

// nonTestableFileSuffixes mark individual files (by base name) that carry no
// testable behaviour: config files, barrel/index re-exports, type-only
// declaration files, and generated stubs. Matched case-insensitively against
// the file base name.
//
// Barrel/index files (`index.ts`, `index.js`, Go `doc.go`) only re-export or
// document; testing them is not meaningful and they padded the denominator.
var nonTestableFileSuffixes = []string{
	".config.ts", ".config.js", ".config.mjs", ".config.cjs",
	".config.json", ".config.yaml", ".config.yml",
	".d.ts",   // TypeScript type-only declarations
	".pb.go",  // protobuf generated Go
	"_pb2.py", // protobuf generated Python
	".g.dart", // generated Dart
	".generated.ts", ".generated.js",
}

// nonTestableBaseNames are exact base names that are non-testable: barrel
// re-export files and package documentation files.
var nonTestableBaseNames = map[string]bool{
	"index.ts":  true, // barrel re-export
	"index.js":  true, // barrel re-export
	"index.tsx": true, // barrel re-export
	"index.jsx": true, // barrel re-export
	"doc.go":    true, // Go package doc, no behaviour
	"mod.rs":    true, // Rust module barrel (re-exports only)
}

// isNonTestableFile returns true when path denotes a file that holds no
// testable production behaviour (scripts, migrations, generated code, config,
// barrel/index re-exports, type-only declarations). See nonTestablePathSegments
// and nonTestableFileSuffixes for the precise rules (#4510).
func isNonTestableFile(path string) bool {
	if path == "" {
		return false
	}
	slashed := "/" + filepath.ToSlash(strings.ToLower(path))
	for _, seg := range nonTestablePathSegments {
		if strings.Contains(slashed, seg) {
			return true
		}
	}
	base := strings.ToLower(filepath.Base(path))
	if nonTestableBaseNames[base] {
		return true
	}
	for _, suf := range nonTestableFileSuffixes {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	return false
}

// nonTestableNameSuffixes flag entities whose role is data-shape or
// cross-cutting annotation rather than behaviour: DTOs, plain data/value
// objects, decorators, and type-only enums. These are excluded from the
// testable denominator even when they live in otherwise-testable files (#4510).
//
// Matching is case-insensitive on the entity Name suffix. The list is kept
// conservative to avoid excluding real services that happen to end in a noun.
var nonTestableNameSuffixes = []string{
	"dto",        // Data Transfer Object
	"dtos",       // pluralised barrel
	"decorator",  // cross-cutting annotation, not behaviour
	"decorators", // pluralised barrel
	"annotation", // cross-cutting annotation, not behaviour
}

// nonTestableKinds are entity kinds that are type-only / data-shape contracts
// with no executable body, so they are never testable production code even when
// they slip into coverageEntityKinds-adjacent territory. These are matched on
// the PERSISTED `Kind` field (round-trips through the flatbuffer read layer via
// load.go fbEntityToGraphEntity), which is reliably set by every extractor —
// unlike `Subtype`, which most extractors leave empty (#4534).
//
// Matching is case-insensitive on the bare kind and on the `SCOPE.<X>` tail, so
// both raw kinds ("Interface", "TypeAlias") and namespaced kinds
// ("SCOPE.Interface") are caught language-agnostically.
var nonTestableKinds = map[string]bool{
	"interface":   true, // type-only contract, no body
	"typealias":   true, // alias, no body
	"type_alias":  true,
	"type":        true, // bare type declaration
	"enum":        true, // value enumeration, no behaviour
	"enummember":  true,
	"enum_member": true,
	"annotation":  true, // decorator/annotation declaration
	"decorator":   true,
}

// kindTail returns the lower-cased, namespace-stripped kind for matching
// against nonTestableKinds: "SCOPE.Interface" → "interface", "Interface" →
// "interface". Operates purely on the persisted Kind field.
func kindTail(kind string) string {
	k := strings.ToLower(kind)
	if i := strings.LastIndex(k, "."); i >= 0 {
		k = k[i+1:]
	}
	return k
}

// isNonTestableEntity returns true when an in-scope-kind entity should still be
// excluded from the testable production denominator because of its role
// (DTO/decorator/type-only) or because it lives in a non-testable file.
//
// This is the single principled definition of "NOT testable production code"
// (#4510/#4534). It is keyed STRICTLY on read-layer-PERSISTED fields — the
// source-file path (isNonTestableFile), the entity Kind, and the entity Name —
// never on `Subtype`. Subtype round-trips through the flatbuffer writer/reader
// but most extractors never populate it with role tags (dto/decorator/
// annotation/enum_member are emitted by ZERO extractors today), so a
// Subtype-keyed exclusion silently no-ops on the live reindexed graph even
// though it passes against an in-memory fixture that hand-sets Subtype (#4534).
// Path/Kind/Name are guaranteed present on the read layer, so the exclusion
// fires identically in-pipeline and live.
func isNonTestableEntity(e *Entity) bool {
	if isNonTestableFile(e.SourceFile) {
		return true
	}
	// Kind-driven exclusions: type-only / data-shape contracts have no
	// executable body. Keyed on the persisted Kind field (not Subtype).
	if nonTestableKinds[kindTail(e.Kind)] {
		return true
	}
	// Name-convention exclusions: DTOs, decorators and annotations whose
	// concrete kind (Class/Struct) would otherwise count. Keyed on the
	// persisted Name field, framework-agnostic.
	lname := strings.ToLower(e.Name)
	for _, suf := range nonTestableNameSuffixes {
		if strings.HasSuffix(lname, suf) {
			return true
		}
	}
	return false
}

// isProductionEntity returns true when e is a TESTABLE production entity in
// scope: a non-test, behaviour-bearing entity (service, controller, repository,
// use-case, function/method with a body, concrete class/struct, http endpoint)
// that is NOT excluded by isNonTestableEntity.
//
// The predicate is the denominator for the coverage percentage. Tightening it
// (#4510) removes scripts, migrations, generated code, config, barrel/index
// files, DTOs, decorators and type-only declarations that previously inflated
// the denominator and dragged coverage down on well-tested repos.
func isProductionEntity(e *Entity) bool {
	return coverageEntityKinds[e.Kind] &&
		!isTestFile(e.SourceFile) &&
		!isNonTestableEntity(e)
}

// isTestEntity returns true when e is a test entity.  Only Function and
// Method entities inside test files are counted — not every symbol extracted
// from a test file (see testEntityKinds for rationale).
//
// EXCEPTION — one-per-file test_suite markers (#4628). Every non-JS test
// framework (pytest, testify/ginkgo/gomega/httptest, junit5, rspec) emits its
// one-suite-per-file node with Kind="SCOPE.Pattern" Subtype="test_suite"
// (the Jest extractor uses Kind="SCOPE.Operation" instead, so JS suites already
// pass the testEntityKinds gate). A SCOPE.Pattern is NOT in testEntityKinds, so
// before this fix those suites were classified as NEITHER production NOR test:
// they were never counted in TotalTests, and — fatally for coverage — their
// CALLS edges to the production handlers they exercise were dropped by the
// phase-2 CALLS attribution (which only walks entities classified as tests).
// That suppressed coverage across the whole Python/Go/Java/Ruby surface (the
// acme-v3 18% symptom): a DRF handler reached only via a pytest suite's CALLS
// edge stayed uncovered, and so did the http_endpoint it backs. A test_suite is
// a single deliberate one-per-file node (not the per-symbol inflation #1410
// guarded against), so counting it is granular-safe.
func isTestEntity(e *Entity) bool {
	if !isTestFile(e.SourceFile) {
		return false
	}
	if testEntityKinds[e.Kind] {
		return true
	}
	// One-per-file suite marker emitted by the non-JS test extractors. The
	// persisted `pattern_type` Property is checked first because it survives
	// the flatbuffer read layer reliably (load.go copies all Properties); the
	// Subtype/Kind checks are kept as compatible fallbacks (#4534). All three
	// are read-layer-persisted, but pattern_type is the signal extractors set
	// most consistently.
	if e.PropLen() > 0 && e.PropGet("pattern_type") == "test_suite" {
		return true
	}
	return e.Subtype == "test_suite" || e.Kind == "test_suite"
}

// entitySeverity classifies the coverage importance of a production entity.
func entitySeverity(e *Entity) string {
	switch e.Kind {
	case "http_endpoint", "http_endpoint_definition":
		return "high"
	case "SCOPE.Operation", "Function", "Method":
		// Exported (capitalised, not leading underscore) → medium.
		if isExported(e.Name) {
			return "medium"
		}
		return "low"
	}
	return "low"
}

// isExported returns true for names that appear exported/public.
func isExported(name string) bool {
	if name == "" {
		return false
	}
	first := rune(name[0])
	return first >= 'A' && first <= 'Z'
}

// dirOf returns the directory portion of a source file path, normalised to
// forward slashes and with a trailing slash stripped.
func dirOf(path string) string {
	d := filepath.ToSlash(filepath.Dir(path))
	if d == "." {
		return ""
	}
	return d
}

// pct computes a percentage clamped to [0,100].
func pct(covered, total int) float64 {
	if total == 0 {
		return 0
	}
	v := 100.0 * float64(covered) / float64(total)
	if v > 100 {
		return 100
	}
	return v
}

// ─────────────────────────────────────────────────────────────────────────────
// Relationship kind constants (shared by all coverage functions)
// ─────────────────────────────────────────────────────────────────────────────

const kindTests = "TESTS"
const kindCalls = "CALLS"
const kindReferences = "REFERENCES"

// handlerEndpointEdgeKinds are the producer-side edge kinds that link a backend
// handler (controller method / view function / Operation) to its
// http_endpoint_definition. The link is recorded in BOTH directions across
// frameworks:
//
//	handler --IMPLEMENTS/SERVES--> definition   (NestJS, DRF/Django, …: FromID = handler)
//	definition --ROUTES_TO/SERVES--> handler    (Spring, Express routers, …: ToID = handler)
//
// Used by creditEndpointsViaHandlers so that an endpoint is credited as covered
// when its backing handler is reached by a test, even though the test (a
// controller spec, MockMvc test, APITestCase, …) targets the handler method —
// not the synthetic endpoint-definition node. See #4553.
var handlerEndpointEdgeKinds = map[string]bool{
	"IMPLEMENTS": true,
	"ROUTES_TO":  true,
	"SERVES":     true,
}

// isEndpointKind reports whether kind denotes an HTTP endpoint definition,
// tolerant of a leading "SCOPE." prefix some extractors emit.
func isEndpointKind(kind string) bool {
	k := kind
	if i := strings.LastIndex(k, "."); i >= 0 {
		k = k[i+1:]
	}
	return strings.EqualFold(k, "http_endpoint_definition") ||
		strings.EqualFold(k, "http_endpoint")
}

// creditEndpointsViaHandlers propagates test coverage from a covered handler to
// the http_endpoint_definition it backs (#4553).
//
// Background: a controller spec / MockMvc test / APITestCase exercises the
// handler METHOD (which gets credited by the TESTS/CALLS/name-affinity phases),
// but the synthetic http_endpoint_definition node it IMPLEMENTS is a separate
// entity that no test points at directly. Without this hop every endpoint reads
// uncovered and dominates the denominator, suppressing the coverage %.
//
// The hop is one level along the handler↔definition edge (IMPLEMENTS/ROUTES_TO/
// SERVES, either direction) and is framework-agnostic — it relies only on the
// edge kinds the endpoint resolver already emits, never on a framework name.
// It mutates covered in place and returns the number of endpoints newly
// credited.
func creditEndpointsViaHandlers(
	entByID map[string]*Entity,
	rels []Relationship,
	prodIDs map[string]bool,
	covered map[string]int,
) int {
	credited := 0
	for i := range rels {
		r := &rels[i]
		if !handlerEndpointEdgeKinds[strings.ToUpper(r.Kind)] {
			continue
		}
		from := entByID[r.FromID]
		to := entByID[r.ToID]
		if from == nil || to == nil {
			continue
		}
		// Determine which endpoint is the endpoint-definition and which is the
		// handler, regardless of edge direction.
		var defID, handlerID string
		switch {
		case isEndpointKind(to.Kind) && !isEndpointKind(from.Kind):
			defID, handlerID = to.ID, from.ID // handler --IMPLEMENTS--> def
		case isEndpointKind(from.Kind) && !isEndpointKind(to.Kind):
			defID, handlerID = from.ID, to.ID // def --ROUTES_TO--> handler
		default:
			continue
		}
		// Only credit endpoints that are in the production denominator and not
		// already covered, and only when their handler is itself covered.
		if !prodIDs[defID] || covered[defID] > 0 {
			continue
		}
		if covered[handlerID] > 0 {
			covered[defID]++
			credited++
		}
	}
	return credited
}

// creditContractRefViaHandlers propagates a contract route-reference one hop
// along the handler↔endpoint edge (IMPLEMENTS/ROUTES_TO/SERVES, either
// direction), so that whichever side a contract spec references — the synthetic
// http_endpoint_definition (via a route-literal match) OR the backing handler
// method (via a describe-label) — the OTHER side is also marked
// contract-referenced. This mirrors creditEndpointsViaHandlers but for the
// contractRef map, and is framework-agnostic (#4662).
//
// Unlike the reach-coverage hop it is unconditional on the partner's reach
// state: contract coverage and reach coverage are orthogonal bands, and the
// final contract-covered-only set subtracts reach-covered entities afterwards.
// It mutates contractRef in place.
func creditContractRefViaHandlers(
	entByID map[string]*Entity,
	rels []Relationship,
	prodIDs map[string]bool,
	contractRef map[string]int,
) {
	for i := range rels {
		r := &rels[i]
		if !handlerEndpointEdgeKinds[strings.ToUpper(r.Kind)] {
			continue
		}
		from := entByID[r.FromID]
		to := entByID[r.ToID]
		if from == nil || to == nil {
			continue
		}
		var defID, handlerID string
		switch {
		case isEndpointKind(to.Kind) && !isEndpointKind(from.Kind):
			defID, handlerID = to.ID, from.ID
		case isEndpointKind(from.Kind) && !isEndpointKind(to.Kind):
			defID, handlerID = from.ID, to.ID
		default:
			continue
		}
		// Propagate a reference from whichever side has one to the other, as
		// long as both sit in the production denominator.
		if !prodIDs[defID] || !prodIDs[handlerID] {
			continue
		}
		if contractRef[handlerID] > 0 && contractRef[defID] == 0 {
			contractRef[defID]++
		}
		if contractRef[defID] > 0 && contractRef[handlerID] == 0 {
			contractRef[handlerID]++
		}
	}
}

// endpointHandlerIDs returns the handler entity IDs that back the
// http_endpoint_definition defID, resolving the handler↔definition edge in
// both directions (IMPLEMENTS/ROUTES_TO/SERVES). Framework-agnostic; used by the
// single-entity coverage path (#4553).
func endpointHandlerIDs(entByID map[string]*Entity, rels []Relationship, defID string) []string {
	var out []string
	for i := range rels {
		r := &rels[i]
		if !handlerEndpointEdgeKinds[strings.ToUpper(r.Kind)] {
			continue
		}
		switch {
		case r.ToID == defID:
			if from := entByID[r.FromID]; from != nil && !isEndpointKind(from.Kind) {
				out = append(out, r.FromID) // handler --IMPLEMENTS--> def
			}
		case r.FromID == defID:
			if to := entByID[r.ToID]; to != nil && !isEndpointKind(to.Kind) {
				out = append(out, r.ToID) // def --ROUTES_TO--> handler
			}
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Per-entity coverage lookup (#1774)
// ─────────────────────────────────────────────────────────────────────────────

// EntityCoverageResult is the single-entity output of ComputeEntityCoverage.
type EntityCoverageResult struct {
	EntityID         string   `json:"entity_id"`
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	SourceFile       string   `json:"source_file"`
	StartLine        int      `json:"start_line"`
	Severity         string   `json:"severity"`
	Tested           bool     `json:"tested"`
	CoveringTests    []string `json:"covering_tests"`
	CoverageFraction float64  `json:"coverage_fraction"`
	// State is the three-state coverage classification (#4662):
	// "reach" (a test executes it), "contract-only" (an offline contract spec
	// asserts its shape but nothing executes it), or "uncovered".
	State string `json:"state"`
	// ContractCovered is true when a contract spec references this entity's
	// route (shape-asserted) regardless of reach state.
	ContractCovered bool `json:"contract_covered"`
}

// ComputeEntityCoverage returns coverage details for a single entity ID within
// doc. It applies the same two-phase algorithm as ComputeCoverage (real TESTS
// edges, then synthetic fallback via CALLS) but only for the requested entity,
// so it avoids iterating the entire graph for the output rendering step.
//
// Returns (result, true) when the entity is found and is a production entity.
// Returns (nil, false) when the entity ID is not present in the document.
// Returns a result with Tested=false and empty CoveringTests when the entity
// exists but is a test entity or out-of-scope kind.
func ComputeEntityCoverage(doc *Document, entityID string) (*EntityCoverageResult, bool) {
	// ── find entity ────────────────────────────────────────────────────────────
	var target *Entity
	for i := range doc.Entities {
		if doc.Entities[i].ID == entityID {
			target = &doc.Entities[i]
			break
		}
	}
	if target == nil {
		return nil, false
	}

	// ── index all entities needed for the two-phase algorithm ─────────────────
	prodIDs := make(map[string]bool)
	testIDs := make(map[string]bool)
	contractTestIDs := make(map[string]bool) // contract-spec test entities (#4662)
	for i := range doc.Entities {
		e := &doc.Entities[i]
		switch {
		case isProductionEntity(e):
			prodIDs[e.ID] = true
		case isTestEntity(e):
			testIDs[e.ID] = true
			if isContractSpecFile(e.SourceFile) {
				contractTestIDs[e.ID] = true
			}
		}
	}

	result := &EntityCoverageResult{
		EntityID:   entityID,
		Name:       target.Name,
		Kind:       target.Kind,
		SourceFile: target.SourceFile,
		StartLine:  target.StartLine,
		Severity:   entitySeverity(target),
		State:      CoverageStateUncovered,
	}

	if !prodIDs[entityID] {
		// Entity exists but is not a production entity (test entity or out-of-scope).
		// Report it as not applicable — Tested=false, empty covering tests.
		return result, true
	}

	// ── phase 1: collect direct TESTS edges targeting entityID ────────────────
	coveringSet := make(map[string]bool)
	// testCallsTo: sets of production entity IDs each test entity calls.
	testCallsToTarget := make(map[string]bool) // test entity IDs that CALL entityID
	// contractRefSeen: a contract spec references this entity's route without
	// executing it (#4662) — the contract-covered signal, separate from reach.
	contractRefSeen := false

	for i := range doc.Relationships {
		rel := &doc.Relationships[i]
		switch strings.ToUpper(rel.Kind) {
		case kindTests:
			if rel.ToID == entityID && testIDs[rel.FromID] {
				if contractTestIDs[rel.FromID] {
					// Offline contract spec: shape reference, not a reach signal.
					contractRefSeen = true
					continue
				}
				coveringSet[rel.FromID] = true
			}
		case kindReferences:
			if rel.ToID == entityID && contractTestIDs[rel.FromID] {
				contractRefSeen = true
			}
		case kindCalls:
			if rel.ToID == entityID && testIDs[rel.FromID] {
				testCallsToTarget[rel.FromID] = true
			}
		}
	}

	// ── phase 2: synthetic TESTS from CALLS (only when no direct TESTS exist) ──
	if len(coveringSet) == 0 {
		for testID := range testCallsToTarget {
			coveringSet[testID] = true
		}
	}

	// ── phase 3: endpoint crediting via handler (#4553) ───────────────────────
	// When the target is an http_endpoint_definition that no test points at
	// directly, credit it as covered if its backing handler (one hop along
	// IMPLEMENTS/ROUTES_TO/SERVES) is itself reached by a test. Mirrors the
	// graph-wide creditEndpointsViaHandlers phase so single-entity and aggregate
	// queries agree.
	if len(coveringSet) == 0 && isEndpointKind(target.Kind) {
		entByID := make(map[string]*Entity, len(doc.Entities))
		for i := range doc.Entities {
			entByID[doc.Entities[i].ID] = &doc.Entities[i]
		}
		for _, hID := range endpointHandlerIDs(entByID, doc.Relationships, entityID) {
			for i := range doc.Relationships {
				rel := &doc.Relationships[i]
				switch strings.ToUpper(rel.Kind) {
				case kindTests:
					if rel.ToID == hID && testIDs[rel.FromID] {
						if contractTestIDs[rel.FromID] {
							contractRefSeen = true
							continue
						}
						coveringSet[rel.FromID] = true
					}
				case kindReferences:
					if rel.ToID == hID && contractTestIDs[rel.FromID] {
						contractRefSeen = true
					}
				case kindCalls:
					if rel.ToID == hID && testIDs[rel.FromID] {
						coveringSet[rel.FromID] = true
					}
				}
			}
		}
	}

	// ── build sorted covering-tests slice ─────────────────────────────────────
	coveringTests := make([]string, 0, len(coveringSet))
	for id := range coveringSet {
		coveringTests = append(coveringTests, id)
	}
	sort.Strings(coveringTests)

	tested := len(coveringTests) > 0
	fraction := 0.0
	if tested {
		fraction = 1.0
	}

	result.Tested = tested
	result.CoveringTests = coveringTests
	result.CoverageFraction = fraction
	result.ContractCovered = contractRefSeen
	switch {
	case tested:
		result.State = CoverageStateReach
	case contractRefSeen:
		result.State = CoverageStateContractOnly
	default:
		result.State = CoverageStateUncovered
	}
	return result, true
}

// normalizeSubjectToken lowercases a name and strips the common test/subject
// affixes and separators so that a test name and its subject collapse to the
// same token. Examples:
//
//	"TestOrderService"   → "orderservice"
//	"order_service_test" → "orderservice"
//	"OrderServiceSpec"   → "orderservice"
//	"OrderService"       → "orderservice"
func normalizeSubjectToken(name string) string {
	s := strings.ToLower(name)
	// Drop separators.
	s = strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(s)
	// Strip leading test affixes.
	for _, p := range []string{"test", "it", "describe", "should"} {
		s = strings.TrimPrefix(s, p)
	}
	// Strip trailing test/spec affixes (repeat to peel e.g. "spec" then "test").
	for changed := true; changed; {
		changed = false
		for _, suf := range []string{"test", "tests", "spec", "specs", "it"} {
			if len(s) > len(suf) && strings.HasSuffix(s, suf) {
				s = strings.TrimSuffix(s, suf)
				changed = true
			}
		}
	}
	return s
}

// attributeByNameAffinity marks still-uncovered production entities as covered
// when a test entity's normalised name token matches the subject's normalised
// name token (#4510). It mutates covered in place and returns the number of
// subjects newly attributed.
//
// To stay conservative and avoid false attributions:
//   - only subjects with a token length ≥ 4 are eligible (skip tiny/ambiguous
//     names like "do", "run");
//   - a test only attributes subjects that share at least one path segment
//     (same directory subtree) OR an exact full-token match, so an unrelated
//     `OrderService` test in another bounded context does not credit a
//     same-named class elsewhere.
func attributeByNameAffinity(
	entByID map[string]*Entity,
	testIDs, prodIDs map[string]bool,
	covered map[string]int,
) int {
	// Build subject token → []prodID index for uncovered subjects only.
	type subj struct {
		id  string
		dir string
	}
	subjByToken := make(map[string][]subj)
	for id := range prodIDs {
		if covered[id] > 0 {
			continue
		}
		e := entByID[id]
		tok := normalizeSubjectToken(e.Name)
		if len(tok) < 4 {
			continue
		}
		subjByToken[tok] = append(subjByToken[tok], subj{id: id, dir: dirOf(e.SourceFile)})
	}
	if len(subjByToken) == 0 {
		return 0
	}

	attributed := 0
	for tid := range testIDs {
		te := entByID[tid]
		ttok := normalizeSubjectToken(te.Name)
		if len(ttok) < 4 {
			continue
		}
		cands, ok := subjByToken[ttok]
		if !ok {
			continue
		}
		tdir := dirOf(te.SourceFile)
		for _, c := range cands {
			if covered[c.id] > 0 {
				continue // already attributed by a previous test
			}
			// Same-subtree affinity: the test and subject share a directory
			// prefix in either direction (tests often sit in a sibling
			// __tests__/ or tests/ dir under the same feature root).
			if !sharesDirSubtree(tdir, c.dir) {
				continue
			}
			covered[c.id]++
			attributed++
		}
	}
	return attributed
}

// sharesDirSubtree returns true when a and b are in the same directory subtree
// (one is a prefix of the other, segment-aligned) or share the same parent.
// Empty directories (repo root) only match each other.
func sharesDirSubtree(a, b string) bool {
	if a == b {
		return true
	}
	if a == "" || b == "" {
		return false
	}
	as := a + "/"
	bs := b + "/"
	return strings.HasPrefix(as, bs) || strings.HasPrefix(bs, as)
}

// ComputeCoverage analyses doc and returns a CoverageReport.
//
// It runs in two phases:
//
//  1. Collect existing TESTS edges (emitted by the testmap extractor or a
//     previous run of ComputeCoverage). Build a covered-entity set.
//
//  2. Synthetic fallback: for test entities with CALLS edges to production
//     entities that are not yet covered, emit a virtual TESTS edge (recorded
//     in the report totals but NOT written back to the document).
//
// The caller is responsible for writing TESTS edges back to the document if
// persistence is desired.
func ComputeCoverage(doc *Document) *CoverageReport {
	report := &CoverageReport{}
	report.EntitiesScanned = len(doc.Entities)
	report.RelationshipsScanned = len(doc.Relationships)

	// ── index entities ────────────────────────────────────────────────────────
	entByID := make(map[string]*Entity, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		entByID[e.ID] = e
	}

	// Classify entities.
	prodIDs := make(map[string]bool)         // production entity IDs
	testIDs := make(map[string]bool)         // test entity IDs (all)
	contractTestIDs := make(map[string]bool) // test entities that live in an OFFLINE contract spec (#4662)

	for id, e := range entByID {
		switch {
		case isProductionEntity(e):
			prodIDs[id] = true
		case isTestEntity(e):
			testIDs[id] = true
			if isContractSpecFile(e.SourceFile) {
				contractTestIDs[id] = true
			}
		}
	}
	report.TotalProduction = len(prodIDs)
	report.TotalTests = len(testIDs)

	// ── phase 1: collect TESTS edges ─────────────────────────────────────────
	// covered maps production-entity-ID → count of REACH-coverage signals
	// (a test that actually executes the entity). To keep the headline %
	// honest (#4671/#4662), a TESTS edge that originates from an OFFLINE
	// contract spec is NOT a reach signal — it is a route REFERENCE (shape
	// assertion) only. Those feed contractRef instead, never covered.
	covered := make(map[string]int, len(prodIDs))
	// contractRef maps production-entity-ID → count of contract-spec route
	// references (TESTS or REFERENCES edges from a contract-spec test entity)
	// with NO execution. Used to compute the contract-covered-only band (#4662).
	contractRef := make(map[string]int, len(prodIDs))
	// testCallsTo: per test-entity-ID, the set of CALLS target IDs.
	testCallsTo := make(map[string][]string)

	for i := range doc.Relationships {
		rel := &doc.Relationships[i]
		switch strings.ToUpper(rel.Kind) {
		case kindTests:
			if !prodIDs[rel.ToID] {
				continue
			}
			if contractTestIDs[rel.FromID] {
				// Offline contract spec asserting the endpoint's shape via a
				// route reference (route-literal / describe('VERB /route') label
				// match). NOT a reach signal — record it as a contract reference.
				contractRef[rel.ToID]++
				continue
			}
			covered[rel.ToID]++
			report.TotalTestsEdges++
		case kindReferences:
			// A REFERENCES edge from a contract-spec test entity to a production
			// endpoint/handler is the explicit route-reference form (some
			// extractors record the route literal as REFERENCES rather than
			// TESTS). It is a shape assertion, never an execution (#4662).
			if prodIDs[rel.ToID] && contractTestIDs[rel.FromID] {
				contractRef[rel.ToID]++
			}
		case kindCalls:
			if testIDs[rel.FromID] {
				testCallsTo[rel.FromID] = append(testCallsTo[rel.FromID], rel.ToID)
			}
		}
	}

	// ── phase 2: synthetic TESTS from CALLS ──────────────────────────────────
	for testID, targets := range testCallsTo {
		_ = testID
		for _, toID := range targets {
			if prodIDs[toID] && covered[toID] == 0 {
				// Virtual TESTS edge: count it but do not add a duplicate.
				covered[toID]++
				report.TotalTestsEdges++
			}
		}
	}

	// ── phase 3: name-affinity attribution (#4510) ────────────────────────────
	// Many tests link (via TESTS/CALLS) to a handler or helper but never reach
	// the testable subject that shares their name — e2e/contract specs are the
	// worst offenders (#4487). Where a test entity's name clearly references a
	// still-uncovered subject (e.g. `TestOrderService` → `OrderService`,
	// `order_service_test` → `OrderService`, `OrderService.spec` → `OrderService`),
	// attribute coverage. This reuses already-extracted test names — it does NOT
	// redo linkage extraction — so it is a cheap, conservative boost.
	if affinity := attributeByNameAffinity(entByID, testIDs, prodIDs, covered); affinity > 0 {
		report.TotalTestsEdges += affinity
	}

	// ── phase 4: endpoint-definition crediting via handler (#4553) ────────────
	// An http_endpoint_definition is a synthetic node that no test points at
	// directly; tests target the backing handler method (controller spec,
	// MockMvc, APITestCase, …). Once handlers are credited by phases 1-3,
	// propagate one hop along IMPLEMENTS/ROUTES_TO/SERVES so a covered handler
	// credits the endpoint it implements. Framework-agnostic. Without this,
	// every endpoint reads uncovered and suppresses the coverage % (the acme-v3
	// symptom: 100% of the uncovered list is http_endpoint_definition).
	if ep := creditEndpointsViaHandlers(entByID, doc.Relationships, prodIDs, covered); ep > 0 {
		report.TotalTestsEdges += ep
	}

	// ── phase 5: contract-covered band (#4662) ───────────────────────────────
	// Propagate contract references one hop along the handler↔endpoint edge so
	// a contract spec that references the handler method credits the endpoint it
	// backs (and vice versa) — mirroring the reach-coverage endpoint hop. This
	// uses the SAME edge kinds (IMPLEMENTS/ROUTES_TO/SERVES) and never touches
	// `covered`, so the reach-coverage % is unaffected.
	creditContractRefViaHandlers(entByID, doc.Relationships, prodIDs, contractRef)

	// contractCoveredOnly: production entities that are NOT reach-covered but
	// DO carry a contract route-reference. This is the honest second band — the
	// endpoint's shape is asserted offline though no test executes it.
	contractCoveredOnly := make(map[string]bool)
	for id := range contractRef {
		if covered[id] == 0 && prodIDs[id] {
			contractCoveredOnly[id] = true
		}
	}

	// ── compute totals ────────────────────────────────────────────────────────
	report.CoveredProduction = len(covered)
	report.CoveragePct = pct(report.CoveredProduction, report.TotalProduction)
	report.ContractCoveredOnly = len(contractCoveredOnly)
	report.ContractCoveredPct = pct(report.CoveredProduction+report.ContractCoveredOnly, report.TotalProduction)

	// ── build uncovered list ──────────────────────────────────────────────────
	for id := range prodIDs {
		if covered[id] > 0 {
			continue
		}
		e := entByID[id]
		mod := ""
		if e.PropLen() > 0 {
			mod = e.PropGet("module")
		}
		// State + severity: a contract-covered-only entity is shape-asserted
		// offline, so it is NOT dangerously untested — downgrade a "high"
		// (endpoint) severity to "medium" and tag the state so the UI renders it
		// in the contract band rather than the alarming uncovered band (#4662).
		sev := entitySeverity(e)
		state := CoverageStateUncovered
		if contractCoveredOnly[id] {
			state = CoverageStateContractOnly
			if sev == "high" {
				sev = "medium"
			}
		}
		report.UncoveredEntities = append(report.UncoveredEntities, UncoveredEntity{
			EntityID:   id,
			Name:       e.Name,
			Kind:       e.Kind,
			SourceFile: e.SourceFile,
			StartLine:  e.StartLine,
			Language:   e.Language,
			Module:     mod,
			Severity:   sev,
			State:      state,
		})
	}

	// Sort: severity (high < medium < low) then file then name.
	severityOrder := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(report.UncoveredEntities, func(i, j int) bool {
		si := severityOrder[report.UncoveredEntities[i].Severity]
		sj := severityOrder[report.UncoveredEntities[j].Severity]
		if si != sj {
			return si < sj
		}
		if report.UncoveredEntities[i].SourceFile != report.UncoveredEntities[j].SourceFile {
			return report.UncoveredEntities[i].SourceFile < report.UncoveredEntities[j].SourceFile
		}
		return report.UncoveredEntities[i].Name < report.UncoveredEntities[j].Name
	})

	// ── per-directory & per-file breakdown ────────────────────────────────────
	// Files are the deepest grouping; directory rollups are the sums of the
	// files that live under them.
	type covStat struct{ total, covered, contractOnly int }
	dirStats := make(map[string]*covStat)
	type fileStat struct {
		dir                          string
		total, covered, contractOnly int
	}
	fileStats := make(map[string]*fileStat)

	for id, e := range entByID {
		if !prodIDs[id] {
			continue
		}
		d := dirOf(e.SourceFile)
		if _, ok := dirStats[d]; !ok {
			dirStats[d] = &covStat{}
		}
		dirStats[d].total++

		f := filepath.ToSlash(e.SourceFile)
		if _, ok := fileStats[f]; !ok {
			fileStats[f] = &fileStat{dir: d}
		}
		fileStats[f].total++

		switch {
		case covered[id] > 0:
			dirStats[d].covered++
			fileStats[f].covered++
		case contractCoveredOnly[id]:
			dirStats[d].contractOnly++
			fileStats[f].contractOnly++
		}
	}

	for d, s := range dirStats {
		report.ByDirectory = append(report.ByDirectory, DirCoverage{
			Dir:          d,
			Total:        s.total,
			Covered:      s.covered,
			ContractOnly: s.contractOnly,
			CoveragePct:  pct(s.covered, s.total),
		})
	}
	sort.Slice(report.ByDirectory, func(i, j int) bool {
		return report.ByDirectory[i].Dir < report.ByDirectory[j].Dir
	})

	for f, s := range fileStats {
		report.ByFile = append(report.ByFile, FileCoverage{
			File:         f,
			Dir:          s.dir,
			Total:        s.total,
			Covered:      s.covered,
			ContractOnly: s.contractOnly,
			CoveragePct:  pct(s.covered, s.total),
		})
	}
	sort.Slice(report.ByFile, func(i, j int) bool {
		return report.ByFile[i].File < report.ByFile[j].File
	})

	// ── per-module breakdown ──────────────────────────────────────────────────
	type modStat struct{ total, covered int }
	modStats := make(map[string]*modStat)

	for id, e := range entByID {
		if !prodIDs[id] {
			continue
		}
		mod := ""
		if e.PropLen() > 0 {
			mod = e.PropGet("module")
		}
		if mod == "" {
			continue // skip entities without a module tag
		}
		if _, ok := modStats[mod]; !ok {
			modStats[mod] = &modStat{}
		}
		modStats[mod].total++
		if covered[id] > 0 {
			modStats[mod].covered++
		}
	}

	for m, s := range modStats {
		report.ByModule = append(report.ByModule, ModuleCoverage{
			Module:      m,
			Total:       s.total,
			Covered:     s.covered,
			CoveragePct: pct(s.covered, s.total),
		})
	}
	sort.Slice(report.ByModule, func(i, j int) bool {
		return report.ByModule[i].Module < report.ByModule[j].Module
	})

	return report
}
