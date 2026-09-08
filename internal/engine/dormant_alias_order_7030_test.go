package engine

import (
	"context"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

// #7030 — dormant-bucket consultation order.
//
// dormantBucketAliases used to be a map, and compile() appended each bucket's
// compiled rule sets onto the shared target language key under `range`. Go
// randomises map iteration order, so the order in which those buckets were
// consulted was random per range. Detect's per-file seenEntities lets the FIRST
// rule set that matches an entity claim it, and the emitted `framework`
// property is the bucket directory name — so on a yaml file matched by two
// aliased buckets, the property flipped from run to run on identical input.
//
// The three tests below grade the fix on three independent axes, and each fails
// on an input the others survive:
//
//   - Order: the exact declared sequence. Reordering two entries fails this and
//     nothing else — the completeness test is order-independent by construction.
//   - Completeness: every listed bucket's sets still reach every one of its
//     targets. Making compile() append only the first target (or skip a bucket)
//     while leaving the declaration untouched fails this and nothing else.
//   - Determinism: the observable consequence. This is the one that fails on
//     main; the other two are structural pins that main would pass.

// wantDormantAliasOrder7030 is the exact expected sequence. Written out here
// rather than derived from the production value so that a change to the
// production value is what the test reports.
var wantDormantAliasOrder7030 = []dormantBucketAlias{
	{bucket: "ansible", targets: []string{"yaml"}},
	{bucket: "cicd", targets: []string{"yaml"}},
	{bucket: "docker", targets: []string{"dockerfile", "yaml"}},
	{bucket: "kubernetes", targets: []string{"yaml"}},
}

// TestDormantAliasOrderIsExactAndStable_7030 pins the consultation order as an
// EXACT SEQUENCE, not as a set. "contains these buckets" would pass under the
// map, which is precisely the bug.
func TestDormantAliasOrderIsExactAndStable_7030(t *testing.T) {
	if got, want := len(dormantBucketAliases), len(wantDormantAliasOrder7030); got != want {
		t.Fatalf("dormantBucketAliases has %d entries, want %d: %v",
			got, want, dormantBucketAliases)
	}
	for i, want := range wantDormantAliasOrder7030 {
		got := dormantBucketAliases[i]
		if got.bucket != want.bucket {
			t.Errorf("dormantBucketAliases[%d].bucket = %q, want %q — the order is "+
				"load-bearing (it decides which bucket's `framework` property wins on a "+
				"shared pattern); change it deliberately, not incidentally",
				i, got.bucket, want.bucket)
			continue
		}
		if len(got.targets) != len(want.targets) {
			t.Errorf("alias %q targets = %v, want %v", got.bucket, got.targets, want.targets)
			continue
		}
		for j := range want.targets {
			if got.targets[j] != want.targets[j] {
				t.Errorf("alias %q targets = %v, want %v (target order also decides "+
					"append order on a shared key)", got.bucket, got.targets, want.targets)
				break
			}
		}
	}

	// The ordered slice must still be a function: one entry per bucket.
	seen := map[string]bool{}
	for _, a := range dormantBucketAliases {
		if seen[a.bucket] {
			t.Errorf("bucket %q listed twice — its sets would be appended twice", a.bucket)
		}
		seen[a.bucket] = true
	}

	// html_templates stays unaliased (doc-only frameworks, no engine schema).
	if dormantAliasTargets("html_templates") != nil {
		t.Error("html_templates must not be aliased: its frameworks carry no engine schema")
	}
}

// TestDormantAliasAppendsEveryBucket_7030 is the OTHER direction: an ordered
// slice that silently dropped a bucket, or a compile() that stopped appending
// onto the second target, would sail through an order assertion.
//
// It is deliberately order-INDEPENDENT — it only counts — so it cannot mask the
// order test and the order test cannot mask it.
//
// len(d.compiled[k]) before aliasing equals len(rules[k]): compile() appends
// exactly one compiledRuleSet per FrameworkRule, unconditionally.
func TestDormantAliasAppendsEveryBucket_7030(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules failed: %v", err)
	}
	det := New(rules)
	det.once.Do(det.compile)

	want := map[string]int{}
	for lang, frs := range rules {
		want[lang] = len(frs)
	}
	for _, a := range dormantBucketAliases {
		src := len(rules[a.bucket])
		if src == 0 {
			t.Errorf("dormant bucket %q loaded 0 rule sets — it cannot fire, so listing "+
				"it in dormantBucketAliases is dead weight", a.bucket)
			continue
		}
		for _, target := range a.targets {
			want[target] += src
		}
	}

	targets := map[string]bool{}
	for _, a := range dormantBucketAliases {
		for _, tg := range a.targets {
			targets[tg] = true
		}
	}
	names := make([]string, 0, len(targets))
	for tg := range targets {
		names = append(names, tg)
	}
	sort.Strings(names)

	for _, tg := range names {
		if got := len(det.compiled[tg]); got != want[tg] {
			t.Errorf("compiled[%q] holds %d rule sets, want %d — a bucket's sets are not "+
				"reaching this language key (a dropped or short-circuited alias)",
				tg, got, want[tg])
		}
	}
}

// TestDormantAliasConsultationOrderIsDeterministic_7030 is the behavioural
// grade, and the one that fails on main.
//
// docker/frameworks/docker_compose.yaml#1 and
// kubernetes/frameworks/kubernetes_manifests.yaml#12 carry the byte-identical
// pattern `image:\s+(\S+)` for entity_type Dependency. On a yaml file matched by
// both, whichever bucket is consulted first claims the entity and stamps its own
// bucket name as `framework`.
//
// A single Detect proves NOTHING here: it would report one winner under the bug
// too. `-count=N` is no defence either — it reruns in the same process, and the
// same *Detector compiles once via sync.Once. What breaks the bug is building
// MANY Detectors: Go re-randomises the start offset on every `range` over a map,
// so each fresh compile() drew an independent order. This test therefore
// compiles the real rules independently many times and requires that all runs
// agree AND that the agreed winner is the declared-first bucket.
//
// Verified: on main this fails (observed both `docker` and `kubernetes` across
// the iterations); on this branch every iteration yields `docker`.
func TestDormantAliasConsultationOrderIsDeterministic_7030(t *testing.T) {
	const composeYAML = `services:
  web:
    image: nginx:1.25
`
	const iterations = 200

	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules failed: %v", err)
	}

	// Which aliased bucket SHOULD win is read off the declared order rather than
	// hard-coded, so this test states the consequence of the order and the order
	// test states the order. Both `docker` and `kubernetes` alias onto `yaml` and
	// both carry the shared `image:` pattern.
	winner := ""
	for _, a := range dormantBucketAliases {
		if a.bucket == "docker" || a.bucket == "kubernetes" {
			winner = a.bucket
			break
		}
	}
	if winner == "" {
		t.Fatal("neither docker nor kubernetes is aliased; this test grades nothing")
	}

	observed := map[string]int{}
	for i := 0; i < iterations; i++ {
		det := New(rules)
		res, err := det.Detect(context.Background(), extractor.FileInput{
			Path:     "deploy/stack.yaml",
			Language: "yaml",
			Content:  []byte(composeYAML),
		})
		if err != nil {
			t.Fatalf("Detect failed on iteration %d: %v", i, err)
		}
		e := findEntity(res.Entities, "Dependency", "nginx:1.25")
		if e == nil {
			t.Fatalf("iteration %d: no Dependency entity %q — the shared `image:` pattern "+
				"no longer fires, so this test grades nothing", i, "nginx:1.25")
		}
		observed[e.Properties["framework"]]++
	}

	if len(observed) != 1 {
		t.Fatalf("the `framework` property on an identical input took %d distinct values "+
			"across %d independent compiles (%v) — bucket consultation order is not "+
			"deterministic", len(observed), iterations, observed)
	}
	if observed[winner] != iterations {
		t.Fatalf("framework property = %v across %d compiles, want %q every time "+
			"(the first of docker/kubernetes in the declared alias order)",
			observed, iterations, winner)
	}
}
