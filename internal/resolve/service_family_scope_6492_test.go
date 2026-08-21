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

// TestProtoOperationFamilyIsTheOnlyWidening6492 is the positive-direction
// killing guard: it pins that the widening exists, that it is confined to the
// proto language segment of the structural ref, and that it adds EXACTLY
// SCOPE.Service (mutant N3's `SCOPE.Controller` dies here, mutant N1's bare
// "Service" dies here and in the mask assertions above).
func TestProtoOperationFamilyIsTheOnlyWidening6492(t *testing.T) {
	base := structuralKindFamilies("operation", "python")
	proto := structuralKindFamilies("operation", "proto")

	if len(proto) != len(base)+1 {
		t.Fatalf("structuralKindFamilies(\"operation\", \"proto\") = %v, want exactly "+
			"one member more than the %v the other languages get (#6492)", proto, base)
	}
	for i := range base {
		if proto[i] != base[i] {
			t.Fatalf("proto operation family %v does not extend the base family %v (#6492)",
				proto, base)
		}
	}
	if got := proto[len(proto)-1]; got != scopeKindPrefix+"Service" {
		t.Fatalf("proto operation family's extra member = %q, want %q (#6492)",
			got, scopeKindPrefix+"Service")
	}
	// Non-operation scope kinds must not gain anything from the language argument.
	for _, scope := range []string{"component", "schema"} {
		a := structuralKindFamilies(scope, "proto")
		b := structuralKindFamilies(scope, "python")
		if len(a) != len(b) {
			t.Fatalf("structuralKindFamilies(%q, …) is language-sensitive: %v vs %v (#6492)",
				scope, a, b)
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

	ref := extractor.BuildOperationStructuralRef("kotlin", ktFile, "OrderService")
	if id, _, _ := idx.lookupStructural(ref); id == marker.ID {
		t.Fatalf("lookupStructural(%q) bound the Spring stereotype MARKER %q; a Kotlin "+
			"operation-space ref must never resolve to a stereotype annotation entity (#6492)",
			ref, id)
	}
}
