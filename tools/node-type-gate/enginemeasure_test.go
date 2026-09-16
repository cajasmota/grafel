package main

import (
	"sort"
	"strings"
	"testing"
)

// TestEngineExemptionIsMeasuredNotArgued re-derives the number in
// skipExemptions["internal/engine"] instead of trusting it.
//
// The exemption claims internal/engine is unmappable by construction, and backs
// it with a measurement: map it to the languages it names as CONSTANTS and
// count the failures that produces. #7076 round 3 caught that measurement going
// stale — it was taken at 84 sites and the package had grown to 92 — which is
// the same defect class as a stale baseline row: prose asserting a number
// nothing observes.
//
// So the exemption's number is computed here, from the tree, and the test fails
// if the text does not match. Re-measuring is now the cost of letting it drift.
//
// VARIED: nothing — this is a measurement, not a table. The point is that the
// number in the prose and the number from the tree are the same object.
// HELD CONSTANT: the constant languages (java, kotlin, python), the empty
// baseline, and the real grammars.
func TestEngineExemptionIsMeasuredNotArgued(t *testing.T) {
	const dir = "internal/engine"
	reason, ok := skipExemptions[dir]
	if !ok {
		t.Skip("internal/engine is no longer exempt; delete this test with its row")
	}

	root := modRoot(t)
	surf, err := LoadSurface(root, []string{"./internal/engine/..."})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// The mapping the exemption says would be wrong: the languages the package
	// names as compile-time constants. If the gate DID map it, this is what it
	// would map it to.
	//
	// grammarKeysFor refuses any dir that has a Parse call with a non-constant
	// language, which is precisely WHY internal/engine is unmappable — so the
	// measurement has to drop those bindings first. Dropping them is the
	// counterfactual: "suppose the runtime dispatch did not exist and the
	// package only ever parsed the languages it names".
	var binds []ParseBinding
	dropped := 0
	for _, b := range surf.ParseBindings {
		if b.Dir == dir && b.Key == "" {
			dropped++
			continue
		}
		binds = append(binds, b)
	}
	if dropped == 0 {
		t.Fatal("internal/engine has no non-constant Parse binding any more, so the exemption's premise (it can receive ANY grammar) no longer holds. Re-derive the exemption; the package may now be mappable.")
	}
	for _, key := range []string{"java", "kotlin", "python"} {
		binds = append(binds, ParseBinding{Dir: dir, Key: key, File: dir + "/x.go", Line: 1})
	}
	res := Evaluate(surf.Scan, surf.Registrations, binds, testGrammars(t), emptyBaseline(t))

	names := map[string]bool{}
	sites := 0
	for _, m := range res.Failures {
		if m.Dir != dir {
			continue
		}
		names[m.Lit] = true
		sites++
	}
	if sites == 0 {
		t.Fatal("mapping internal/engine to its constant languages produced ZERO failures, so the exemption's measurement no longer supports it. Either the package changed or the claim was wrong — re-derive the exemption before trusting it.")
	}

	var got []string
	for n := range names {
		got = append(got, n)
	}
	sort.Strings(got)
	t.Logf("mapping %s to java/kotlin/python yields %d failures over %d distinct names: %v",
		dir, sites, len(names), got)

	// The exemption text must quote THESE numbers. A measured claim whose
	// number has drifted is indistinguishable from an argued one.
	for _, want := range []string{
		sprintInt(sites) + " FALSE failures",
		sprintInt(len(names)) + " distinct names",
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("skipExemptions[%q] does not say %q. Measured on this tree: %d failures over %d distinct names. Update the reason.",
				dir, want, sites, len(names))
		}
	}
	// And the sites the gate reports as skipped must be the same population the
	// measurement was taken over, or the two numbers are about different things.
	var skipped int
	for _, sk := range res.Skipped {
		if sk.Dir == dir {
			skipped = sk.Sites
		}
	}
	if skipped != 0 {
		t.Errorf("internal/engine is still reported as skipped (%d sites) even though this test mapped it — the measurement did not take effect", skipped)
	}
}

func sprintInt(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
