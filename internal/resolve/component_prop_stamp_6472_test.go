// Package resolve_test — #6472: isLocalBindingKind keys on the local_scope
// stamp, and on nothing else.
//
// Until #6472 the predicate carried a second arm:
//
//	return subtype == "component_prop" && props["framework"] == "react"
//
// It existed only because internal/extractors/javascript/dataflow_react.go did
// not stamp local_scope on the props it emitted. A framework NAME is the
// weakest usable signal — it cannot distinguish a React props parameter (a
// formal parameter of the component callable, non-addressable, must lose the
// repository-wide byName slot) from angular's @Input() or vue's defineProps
// (a component's PUBLIC surface, addressable from a parent template, must keep
// it). The three records are otherwise identical: same Kind SCOPE.Operation,
// same bare Name, same "Comp.Prop" QualifiedName.
//
// #6472 stamped the React emitter instead and deleted the arm. This file pins
// the deletion.
package resolve_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// TestComponentPropLocalityKeysOnStampNotFramework_6472 pins that the framework
// name is no longer consulted: locality is decided by the local_scope stamp
// alone.
//
// THE KILLING ROW is "react, unstamped". Restore the deleted arm and that row
// goes red.
//
// THAT ROW IS NOT HYPOTHETICAL. An earlier draft of this file called it a
// "counterfactual", on the reasoning that the React emitter always stamps now
// so both predicates agree on everything producible. That was wrong, and
// review caught it: a graph written by a PRE-#6472 binary holds exactly this
// record — component_prop + framework=react + no stamp — and Path B's
// incremental reindex feeds those carried-forward records straight into
// BuildIndex. The shape is production-reachable for the whole window between a
// user upgrading and their next full reindex.
//
// That window is closed at the carry-forward seam
// (cmd/grafel/incremental_local_scope_compat.go), which stamps such records on
// the way into the resolver index, and is pinned end-to-end through the real
// indexer by TestPathBIncremental_LegacyComponentPropKeepsLocality_6472. So
// the rows below state this predicate's contract — the stamp decides, the
// framework name is never consulted — while the seam guarantees the stamp is
// present for old graphs. Both halves are needed; neither alone is safe.
//
// Non-vacuity: every subtest asserts exactly one IMPORTS edge was found before
// judging where it points (the shared helpers fail the run otherwise), and the
// stamped row below proves the locality tier still fires at all — without it
// this test could pass with the whole tier deleted.
func TestComponentPropLocalityKeysOnStampNotFramework_6472(t *testing.T) {
	// Unstamped component_props keep the slot, whatever framework says.
	unstamped := []struct {
		label     string
		framework string
		language  string
		file      string
	}{
		// The killing row: the deleted arm matched exactly this record, and a
		// pre-#6472 graph still produces it (see the header).
		{"react component_prop with no local_scope stamp", "react", "typescript", "src/Chart.tsx"},
		// Already pinned individually by the #6467 tests; repeated here so the
		// three shapes are judged by one rule, which is the point of #6472.
		{"angular component_prop with no local_scope stamp", "angular", "typescript", "src/chart.component.ts"},
		{"vue component_prop with no local_scope stamp", "vue", "vue", "src/Chart.vue"},
	}
	for _, tc := range unstamped {
		t.Run(tc.label, func(t *testing.T) {
			assertDeclarationKeepsSlot6467(t, tc.label, types.EntityRecord{
				ID: "dddd000000000007", Kind: "SCOPE.Operation", Name: "Data",
				QualifiedName: "Chart.Data", Subtype: "component_prop",
				SourceFile: tc.file, Language: tc.language,
				Properties: map[string]string{
					"kind": "SCOPE.Operation", "subtype": "component_prop",
					"component": "Chart", "prop": "Data", "framework": tc.framework,
				},
			})
		})
	}

	// The mirror, and the non-vacuity control for the rows above: the SAME
	// record, differing only by the stamp, loses the slot. Without this the
	// test would pass with isLocalBindingKind hard-coded to false.
	t.Run("react component_prop WITH the local_scope stamp loses the slot", func(t *testing.T) {
		assertDeclarationLosesSlot6467(t, "stamped react component_prop", types.EntityRecord{
			ID: "dddd000000000008", Kind: "SCOPE.Operation", Name: "Data",
			QualifiedName: "Chart.Data", Subtype: "component_prop",
			SourceFile: "src/Chart.tsx", Language: "typescript",
			Properties: map[string]string{
				"kind": "SCOPE.Operation", "subtype": "component_prop",
				"component": "Chart", "prop": "Data", "framework": "react",
				"local_scope": "true",
			},
		})
	})

	// The stamp is not component_prop-specific either: a non-prop subtype
	// carrying it is equally local. This pins that the deletion did not leave a
	// hidden subtype condition behind.
	//
	// The angular/vue rows above are NOT decorative, which is worth recording
	// because review assumed they were: widening this predicate to
	// `|| subtype == "component_prop"` — the #6470 regression — fails both of
	// them, plus TestAngularInputIsNotALocalBinding_6467 and
	// TestVueDefinePropsIsNotALocalBinding_6467. Mutant scored, not argued.
	t.Run("non-prop subtype WITH the stamp also loses the slot", func(t *testing.T) {
		assertDeclarationLosesSlot6467(t, "stamped const_destructure", types.EntityRecord{
			ID: "dddd000000000009", Kind: "SCOPE.Component", Name: "Data",
			QualifiedName: "Chart.Data", Subtype: "const_destructure",
			SourceFile: "src/Chart.tsx", Language: "typescript",
			Properties: map[string]string{
				"kind": "SCOPE.Component", "subtype": "const_destructure",
				"local_scope": "true",
			},
		})
	})
}
