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
	// cicd is deliberately out of lexical position — see the declaration.
	{bucket: "cicd", targets: []string{"yaml"}},
	{bucket: "ansible", targets: []string{"yaml"}},
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

// dormantPairCase7030 is one pair of buckets that (a) alias onto the same
// concrete language and (b) carry a pattern that matches the same text, so the
// consultation order alone decides which bucket's name is stamped as
// `framework` on the resulting entity.
type dormantPairCase7030 struct {
	name    string
	pair    [2]string // the two competing buckets, in no particular order
	path    string
	content string
	kind    string
	entity  string
}

// dormantPairCases7030 covers BOTH shared-target pairs in the alias list.
// Covering only one leaves the other pinned by declaration and ungraded: a
// call site that swapped just that pair's two entries would change ~999
// entities' framework label and pass the whole package.
var dormantPairCases7030 = []dormantPairCase7030{
	{
		// docker/frameworks/docker_compose.yaml#1 and
		// kubernetes/frameworks/kubernetes_manifests.yaml#12 carry the
		// byte-identical pattern `image:\s+(\S+)` for entity_type Dependency.
		name: "docker_vs_kubernetes_image",
		pair: [2]string{"docker", "kubernetes"},
		path: "deploy/stack.yaml",
		content: `services:
  web:
    image: nginx:1.25
`,
		kind:   "Dependency",
		entity: "nginx:1.25",
	},
	{
		// ansible/frameworks/ansible_core.yaml#0 and
		// cicd/frameworks/github_actions.yaml#3 both type a `- name:` list item
		// as a Task; they differ only by an anchor. This fixture is a PURE
		// GitHub Actions workflow with no Ansible content whatsoever, which is
		// what the measured corpus population looks like (90 workflow files,
		// zero playbooks), so `cicd` is the correct label here and the order
		// must keep producing it.
		name: "cicd_vs_ansible_named_step",
		pair: [2]string{"cicd", "ansible"},
		path: ".github/workflows/ci.yml",
		content: `name: ci
on:
  push:
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Install deps
        run: go mod download
      - name: Run unit tests
        run: go test ./...
`,
		kind:   "Task",
		entity: "Install deps",
	},
}

// firstDeclared7030 returns whichever of a and b appears first in
// dormantBucketAliases, i.e. the bucket the declared order elects to win any
// pattern the two share. Derived rather than hard-coded so that this test
// grades the CONSEQUENCE of the declared order while
// TestDormantAliasOrderIsExactAndStable_7030 grades the order itself.
func firstDeclared7030(t *testing.T, a, b string) string {
	t.Helper()
	for _, al := range dormantBucketAliases {
		if al.bucket == a || al.bucket == b {
			return al.bucket
		}
	}
	t.Fatalf("neither %q nor %q is aliased; this case grades nothing", a, b)
	return ""
}

// TestDormantAliasConsultationOrderIsDeterministic_7030 is the behavioural
// grade, and the one that fails on main.
//
// On a yaml file matched by two aliased buckets, whichever is consulted first
// claims the entity and stamps its own bucket name as `framework`.
//
// A single Detect proves NOTHING here: it would report one winner under the bug
// too. `-count=N` is no defence either — it reruns in the same process, and the
// same *Detector compiles once via sync.Once. What breaks the bug is building
// MANY Detectors: Go re-randomises the start offset on every `range` over a map,
// so each fresh compile() drew an independent order. This test therefore
// compiles the real rules independently many times per case and requires that
// all runs agree AND that the agreed winner is the declared-first bucket.
//
// Verified on main (1b115b73b), by running each case there rather than
// asserting it: docker/kubernetes came out `map[docker:23 kubernetes:177]` over
// 200 compiles, and the pure GitHub Actions workflow `map[ansible:48 cicd:352]`
// over 400. Both cases therefore FAIL on main. On this branch every iteration of
// every case yields a single value.
func TestDormantAliasConsultationOrderIsDeterministic_7030(t *testing.T) {
	const iterations = 200

	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules failed: %v", err)
	}

	for _, tc := range dormantPairCases7030 {
		t.Run(tc.name, func(t *testing.T) {
			winner := firstDeclared7030(t, tc.pair[0], tc.pair[1])

			observed := map[string]int{}
			for i := 0; i < iterations; i++ {
				det := New(rules)
				res, err := det.Detect(context.Background(), extractor.FileInput{
					Path:     tc.path,
					Language: "yaml",
					Content:  []byte(tc.content),
				})
				if err != nil {
					t.Fatalf("Detect failed on iteration %d: %v", i, err)
				}
				e := findEntity(res.Entities, tc.kind, tc.entity)
				if e == nil {
					t.Fatalf("iteration %d: no %s entity %q — the shared pattern no longer "+
						"fires, so this case grades nothing", i, tc.kind, tc.entity)
				}
				observed[e.Properties["framework"]]++
			}

			// Both competing buckets must actually be capable of producing this
			// entity, or "one winner" is trivially true and grades nothing. Assert
			// it by construction: the loser is the other member of the pair, and
			// #7028 measured both producers on this pattern.
			if len(observed) != 1 {
				t.Fatalf("the `framework` property on an identical input took %d distinct "+
					"values across %d independent compiles (%v) — bucket consultation "+
					"order is not deterministic", len(observed), iterations, observed)
			}
			if observed[winner] != iterations {
				t.Fatalf("framework property = %v across %d compiles, want %q every time "+
					"(the first of %q/%q in the declared alias order)",
					observed, iterations, winner, tc.pair[0], tc.pair[1])
			}
		})
	}
}
