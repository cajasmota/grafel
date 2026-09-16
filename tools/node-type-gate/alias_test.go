package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// CONTROL 8 — the alias shapes (#7076 round 2, blocker 1).
//
// The reviewer's finding: the grep cross-check offered as evidence that the
// `cmp` surface was complete shares ONE blind spot with the scan, so it was one
// confirmation, not two. Both were syntactic: they saw `n.Type() == "…"` and
// nothing else. A node type that reaches a comparison through a VALUE —
//
//	A) a local:     t := n.Type(); if t == "…"
//	B) a parameter: isKotlinDeclType(nx.Type()), with the literal inside the callee
//
// — produced zero sites, not even a `<dynamic>` position. The reviewer's manual
// enumeration found 9 live dead literals hiding in those two shapes.
//
// This control injects BOTH shapes with names that exist in no grammar and
// requires the gate to name them. It is the only thing standing between the new
// `sources` fixpoint and a silent regression back to invisibility, because a
// blind spot has no diagnostic of its own: it looks exactly like a clean tree.
//
// VARIED across the three rows: the shape the node type travels through —
// direct call (the pre-existing form, carried here as a negative control on the
// new code: closing the blind spot must not reclassify what already worked), a
// local, and a parameter. That is the axis blocker 1 is about.
// HELD CONSTANT: the package (scala), the file, the comparison operator, the
// literal's absence from every grammar, and the baseline (empty) — so the only
// thing that can explain a differing verdict is the shape.
func TestControl_AliasFormsAreDetected(t *testing.T) {
	root := modRoot(t)
	file := filepath.Join(root, "internal", "extractors", "scala", "field_type_refs.go")

	const anchor = "func scalaFieldTypeCandidates(typeNode ts.Node, src []byte, typeParams map[string]bool) []string {"
	probes := `
func ntGateProbeDirect(n ts.Node) bool { return n.Type() == "ntgate_direct_probe" }

func ntGateProbeLocal(n ts.Node) bool {
	t := n.Type()
	return t == "ntgate_local_probe"
}

func ntGateProbeParamCallee(t string) bool { return t == "ntgate_param_probe" }

func ntGateProbeParamCaller(n ts.Node) bool { return ntGateProbeParamCallee(n.Type()) }

` + anchor
	patched := overlayReplace(t, file, anchor, probes, 1)

	rows := []struct {
		name      string
		lit       string
		wantAlias bool
	}{
		{"direct x.Type() call", "ntgate_direct_probe", false},
		{"local holding a node type", "ntgate_local_probe", true},
		{"parameter fed a node type", "ntgate_param_probe", true},
	}

	base := evalPkg(t, root, "./internal/extractors/scala", nil, emptyBaseline(t))
	for _, r := range rows {
		if len(sitesFor(base, r.lit)) != 0 {
			t.Fatalf("baseline run already derives %q — the control cannot show the overlay caused it", r.lit)
		}
	}

	res := evalPkg(t, root, "./internal/extractors/scala",
		map[string][]byte{file: patched}, emptyBaseline(t))

	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			sites := sitesFor(res, r.lit)
			if len(sites) != 1 {
				t.Fatalf("derived %d sites for %q, want 1 — the shape is invisible to the scan.\nemitted:\n%s",
					len(sites), r.lit, diagLines(res))
			}
			if got := sites[0].Alias; got != r.wantAlias {
				t.Errorf("site for %q has Alias=%v, want %v — the two shapes must be told apart, because one is enforced and the other is only reported",
					r.lit, got, r.wantAlias)
			}
			// Detection is not action. A derived-but-unreported site is the
			// same no-op as an underived one, so require the diagnostic — for
			// the alias shapes, on the alias channel.
			var found *Miss
			pool := res.Failures
			if r.wantAlias {
				pool = res.AliasMisses
			}
			for i := range pool {
				if pool[i].Lit == r.lit {
					found = &pool[i]
				}
			}
			if found == nil {
				t.Fatalf("%q was derived but never reported.\nemitted:\n%s", r.lit, diagLines(res))
			}
			if found.Dir != "internal/extractors/scala" {
				t.Errorf("%q attributed to %q", r.lit, found.Dir)
			}
			if !strings.HasSuffix(found.File, "scala/field_type_refs.go") {
				t.Errorf("%q attributed to file %q", r.lit, found.File)
			}
		})
	}

	// The alias sites must be COUNTED, not only listed: round 2's minimum
	// acceptable outcome was that the newly-visible surface shows up in the
	// output the way the exempted packages do.
	if res.AliasSites < 2 {
		t.Errorf("AliasSites = %d with two injected alias probes, want >= 2", res.AliasSites)
	}
	// And the direct form must not be laundered onto the alias channel, where
	// it would stop failing the gate. That is the permissive direction.
	for _, m := range res.AliasMisses {
		if m.Lit == "ntgate_direct_probe" {
			t.Errorf("a plain x.Type() == %q comparison was classified as an alias, which would exempt it from enforcement", m.Lit)
		}
	}
}

// CONTROL 9 — the polymorphic-helper guard.
//
// `sources` marks a parameter as receiving a node type only when it has at
// least one call site and EVERY call site passes one. Without that rule a
// generic string helper called once with n.Type() would drag all of its
// literals into node-type resolution and invent failures — which is worse than
// the blind spot, because a gate that cries wolf gets switched off.
//
// VARIED: whether the second call site passes a node type. That single axis
// flips the parameter from a node-type source to a polymorphic one.
// HELD CONSTANT: the callee, its literal, the first call site, the package and
// the baseline.
func TestControl_PolymorphicHelperParamIsNotANodeTypeSource(t *testing.T) {
	root := modRoot(t)
	file := filepath.Join(root, "internal", "extractors", "scala", "field_type_refs.go")
	const anchor = "func scalaFieldTypeCandidates(typeNode ts.Node, src []byte, typeParams map[string]bool) []string {"

	mk := func(secondArg string) []byte {
		return overlayReplace(t, file, anchor, `
func ntGatePolyCallee(t string) bool { return t == "ntgate_poly_probe" }

func ntGatePolyCallerA(n ts.Node) bool { return ntGatePolyCallee(n.Type()) }

func ntGatePolyCallerB(n ts.Node, s string) bool { return ntGatePolyCallee(`+secondArg+`) }

`+anchor, 1)
	}

	rows := []struct {
		name       string
		secondArg  string
		wantDerive bool
	}{
		{"every call site passes a node type", "n.Type()", true},
		{"one call site passes an ordinary string", "s", false},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			res := evalPkg(t, root, "./internal/extractors/scala",
				map[string][]byte{file: mk(r.secondArg)}, emptyBaseline(t))
			got := len(sitesFor(res, "ntgate_poly_probe")) > 0
			if got != r.wantDerive {
				t.Errorf("derived the callee's literal as a node type = %v, want %v.\nemitted:\n%s",
					got, r.wantDerive, diagLines(res))
			}
		})
	}
}

// aliasFindingsOnDisk is the EXACT set of alias-form dead literals the tree
// carries today that the baseline does not already tolerate. It is checked in
// so that enforceAliasForms=false cannot become a place for new dead literals
// to hide: the set is pinned, and anything joining it fails.
//
// Every row was read at source. All of them are the paired-with-correct-name
// class — a dead spelling sitting beside a LIVE one in the same matcher, so the
// matcher still works and the dead arm is inert. They are real findings and
// they are being FILED, not baselined: a baseline row is a suppression, and
// suppressing what the tool found on its first honest look would be the wrong
// first act for it.
var aliasFindingsOnDisk = []string{
	"internal/extractors/golang/references.go method_spec",
	"internal/extractors/javascript/extractor.go object_expression",
	"internal/extractors/javascript/extractor.go property",
	"internal/extractors/kotlin/kotlin.go object_body",
	"internal/extractors/kotlin/references.go object_body",
	"internal/extractors/php/config_consumer.go string_value",
	"internal/extractors/python/data_dispatch.go annotated_assignment",
	"internal/extractors/scala/scala.go import_expression",
	"internal/extractors/scala/scala.go import_selectors",
	"internal/extractors/swift/swift.go attributes",
}

// TestAliasEnforcementWouldFailOnExactlyTheKnownSet is the promise made in
// enforceAliasForms' doc comment, and the thing that keeps the constant from
// being a permanent two-tier gate. It asserts, against the REAL tree and the
// REAL baseline, that flipping the constant to true would red on exactly the
// filed set and nothing else.
//
// It fails in BOTH directions on purpose. A new alias-form dead literal is a
// regression the reported-not-enforced state must not hide; a row that stops
// matching means the defect was FIXED and the row is now dead prose, which is
// the same stale-row rule the baseline itself is held to.
func TestAliasEnforcementWouldFailOnExactlyTheKnownSet(t *testing.T) {
	root := modRoot(t)
	surf, err := LoadSurface(root, scanPatterns)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	res := Evaluate(surf.Scan, surf.Registrations, surf.ParseBindings, testGrammars(t), realBaseline(t, root))

	got := map[string]bool{}
	for _, m := range res.AliasMisses {
		if m.Baselined {
			continue
		}
		rel := m.File
		if i := strings.Index(rel, "internal/"); i >= 0 {
			rel = rel[i:]
		}
		got[rel+" "+m.Lit] = true
	}
	want := map[string]bool{}
	for _, k := range aliasFindingsOnDisk {
		want[k] = true
	}
	for k := range got {
		if !want[k] {
			t.Errorf("NEW alias-form dead literal: %s. enforceAliasForms is false so it did not fail the gate — that state exists to let the KNOWN findings be filed, not to let new ones in. Fix it, or add it here with an issue.", k)
		}
	}
	for k := range want {
		if !got[k] {
			t.Errorf("aliasFindingsOnDisk names %s but the gate no longer finds it. If it was FIXED, delete this row; if it MOVED, rewrite it. A row that matches nothing is the same dead prose a stale baseline row is.", k)
		}
	}
	// A vacuous pass is the failure mode this test shares with every other
	// scan-and-assert-absence tool: if the alias scan produced nothing at all,
	// both loops above are empty and it reports clean.
	if res.AliasSites == 0 {
		t.Fatal("the scan derived ZERO alias sites across the whole tree, so this test graded nothing. The sources fixpoint is not running.")
	}
	t.Logf("alias surface: %d sites, %d misses, %d un-baselined", res.AliasSites, len(res.AliasMisses), len(got))
}
