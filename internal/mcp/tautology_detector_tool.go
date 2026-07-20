// grafel_contract_test_effectiveness MCP tool (#4893, epic #4419).
//
// Flags test/spec entities whose assertions are ORACLE-BLIND / tautological and
// therefore FALSE-GREEN the parity gate: a green spec is taken as a witness that
// the endpoint behaves, but a spec that asserts a value against ITSELF, asserts
// a constant true, or codifies the WRONG expected SHAPE as a literal can never
// fail — it certifies nothing. A real miss this caught: a status_counts dict→
// array shape drift PASSED because the spec asserted the (wrong) array shape
// against the handler's array output, an assertion that could never fail.
//
// The detector is the sibling of stub_detector (#4425) on the spec side: where
// stub_detector asks "does the v3 handler COMPUTE?", this asks "does the v3 SPEC
// actually CHECK?". It is single-group (the spec/v3 group), unlike the cross-
// group diff tools — a tautological assertion is visible from the spec alone.
//
// Signature:
//
//	contract_test_effectiveness(
//	  group:       "<group>",                (optional; resolved like other tools)
//	  cwd:         "<dir>",                   (optional; resolves group)
//	  repo_filter: ["repoA", ...],            (optional)
//	  entity_id:   "<prefixed or label>",     (optional; one spec only)
//	  only_ineffective: true,                 (optional; default true — omit
//	                                           effective specs to save tokens)
//	)
//
// Result (per analysed spec):
//
//	{
//	  "spec":          "test/orders.spec.ts::should_return_counts",
//	  "entity_id":     "orders-v3::abcd1234",
//	  "file":          "test/orders.spec.ts",
//	  "language":      "typescript",
//	  "verdict":       "ineffective"|"effective"|"unknown",
//	  "findings": [ {"reason":"self_compare","line":42,"snippet":"...","detail":"..."} ],
//	  "no_golden_linkage": false
//	}
//
// # What it detects (from the spec's own source)
//
//   - self_compare  — both sides of an equality assertion are the SAME
//     expression: expect(x).toEqual(x), assertEquals(x, x),
//     assertBodyContract(body, body).
//   - constant_true — expect(true).toBe(true), assert True, assertTrue(true).
//   - same_literal  — expected and actual are the SAME literal:
//     expect("ok").toBe("ok"), assertEquals(200, 200).
//   - no_golden_linkage (advisory, low confidence) — the spec body never
//     references the symbol(s) it claims to test (its inbound-TESTS targets) nor
//     the route it covers. Surfaced separately; never on its own ⇒ ineffective.
//
// JS/TS (Jest/vitest — the live NestJS spec style) is first-class; the common
// patterns are generalised across pytest, Go testing/testify, JUnit/AssertJ and
// RSpec via internal/tautology's per-language vocabularies. The pure detection +
// per-language vocabularies live in internal/tautology, unit-tested independently.
package mcp

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/substrate"
	"github.com/cajasmota/grafel/internal/tautology"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// tautologyMaxSpecSourceLines bounds the source window read per spec so a
// pathological entity span can never become a whole-file dump.
const tautologyMaxSpecSourceLines = 600

// handleContractTestEffectiveness implements grafel_contract_test_effectiveness.
func (s *Server) handleContractTestEffectiveness(_ context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	_, lg, errRes := s.resolveAndGroup(req)
	if errRes != nil {
		return errRes, nil
	}
	repos := reposToConsider(lg, argStringSlice(req, "repo_filter"))

	onlyIneffective := true
	if v := strings.TrimSpace(argString(req, "only_ineffective", "")); v == "false" || v == "0" {
		onlyIneffective = false
	}
	entityFilter := strings.TrimSpace(argString(req, "entity_id", ""))

	type specRec struct {
		spec     string
		entityID string
		file     string
		language string
		res      tautology.Result
	}

	out := make([]specRec, 0)
	analysed := 0
	ineffective := 0

	for _, r := range repos {
		if r == nil || r.Doc == nil {
			continue
		}
		linkage := buildTestLinkageTerms(r)
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if !isTautologyCandidate(e) {
				continue
			}
			pid := prefixedID(r.Repo, e.ID)
			if entityFilter != "" && entityFilter != pid && entityFilter != e.Name && entityFilter != e.QualifiedName {
				continue
			}

			src, lang := readTautologySource(r, e)
			if src == "" {
				continue
			}

			res := tautology.Analyze(tautology.Input{
				Language:     lang,
				Source:       src,
				StartLine:    specStartLine(e),
				LinkageTerms: linkage[e.ID],
			})
			if !res.Supported {
				// Unsupported language — skip silently (honest-partial); these add
				// no signal and would just be noise.
				continue
			}
			tautology.SortFindings(res.Findings)
			analysed++
			if res.Verdict == tautology.VerdictIneffective {
				ineffective++
			}
			if onlyIneffective && res.Verdict != tautology.VerdictIneffective && !res.NoGoldenLinkage {
				continue
			}

			out = append(out, specRec{
				spec:     specLabel(e),
				entityID: pid,
				file:     e.SourceFile,
				language: lang,
				res:      res,
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		// ineffective first, then by spec label.
		ii := out[i].res.Verdict == tautology.VerdictIneffective
		jj := out[j].res.Verdict == tautology.VerdictIneffective
		if ii != jj {
			return ii
		}
		return out[i].spec < out[j].spec
	})

	specs := make([]map[string]any, 0, len(out))
	for _, rec := range out {
		findings := make([]map[string]any, 0, len(rec.res.Findings))
		for _, f := range rec.res.Findings {
			findings = append(findings, map[string]any{
				"reason":  string(f.Reason),
				"line":    f.Line,
				"snippet": f.Snippet,
				"detail":  f.Detail,
			})
		}
		specs = append(specs, map[string]any{
			"spec":              rec.spec,
			"entity_id":         rec.entityID,
			"file":              rec.file,
			"language":          rec.language,
			"verdict":           string(rec.res.Verdict),
			"findings":          findings,
			"no_golden_linkage": rec.res.NoGoldenLinkage,
		})
	}

	return jsonResult(map[string]any{
		"group":             lg.Name,
		"analysed_specs":    analysed,
		"ineffective_specs": ineffective,
		"specs":             specs,
		"detects":           "self_compare | constant_true | same_literal (tautological assertions) + no_golden_linkage advisory",
		"source":            "per-spec assertion scan over the test entity's file+line span; per-language vocabulary (JS/TS Jest/vitest first-class; pytest/Go/JUnit/RSpec common patterns)",
	}), nil
}

// isTautologyCandidate reports whether an entity is a test/spec function worth
// scanning: a callable operation living in a recognised test file. Mirrors the
// dead-code test-file convention (isTestFileMCP) + the operation-kind predicate.
func isTautologyCandidate(e *graph.Entity) bool {
	if e == nil || !isOperationKind(e) {
		return false
	}
	return isTestFileMCP(e.SourceFile)
}

// readTautologySource reads the spec entity's source window from disk and
// resolves its language. Returns ("", "") when the span is degenerate or the
// file is unreadable.
func readTautologySource(r *LoadedRepo, e *graph.Entity) (string, string) {
	start := specStartLine(e)
	if start <= 0 {
		return "", ""
	}
	end := e.EndLine
	if end < start {
		end = start + 40 // degenerate span: read a small window
	}
	if end-start+1 > tautologyMaxSpecSourceLines {
		end = start + tautologyMaxSpecSourceLines - 1
	}
	abs := e.SourceFile
	if !filepath.IsAbs(abs) && r.Path != "" {
		abs = filepath.Join(r.Path, e.SourceFile)
	}
	src, err := readRawSourceWindow(abs, start, end)
	if err != nil || strings.TrimSpace(src) == "" {
		return "", ""
	}
	lang := strings.ToLower(strings.TrimSpace(e.Language))
	if lang == "" {
		lang = substrate.LanguageForPath(e.SourceFile)
	}
	return src, lang
}

// specStartLine returns the 1-based start line of a spec entity, clamped.
func specStartLine(e *graph.Entity) int {
	if e.StartLine < 1 {
		return 0
	}
	return e.StartLine
}

// specLabel renders a stable human label for a spec: <file-base>::<name>.
func specLabel(e *graph.Entity) string {
	base := e.SourceFile
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	name := e.Name
	if name == "" {
		name = e.QualifiedName
	}
	if base == "" {
		return name
	}
	return base + "::" + name
}

// buildTestLinkageTerms collects, per test entity ID, the case-insensitive terms
// a genuine spec should reference: the NAMES of the entities it points at over
// outbound TESTS edges, plus any route path / verb those targets expose. Used to
// power the no_golden_linkage advisory. A test with no TESTS edges gets no terms
// (advisory disabled) so we never flag a spec we cannot link.
func buildTestLinkageTerms(r *LoadedRepo) map[string][]string {
	byID := r.getByID()
	terms := map[string][]string{}
	for i := range r.Doc.Relationships {
		rel := &r.Doc.Relationships[i]
		if rel.Kind != "TESTS" {
			continue
		}
		target := byID[rel.ToID]
		if target == nil {
			continue
		}
		set := terms[rel.FromID]
		if target.Name != "" {
			set = appendUniqueStr(set, target.Name)
		}
		if p := target.PropsSnapshot(); p != nil {
			if path := strings.TrimSpace(p["path"]); path != "" {
				set = appendUniqueStr(set, path)
			}
		}
		terms[rel.FromID] = set
	}
	return terms
}
