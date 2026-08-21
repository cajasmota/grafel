package resolve

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// celeryIndex builds the wired shape that internal/custom/python/celery.go
// produces for
//
//	@shared_task
//	def send_email(...): ...
//
// The decorator handler emits entity(funcName, "SCOPE.Service", "task", …) —
// see internal/custom/python/celery.go:94 — while the Python extractor has
// ALREADY emitted the same-named SCOPE.Operation function from the same file.
// internal/extractors/python/celery.go:13-16 documents the duplication as
// deliberate. Every `.delay()` / `.apply_async()` CALLS edge minted at
// internal/extractors/python/celery.go:231 addresses the FUNCTION through
// extractor.BuildOperationStructuralRef("python", file, taskName), i.e. the
// operation address space, at exactly the (file, name) the SCOPE.Service task
// marker also occupies.
func celeryIndex() (Index, types.EntityRecord, types.EntityRecord) {
	const taskFile = "app/tasks.py"
	fn := types.EntityRecord{
		ID: "f1f1f1f1f1f1f1f1", Kind: "SCOPE.Operation", Subtype: "function",
		Name: "send_email", SourceFile: taskFile, Language: "python",
	}
	task := types.EntityRecord{
		ID: "e2e2e2e2e2e2e2e2", Kind: "SCOPE.Service", Subtype: "task",
		Name: "send_email", SourceFile: taskFile, Language: "python",
	}
	return BuildIndex([]types.EntityRecord{fn, task}), fn, task
}

// TestCeleryTaskCallStillBindsToTheFunction6492 is the regression guard for the
// first shape of the #6459 fix, which added scopeKindPrefix+"Service" to the
// SHARED operationKindFamily slice.
//
// SCOPE.Service is not a proto-only kind. It is emitted by ~60 sites across
// internal/patterns/, internal/custom/{python,golang,rust,elixir}/ and
// internal/extractors/{kotlin,yaml,proto}/, and several of them name the entity
// after a function or class that ALREADY EXISTS in the same file. Because
// uniqueMatchInFamily returns ("", false) when two DISTINCT ids in the family
// match, adding a member to a family cannot only gain bindings — it destroys
// every binding that was unique solely because the new member was absent.
//
// Measured on the celery shape at the shared-slice head: lookupStructural
// returned handled=true with id="" (ambiguous), and lookupByKindHint returned
// ("", false). Both had returned the function before. The damage is not caught
// by lookupLocationKind's tier-1 `.real` pass because BOTH entities are
// SCOPE.*-kinded, so tier 1 is empty and tier 2 sees the collision.
func TestCeleryTaskCallStillBindsToTheFunction6492(t *testing.T) {
	idx, fn, task := celeryIndex()

	ref := extractor.BuildOperationStructuralRef("python", fn.SourceFile, fn.Name)
	id, _, handled := idx.lookupStructural(ref)
	if !handled {
		t.Fatalf("lookupStructural did not claim %q (handled=false)", ref)
	}
	if id != fn.ID {
		t.Fatalf("lookupStructural(%q) = %q, want the SCOPE.Operation function %q "+
			"(the SCOPE.Service celery task marker is %q). A SCOPE.Service member in "+
			"the operation family for a NON-proto language turns this unique match "+
			"into an ambiguity and dangles every .delay()/.apply_async() CALLS edge (#6492)",
			ref, id, fn.ID, task.ID)
	}
}

// TestCeleryTaskBareCallHintStillBindsToTheFunction6492 is the same regression
// on the bare-name path. hintKinds("CALLS") returns operationKindFamily, so
// widening that slice propagates into lookupByKindHint as well as into
// familyMaskByKind. This is the direction PR #6492's original safety argument
// left unexamined: it argued only that the uniqueness requirement prevents a
// SCOPE.Service from STEALING a real function's binding, which is true — and
// which is precisely why it instead guarantees the binding is LOST.
func TestCeleryTaskBareCallHintStillBindsToTheFunction6492(t *testing.T) {
	idx, fn, task := celeryIndex()

	id, ok := idx.lookupByKindHint(fn.Name, "CALLS")
	if !ok || id != fn.ID {
		t.Fatalf("lookupByKindHint(%q, \"CALLS\") = (%q, %v), want (%q, true) — the "+
			"celery SCOPE.Service task marker %q must not collide the CALLS hint (#6492)",
			fn.Name, id, ok, fn.ID, task.ID)
	}
}

// TestOperationKindFamilyExcludesService6492 pins the SHAPE of the fix, not
// just its effect. The narrowed fix keeps the shared operationKindFamily slice
// free of SCOPE.Service so that neither hintKinds nor the familyMaskByKind /
// memberFamilyMask side-table (issue #6141) changes at all; only
// structuralKindFamilies("operation", "proto") admits the kind.
//
// This is the killing guard for mutant N1 (bare "Service" added alongside the
// scoped form) and N3 (scopeKindPrefix+"Controller" added), which both survived
// the first revision: it asserts the family's exact membership rather than the
// absence of one string.
func TestOperationKindFamilyExcludesService6492(t *testing.T) {
	want := []string{"Operation", "Function", "Method", scopeKindPrefix + "Operation"}
	if len(operationKindFamily) != len(want) {
		t.Fatalf("operationKindFamily = %v, want exactly %v — widening the SHARED "+
			"family changes hintKinds and familyMaskByKind for every language and "+
			"destroys unique matches (#6492)", operationKindFamily, want)
	}
	for i, k := range want {
		if operationKindFamily[i] != k {
			t.Fatalf("operationKindFamily = %v, want exactly %v (#6492)",
				operationKindFamily, want)
		}
	}
	if _, ok := familyMaskByKind[scopeKindPrefix+"Service"]; ok {
		t.Fatal("familyMaskByKind classifies SCOPE.Service; the leaf-name family " +
			"filter must be untouched by the proto-scoped fix (#6492)")
	}
	if _, ok := familyMaskByKind["Service"]; ok {
		t.Fatal("familyMaskByKind classifies bare \"Service\" (#6492)")
	}
	if memberFamilyMask(scopeKindPrefix+"Service") != 0 {
		t.Fatal("memberFamilyMask(SCOPE.Service) != 0; the proto-scoped fix must not " +
			"reclassify Service entities for the leaf-name tiers (#6492)")
	}
}

// TestStructuralKindFamiliesIsLanguageBlind6492 pins the SHAPE of the round-3
// fix on the family side: structuralKindFamilies takes no language argument at
// all any more, and returns the same three untouched slices it did before
// #6459. Rounds 1 and 2 both regressed by widening a family; this asserts that
// no family is widened, by identity.
func TestStructuralKindFamiliesIsLanguageBlind6492(t *testing.T) {
	for _, tc := range []struct {
		scope string
		want  []string
	}{
		{"component", componentKindFamily},
		{"operation", operationKindFamily},
		{"schema", schemaKindFamily},
		{"OPERATION", operationKindFamily},
	} {
		got := structuralKindFamilies(tc.scope)
		if len(got) != len(tc.want) {
			t.Fatalf("structuralKindFamilies(%q) = %v, want exactly %v (#6492)",
				tc.scope, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("structuralKindFamilies(%q) = %v, want exactly %v (#6492)",
					tc.scope, got, tc.want)
			}
		}
	}
	if got := structuralKindFamilies("bogus"); got != nil {
		t.Fatalf("structuralKindFamilies(\"bogus\") = %v, want nil (#6492)", got)
	}
}

// TestProtoServiceTierIsPinnedToProto6492 is the B2 guard: the language
// boundary of the fix, pinned exhaustively rather than at two sample points.
//
// Round 2's gate (`case "proto", "protobuf":`) was UNPINNED — a mutant widening
// it to `case "proto", "protobuf", "go", "java", "typescript":` survived a
// clean `go vet` and the whole suite, because only python and kotlin were ever
// probed. Any language admitted by mistake binds an operation-space ref to a
// SCOPE.Service MARKER (celery task, Spring stereotype, systemd unit) instead
// of the real function or class beside it.
//
// The table below covers every language that emits SCOPE.Service entities or
// operation-space structural refs, plus the empty segment, plus a case-variant
// and an alias to pin normalizeLang's role. For each, the same #6459 fixture is
// indexed and the tier must fire for proto spellings ONLY.
func TestProtoServiceTierIsPinnedToProto6492(t *testing.T) {
	cases := []struct {
		lang      string
		wantProto bool
	}{
		{"proto", true},
		{"protobuf", true},
		{"PROTO", true},   // normalizeLang lowercases
		{" proto ", true}, // normalizeLang trims
		{"", false},
		{"go", false},
		{"golang", false},
		{"java", false},
		{"kotlin", false},
		{"kt", false},
		{"python", false},
		{"py", false},
		{"typescript", false},
		{"ts", false},
		{"javascript", false},
		{"js", false},
		{"ruby", false},
		{"rust", false},
		{"elixir", false},
		{"csharp", false},
		{"vbnet", false},
		{"yaml", false},
		{"scala", false},
		{"swift", false},
		{"php", false},
		{"protocol-buffers", false}, // NOT a spelling the extractor emits
		{"prototype", false},        // substring of "proto" must not match
	}

	// The table itself is the ONLY guard on the language boundary, so the
	// table needs a guard of its own. Deleting its rows and widening
	// isProtoLangSegment to `case "proto", "protobuf", "go", "java",
	// "typescript", "ruby", "rust", "elixir", "csharp", "yaml":` survived the
	// whole suite: only python and kotlin are pinned anywhere else (by
	// TestCeleryTaskCallStillBindsToTheFunction6492 and
	// TestKotlinStereotypeMarkerIsNotAnOperationTarget6492). That unpinned
	// table is exactly how round 2's widening shipped. A row floor plus a
	// required-coverage set makes the table un-deletable (#6492 S6).
	if len(cases) < 20 {
		t.Fatalf("the language table has %d rows, want >= 20 — this table is the "+
			"only exhaustive pin on isProtoLangSegment; thinning it silently "+
			"re-opens the round-2 widening (#6492 S6)", len(cases))
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		seen[tc.lang] = true
	}
	for _, required := range []string{
		// Both proto spellings (positive direction) …
		"proto", "protobuf",
		// … and every language a plausible widening would reach for. Each
		// emits SCOPE.Service entities and/or operation-space structural
		// refs, so each must be pinned NEGATIVE here.
		"go", "golang", "java", "kotlin", "python", "typescript",
		"javascript", "ruby", "rust", "elixir", "csharp", "yaml",
		// … and the empty segment, which normalizeLang must not treat as
		// a proto spelling.
		"",
	} {
		if !seen[required] {
			t.Fatalf("the language table does not cover %q — the required set is "+
				"fixed so a future edit cannot drop a row and let that language "+
				"into the #6459 tier (#6492 S6)", required)
		}
	}

	for _, tc := range cases {
		if got := isProtoLangSegment(tc.lang); got != tc.wantProto {
			t.Fatalf("isProtoLangSegment(%q) = %v, want %v — the #6459 service tier's "+
				"language boundary must admit the proto spellings and NOTHING else (#6492 B2)",
				tc.lang, got, tc.wantProto)
		}

		// End-to-end through lookupStructural, so the pin cannot be satisfied
		// by a predicate nobody consults.
		file := "svc/" + "x.src"
		schema := types.EntityRecord{
			ID: "a1a1a1a1a1a1a1a1", Kind: "SCOPE.Schema", Subtype: "message",
			Name: "Foo", SourceFile: file, Language: tc.lang,
		}
		service := types.EntityRecord{
			ID: "b2b2b2b2b2b2b2b2", Kind: "SCOPE.Service", Subtype: "service",
			Name: "Foo", SourceFile: file, Language: tc.lang,
		}
		idx := BuildIndex([]types.EntityRecord{schema, service})
		ref := extractor.BuildOperationStructuralRef(tc.lang, file, "Foo")
		id, _, _ := idx.lookupStructural(ref)
		if tc.wantProto && id != service.ID {
			t.Fatalf("lookupStructural(%q) = %q, want the service %q — the tier must "+
				"fire for the proto spelling %q (#6492 B2)", ref, id, service.ID, tc.lang)
		}
		if !tc.wantProto && id == service.ID {
			t.Fatalf("lookupStructural(%q) bound the SCOPE.Service %q for language %q; "+
				"the #6459 tier is proto-only — every other emitter of that Kind is a "+
				"MARKER sharing a (file, name) with a real entity (#6492 B2)",
				ref, id, tc.lang)
		}
	}
}

// TestKotlinStereotypeMarkerIsNotAnOperationTarget6492 answers the Kotlin
// question the review raised.
//
// internal/extractors/kotlin/kotlin.go:815 emits Kind "SCOPE.Service" with
// Name = className for Spring stereotypes (@Service / @Component / …). It is a
// MARKER that sits beside the real class in the same file — it is not what a
// scope:operation:method:kotlin:<file>:OrderService ref addresses. Under the
// shared-slice fix such a ref started binding to the stereotype marker; under
// the proto-scoped fix it does not, which is the correct outcome: the Kotlin
// extractor never mints an operation-space ref that MEANS the stereotype.
func TestKotlinStereotypeMarkerIsNotAnOperationTarget6492(t *testing.T) {
	const ktFile = "src/main/kotlin/com/acme/OrderService.kt"
	marker := types.EntityRecord{
		ID: "d4d4d4d4d4d4d4d4", Kind: "SCOPE.Service", Subtype: "service",
		Name: "OrderService", SourceFile: ktFile, Language: "kotlin",
	}
	class := types.EntityRecord{
		ID: "a5a5a5a5a5a5a5a5", Kind: "Class", Subtype: "class",
		Name: "OrderService", SourceFile: ktFile, Language: "kotlin",
	}
	idx := BuildIndex([]types.EntityRecord{marker, class})

	// Non-vacuity guard (#6492 N5). The assertion below is a NEGATIVE — it
	// would pass just as happily against an empty index, or against a typo'd
	// file path that matches nothing. Prove first that this (file, name) is
	// really populated and really addressable: the COMPONENT-space ref for the
	// same pair binds to the real class, which it can only do if BuildIndex
	// recorded both entities at that location.
	componentRef := extractor.BuildComponentStructuralRef("kotlin", ktFile, "OrderService")
	if id, _, handled := idx.lookupStructural(componentRef); !handled || id != class.ID {
		t.Fatalf("premise: lookupStructural(%q) = (%q, handled=%v), want the class %q — "+
			"the fixture is not addressable, so the negative assertion below would be "+
			"vacuous (#6492 N5)", componentRef, id, handled, class.ID)
	}

	ref := extractor.BuildOperationStructuralRef("kotlin", ktFile, "OrderService")
	id, status, handled := idx.lookupStructural(ref)
	if !handled {
		t.Fatalf("lookupStructural did not claim %q (handled=false)", ref)
	}
	if id == marker.ID {
		t.Fatalf("lookupStructural(%q) bound the Spring stereotype MARKER %q; a Kotlin "+
			"operation-space ref must never resolve to a stereotype annotation entity (#6492)",
			ref, id)
	}
	// Pin the actual disposition, not merely "not the marker": the Kotlin
	// extractor mints no operation-space ref that MEANS the stereotype, so the
	// correct outcome is the kind-agnostic ambiguity that existed before #6459
	// — not a silent rebind to some third entity.
	if id != "" || status != statusAmbiguous {
		t.Fatalf("lookupStructural(%q) = (%q, status=%d), want (\"\", statusAmbiguous=%d) "+
			"— the marker and the class share this (file, name) and no operation-family "+
			"kind matches either (#6492 N5)", ref, id, status, statusAmbiguous)
	}
}
