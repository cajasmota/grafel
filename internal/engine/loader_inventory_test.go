package engine

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// This file implements the rule-file inventory assertion for #6341.
//
// Before this, a malformed rule YAML vanished silently: LoadAllRulesFromFS
// collected the failure into loadErrors and then discarded it with
// `_ = loadErrors`. No error, no exit code, no failing CI — the rules in that
// file were simply absent from the engine while the index still reported
// healthy. That is exactly how 6 of 31 classifier skip-configs disappeared in
// #6330.
//
// The inventory assertion closes that hole: the number of rule files PRESENT in
// the embed tree must equal the number SUCCESSFULLY PARSED, and any shortfall
// names the offending file.

// minInventoryCandidates is the non-vacuity floor. A test that is satisfied by
// zero files on both sides of the equality proves nothing (see #6387, #6277,
// #6273), so an embed tree that suddenly holds fewer rule files than this is
// itself a failure — an empty embed tree must never read as "all parsed".
//
// 517 = the historic floor from expectedMinRuleFiles; the tree currently holds
// 533 rule-shaped YAML files.
const minInventoryCandidates = expectedMinRuleFiles

// inventoryProblems returns a human-readable list of everything wrong with a
// rule-load report. An empty slice means the inventory balances.
//
// It is a free function rather than inline assertions so that the inventory
// logic itself can be tested against synthetic reports — otherwise a checker
// that always returns "fine" would go unnoticed.
func inventoryProblems(rep RuleLoadReport, minCandidates int) []string {
	var problems []string

	if len(rep.Candidates) < minCandidates {
		problems = append(problems, fmt.Sprintf(
			"inventory is vacuous or the embed tree shrank: found %d rule-shaped YAML files, want >= %d",
			len(rep.Candidates), minCandidates))
	}

	if len(rep.Candidates) != len(rep.Loaded) {
		problems = append(problems, fmt.Sprintf(
			"rule inventory mismatch: %d rule files present in the embed tree, %d successfully parsed (%d lost)",
			len(rep.Candidates), len(rep.Loaded), len(rep.Candidates)-len(rep.Loaded)))
	}

	for _, f := range rep.Failures {
		problems = append(problems, fmt.Sprintf("  silently discarded: %s", f))
	}

	// Belt and braces: a failure list that is empty while files went missing
	// means the loader lost a file without recording why.
	if len(rep.Failures) == 0 && len(rep.Candidates) != len(rep.Loaded) {
		problems = append(problems,
			"  files were lost but the loader recorded no failure for them")
	}

	return problems
}

// TestRuleInventory_EveryEmbeddedRuleFileParses is the inventory assertion over
// the real embedded rules tree. If any rule YAML fails to read or parse, this
// fails loudly and names the file.
func TestRuleInventory_EveryEmbeddedRuleFileParses(t *testing.T) {
	_, rep, err := LoadAllRulesReport()
	if err != nil {
		t.Fatalf("LoadAllRulesReport: %v", err)
	}

	if problems := inventoryProblems(rep, minInventoryCandidates); len(problems) > 0 {
		t.Fatalf("embedded rule inventory does not balance:\n%s", strings.Join(problems, "\n"))
	}

	t.Logf("rule inventory balances: %d/%d rule files parsed", len(rep.Loaded), len(rep.Candidates))
}

// TestRuleInventory_DetectsUnparseableFile proves the assertion fires in the
// "a rule file is unparseable" direction, without touching the real tree.
func TestRuleInventory_DetectsUnparseableFile(t *testing.T) {
	fsys := fstest.MapFS{
		"rules/go/frameworks/good_a.yaml":  {Data: []byte("framework: good_a\nlanguage: go\n")},
		"rules/go/frameworks/good_b.yaml":  {Data: []byte("framework: good_b\nlanguage: go\n")},
		"rules/go/frameworks/broken.yaml":  {Data: []byte("framework: broken\n\tbad: [unclosed\n")},
		"rules/go/_manifest.yaml":          {Data: []byte("ignored: true\n")},
		"rules/go/frameworks/notes.txt":    {Data: []byte("not yaml")},
		"rules/_engine/database_index.yml": {Data: []byte("ignored: true\n")},
	}

	rules, rep, err := LoadAllRulesFromFSReport(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFSReport returned a hard error: %v", err)
	}

	// Runtime tolerance is preserved: the good rules still load.
	if got := len(rules["go"]); got != 2 {
		t.Errorf("tolerance regression: loaded %d go rules, want 2 (the good ones)", got)
	}

	if len(rep.Candidates) != 3 {
		t.Errorf("candidates = %d, want 3 (only <lang>/<subdir>/*.yaml counts): %v",
			len(rep.Candidates), sorted(rep.Candidates))
	}
	if len(rep.Loaded) != 2 {
		t.Errorf("loaded = %d, want 2: %v", len(rep.Loaded), sorted(rep.Loaded))
	}

	if len(rep.Failures) != 1 {
		t.Fatalf("failures = %d, want 1: %v", len(rep.Failures), rep.Failures)
	}
	if !strings.Contains(rep.Failures[0].Path, "broken.yaml") {
		t.Errorf("failure does not name the offending file: %v", rep.Failures[0])
	}

	problems := inventoryProblems(rep, 0)
	if len(problems) == 0 {
		t.Fatal("inventoryProblems reported no problem for a tree with an unparseable rule file")
	}
	if !strings.Contains(strings.Join(problems, "\n"), "broken.yaml") {
		t.Errorf("problem report does not name broken.yaml:\n%s", strings.Join(problems, "\n"))
	}
}

// TestRuleInventory_EmptyTreeIsNotAllParsed proves the assertion fires in the
// other direction: if the inventory logic stops counting — an empty embed tree,
// a walk that matches nothing — 0 == 0 must NOT read as "all parsed".
func TestRuleInventory_EmptyTreeIsNotAllParsed(t *testing.T) {
	fsys := fstest.MapFS{"rules/go/_manifest.yaml": {Data: []byte("ignored: true\n")}}

	_, rep, err := LoadAllRulesFromFSReport(fsys, "rules")
	if err != nil {
		t.Fatalf("LoadAllRulesFromFSReport: %v", err)
	}
	if len(rep.Candidates) != 0 || len(rep.Loaded) != 0 {
		t.Fatalf("expected an empty inventory, got candidates=%d loaded=%d", len(rep.Candidates), len(rep.Loaded))
	}

	// 0 == 0 balances, so equality alone is vacuous — the floor must catch it.
	if problems := inventoryProblems(rep, minInventoryCandidates); len(problems) == 0 {
		t.Fatal("an empty rule tree passed the inventory assertion: the assertion is vacuous")
	}
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
