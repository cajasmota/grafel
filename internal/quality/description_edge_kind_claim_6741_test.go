package quality

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// #6741 arm 5 — a golden fixture's own description may not claim an edge kind
// its expectations never assert.
//
// THE DEFECT THIS PINS. Five fixtures — csharp-hangfire-mini,
// csharp-quartz-net-mini, java-quartz-mini, python-dramatiq-mini,
// python-rq-mini — carried the identical sentence at expected.json:4:
//
//	"Used by the extraction-quality benchmark to verify PRODUCES/CONSUMES
//	 edge emission for <framework>."
//
// No PRODUCES or CONSUMES relationship kind existed in the vocabulary at all;
// the word named an inert `edge_kind` ENTITY PROPERTY. The sentence was false
// in all five for months, and nothing could notice, because `go test` grades
// fixture SHAPE and `grafel quality` grades the ROWS — neither reads the prose.
// This test is the missing reader.
//
// THE RULE. A description that names PRODUCES or CONSUMES must either
//
//	(a) back it with a must_exist row of that kind in the same fixture, or
//	(b) negate it explicitly, in the machine-checkable form
//	    "no <KIND>" / "not <KIND>" / "never <KIND>" / "unmet <KIND>".
//
// (b) exists because the honest description of the Python fixtures has to say
// PRODUCES out loud — "ENQUEUES, deliberately not PRODUCES, because ADR-0028 §3
// forbids the second pair" — and the acceptance criterion for #6741 forbids
// deleting the sentence to dodge the check. A negation is a claim too: it is
// false the moment the language starts emitting the kind, and the mutant list
// below scores that direction as well.
//
// A must_exist row is required rather than "any row": csharp-hangfire-mini's
// PRODUCES row is nice_to_have and UNMET (its dispatch sites name types the
// fixture does not declare), so under an "any row" rule its description could
// have claimed to verify a PRODUCES edge that never binds — the same class of
// false claim, one step weaker.
//
// WHY THE VOCABULARY IS THESE TWO KINDS AND NOT ALL 112. Widening it to every
// declared relationship kind fails today on two fixtures whose prose is honest:
// express-baseurl-mini names IMPORTS while describing the substrate constant
// fold (a mechanism, not an assertion), and vbnet-mini names REFERENCES inside
// "Deliberately NOT asserted, because these three sources contain none of them"
// — a negation the (b) form cannot see, because the "NOT" is 120 characters
// upstream. Making the check general therefore means either rewording two
// unrelated fixtures or sniffing prose with a proximity window loose enough to
// pass on an accidental "no" elsewhere in the paragraph. Neither is worth it
// here: PRODUCES and CONSUMES are the kinds #6741 is about, CONSUMES is emitted
// by NOTHING in the tree (ADR-0028), and the check applies to every fixture in
// the corpus rather than to a named list of five. It is a rule, not a ledger.
var claimKinds = []string{"PRODUCES", "CONSUMES"}

// negationForms are the phrasings that count as an explicit disclaimer for a
// kind. Matched case-insensitively against the description.
var negationForms = []string{"no %s", "not %s", "never %s", "unmet %s"}

func TestGoldenDescriptionDoesNotClaimUnassertedEdgeKind_6741(t *testing.T) {
	ents, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatal(err)
	}

	// Word-boundary match so a kind named inside a longer token (CONSUMES_API,
	// CONSUMES_QUEUE) is not read as a claim about CONSUMES.
	word := map[string]*regexp.Regexp{}
	for _, k := range claimKinds {
		word[k] = regexp.MustCompile(`\b` + k + `\b(?:_[A-Z]+)?`)
	}

	inspected := 0
	var violations []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(goldenDir, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "expected.json")); err != nil {
			continue
		}
		fx, err := LoadFixture(dir)
		if err != nil {
			t.Fatalf("%s: load: %v", e.Name(), err)
		}
		inspected++

		desc := fx.Description
		lower := strings.ToLower(desc)
		for _, k := range claimKinds {
			// Only a bare occurrence of the kind counts. CONSUMES_API in a
			// description is a claim about a different kind entirely.
			hits := word[k].FindAllString(desc, -1)
			bare := false
			for _, h := range hits {
				if h == k {
					bare = true
				}
			}
			if !bare {
				continue
			}

			mustExist := false
			for _, r := range fx.ExpectedRelationships {
				if r.Kind == k && r.MustExist {
					mustExist = true
				}
			}
			if mustExist {
				continue
			}

			negated := false
			for _, form := range negationForms {
				if strings.Contains(lower, strings.ToLower(strings.Replace(form, "%s", k, 1))) {
					negated = true
				}
			}
			if !negated {
				violations = append(violations, e.Name()+": description names "+k+
					" but the fixture asserts no must_exist "+k+" row and the description does not negate it")
			}
		}
	}

	// The corpus size is pinned so a fixture cannot escape the check by being
	// added in a form the walk skips. Counting fixtures INSPECTED (after the
	// load) rather than directories seen makes the pin defend itself.
	if inspected < 26 {
		t.Fatalf("inspected %d fixtures, expected at least 26 — the walk is skipping fixtures", inspected)
	}
	if len(violations) > 0 {
		t.Fatalf("golden description claims an edge kind the fixture does not assert:\n  %s",
			strings.Join(violations, "\n  "))
	}
}
