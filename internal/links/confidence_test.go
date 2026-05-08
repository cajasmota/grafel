package links

import "testing"

func TestScoreImport_TopOfBand(t *testing.T) {
	got := ScoreImport()
	if got < 0.9 || got > 1.0 {
		t.Errorf("ScoreImport: want in [0.9, 1.0], got %v", got)
	}
}

func TestScoreLabel_BandClamping(t *testing.T) {
	cases := []struct {
		name string
		raw  float64
		// expected band: -1 means "raw passes through unchanged" (below
		// candidate threshold), otherwise the value must sit in
		// [labelBandLow, labelBandHigh].
		passthrough bool
	}{
		{"below_threshold_passes_through", labelCandidateThreshold / 2, true},
		{"at_threshold_lands_at_band_low", labelCandidateThreshold, false},
		{"midband", 0.6, false},
		{"max_raw_lands_at_band_high", 1.0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ScoreLabel(c.raw)
			if c.passthrough {
				if got != c.raw {
					t.Errorf("expected passthrough, got %v from raw %v", got, c.raw)
				}
				return
			}
			if got < labelBandLow-1e-9 || got > labelBandHigh+1e-9 {
				t.Errorf("ScoreLabel(%v) = %v, want in [%v, %v]",
					c.raw, got, labelBandLow, labelBandHigh)
			}
		})
	}
}

func TestScoreString_AllCategoriesInBand(t *testing.T) {
	cats := []extractionCategory{
		catWebhookPath, catHTTPPath, catS3URI, catRedisKey, catKafkaTopic,
		catNATSSubject, catFeatureFlag, catSQSARN, catSQSURL, catSNSARN,
		catLambdaARN, catEventbridgeARN,
	}
	for _, c := range cats {
		got := ScoreString(c)
		if got < stringBandLow-1e-9 || got > stringBandHigh+1e-9 {
			t.Errorf("ScoreString(%s) = %v, want in [%v, %v]",
				c, got, stringBandLow, stringBandHigh)
		}
	}
}

func TestScoreString_AWSResourcesAtTop(t *testing.T) {
	// AWS ARN/URL categories carry an account id + region and should
	// sit at the top of the P3 band.
	awsCats := []extractionCategory{catSQSARN, catSNSARN, catLambdaARN, catEventbridgeARN, catSQSURL}
	for _, c := range awsCats {
		got := ScoreString(c)
		if got < 0.55 {
			t.Errorf("ScoreString(%s) = %v, want ≥ 0.55", c, got)
		}
	}
	// Redis keys are the broadest pattern.
	redis := ScoreString(catRedisKey)
	if redis > 0.4 {
		t.Errorf("ScoreString(redis_key) = %v, want ≤ 0.4", redis)
	}
}

func TestImportPassConfidenceWithinBand(t *testing.T) {
	root := fixtureRoot(t)
	twoRepoGraphs(t, root)
	home := root + "/ag-home"
	res, err := RunAllPasses("g1", root, home)
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalLinks == 0 {
		t.Fatal("expected at least one import link")
	}
	doc, err := readDoc(res.OutLinks)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Links {
		if l.Method == MethodImport && l.Confidence < 0.9 {
			t.Errorf("import confidence %v out of band [0.9, 1.0]", l.Confidence)
		}
	}
}

func TestLabelPassConfidenceWithinBand(t *testing.T) {
	root := fixtureRoot(t)
	mkNoise := func(prefix string, n int) []map[string]any {
		out := []map[string]any{}
		for i := 0; i < n; i++ {
			out = append(out, map[string]any{
				"id": prefix + "n" + itoa(i), "name": prefix + "_unique_" + itoa(i),
				"kind": "function", "source_file": "f.py",
			})
		}
		return out
	}
	a := append(mkNoise("a_", 30), map[string]any{
		"id": "a_special", "name": "PaymentReconciler", "kind": "class", "source_file": "p.py",
	})
	b := append(mkNoise("b_", 30), map[string]any{
		"id": "b_special", "name": "PaymentReconciler", "kind": "class", "source_file": "p.go",
	})
	writeFixture(t, root, fixtureGraph{Repo: "alpha", Entities: a})
	writeFixture(t, root, fixtureGraph{Repo: "beta", Entities: b})
	home := root + "/ag-home"
	if _, err := RunAllPasses("gL", root, home); err != nil {
		t.Fatal(err)
	}
	doc, err := readDoc(home + "/groups/gL-links.json")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range doc.Links {
		if l.Method != MethodLabelMatch {
			continue
		}
		found = true
		if l.Confidence < labelBandLow || l.Confidence > labelBandHigh {
			t.Errorf("label confidence %v out of band [%v, %v]",
				l.Confidence, labelBandLow, labelBandHigh)
		}
	}
	if !found {
		t.Fatal("expected at least one label link in fixture")
	}
}
