package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
)

// #6997 — the subprocess extract path used to dispatch custom extractors under
// `runExtract` alone: no custom-extractor gate, no `TSTree != nil`. The two
// other dispatch sites (cmd/grafel/index.go, internal/extractors/incremental.go)
// carry both. This pins the gate half BEHAVIOURALLY, at this path's own
// entrypoint, so a source-level scan is not the only thing standing between the
// paths and a fresh divergence.
//
// Measured consequence of the fix, macOS/APFS, GRAFEL_SUBPROC_EXTRACT=1 (the
// only way to reach this file in production; default-OFF), against 81f99d691:
// django-realworld 456 -> 371 entities with the gate OFF, and 456 -> 456 with
// GRAFEL_INPROC_CUSTOM_EXTRACTORS=1 — i.e. byte-for-byte the old behaviour when
// the gate is on, and the in-proc path's behaviour when it is not.

// runCustomGateArm runs one extraction of a Django-shaped python file and
// returns the emitted entity identities as "<kind>|<name>".
func runCustomGateArm(t *testing.T, custom *bool) []string {
	t.Helper()
	repo := t.TempDir()
	src := "from django.db import models\n" +
		"\n" +
		"\n" +
		"class Widget(models.Model):\n" +
		"    name = models.CharField(max_length=10)\n"
	if err := os.WriteFile(filepath.Join(repo, "models.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	batch := filepath.Join(t.TempDir(), "batch.txt")
	if err := os.WriteFile(batch, []byte("models.py\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Run(context.Background(), SubprocessOptions{
		RepoRoot:         repo,
		BatchPath:        batch,
		BatchID:          "gate-6997",
		Output:           &buf,
		CustomExtractors: custom,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var got []string
	dec := json.NewDecoder(strings.NewReader(buf.String()))
	for dec.More() {
		var env Envelope
		if err := dec.Decode(&env); err != nil {
			t.Fatalf("decode: %v\n---\n%s", err, buf.String())
		}
		if env.Type == KindEntity && env.Entity != nil {
			got = append(got, string(env.Entity.Kind)+"|"+env.Entity.Name)
		}
	}
	sort.Strings(got)
	return got
}

func TestSubprocCustomExtractorsHonourTheGate6997(t *testing.T) {
	ptr := func(b bool) *bool { return &b }

	cases := []struct {
		name string
		env  string // "" means unset
		cfg  *bool
		want bool // custom entities expected
		why  string
	}{
		{"default: env unset, no programmatic opt-in", "", nil, false,
			"this is what every production caller passes; #6966 keeps the gate default-OFF"},
		{"env opt-in", "1", nil, true,
			"the child inherits os.Environ() from the coordinator, so the env half needs no plumbing"},
		{"programmatic opt-in, env unset", "", ptr(true), true,
			"WithCustomExtractors must survive the process boundary or `grafel quality` " +
				"would measure a different graph on this path than in-process"},
		{"config false BEATS a set env var", "1", ptr(false), false,
			"ExtractorConfig precedence is config-first in BOTH directions (#2320); the " +
				"tri-state pointer is what distinguishes 'config said no' from 'config did not say'"},
	}

	// Baseline: the entities the base python extractor emits with the gate off.
	// Every custom-on arm must be a strict SUPERSET of it — the gate adds, it
	// never replaces, on this path (the merge that can supersede lives in the
	// in-process path, not here).
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	base := runCustomGateArm(t, nil)
	if len(base) == 0 {
		t.Fatal("the base python extractor emitted nothing; the fixture no longer " +
			"exercises anything and every assertion below would be vacuous")
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", tc.env)
			got := runCustomGateArm(t, tc.cfg)

			extra := diffMultiset6997(got, base)
			hasCustom := len(extra) > 0
			if hasCustom != tc.want {
				t.Fatalf("custom entities present = %v, want %v (%s)\n  extra over the "+
					"gate-off baseline: %v\n  full: %v", hasCustom, tc.want, tc.why, extra, got)
			}
			if !tc.want {
				// Grade the negative direction on identity, not just on a count:
				// an arm that swapped one entity for another would keep the count.
				if strings.Join(got, "\n") != strings.Join(base, "\n") {
					t.Fatalf("gate OFF but the output differs from the gate-off baseline:\n  got:  %v\n  base: %v", got, base)
				}
			}
			if len(diffMultiset6997(base, got)) > 0 {
				t.Fatalf("an arm LOST base entities; the custom pass on this path is "+
					"additive by construction (no MergeWithCustom here)\n  missing: %v",
					diffMultiset6997(base, got))
			}
		})
	}
}

// TestSubprocCustomExtractorsRequireAParseTree6997 pins the SECOND guard
// separately from the first, because `A && B` scored only as a unit grades
// neither half, and two guards that only ever fire together grade nothing.
//
// The gate is ON throughout, so the only thing that can suppress output here is
// `file.TSTree != nil`.
//
// THE FIXTURE IS A REAL PRODUCTION SHAPE, not a contrived nil. Nim has a
// registered custom-extractor prefix (custom_nim_) and NO tree-sitter grammar,
// so parser.Parse fails and file.TSTree stays nil for every .nim file in every
// repo. internal/custom/nim/norm_orm.go is content-only — it never touches the
// parse tree — so before this fix the subprocess path emitted its Norm ORM
// schema entities while the in-process and incremental paths emitted none.
// clojure, crystal, dart, erlang and fsharp are in the same position.
//
// This is also why the divergence could not be closed by DELETING the guard
// from the other two sites: content-only extractors emit for files that failed
// to parse, which is exactly the input the indexer decided it could not read.
func TestSubprocCustomExtractorsRequireAParseTree6997(t *testing.T) {
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")

	repo := t.TempDir()
	src := "import norm/model\n" +
		"\n" +
		"type\n" +
		"  User* = ref object of Model\n" +
		"    name*: string\n" +
		"    email*: string\n"
	if err := os.WriteFile(filepath.Join(repo, "models.nim"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	batch := filepath.Join(t.TempDir(), "batch.txt")
	if err := os.WriteFile(batch, []byte("models.nim\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Run(context.Background(), SubprocessOptions{
		RepoRoot: repo, BatchPath: batch, BatchID: "niltree-6997", Output: &buf,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var got []string
	dec := json.NewDecoder(strings.NewReader(buf.String()))
	for dec.More() {
		var env Envelope
		if err := dec.Decode(&env); err != nil {
			t.Fatalf("decode: %v\n---\n%s", err, buf.String())
		}
		if env.Type == KindEntity && env.Entity != nil {
			got = append(got, string(env.Entity.Kind)+"|"+env.Entity.Name+"|"+env.Entity.Subtype)
		}
	}
	sort.Strings(got)

	// POSITIVE CONTROL, so a fixture that stopped exercising anything cannot
	// read as a pass: the same content, dispatched directly, MUST produce the
	// Norm entities. If this ever stops holding, the assertion below is vacuous
	// and the test says so instead of going green.
	direct, _ := extractors.RunCustomExtractors(context.Background(), extractors.FileInput{
		Path: "models.nim", Language: "nim", Content: []byte(src),
	})
	if len(direct) == 0 {
		t.Fatal("positive control failed: RunCustomExtractors emits nothing for this " +
			"nim fixture even with a nil tree, so the assertion below cannot distinguish " +
			"the guard from an inert extractor. Fix the fixture, do not delete the check.")
	}

	for _, g := range got {
		if strings.HasPrefix(g, "SCOPE.Schema|") {
			t.Fatalf("custom-extractor output survived a nil parse tree: %q\n  all: %v\n"+
				"  direct dispatch produces %d entities for the same content",
				g, got, len(direct))
		}
	}
}

// diffMultiset6997 returns the members of a that are not covered by b,
// counting duplicates.
func diffMultiset6997(a, b []string) []string {
	remaining := map[string]int{}
	for _, x := range b {
		remaining[x]++
	}
	var out []string
	for _, x := range a {
		if remaining[x] > 0 {
			remaining[x]--
			continue
		}
		out = append(out, x)
	}
	return out
}

// TestCoordinateForwardsTheProgrammaticCustomGate6997 grades the PROPAGATION,
// which the two tests above cannot: they call Run in-process and hand it the
// option directly, so they would pass just as well if the coordinator never
// forwarded anything and `grafel quality`-style programmatic opt-in silently
// died at the process boundary.
//
// The env half needs no forwarding (children get os.Environ()), so this test
// clears GRAFEL_INPROC_CUSTOM_EXTRACTORS and drives the config half alone,
// end to end: CoordinatorConfig → --custom-extractors → flag parse →
// SubprocessOptions → the gate.
func TestCoordinateForwardsTheProgrammaticCustomGate6997(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (forks a built binary) — skipped in -short mode")
	}
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	bin := buildGrafel(t)

	repo := t.TempDir()
	src := "from django.db import models\n\n\nclass Widget(models.Model):\n" +
		"    name = models.CharField(max_length=10)\n"
	if err := os.WriteFile(filepath.Join(repo, "models.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	count := func(custom *bool) int {
		var stderr bytes.Buffer
		res, err := Coordinate(context.Background(), repo, []string{"models.py"},
			CoordinatorConfig{BinaryPath: bin, Stderr: &stderr, CustomExtractors: custom})
		if err != nil {
			t.Fatalf("Coordinate: %v\n%s", err, stderr.String())
		}
		return len(res.Entities)
	}

	ptr := func(b bool) *bool { return &b }
	off := count(nil)
	on := count(ptr(true))
	if off == 0 {
		t.Fatal("the gate-off arm produced no entities at all; the fixture is not " +
			"exercising the extract path and the comparison below is vacuous")
	}
	if on <= off {
		t.Fatalf("CoordinatorConfig.CustomExtractors=true did not reach the child: "+
			"entities off=%d on=%d (want on > off). The programmatic opt-in has to "+
			"cross the process boundary as --custom-extractors; the env half does not.",
			off, on)
	}

	// The false branch of the forwarding is not decoration: config beats env in
	// BOTH directions, so an explicit false must cross the boundary too and
	// suppress a set env var in the child. Without this arm, deleting the else
	// branch in the coordinator is invisible.
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")
	if envOn, cfgOff := count(nil), count(ptr(false)); cfgOff != off || envOn <= off {
		t.Fatalf("config-false did not cross the process boundary: entities "+
			"env-on=%d cfg-false=%d gate-off baseline=%d (want env-on > baseline and "+
			"cfg-false == baseline)", envOn, cfgOff, off)
	}
}
