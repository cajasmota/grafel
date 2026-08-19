package feedback

import (
	"testing"
)

func TestRunSanityChecks_AllPass(t *testing.T) {
	r := &Report{
		TotalEntities: 100,
		EntitiesByLanguage: map[string]int{
			"go": 100,
		},
		OrphanByKind: map[string]KindStats{
			"function": {Total: 50, OrphanCount: 10, OrphanPct: 20.0},
		},
		Resolution: ResolutionVector{
			ResolvedPct:        70.0,
			ExternalKnownPct:   10.0,
			ExternalUnknownPct: 10.0,
			BugExtractorPct:    5.0,
			BugResolverPct:     4.0,
			DynamicPct:         1.0,
		},
		ResolutionTotal:        100,
		FrameworkFilesDetected: 5,
		FrameworkHits:          map[string]int{"spring": 20},
	}

	results, confidence := runSanityChecks(r)
	if confidence < 80 {
		t.Errorf("expected high confidence, got %d%%", confidence)
	}
	for _, res := range results {
		if !res.Passed {
			t.Errorf("expected check %q to pass, note: %s", res.Name, res.Note)
		}
	}
}

func TestRunSanityChecks_SuppressedByCount(t *testing.T) {
	r := &Report{
		TotalEntities:      30, // below minimum
		EntitiesByLanguage: map[string]int{"go": 30},
		OrphanByKind:       map[string]KindStats{},
		ResolutionTotal:    0,
		FrameworkHits:      map[string]int{},
	}

	results, confidence := runSanityChecks(r)
	_ = confidence

	// The minimum-entity-count check must fail.
	found := false
	for _, res := range results {
		if res.Name == "minimum-entity-count" {
			if res.Passed {
				t.Errorf("minimum-entity-count should FAIL for %d entities", r.TotalEntities)
			}
			found = true
		}
	}
	if !found {
		t.Error("minimum-entity-count check not found in results")
	}
}

func TestRunSanityChecks_OrphanRateAtCeiling(t *testing.T) {
	r := &Report{
		TotalEntities:      200,
		EntitiesByLanguage: map[string]int{"java": 200},
		OrphanByKind: map[string]KindStats{
			"endpoint": {Total: 50, OrphanCount: 50, OrphanPct: 100.0},
		},
		ResolutionTotal: 0,
		FrameworkHits:   map[string]int{},
	}

	results, _ := runSanityChecks(r)
	found := false
	for _, res := range results {
		if res.Name == orphanCheckName("endpoint") {
			if res.Passed {
				t.Error("orphan-rate check should FAIL for 100% orphan rate")
			}
			found = true
		}
	}
	if !found {
		t.Errorf("%s not found in results", orphanCheckName("endpoint"))
	}
}

func TestRunSanityChecks_ResolutionVectorOff(t *testing.T) {
	r := &Report{
		TotalEntities:      200,
		EntitiesByLanguage: map[string]int{"python": 200},
		OrphanByKind:       map[string]KindStats{},
		Resolution: ResolutionVector{
			ResolvedPct:        50.0,
			ExternalKnownPct:   10.0,
			ExternalUnknownPct: 10.0,
			BugExtractorPct:    10.0,
			BugResolverPct:     5.0,
			DynamicPct:         0.0, // sum = 85, not 100
		},
		ResolutionTotal: 100,
		FrameworkHits:   map[string]int{},
	}

	results, _ := runSanityChecks(r)
	found := false
	for _, res := range results {
		if res.Name == "resolution-vector-sums-to-100pct" {
			if res.Passed {
				t.Error("resolution vector check should FAIL when sum != 100")
			}
			found = true
		}
	}
	if !found {
		t.Error("resolution-vector-sums-to-100pct not found in results")
	}
}

func TestRunSanityChecks_FrameworkFilesNoHits(t *testing.T) {
	r := &Report{
		TotalEntities:          200,
		EntitiesByLanguage:     map[string]int{"java": 200},
		OrphanByKind:           map[string]KindStats{},
		ResolutionTotal:        0,
		FrameworkFilesDetected: 10,
		FrameworkHits:          map[string]int{}, // 0 hits despite framework files
	}

	results, _ := runSanityChecks(r)
	found := false
	for _, res := range results {
		if res.Name == "framework-hits-if-detected" {
			if res.Passed {
				t.Error("framework-hits check should FAIL when files detected but hits = 0")
			}
			found = true
		}
	}
	if !found {
		t.Error("framework-hits-if-detected not found in results")
	}
}

// TestRunSanityChecks_ContainerTerminalKindNoFalseFailure is the sanity-check
// parity guard for Fix 4: the orphan-rate check must be evaluated against the
// POST-classification DEFECT orphan set (r.OrphanByKind), not the raw
// pre-classification orphan count. A kind with zero observed semantic
// participation (every instance routed to OrphanTerminalByKind by report.go's
// derived terminality) reports 0% in OrphanByKind and must NOT trip the gate.
func TestRunSanityChecks_ContainerTerminalKindNoFalseFailure(t *testing.T) {
	r := &Report{
		TotalEntities:      200,
		EntitiesByLanguage: map[string]int{"go": 200},
		// Every SCOPE.Component instance was classified as container-terminal
		// (Fix 3), so the DEFECT orphan count/pct is zero even though all 20
		// instances are orphan by the raw semanticOut==0 test.
		OrphanByKind: map[string]KindStats{
			"SCOPE.Component": {Total: 20, OrphanCount: 0, OrphanPct: 0.0},
		},
		OrphanTerminalByKind: map[string]KindStats{
			"SCOPE.Component": {Total: 20, OrphanCount: 20, OrphanPct: 100.0},
		},
		ResolutionTotal: 0,
		FrameworkHits:   map[string]int{},
	}

	results, _ := runSanityChecks(r)
	found := false
	for _, res := range results {
		if res.Name != orphanCheckName("SCOPE.Component") {
			continue
		}
		found = true
		if !res.Passed {
			t.Errorf("%s should PASS for a zero-participation kind (defect pct is 0%%), note: %s",
				res.Name, res.Note)
		}
	}
	if !found {
		t.Fatalf("%s not emitted — the guard would be vacuous", orphanCheckName("SCOPE.Component"))
	}
}

func TestRunSanityChecks_ConfidenceFormula(t *testing.T) {
	// 1 passing check out of 1 total = 100%
	r := &Report{
		TotalEntities:      100,
		EntitiesByLanguage: map[string]int{},
		OrphanByKind:       map[string]KindStats{},
		ResolutionTotal:    0,
		FrameworkHits:      map[string]int{},
	}

	_, confidence := runSanityChecks(r)
	if confidence != 100 {
		t.Errorf("expected 100%% confidence when only check is minimum-entity-count passing, got %d%%", confidence)
	}
}
