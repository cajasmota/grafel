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

// TestKotlinSpring6499_TopLevelFunctionIsNotAHandlerShape records why this arm
// ships no "top-level handler stays bare" fixture: the shape is UNREACHABLE in
// this pass. walkKotlinClasses descends only into class_declaration nodes, and
// processKotlinSpringClass reads handlers from that class's class_body — so a
// top-level `fun` can never become a Spring handler here, whatever annotation
// it carries, and there is no bare-name branch left to exercise.
//
// This asserts that unreachability rather than asserting on a fixture invented
// to look like it. The positive control is a SEPARATE file whose only
// difference is that the annotated handler sits inside a controller class: it
// routes. The top-level file, with the same annotations and the same
// @RestController marker text present, emits nothing.
func TestKotlinSpring6499_TopLevelFunctionIsNotAHandlerShape(t *testing.T) {
	// Positive control — the in-class shape this pass DOES reach.
	control := detectKotlin6499(t, "src/main/kotlin/Kept.kt", `package io.shipfast.toplevel

import org.springframework.web.bind.annotation.*

@RestController
@RequestMapping("/api/top")
class RealController {

    @GetMapping("/kept")
    fun kept(): String = "ok"
}
`)
	controlTargets := astDrivenRouteTargets(control)
	got, ok := controlTargets["http:GET:/api/top/kept"]
	if !ok {
		t.Fatalf("positive control failed: no ast_driven ROUTES_TO from http:GET:/api/top/kept; got %v", controlTargets)
	}
	if got != "SCOPE.Operation:RealController.kept" {
		t.Errorf("ROUTES_TO target = %q, want %q", got, "SCOPE.Operation:RealController.kept")
	}

	// The same handler, hoisted to file top level. `@RestController` is still
	// present in the file so the pass's cheap byte gate does not short-circuit
	// — the pass really runs and really finds no class to walk.
	loose := detectKotlin6499(t, "src/main/kotlin/Loose.kt", `package io.shipfast.toplevel

import org.springframework.web.bind.annotation.*

// @RestController — deliberately not on a class; nothing here is a handler.

@RequestMapping("/api/top")
@GetMapping("/loose")
fun loose(): String = "nope"
`)
	if looseTargets := astDrivenRouteTargets(loose); len(looseTargets) != 0 {
		t.Errorf("top-level fun produced ast_driven routes %v; the pass is class-scoped "+
			"(walkKotlinClasses descends only into class_declaration) and must not reach it",
			looseTargets)
	}
	for _, e := range loose.Entities {
		if e.Kind == httpEndpointDefinitionKind && e.Properties["pattern_type"] == "ast_driven" &&
			strings.Contains(e.Properties["source_handler"], "loose") {
			t.Errorf("top-level fun produced an ast_driven endpoint with source_handler %q",
				e.Properties["source_handler"])
		}
	}
}
