// spring_routes_kotlin_6499_test.go — arm 3 of #6499.
//
// Arms 1+2 (b1dbdf010) made every Kotlin SCOPE.Operation declared inside a
// class / object / interface body CLASS-QUALIFIED (`UserController.health`).
// spring_routes_kotlin.go still addressed its handler by the BARE leaf name at
// two sites — the `source_handler` property and the ROUTES_TO `ToID` — so both
// named an entity that no longer exists and the route→handler hop dangled.
//
// This file pins the post-arm-3 shape: both sites address the handler as
// `SCOPE.Operation:<Class>.<method>`, the same string Java's AST-driven route
// pass emits post-#6429 (spring_routes.go), and the edge RESOLVES to the real
// handler entity rather than being excused as runtime-dynamic.
//
// Every test asserts a positive control (the edge/entity exists at all) before
// asserting anything about its target, so a fixture that stopped parsing cannot
// masquerade as a pass.
package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/kotlin"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
)

const kotlinUserControllerSrc6499 = `package io.shipfast.users

import org.springframework.web.bind.annotation.*

@RestController
@RequestMapping("/api/users")
class UserController {

    @GetMapping("/health")
    fun health(): String = "ok"
}
`

const kotlinUserControllerPath6499 = "src/main/kotlin/io/shipfast/users/UserController.kt"

// detectKotlin6499 runs the full detector pass over a Kotlin source file.
func detectKotlin6499(t *testing.T, path, src string) *DetectResult {
	t.Helper()
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	result, err := New(rules).Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "kotlin",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return result
}

// astDrivenRouteTargets returns the ToIDs of every ast_driven spring_mvc
// ROUTES_TO edge in a detect result, keyed by the edge's FromID.
func astDrivenRouteTargets(result *DetectResult) map[string]string {
	out := map[string]string{}
	for _, r := range result.Relationships {
		if r.Kind != "ROUTES_TO" {
			continue
		}
		pt := ""
		fw := ""
		for _, p := range r.Properties {
			switch p.K {
			case "pattern_type":
				pt = p.V
			case "framework":
				fw = p.V
			}
		}
		if pt == "ast_driven" && fw == "spring_mvc" {
			out[r.FromID] = r.ToID
		}
	}
	return out
}

// topChildTypes6499 lists a root node's immediate child types, for failure
// messages that need to show HOW a fixture parsed.
func topChildTypes6499(root ts.Node) string {
	var b strings.Builder
	for i := 0; i < int(root.ChildCount()); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(root.Child(i).Type())
	}
	return b.String()
}

// TestKotlinSpring6499_RouteEdgeTargetsQualifiedHandler pins edit site 2 (the
// ROUTES_TO ToID) and edit site 1 (the `source_handler` property) on the same
// fixture, after a positive control that the edge exists at all.
func TestKotlinSpring6499_RouteEdgeTargetsQualifiedHandler(t *testing.T) {
	result := detectKotlin6499(t, kotlinUserControllerPath6499, kotlinUserControllerSrc6499)

	// Positive control — the endpoint entity and its ROUTES_TO edge exist.
	targets := astDrivenRouteTargets(result)
	got, ok := targets["http:GET:/api/users/health"]
	if !ok {
		t.Fatalf("positive control failed: no ast_driven ROUTES_TO from http:GET:/api/users/health; got %v", targets)
	}

	const want = "SCOPE.Operation:UserController.health"
	if got != want {
		t.Errorf("ROUTES_TO target = %q, want %q", got, want)
	}
	if got == "Controller:health" {
		t.Errorf("ROUTES_TO still targets the pre-#6499 bare stub %q", got)
	}

	// Edit site 1 — the `source_handler` property on the endpoint entity.
	var sh string
	found := false
	for _, e := range result.Entities {
		if e.Kind == httpEndpointDefinitionKind && e.ID == "http:GET:/api/users/health" {
			sh = e.Properties["source_handler"]
			found = true
		}
	}
	if !found {
		t.Fatalf("positive control failed: no http_endpoint_definition http:GET:/api/users/health")
	}
	if sh != want {
		t.Errorf("source_handler = %q, want %q", sh, want)
	}
}

// TestKotlinSpring6499_RouteEdgeResolvesToHandlerEntity is the end-to-end
// claim: the emitted stub is not merely qualified, it BINDS. The Kotlin
// extractor's SCOPE.Operation entities are indexed exactly as the pipeline
// indexes them, References() is run over the real ROUTES_TO edge, and the
// resolved ToID is compared against the handler entity's own graph ID.
//
// This is the assertion that distinguishes "resolved" from "excused": a stub
// that classifyDispositionLang waved through as DispositionDynamic would come
// back from References() unchanged, still spelled as a stub.
func TestKotlinSpring6499_RouteEdgeResolvesToHandlerEntity(t *testing.T) {
	// The handler entities, exactly as the Kotlin extractor emits them.
	pr, err := treesitter.NewParserFactory(nil).
		Parse(context.Background(), []byte(kotlinUserControllerSrc6499), "kotlin")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer pr.TSTree.Close()
	ext, ok := extractor.Get("kotlin")
	if !ok {
		t.Fatalf("positive control failed: no registered kotlin extractor")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     kotlinUserControllerPath6499,
		Content:  []byte(kotlinUserControllerSrc6499),
		Language: "kotlin",
		TSTree:   pr.TSTree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Positive control — the qualified handler entity is what we will bind to.
	handlerID := ""
	for i := range ents {
		// The pipeline stamps IDs before the resolve pass; BuildIndex skips
		// ID-less entities.
		ents[i].ID = graph.EntityID("", ents[i].Kind, ents[i].Name, ents[i].SourceFile)
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Name == "UserController.health" {
			handlerID = ents[i].ID
		}
	}
	if handlerID == "" {
		var names []string
		for i := range ents {
			if ents[i].Kind == "SCOPE.Operation" {
				names = append(names, ents[i].Name)
			}
		}
		t.Fatalf("positive control failed: no SCOPE.Operation UserController.health; got %v", names)
	}

	// The route edge the pass under test actually emits.
	result := detectKotlin6499(t, kotlinUserControllerPath6499, kotlinUserControllerSrc6499)
	var routeRel *types.RelationshipRecord
	for i := range result.Relationships {
		r := &result.Relationships[i]
		if r.Kind == "ROUTES_TO" && r.FromID == "http:GET:/api/users/health" {
			routeRel = r
			break
		}
	}
	if routeRel == nil {
		t.Fatalf("positive control failed: no ROUTES_TO edge from http:GET:/api/users/health")
	}
	stub := routeRel.ToID

	rels := []types.RelationshipRecord{*routeRel}
	stats := resolve.References(rels, resolve.BuildIndex(ents))

	// RESOLVED, not merely EXCUSED. classifyDispositionLang's `Controller:`
	// hatch (refs.go) accounts an unbindable Spring stub as intrinsically
	// runtime-dynamic, which reads as "fine" in the metrics while the edge
	// dangles. The classifier is only reached after resolution has already
	// failed (it opens `if isHexID(resolvedID) { return DispositionResolved }`
	// and never writes a ToID), so a genuine bind must land in Resolved and
	// leave Dynamic empty.
	if n := stats.DispositionCounts[resolve.DispositionResolved]; n != 1 {
		t.Errorf("DispositionResolved = %d, want 1 (counts: %v)", n, stats.DispositionCounts)
	}
	if n := stats.DispositionCounts[resolve.DispositionDynamic]; n != 0 {
		t.Errorf("DispositionDynamic = %d, want 0 — the edge is being EXCUSED, not bound (counts: %v)",
			n, stats.DispositionCounts)
	}

	if rels[0].ToID == stub {
		t.Fatalf("ROUTES_TO stub %q did not resolve: References() left it unchanged "+
			"(a dangling edge the resolver can only excuse, not bind)", stub)
	}
	if rels[0].ToID != handlerID {
		t.Errorf("ROUTES_TO resolved to %q, want the SCOPE.Operation UserController.health entity ID %q",
			rels[0].ToID, handlerID)
	}
}

// TestKotlinSpring6499_NestedControllerQualifiedByInnermostClass pins WHICH
// class name qualifies the handler: the innermost enclosing class_declaration,
// matching kotlinQualifiedFuncName's innermost-wins rule in the extractor. A
// mutant that reached for the outer class would produce `Outer.ping` here and
// name no entity.
func TestKotlinSpring6499_NestedControllerQualifiedByInnermostClass(t *testing.T) {
	src := `package io.shipfast.nested

import org.springframework.web.bind.annotation.*

class Outer {

    @RestController
    @RequestMapping("/api/inner")
    class InnerController {

        @GetMapping("/ping")
        fun ping(): String = "pong"
    }
}
`
	result := detectKotlin6499(t, "src/main/kotlin/Outer.kt", src)
	targets := astDrivenRouteTargets(result)
	got, ok := targets["http:GET:/api/inner/ping"]
	if !ok {
		t.Fatalf("positive control failed: no ast_driven ROUTES_TO from http:GET:/api/inner/ping; got %v", targets)
	}
	if got != "SCOPE.Operation:InnerController.ping" {
		t.Errorf("ROUTES_TO target = %q, want %q", got, "SCOPE.Operation:InnerController.ping")
	}
}

// scopedControllerSrc6499 renders the SAME controller under a chosen Kotlin
// declaration keyword. `class` yields a class_declaration; `object` yields an
// object_declaration. Everything else — annotations, paths, the handler
// function, the member name — is byte-identical, so the declaration node type
// is the ONLY variable between the two renderings.
func scopedControllerSrc6499(keyword string) string {
	return `package io.shipfast.scoped

import org.springframework.web.bind.annotation.*

@RestController
@RequestMapping("/api/scoped")
` + keyword + ` ScopedController {

    @GetMapping("/ping")
    fun ping(): String = "pong"
}
`
}

// TestKotlinSpring6499_PassIsScopedToClassDeclarations pins the reach of this
// pass — the reason arm 3 ships no "top-level handler stays bare" fixture.
//
// walkKotlinClasses descends only into `class_declaration`, and
// processKotlinSpringClass reads handlers from that class's class_body. So the
// only handler shape this pass can address is a method of a class, and the
// bare-name branch the pre-#6499 code needed has no reachable caller left.
//
// The discriminator is one KEYWORD. Both renderings are valid Kotlin that
// tree-sitter parses into a proper declaration node (`class_declaration` vs
// `object_declaration`); both carry the same @RestController, the same
// class-level @RequestMapping and the same annotated `ping` handler. Only the
// class version routes.
//
// An earlier version of this test used an annotated TOP-LEVEL `fun` as the
// negative. That fixture was vacuous: tree-sitter-kotlin does not parse an
// annotated top-level function as a function_declaration at all (it degrades to
// a prefix_expression), so the absent route was attributable to the parse, not
// to the walk's scoping — the assertion would have held even if the walk
// visited every node in the file. The object/class pair cannot be confused that
// way, and the same-file control below proves it.
//
// NOTE this pins the pass's CURRENT reach, not a desirable end state: a Kotlin
// `object` Spring controller is unusual but legal, and this pass does not see
// it. Recorded as a boundary, not endorsed. #6736 tracks a larger reach hole in
// the same walk.
func TestKotlinSpring6499_PassIsScopedToClassDeclarations(t *testing.T) {
	// Positive control — the class rendering routes, with the qualified target.
	classTargets := astDrivenRouteTargets(
		detectKotlin6499(t, "src/main/kotlin/ScopedController.kt", scopedControllerSrc6499("class")))
	got, ok := classTargets["http:GET:/api/scoped/ping"]
	if !ok {
		t.Fatalf("positive control failed: the class rendering emitted no ast_driven ROUTES_TO "+
			"from http:GET:/api/scoped/ping; got %v", classTargets)
	}
	if got != "SCOPE.Operation:ScopedController.ping" {
		t.Errorf("ROUTES_TO target = %q, want %q", got, "SCOPE.Operation:ScopedController.ping")
	}

	objSrc := scopedControllerSrc6499("object")

	// Same-file control — the object rendering PARSES and really does contain
	// the annotated handler, which the Kotlin extractor lands as a qualified
	// SCOPE.Operation. Without this, "no route" could not be told apart from
	// "the fixture did not parse" — the exact hole the top-level `fun` version
	// of this test fell into.
	pr, err := treesitter.NewParserFactory(nil).
		Parse(context.Background(), []byte(objSrc), "kotlin")
	if err != nil {
		t.Fatalf("object rendering failed to parse: %v", err)
	}
	defer pr.TSTree.Close()
	root := pr.TSTree.RootNode()
	foundObjDecl := false
	for i := 0; i < int(root.ChildCount()); i++ {
		switch root.Child(i).Type() {
		case "object_declaration":
			foundObjDecl = true
		case "class_declaration":
			t.Fatalf("fixture control failed: the object rendering parsed a class_declaration; "+
				"the two renderings are no longer distinguished by declaration kind (top children: %s)",
				topChildTypes6499(root))
		}
	}
	if !foundObjDecl {
		t.Fatalf("fixture control failed: no object_declaration in the object rendering — it did not "+
			"parse as a declaration, so a missing route would prove nothing about scoping "+
			"(top children: %s)", topChildTypes6499(root))
	}
	ext, ok := extractor.Get("kotlin")
	if !ok {
		t.Fatalf("positive control failed: no registered kotlin extractor")
	}
	objEnts, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "src/main/kotlin/ScopedObject.kt", Content: []byte(objSrc),
		Language: "kotlin", TSTree: pr.TSTree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	handlerSeen := false
	for i := range objEnts {
		if objEnts[i].Kind == "SCOPE.Operation" && objEnts[i].Name == "ScopedController.ping" {
			handlerSeen = true
		}
	}
	if !handlerSeen {
		t.Fatalf("fixture control failed: the object rendering yielded no SCOPE.Operation " +
			"ScopedController.ping, so its handler is not a real declaration in this fixture")
	}

	// The claim: an object_declaration carrying the identical controller
	// annotations produces NO ast_driven route, because the walk never reaches
	// it — not because anything about the file failed to parse.
	objTargets := astDrivenRouteTargets(
		detectKotlin6499(t, "src/main/kotlin/ScopedObject.kt", objSrc))
	if len(objTargets) != 0 {
		t.Errorf("object rendering produced ast_driven routes %v; walkKotlinClasses descends only "+
			"into class_declaration and must not reach an object_declaration", objTargets)
	}
	for _, e := range detectKotlin6499(t, "src/main/kotlin/ScopedObject.kt", objSrc).Entities {
		if e.Kind == httpEndpointDefinitionKind && e.Properties["pattern_type"] == "ast_driven" &&
			strings.Contains(e.Properties["source_handler"], "ping") {
			t.Errorf("object rendering produced an ast_driven endpoint with source_handler %q",
				e.Properties["source_handler"])
		}
	}
}
