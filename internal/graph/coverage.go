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
	CoveragePct float64 `json:"coverage_pct"`

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
	// Severity is "high" | "medium" | "low".
	Severity string `json:"severity"`
}

// DirCoverage is per-directory coverage statistics.
type DirCoverage struct {
	Dir         string  `json:"dir"`
	Total       int     `json:"total"`
	Covered     int     `json:"covered"`
	CoveragePct float64 `json:"coverage_pct"`
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

// coverageEntityKinds is the set of entity kinds that count as "production
// entities" for coverage purposes.
//
// "SCOPE.Operation" is the canonical kind emitted by every language extractor
// for functions and methods (Go, Python, JS/TS, Java, Rust, Ruby, …). The
// bare "Function" / "Method" keys are kept for forward compatibility with any
// third-party or future extractors that emit those kinds directly.
var coverageEntityKinds = map[string]bool{
	"SCOPE.Operation":          true, // canonical: all language extractors
	"Function":                 true, // compat / future
	"Method":                   true, // compat / future
	"Class":                    true,
	"Interface":                true,
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

// isProductionEntity returns true when e is a production entity in scope.
func isProductionEntity(e *Entity) bool {
	return coverageEntityKinds[e.Kind] && !isTestFile(e.SourceFile)
}

// isTestEntity returns true when e is a test entity.  Only Function and
// Method entities inside test files are counted — not every symbol extracted
// from a test file (see testEntityKinds for rationale).
func isTestEntity(e *Entity) bool {
	return isTestFile(e.SourceFile) && testEntityKinds[e.Kind]
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
	for i := range doc.Entities {
		e := &doc.Entities[i]
		switch {
		case isProductionEntity(e):
			prodIDs[e.ID] = true
		case isTestEntity(e):
			testIDs[e.ID] = true
		}
	}

	result := &EntityCoverageResult{
		EntityID:   entityID,
		Name:       target.Name,
		Kind:       target.Kind,
		SourceFile: target.SourceFile,
		StartLine:  target.StartLine,
		Severity:   entitySeverity(target),
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

	for i := range doc.Relationships {
		rel := &doc.Relationships[i]
		switch strings.ToUpper(rel.Kind) {
		case kindTests:
			if rel.ToID == entityID && testIDs[rel.FromID] {
				coveringSet[rel.FromID] = true
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
	return result, true
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
	prodIDs := make(map[string]bool) // production entity IDs
	testIDs := make(map[string]bool) // test entity IDs

	for id, e := range entByID {
		switch {
		case isProductionEntity(e):
			prodIDs[id] = true
		case isTestEntity(e):
			testIDs[id] = true
		}
	}
	report.TotalProduction = len(prodIDs)
	report.TotalTests = len(testIDs)

	// ── phase 1: collect TESTS edges ─────────────────────────────────────────
	// covered maps production-entity-ID → count of TESTS inbound.
	covered := make(map[string]int, len(prodIDs))
	// testCallsTo: per test-entity-ID, the set of CALLS target IDs.
	testCallsTo := make(map[string][]string)

	for i := range doc.Relationships {
		rel := &doc.Relationships[i]
		switch strings.ToUpper(rel.Kind) {
		case kindTests:
			if prodIDs[rel.ToID] {
				covered[rel.ToID]++
				report.TotalTestsEdges++
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

	// ── compute totals ────────────────────────────────────────────────────────
	report.CoveredProduction = len(covered)
	report.CoveragePct = pct(report.CoveredProduction, report.TotalProduction)

	// ── build uncovered list ──────────────────────────────────────────────────
	for id := range prodIDs {
		if covered[id] > 0 {
			continue
		}
		e := entByID[id]
		mod := ""
		if e.Properties != nil {
			mod = e.Properties["module"]
		}
		report.UncoveredEntities = append(report.UncoveredEntities, UncoveredEntity{
			EntityID:   id,
			Name:       e.Name,
			Kind:       e.Kind,
			SourceFile: e.SourceFile,
			StartLine:  e.StartLine,
			Language:   e.Language,
			Module:     mod,
			Severity:   entitySeverity(e),
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

	// ── per-directory breakdown ───────────────────────────────────────────────
	type dirStat struct{ total, covered int }
	dirStats := make(map[string]*dirStat)

	for id, e := range entByID {
		if !prodIDs[id] {
			continue
		}
		d := dirOf(e.SourceFile)
		if _, ok := dirStats[d]; !ok {
			dirStats[d] = &dirStat{}
		}
		dirStats[d].total++
		if covered[id] > 0 {
			dirStats[d].covered++
		}
	}

	for d, s := range dirStats {
		report.ByDirectory = append(report.ByDirectory, DirCoverage{
			Dir:         d,
			Total:       s.total,
			Covered:     s.covered,
			CoveragePct: pct(s.covered, s.total),
		})
	}
	sort.Slice(report.ByDirectory, func(i, j int) bool {
		return report.ByDirectory[i].Dir < report.ByDirectory[j].Dir
	})

	// ── per-module breakdown ──────────────────────────────────────────────────
	type modStat struct{ total, covered int }
	modStats := make(map[string]*modStat)

	for id, e := range entByID {
		if !prodIDs[id] {
			continue
		}
		mod := ""
		if e.Properties != nil {
			mod = e.Properties["module"]
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
