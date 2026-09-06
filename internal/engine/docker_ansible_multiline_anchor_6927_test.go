package engine

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
)

// #6927 — the docker_compose + ansible_core arm, i.e. the two files the sweep
// flagged as the `$`-side over-fire HAZARD and the previous three arms
// deliberately deferred.
//
// detector.go compiles rule patterns with plain regexp.Compile at :156 and
// :177, so `^` means start of TEXT — and, less obviously, `$` means end of
// TEXT. Go's `$` is stricter than Perl's: it does not even allow a trailing
// newline before it. Five patterns were affected:
//
//	docker_compose.yaml  ^\s{2}(\w[\w-]*):\s*$                  -> Service
//	docker_compose.yaml  ^volumes:\s*\n\s{2}(\w[\w-]*):         -> Config
//	docker_compose.yaml  ^networks:\s*\n\s{2}(\w[\w-]*):        -> Config
//	ansible_core.yaml    \s+(\w+\.\w+\.\w+):\s*$                -> Module
//	ansible_core.yaml    ^\s+(apt|yum|…|import_role):\s*$       -> Module
//
// The three `$`-bearing ones needed start-of-text and end-of-text at the same
// time (or, for the FQCN rule which has no `^`, needed the module key to be the
// last construct in the file), so they were near-dead: ZERO matches across 992
// real YAML files.
//
// `(?m)` ALONE is what this issue predicted would over-fire, and the
// measurement is why the service rule was REWRITTEN rather than merely
// widened. Under `(?m)`, `^\s{2}(\w[\w-]*):\s*$` reads "every two-space key
// alone on its line": 1867 matches in 611 of the 941 non-compose corpus files,
// AND 40 junk Services inside real compose files, where no framework gate can
// help.
//
// What is graded HERE rather than in the two golden fixtures
// (internal/quality/golden/{docker-compose-services-mini,
// ansible-playbook-modules-mini}), and why this file exists alongside them:
//
//  1. WHICH RULE produced an entity. The docker and ansible buckets are both
//     aliased onto the whole `yaml` language (dormantBucketAliases), so at
//     graph level several rule files see every YAML file and one file's output
//     can stand in for another's. Every case below runs against a detector
//     holding ONE rule set.
//  2. Each import_marker on its own. A realistic fixture file carries several
//     markers at once, so deleting one is invisible there — the failure #6945
//     had to be told about by a reviewer.
//  3. Rule SELECTION by behaviour rather than by pattern text. #6945 shipped a
//     test that selected its rule with strings.Contains on the pattern and so
//     went red on a behaviour-preserving rewrite; its apparent kill of a real
//     anchor weakening was selector loss, not grading. Everything here selects
//     by what a pattern DOES, and TestIssue6927_DockerAnsible_SelectorSurvivesA
//     BehaviourPreservingRewrite is the control that must PASS.
//
// Graded in BOTH directions (#6902): a recall assertion is structurally blind
// to over-firing.

// rules6927 returns the loaded rule map reduced to a single framework by name.
// The rules are the REAL embedded YAML — the defect IS the shipped pattern
// text, so a hand-written copy would leave the shipped one unobserved — but
// every sibling rule set is dropped, so nothing another rule file mints can be
// mistaken for evidence about this one.
func rules6927(t *testing.T, frameworkName, wantBucket string) map[string][]FrameworkRule {
	t.Helper()
	all, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	out := map[string][]FrameworkRule{}
	for lang, frs := range all {
		for _, fr := range frs {
			if fr.Frameworks.Name == frameworkName {
				out[lang] = append(out[lang], fr)
			}
		}
	}
	// The loader keys rule sets by their BUCKET directory (`docker`,
	// `ansible`); compile() aliases the bucket onto the `yaml` language via
	// dormantBucketAliases, which is why these tests hand Detect a Language of
	// "yaml" while the map key stays the bucket name. If that stops being true
	// the tests measure nothing, so it is asserted rather than assumed.
	if len(out[wantBucket]) != 1 {
		t.Fatalf("expected exactly one %q rule set in the %q bucket, got %d",
			frameworkName, wantBucket, len(out[wantBucket]))
	}
	if targets := dormantBucketAliases[wantBucket]; !containsStr6927(targets, "yaml") {
		t.Fatalf("bucket %q is no longer aliased onto yaml (%v); these tests hand Detect "+
			"Language=yaml and would grade nothing", wantBucket, targets)
	}
	return out
}

func containsStr6927(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func detect6927(t *testing.T, rules map[string][]FrameworkRule, path, src string) *DetectResult {
	t.Helper()
	res, err := New(rules).Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "yaml",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return res
}

func has6927(res *DetectResult, kind, name string) bool {
	for _, e := range res.Entities {
		if string(e.Kind) == kind && e.Name == name {
			return true
		}
	}
	return false
}

func names6927(res *DetectResult, kind string) []string {
	var out []string
	for _, e := range res.Entities {
		if string(e.Kind) == kind {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out
}

func composeRules6927(t *testing.T) map[string][]FrameworkRule {
	t.Helper()
	return rules6927(t, "Docker Compose", "docker")
}

func ansibleRules6927(t *testing.T) map[string][]FrameworkRule {
	t.Helper()
	return rules6927(t, "Ansible Core", "ansible")
}

// composeFixtureSrc is a minimal but complete compose file. It carries the
// marker, three services and a top-level volumes/networks pair, and is reused
// wherever a case needs "a real compose file" as its positive control.
const composeFixtureSrc6927 = `services:
  api:
    image: ghcr.io/example/api:1
    depends_on:
      - db
  db:
    # a comment between the key and its first argument
    image: postgres:16

volumes:
  db-data:
  cache-data:

secrets:
  db-password:
    file: ./secrets/db.txt

networks:
  backend:
    driver: bridge
`

// ansibleTasksSrc6927 is a minimal role tasks file in block form, carrying
// several markers on purpose — it is the "realistic input" control, and the
// per-marker cases below are the ones that carry exactly one.
const ansibleTasksSrc6927 = `- name: Install nginx
  ansible.builtin.apt:
    name: nginx
    state: present

- name: Render the config
  template:
    src: nginx.conf.j2
    dest: /etc/nginx/nginx.conf
`

// TestIssue6927_DockerAnsible_WidenedPatternsCarryMultiline pins the fix itself
// at the level the defect lives at: the compiled pattern text.
//
// Rules are selected by BEHAVIOUR — which compiled pattern reads which
// construct out of a real file — and the `(?m)` flag is then read off the
// pattern that was selected. Selecting by `strings.Contains(sp.Pattern, "^\\s{2}")`
// would make this test a spelling assertion, which is exactly the #6945 defect.
func TestIssue6927_DockerAnsible_WidenedPatternsCarryMultiline(t *testing.T) {
	type probe struct {
		framework, bucket string
		// what the rule must read out of src, which is how it is identified
		src, wantCapture   string
		entityType         string
		describe           string
		mustNotCarryDollar bool
	}
	probes := []probe{
		{
			framework: "Docker Compose", bucket: "docker",
			src:         "services:\n  api:\n    image: nginx\n",
			wantCapture: "api", entityType: "Service",
			describe: "the compose service rule",
			// The `$` is GONE from this rule on purpose: `(?m)` would have
			// turned it into end-of-line, i.e. "every two-space key alone on
			// its line", which is not what a service is. `[ \t]*\n` carries
			// the same "nothing else on this line" half without the
			// end-of-text / end-of-line ambiguity.
			mustNotCarryDollar: true,
		},
		{
			framework: "Docker Compose", bucket: "docker",
			src:         "volumes:\n  db-data:\n",
			wantCapture: "db-data", entityType: "Config",
			describe: "the compose named-volume rule",
		},
		{
			framework: "Docker Compose", bucket: "docker",
			src:         "networks:\n  backend:\n    driver: bridge\n",
			wantCapture: "backend", entityType: "Config",
			describe: "the compose named-network rule",
		},
		{
			framework: "Ansible Core", bucket: "ansible",
			src:         "- name: x\n  ansible.builtin.apt:\n    name: nginx\n",
			wantCapture: "ansible.builtin.apt", entityType: "Module",
			describe: "the ansible FQCN module rule",
		},
		{
			framework: "Ansible Core", bucket: "ansible",
			src:         "- name: x\n  template:\n    src: a.j2\n",
			wantCapture: "template", entityType: "Module",
			describe: "the ansible shorthand module rule",
		},
	}

	for _, p := range probes {
		fr := rules6927(t, p.framework, p.bucket)[p.bucket][0]
		var selected []SourcePattern
		for _, sp := range fr.SourcePatterns {
			if sp.EntityType != p.entityType {
				continue
			}
			re, err := regexp.Compile(sp.Pattern)
			if err != nil {
				t.Fatalf("%s: rule file carries an uncompilable pattern %q: %v", p.describe, sp.Pattern, err)
			}
			m := re.FindStringSubmatch(p.src)
			if len(m) > sp.NameGroup && m[sp.NameGroup] == p.wantCapture {
				selected = append(selected, sp)
			}
		}
		if len(selected) != 1 {
			t.Errorf("%s: expected exactly ONE pattern to read %q out of the probe, got %d. "+
				"Either the rule is gone or two rules now overlap, and in both cases the "+
				"assertions below would be about the wrong pattern", p.describe, p.wantCapture, len(selected))
			continue
		}
		sp := selected[0]
		if !strings.Contains(sp.Pattern, "(?m)") {
			t.Errorf("%s lost its `(?m)`: %q. Without it `^` is start of TEXT and `$` is end "+
				"of TEXT (Go allows no trailing newline before it), which is the whole of "+
				"#6927 — this rule measured ZERO matches in 992 real YAML files that way",
				p.describe, sp.Pattern)
		}
		if p.mustNotCarryDollar && strings.Contains(sp.Pattern, "$") {
			t.Errorf("%s grew a `$` back: %q. Under `(?m)` that is end of LINE, i.e. `every "+
				"two-space key alone on its line`, which mints a Service for every named "+
				"volume, network and secret in a real compose file — 40 of them across the "+
				"measured corpus", p.describe, sp.Pattern)
		}
	}
}

// TestIssue6927_DockerAnsible_GatesAreDeclaredAndLoaded pins the OTHER half of
// the remedy. requires_framework is inert without import_markers — detector.go
// logs that combination and frameworkPresent then returns false for every file,
// so a gate that looks present in the YAML can be an off switch — and a gate
// that quietly disappears is a silent 1867-match over-fire, not a failure.
func TestIssue6927_DockerAnsible_GatesAreDeclaredAndLoaded(t *testing.T) {
	for _, tc := range []struct {
		framework, bucket string
		wantGated         int
		wantMarkers       int
	}{
		{"Docker Compose", "docker", 1, 1},
		{"Ansible Core", "ansible", 2, 7},
	} {
		fr := rules6927(t, tc.framework, tc.bucket)[tc.bucket][0]
		markers := fr.Frameworks.Detection.ImportMarkers
		if len(markers) != tc.wantMarkers {
			t.Errorf("%s declares %d import_markers, want %d: %v — every marker is graded "+
				"individually below, so a new one would be ungraded and a lost one is a gate "+
				"getting weaker in silence", tc.framework, len(markers), tc.wantMarkers, markers)
		}
		if len(markers) == 0 {
			t.Errorf("%s declares no import_markers at all, so every requires_framework "+
				"pattern in it can NEVER fire", tc.framework)
		}
		gated := 0
		for _, sp := range fr.SourcePatterns {
			if sp.RequiresFramework {
				gated++
			}
		}
		if gated != tc.wantGated {
			t.Errorf("%s has %d requires_framework patterns, want %d", tc.framework, gated, tc.wantGated)
		}
	}
}

// TestIssue6927_Compose_ServiceRuleNeedsBothFences is the core of the docker
// arm. The rewritten service rule has TWO independent fences — its shape (a
// two-space key whose block opens with a compose service-level key, at a
// deeper LITERAL-SPACE indent) and its framework gate — and this table holds
// each of them on inputs where the other one says nothing.
//
// Every "must not mint" row here is a shape measured in the real corpus, not
// an invention: the workflow `deploy:` / `environment:` pair is what GitHub's
// own starter-workflows carries in eleven files.
func TestIssue6927_Compose_ServiceRuleNeedsBothFences(t *testing.T) {
	rules := composeRules6927(t)
	marker := rules["docker"][0].Frameworks.Detection.ImportMarkers[0]

	cases := []struct {
		name       string
		src        string
		wantHeld   []string // Service names that must NOT be minted
		wantMinted []string // Service names that must be
		fence      string
	}{
		{
			name:       "a real compose file yields exactly its services",
			src:        composeFixtureSrc6927,
			wantMinted: []string{"api", "db"},
			// The volume, network and secret names are the 40-per-corpus
			// over-fire the un-rewritten `(?m)` form produced.
			wantHeld: []string{"db-data", "cache-data", "db-password", "backend"},
			fence:    "shape",
		},
		{
			name: "a workflow job whose first key is compose vocabulary, WITHOUT the marker",
			src: `name: Release
jobs:
  deploy:
    environment:
      name: production
    runs-on: ubuntu-latest
`,
			wantHeld: []string{"deploy"},
			fence:    "gate",
		},
		{
			name: "a workflow that DOES carry the marker is still declined by shape alone",
			src: `name: Test
jobs:
  unit:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16
`,
			// `unit:` is declined because `runs-on` is not compose vocabulary;
			// `postgres:` because it is six-space indented, not two.
			wantHeld: []string{"unit", "postgres"},
			fence:    "shape",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Premise, asserted rather than eyeballed: a "gate" case that
			// quietly grew the marker would grade nothing, and a "shape" case
			// that lost it would pass for the wrong reason.
			gated := strings.Contains(tc.src, marker)
			if tc.fence == "gate" && gated {
				t.Fatalf("this case is meant to be held by the GATE but carries the marker %q, "+
					"so it is held by the shape instead", marker)
			}
			if tc.fence == "shape" && !gated {
				t.Fatalf("this case is meant to be held by the SHAPE but does not carry the "+
					"marker %q, so the gate declines it before the shape is ever consulted", marker)
			}
			res := detect6927(t, rules, "deploy/case.yaml", tc.src)
			for _, want := range tc.wantMinted {
				if !has6927(res, "Service", want) {
					t.Errorf("Service %q missing; got %v", want, names6927(res, "Service"))
				}
			}
			for _, bad := range tc.wantHeld {
				if has6927(res, "Service", bad) {
					t.Errorf("Service %q was minted and must not be — the %s fence is not holding. "+
						"Got %v", bad, tc.fence, names6927(res, "Service"))
				}
			}
		})
	}
}

// TestIssue6927_Compose_ServiceRuleMintsAnExactSet grades the rule as a SET
// rather than by membership, because membership is blind to the direction the
// dangerous mutants move in: widening the indent, the indent CLASS or the
// vocabulary all ADD names, and every `has(...)` assertion above stays green
// while they do.
func TestIssue6927_Compose_ServiceRuleMintsAnExactSet(t *testing.T) {
	res := detect6927(t, composeRules6927(t), "deploy/compose.yaml", composeFixtureSrc6927)
	got := names6927(res, "Service")
	want := []string{"api", "db"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("compose Services = %v, want exactly %v.\n"+
			"  extra `db-data`/`cache-data`  -> the `[ ]{3,}` indent CLASS or the `$`-form revert\n"+
			"  extra `db-password`           -> the next-line VOCABULARY was widened\n"+
			"  missing `db`                  -> the comment arm `(?:#[^\\n]*)?\\n` was dropped",
			got, want)
	}
}

// TestIssue6927_Compose_TheImportMarkerAdmitsAComposeFileAlone grades the
// docker marker set marker by marker in the RECALL direction. The set has one
// entry today, deliberately: on the measured corpus `depends_on:` and
// `container_name:` are strictly redundant with it, and #6945 records that
// shipping a marker no input can grade is the shape to avoid. If a later arm
// adds one, this test fails until a case for it is added.
func TestIssue6927_Compose_TheImportMarkerAdmitsAComposeFileAlone(t *testing.T) {
	rules := composeRules6927(t)
	markers := rules["docker"][0].Frameworks.Detection.ImportMarkers

	cases := []struct{ marker, src, wantName string }{
		{
			marker: "services:",
			src: `services:
  api:
    image: nginx
`,
			wantName: "api",
		},
	}
	if len(cases) != len(markers) {
		t.Fatalf("%d cases for %d markers (%v) — a new marker is ungraded", len(cases), len(markers), markers)
	}
	for _, tc := range cases {
		var present []string
		for _, m := range markers {
			if strings.Contains(tc.src, m) {
				present = append(present, m)
			}
		}
		if len(present) != 1 || present[0] != tc.marker {
			t.Errorf("case %q carries markers %v, want exactly [%q]", tc.marker, present, tc.marker)
			continue
		}
		res := detect6927(t, rules, "deploy/compose.yaml", tc.src)
		if !has6927(res, "Service", tc.wantName) {
			t.Errorf("marker %q alone did not admit a compose file: Service %q missing. "+
				"Deleting this marker turns the service rule off entirely", tc.marker, tc.wantName)
		}
	}
}

// TestIssue6927_Compose_VolumeAndNetworkRulesAreColumnZeroOnly grades the two
// UNGATED compose rules. They take no requires_framework — measured ZERO
// matches across 941 non-compose corpus files — so the column-0 `^` is their
// entire fence, and it has to be graded against WEAKENING and not only against
// removal (#6945's third review finding).
func TestIssue6927_Compose_VolumeAndNetworkRulesAreColumnZeroOnly(t *testing.T) {
	rules := composeRules6927(t)
	// A helm values file: an empty `volumes:` / `networks:` key INDENTED under
	// a parent, each followed by a two-space sibling. This is the shape a
	// `^`-less or `^\s*`-weakened rule reads as a compose block.
	src := `replicaCount: 2
serverSide:
  volumes:
  extraArgs: {}
  networks:
  hostAliases: []
`
	res := detect6927(t, rules, "deploy/helm/values.yaml", src)
	for _, bad := range []string{"extraArgs", "hostAliases"} {
		if has6927(res, "Config", bad) {
			t.Errorf("Config %q was minted from an INDENTED volumes:/networks: key — the "+
				"column-0 anchor is the only fence these two rules have, and it is not "+
				"holding. Got %v", bad, names6927(res, "Config"))
		}
	}
	// Positive control on the same rules and the same file shape, so a failure
	// above cannot be "the rules stopped loading".
	ctl := detect6927(t, rules, "deploy/compose.yaml", composeFixtureSrc6927)
	for _, want := range []string{"db-data", "backend"} {
		if !has6927(ctl, "Config", want) {
			t.Errorf("control: Config %q missing from a real compose file; got %v",
				want, names6927(ctl, "Config"))
		}
	}
}

// TestIssue6927_Ansible_ShorthandModuleRuleNeedsItsGate is the ansible arm's
// gate row. `copy`, `template`, `file`, `service`, `command` and `debug` are
// ordinary English words and the ansible bucket sees every YAML file in every
// repo, so ungated this rule matched 59 times in 43 non-ansible corpus files —
// 41 of them `template:`, from argocd ApplicationSets, helm chart templates and
// k8s PodTemplateSpecs.
//
// This is invisible in a golden fixture in one direction: the entities it mints
// have plausible names and nothing else in the graph contradicts them.
func TestIssue6927_Ansible_ShorthandModuleRuleNeedsItsGate(t *testing.T) {
	rules := ansibleRules6927(t)
	markers := rules["ansible"][0].Frameworks.Detection.ImportMarkers

	// A k8s Deployment: block-form `template:` and `command:` at matching
	// indentation, and no ansible marker anywhere.
	k8s := `apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
        - name: api
          command:
            - /usr/local/bin/api
`
	for _, m := range markers {
		if strings.Contains(k8s, m) {
			t.Fatalf("the k8s case carries the ansible marker %q, so it grades nothing", m)
		}
	}
	res := detect6927(t, rules, "deploy/k8s/deployment.yaml", k8s)
	for _, bad := range []string{"template", "command"} {
		if has6927(res, "Module", bad) {
			t.Errorf("Module %q minted from a Kubernetes manifest — requires_framework is not "+
				"holding. Got %v", bad, names6927(res, "Module"))
		}
	}
	// Control: the same rule on a real task file must still fire, so "the gate
	// holds" and "the rule is dead" stay distinguishable.
	ctl := detect6927(t, rules, "provisioning/tasks/main.yml", ansibleTasksSrc6927)
	if !has6927(ctl, "Module", "template") {
		t.Errorf("control: Module \"template\" missing from a real task file; got %v",
			names6927(ctl, "Module"))
	}
}

// TestIssue6927_Ansible_EachImportMarkerAdmitsATaskFileAlone grades the ansible
// marker set marker by marker. A realistic task file carries three or four of
// them at once — ansibleTasksSrc6927 carries `ansible.builtin.`, `state: present`
// and `.j2` — so no single deletion is observable on a realistic input, which
// is precisely the hole a reviewer had to find in #6945.
//
// Each case carries EXACTLY ONE marker, and that premise is asserted.
func TestIssue6927_Ansible_EachImportMarkerAdmitsATaskFileAlone(t *testing.T) {
	rules := ansibleRules6927(t)
	markers := rules["ansible"][0].Frameworks.Detection.ImportMarkers

	cases := []struct{ marker, src, wantName string }{
		{
			marker: "ansible.builtin.",
			// The FQCN rule is gated too, so its own marker must admit it.
			src: `- name: t
  ansible.builtin.copy:
    dest: /tmp/x
`,
			wantName: "ansible.builtin.copy",
		},
		{
			marker: "become:",
			src: `- name: t
  become: true
  service:
    state: started
`,
			wantName: "service",
		},
		{
			marker: "gather_facts",
			src: `- hosts: all
  gather_facts: false
  tasks:
    - name: t
      debug:
        var: x
`,
			wantName: "debug",
		},
		{
			marker: "ansible_",
			src: `- name: t
  command:
    cmd: echo {{ ansible_hostname }}
`,
			wantName: "command",
		},
		{
			marker: "with_items:",
			src: `- name: t
  apt:
    name: "{{ item }}"
  with_items:
    - nginx
`,
			wantName: "apt",
		},
		{
			marker: ".j2",
			src: `- name: t
  template:
    src: nginx.conf.j2
`,
			wantName: "template",
		},
		{
			marker: "state: present",
			src: `- name: t
  yum:
    name: nginx
    state: present
`,
			wantName: "yum",
		},
	}
	if len(cases) != len(markers) {
		t.Fatalf("%d cases for %d markers (%v) — a new marker would be ungraded",
			len(cases), len(markers), markers)
	}
	for _, tc := range cases {
		var present []string
		for _, m := range markers {
			if strings.Contains(tc.src, m) {
				present = append(present, m)
			}
		}
		if len(present) != 1 || present[0] != tc.marker {
			t.Errorf("case %q carries markers %v, want exactly [%q] — it grades the marker SET, "+
				"not this marker", tc.marker, present, tc.marker)
			continue
		}
		res := detect6927(t, rules, "provisioning/tasks/main.yml", tc.src)
		if !has6927(res, "Module", tc.wantName) {
			t.Errorf("marker %q alone did not admit a task file: Module %q missing. Deleting "+
				"this one marker silently drops every gated Module from a file that carries "+
				"only it", tc.marker, tc.wantName)
		}
	}
}

// TestIssue6927_Ansible_ModuleRulesReadBlockFormOnly grades the `$` — which is
// the whole point of this arm. Under `(?m)` it becomes end of LINE, and that is
// exactly the block form: a module key with its arguments in the mapping below
// it. The inline `key=value` form must stay invisible, and a commented-out
// module must not be read as a live one.
//
// Both cases sit inside content that SATISFIES the framework gate, so they are
// held by the anchors alone and are distinguishable from the gate rows above.
func TestIssue6927_Ansible_ModuleRulesReadBlockFormOnly(t *testing.T) {
	rules := ansibleRules6927(t)
	markers := rules["ansible"][0].Frameworks.Detection.ImportMarkers
	src := `- name: Install nginx
  ansible.builtin.apt:
    name: nginx
    state: present

- name: Drop the maintenance page
  copy: src=maintenance.html dest=/var/www/maintenance.html

# - systemd:
#     name: nginx
#     state: reloaded

- name: Report the path
  ansible.builtin.debug: msg=/etc/nginx/nginx.conf

- name: Ensure the include
  lineinfile:
    path: /etc/nginx/nginx.conf

- name: Tune worker counts
  ansible.builtin.set_fact:
    nginx.conf:
      worker_processes: 4
`
	var present []string
	for _, m := range markers {
		if strings.Contains(src, m) {
			present = append(present, m)
		}
	}
	if len(present) == 0 {
		t.Fatal("this case must SATISFY the gate — otherwise the gate declines it and the " +
			"anchors are never consulted, so every assertion below passes for the wrong reason")
	}

	res := detect6927(t, rules, "provisioning/tasks/main.yml", src)
	got := names6927(res, "Module")
	want := []string{"ansible.builtin.apt", "ansible.builtin.set_fact"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ansible Modules = %v, want exactly %v.\n"+
			"  extra `copy`                  -> the shorthand rule lost its `\\s*$`; the inline form is not a block\n"+
			"  extra `systemd`               -> the shorthand rule lost its `^`; that key is inside a COMMENT\n"+
			"  extra `ansible.builtin.debug` -> the FQCN rule lost its `\\s*$` (its only anchor)\n"+
			"  extra `lineinfile`            -> the shorthand alternation was replaced by a generic \\w+\n"+
			"  extra `nginx.conf`            -> the FQCN rule's third dotted segment became optional",
			got, want)
	}
}

// TestIssue6927_DockerAnsible_SelectorSurvivesABehaviourPreservingRewrite is
// the control that must PASS, and the reason every selector in this file is
// behavioural.
//
// #6945 shipped a test that picked its rule with strings.Contains over the
// pattern TEXT. It went red on a deliberately behaviour-preserving rewrite with
// the same message it produced for a real anchor weakening — i.e. its apparent
// kill was selector loss, not grading, and only a control expected to pass
// could find that. This test performs such a rewrite on each widened pattern
// (`\w` -> `[\w]`, which RE2 treats identically) and asserts that the same
// construct is still read out of the same input. A selector that cannot tell a
// rename from a break reads as coverage and is worse than none.
func TestIssue6927_DockerAnsible_SelectorSurvivesABehaviourPreservingRewrite(t *testing.T) {
	cases := []struct {
		framework, bucket, entityType, src, wantCapture string
	}{
		{"Docker Compose", "docker", "Service", "services:\n  api:\n    image: nginx\n", "api"},
		{"Docker Compose", "docker", "Config", "volumes:\n  db-data:\n", "db-data"},
		{"Docker Compose", "docker", "Config", "networks:\n  backend:\n    driver: bridge\n", "backend"},
		{"Ansible Core", "ansible", "Module", "- name: x\n  ansible.builtin.apt:\n    name: nginx\n", "ansible.builtin.apt"},
		{"Ansible Core", "ansible", "Module", "- name: x\n  template:\n    src: a.j2\n", "template"},
	}
	for _, tc := range cases {
		fr := rules6927(t, tc.framework, tc.bucket)[tc.bucket][0]
		found := 0
		for _, sp := range fr.SourcePatterns {
			if sp.EntityType != tc.entityType {
				continue
			}
			// The rewrite: `\w` -> its exact RE2 expansion. Byte-for-byte a
			// different pattern, character-for-character the same language,
			// and invasive enough that any substring selector over the text
			// would miss.
			rewritten := expandWordClass6927(sp.Pattern)
			re, err := regexp.Compile(rewritten)
			if err != nil {
				t.Fatalf("behaviour-preserving rewrite of %q did not compile: %v", sp.Pattern, err)
			}
			m := re.FindStringSubmatch(tc.src)
			if len(m) > sp.NameGroup && m[sp.NameGroup] == tc.wantCapture {
				found++
			}
		}
		if found != 1 {
			t.Errorf("%s/%s: after a behaviour-preserving rewrite, %d patterns read %q out of "+
				"the probe, want 1. This test is the control that must PASS; a failure here "+
				"means the selection in this file is about pattern SPELLING, not behaviour",
				tc.framework, tc.entityType, found, tc.wantCapture)
		}
	}
}

// expandWordClass6927 replaces every `\w` in an RE2 pattern with its exact
// expansion, `[0-9A-Za-z_]`, tracking character-class depth so that a `\w`
// INSIDE a class (`[\w-]`) expands to the bare set rather than to a nested
// bracket. RE2 defines `\w` as exactly this set, so the rewrite is
// behaviour-preserving by construction rather than by inspection.
func expandWordClass6927(pattern string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '\\' && i+1 < len(pattern) {
			if pattern[i+1] == 'w' {
				if inClass {
					b.WriteString("0-9A-Za-z_")
				} else {
					b.WriteString("[0-9A-Za-z_]")
				}
			} else {
				b.WriteByte(c)
				b.WriteByte(pattern[i+1])
			}
			i++
			continue
		}
		switch {
		case c == '[' && !inClass:
			inClass = true
		case c == ']' && inClass:
			inClass = false
		}
		b.WriteByte(c)
	}
	return b.String()
}
