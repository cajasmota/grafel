package quality

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// baselineDoc mirrors internal/quality/golden/baseline.json. Only the fields
// this test gates are modelled; unknown keys are ignored.
type baselineDoc struct {
	Version    int                        `json:"version"`
	Regenerate string                     `json:"regenerate"`
	Fixtures   map[string]baselineFixture `json:"fixtures"`
}

type baselineFixture struct {
	ExpectationsMissing bool `json:"expectations_missing"`
	EntityFound         int  `json:"entity_found"`
	EntityExpected      int  `json:"entity_expected"`
	RelFound            int  `json:"relationship_found"`
	RelExpected         int  `json:"relationship_expected"`
}

const (
	goldenDir    = "golden"
	baselinePath = "golden/baseline.json"
)

// ungradedFixtures is the closed set of fixtures allowed to carry no
// expected.json. Both arrived as resolver test corpora (#1030, #1008) and
// landed under golden/ without expectations, so they are never evaluated.
//
// This list is deliberately in Go rather than derived from baseline.json.
// Without it, a graded fixture can be demoted out of the gate in a single
// commit — delete its expected.json AND flip its baseline entry to
// {"expectations_missing": true} — and both the ratchet and the other tests
// here pass, because each half of the edit justifies the other. That is the
// cheapest possible way to make csharp-hangfire-mini's 0/5 disappear. Adding a
// third name now requires editing this file, which is the point: it makes
// demotion a reviewable act rather than a bookkeeping side effect.
var ungradedFixtures = map[string]bool{
	"groovy-grails-mini": true,
	"swift-swiftui-mini": true,
}

func loadBaseline(t *testing.T) baselineDoc {
	t.Helper()
	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read %s: %v", baselinePath, err)
	}
	var doc baselineDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", baselinePath, err)
	}
	if doc.Version == 0 {
		t.Fatalf("%s: missing version field", baselinePath)
	}
	if doc.Regenerate == "" {
		t.Fatalf("%s: missing regenerate field — the baseline must name the "+
			"command that re-derives it (Refs #6231)", baselinePath)
	}
	return doc
}

func fixtureDirs(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatalf("read %s: %v", goldenDir, err)
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() || e.Name()[0] == '.' {
			continue
		}
		names = append(names, e.Name())
	}
	return names
}

// TestBaselineCoversEveryFixture is the cheap, always-run half of the quality
// ratchet (Refs #6231). The expensive half — actually indexing all twenty
// fixtures — lives in scripts/quality/run.sh --ratchet. This test guards the
// structural invariant that makes that gate meaningful: every fixture
// directory is accounted for in the baseline, so a new fixture cannot be added
// and silently escape gating, and a fixture cannot be deleted to make a red
// number disappear.
func TestBaselineCoversEveryFixture(t *testing.T) {
	doc := loadBaseline(t)
	dirs := fixtureDirs(t)

	seen := make(map[string]bool, len(dirs))
	for _, name := range dirs {
		seen[name] = true
		if _, ok := doc.Fixtures[name]; !ok {
			t.Errorf("fixture %q has no baseline entry — run %q so it is gated",
				name, doc.Regenerate)
		}
	}
	for name := range doc.Fixtures {
		if !seen[name] {
			t.Errorf("baseline records fixture %q but no such directory exists "+
				"under internal/quality/golden/", name)
		}
	}
}

// TestBaselineExpectationsPresenceMatchesDisk pins the two-state distinction
// the issue asked to be split out: a fixture either carries expected.json and
// is graded on recall, or carries none and is recorded as ungraded. Silently
// dropping an expected.json (which would make a red fixture vanish from the
// gate rather than fail it) is a test failure.
func TestBaselineExpectationsPresenceMatchesDisk(t *testing.T) {
	doc := loadBaseline(t)
	for _, name := range fixtureDirs(t) {
		base, ok := doc.Fixtures[name]
		if !ok {
			continue // reported by TestBaselineCoversEveryFixture
		}
		_, statErr := os.Stat(filepath.Join(goldenDir, name, "expected.json"))
		onDisk := statErr == nil
		if onDisk && base.ExpectationsMissing {
			t.Errorf("fixture %q now has expected.json but the baseline records it "+
				"as missing — run %q to start gating it", name, doc.Regenerate)
		}
		if !onDisk && !base.ExpectationsMissing {
			t.Errorf("fixture %q has no expected.json but the baseline expects one — "+
				"the expectations file was deleted", name)
		}
	}
}

// TestUngradedFixturesAreTheKnownTwo closes the demotion escape: a fixture may
// only be ungraded if it is named in ungradedFixtures above. Deleting an
// expected.json and flipping the matching baseline entry in the same commit is
// otherwise self-consistent and passes everything else here.
func TestUngradedFixturesAreTheKnownTwo(t *testing.T) {
	doc := loadBaseline(t)

	for name, base := range doc.Fixtures {
		if base.ExpectationsMissing && !ungradedFixtures[name] {
			t.Errorf("fixture %q is recorded as having no expectations, but it is "+
				"not one of the known ungraded fixtures. A graded fixture cannot be "+
				"demoted out of the recall gate as a bookkeeping change — either "+
				"restore its expected.json, or add it to ungradedFixtures in this "+
				"file and justify that in review", name)
		}
	}

	for name := range ungradedFixtures {
		base, ok := doc.Fixtures[name]
		if !ok {
			continue // reported by TestBaselineCoversEveryFixture
		}
		if !base.ExpectationsMissing {
			t.Errorf("fixture %q now carries expectations — good; drop it from "+
				"ungradedFixtures in this file so it can never silently go back", name)
		}
	}
}

// TestBaselineRecordedCountsAreSane keeps the recorded floor from being edited
// into something vacuous: a fixture cannot claim to have found more must-haves
// than it declares, and a graded fixture must declare at least one must-have.
func TestBaselineRecordedCountsAreSane(t *testing.T) {
	doc := loadBaseline(t)
	for name, base := range doc.Fixtures {
		if base.ExpectationsMissing {
			continue
		}
		if base.EntityFound > base.EntityExpected {
			t.Errorf("fixture %q: entity_found %d exceeds entity_expected %d",
				name, base.EntityFound, base.EntityExpected)
		}
		if base.RelFound > base.RelExpected {
			t.Errorf("fixture %q: relationship_found %d exceeds relationship_expected %d",
				name, base.RelFound, base.RelExpected)
		}
		if base.EntityExpected == 0 && base.RelExpected == 0 {
			t.Errorf("fixture %q declares no must-have entities or relationships — "+
				"it cannot fail, so it is not a gate", name)
		}
	}
}
