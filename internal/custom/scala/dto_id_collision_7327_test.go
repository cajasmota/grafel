package scala_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/scala"
)

// Issue #7327 — types.ComputeID hashes OrgID+ProjectID+SourceFile+Kind+Name and
// does NOT hash Subtype. Two custom-scala passes used to emit the SAME bare
// case-class name under the SAME kind (SCOPE.Type) in the same file:
//
//	type_system.go  -> SCOPE.Type / case_class / "CreateUserRequest"
//	validation.go   -> SCOPE.Type / dto        / "CreateUserRequest"
//
// so both got the same id — measured for the with_fields fixture below, on the
// pre-fix tree: id=5076c47caf7ccb8f. Both survive MergeWithCustom (nothing in the
// write path enforces entity-ID uniqueness), and every downstream
// byID[e.ID] = &e consumer is silent last-writer-wins by slice order.
//
// The fix honours the intent validation.go:16-17 already states — "Every entity
// carries a synthetic, non-colliding name ... so it never shadows a real
// class/function node" — by giving the parent DTO the same synthetic
// "<subtype>:<name>" shape its own dto_field children already use.

// The fixtures are deliberately minimal: ONE case class, no companion object, no
// validation annotation. extractScalaDTOFields is unconditional, so this alone
// produced the duplicate id.
//
// There are two, because extractScalaDTOFields has TWO makeEntity call sites for
// the parent DTO — one for a case class WITH constructor fields and a separate
// parameterless branch — and a fixture exercising only one of them would leave
// the other ungraded.
var scalaDTOCollisionFixtures = []struct {
	name string // subtest name
	path string
	src  string
}{
	{
		name: "with_fields",
		path: "demo/CreateUserRequest.scala",
		src: `package demo

case class CreateUserRequest(name: String, age: Int)
`,
	},
	{
		// Parameterless case class -> the len(fields) == 0 branch.
		// The path differs from with_fields on purpose: SourceFile is a ComputeID
		// input, so distinct paths keep the two fixtures' ids independent.
		name: "parameterless",
		path: "demo/CreateUserRequestEmpty.scala",
		src: `package demo

case class CreateUserRequest()
`,
	},
}

// runScalaExtractor runs one registered extractor and returns full EntityRecords
// (the shared entitySummary helper in extractors_test.go drops ID, which is the
// field under test here).
func runScalaExtractor(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extractor %q: %v", name, err)
	}
	return ents
}

// runAllScalaCustomExtractors runs every registered custom_scala_* extractor over
// one file, exactly as RunCustomExtractors does for a real index, and returns the
// concatenated records. Enumerating the registry (rather than naming the two
// implicated passes) is what makes the uniqueness assertion below a property of
// the whole language pack, not of one pair.
func runAllScalaCustomExtractors(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	file := extreg.FileInput{Path: path, Language: "scala", Content: []byte(src)}
	var names []string
	for _, n := range extreg.List() {
		if strings.HasPrefix(n, "custom_scala_") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) < 2 {
		t.Fatalf("expected the custom_scala_* pack to be registered, got %v", names)
	}
	var out []types.EntityRecord
	for _, n := range names {
		out = append(out, runScalaExtractor(t, n, file)...)
	}
	if len(out) == 0 {
		t.Fatalf("no entities extracted from %q — fixture or registration is broken", path)
	}
	return out
}

func describeEntity(e types.EntityRecord) string {
	return fmt.Sprintf("%s/%s %q (line %d)", e.Kind, e.Subtype, e.Name, e.StartLine)
}

// TestScalaCaseClassDTOAndTypeEntitiesHaveDistinctIDs pins the specific reported
// collision: the dto facet and the case_class facet of ONE case class must not
// share an entity ID.
//
// Pre-fix this fails: both are SCOPE.Type/"CreateUserRequest" -> 2a488031fd89facd.
func TestScalaCaseClassDTOAndTypeEntitiesHaveDistinctIDs(t *testing.T) {
	for _, fx := range scalaDTOCollisionFixtures {
		t.Run(fx.name, func(t *testing.T) {
			assertDTOAndCaseClassIDsDiffer(t, fx.name, fx.path, fx.src)
		})
	}
}

func assertDTOAndCaseClassIDsDiffer(t *testing.T, fixture, path, src string) {
	t.Helper()
	ents := runAllScalaCustomExtractors(t, path, src)

	var dto, caseClass *types.EntityRecord
	for i := range ents {
		switch {
		case ents[i].Subtype == "dto" && strings.Contains(ents[i].Name, "CreateUserRequest"):
			dto = &ents[i]
		case ents[i].Subtype == "case_class" && ents[i].Name == "CreateUserRequest":
			caseClass = &ents[i]
		}
	}
	if dto == nil {
		t.Fatalf("no dto entity for CreateUserRequest; got %s", formatEntities(ents))
	}
	if caseClass == nil {
		t.Fatalf("no case_class entity for CreateUserRequest; got %s", formatEntities(ents))
	}
	if dto.ID == "" || caseClass.ID == "" {
		t.Fatalf("entity IDs must be computed at construction: dto=%q case_class=%q", dto.ID, caseClass.ID)
	}
	// Prove which makeEntity branch this fixture reached, so a regex change that
	// silently routed both fixtures through the same branch cannot leave the
	// other call site ungraded while the suite stays green.
	_, hasFieldCount := dto.Properties["field_count"]
	switch fixture {
	case "with_fields":
		if !hasFieldCount {
			t.Fatalf("fixture %q was meant to reach the fields branch, but the dto entity has no field_count: %v", fixture, dto.Properties)
		}
	case "parameterless":
		if hasFieldCount {
			t.Fatalf("fixture %q was meant to reach the parameterless branch, but the dto entity has field_count=%s", fixture, dto.Properties["field_count"])
		}
	default:
		t.Fatalf("unknown fixture %q — every fixture must declare which branch it grades", fixture)
	}

	if dto.ID == caseClass.ID {
		t.Fatalf("issue #7327: dto and case_class facets of one case class share id %s\n  %s\n  %s\n"+
			"ComputeID does not hash Subtype, so the parent DTO must carry a synthetic name",
			dto.ID, describeEntity(*dto), describeEntity(*caseClass))
	}

	// The synthetic name must still be traceable back to the class it models,
	// otherwise "distinct" could be satisfied by an opaque or unstable name.
	if !strings.Contains(dto.Name, "CreateUserRequest") {
		t.Fatalf("dto entity name %q no longer names the case class it models", dto.Name)
	}
	// ...and it must not be the bare class name, which is what collided.
	if dto.Name == "CreateUserRequest" {
		t.Fatalf("dto entity still uses the bare case-class name %q", dto.Name)
	}
	// The exact spelling is pinned HERE, not only in frameworks_test.go. Those
	// assertions are about codecs and nullability and happen to name the entity;
	// a later refactor that looked the DTO up by its "dto" property instead would
	// silently leave the chosen prefix with no coverage at all.
	if dto.Name != "dto:CreateUserRequest" {
		t.Fatalf("dto entity name = %q, want %q (the synthetic <subtype>:<name> form "+
			"shared with the dto_field children)", dto.Name, "dto:CreateUserRequest")
	}
	// The bare class name is what consumers join on (dto_field children already
	// carry it as "dto"), so renaming the entity must not lose it.
	if dto.Properties["dto"] != "CreateUserRequest" {
		t.Fatalf("dto entity no longer carries the bare class name as the \"dto\" property: %v", dto.Properties)
	}
}

// TestScalaCustomEntityIDsAreUniquePerFile grades the INTENT rather than the one
// reported pair: no two entities emitted by the custom_scala_* pack for a single
// file may share an entity ID. ComputeID ignores Subtype, so a duplicate ID here
// always means two facets are competing for the same byID slot downstream.
//
// Pre-fix this fails on the same dto/case_class pair, so it is not vacuous.
//
// SCOPE NOTE — this pins the property for THESE fixtures, not for Scala at large.
// A whole FAMILY of the same defect class survives this fix and is deliberately out
// of scope here: the Scala companion-object idiom `X ... ; object X`. type_system.go
// emits the companion as SCOPE.Type/object under the bare name, which collides with
// whatever SCOPE.Type record the declaration itself produced. Measured, one probe
// per row, identical at merge base aeb69817c and at this commit (type_system.go is
// untouched by this fix):
//
//	case class Foo(a: Int) + object Foo   -> case_class   vs object  id 3fdde75322e4825a
//	class Bar(a: Int)      + object Bar   -> class        vs object  id 4debf40abf1a9dff
//	sealed trait Baz       + object Baz   -> sealed_trait vs object  id c334cb648a0b7a48
//	enum Col { case Red }  + object Col   -> enum         vs object  id 7b48ed3234d42271
//
// The enum row is syntax-conditional and the other three are not: the braced Scala 3
// form above collides, while `enum Col:` (indentation form) and `enum Col(val i: Int)
// { ... }` produced NO duplicate in the same probe — the enum recogniser simply does
// not see them. That is a separate recall gap, noted so the #7310 sizing is not read
// off the braced case alone.
//
// In every row both names are real declaration names, so the remedy is the #7310
// taxonomy decision, not a synthetic rename — which is why the fixtures above carry
// no companion object. Adding one (of ANY of those four shapes) will fail this test
// for that unrelated, unfixed reason.
func TestScalaCustomEntityIDsAreUniquePerFile(t *testing.T) {
	for _, fx := range scalaDTOCollisionFixtures {
		t.Run(fx.name, func(t *testing.T) {
			assertScalaEntityIDsUnique(t, fx.path, fx.src)
		})
	}
}

func assertScalaEntityIDsUnique(t *testing.T, path, src string) {
	t.Helper()
	ents := runAllScalaCustomExtractors(t, path, src)

	byID := map[string][]types.EntityRecord{}
	for _, e := range ents {
		byID[e.ID] = append(byID[e.ID], e)
	}
	var dupes []string
	for id, group := range byID {
		if len(group) < 2 {
			continue
		}
		var lines []string
		for _, e := range group {
			lines = append(lines, "    "+describeEntity(e))
		}
		sort.Strings(lines)
		dupes = append(dupes, fmt.Sprintf("  id %s shared by %d entities:\n%s",
			id, len(group), strings.Join(lines, "\n")))
	}
	if len(dupes) > 0 {
		sort.Strings(dupes)
		t.Fatalf("issue #7327: %d entity id(s) are shared within one file:\n%s",
			len(dupes), strings.Join(dupes, "\n"))
	}
}

func formatEntities(ents []types.EntityRecord) string {
	var lines []string
	for _, e := range ents {
		lines = append(lines, "\n    "+describeEntity(e)+" id="+e.ID)
	}
	sort.Strings(lines)
	return strings.Join(lines, "")
}
