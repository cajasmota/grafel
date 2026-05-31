package java

import "testing"

// Regression tests for the deferred parens-in-annotation-string site in the
// JAX-RS DTO extractor (follow-up to PR #3425, closes #3561).
//
// Root cause: jaxrsVerbMethodRE / jaxrsResourceClassRE bound a verb annotation
// to its method (and @Path to its class) with a `[^)]*` skip over intervening
// annotations. A Swagger @Operation(summary = "Get (all) widgets") between
// @GET/@Path and the method stopped the skip at the first ')' inside the
// string, so the verb never reached the method declaration and the route's
// request/response DTO relationships were silently dropped.

// jaxrsDTORelsByType returns ACCEPTS_INPUT / RETURNS relationship dto_type
// values keyed by relationship type for the given source.
func jaxrsDTORels(t *testing.T, source string) (accepts, returns []string) {
	t.Helper()
	r := ExtractJakartaJaxrsDTO(PatternContext{
		Source: source, Language: "java", Framework: "jakarta_ee",
		FilePath: "WidgetResource.java",
	})
	for _, rel := range r.Relationships {
		switch rel.RelationshipType {
		case "ACCEPTS_INPUT":
			accepts = append(accepts, rel.Properties["dto_type"])
		case "RETURNS":
			returns = append(returns, rel.Properties["dto_type"])
		}
	}
	return accepts, returns
}

// A JAX-RS resource method with @Operation(summary = "Get (all) widgets")
// between @GET/@Path and the method declaration must still emit its RETURNS
// DTO. A sibling method without the paren proves no undercount.
func TestParenDeferred_JAXRS_OperationParenInString_DTOSurvives(t *testing.T) {
	src := `package com.example;

import jakarta.ws.rs.GET;
import jakarta.ws.rs.POST;
import jakarta.ws.rs.Path;
import jakarta.ws.rs.Produces;

@Path("/widgets")
public class WidgetResource {

    @GET
    @Path("/all")
    @Operation(summary = "Get (all) widgets")
    @Produces("application/json")
    public WidgetList getAll() {
        return repo.findAll();
    }

    @GET
    @Path("/safe")
    @Operation(summary = "Get safe widgets")
    public WidgetList getSafe() {
        return repo.findAll();
    }

    @POST
    @Path("/create")
    @Operation(summary = "Create a widget (v2)")
    public Widget create(CreateWidgetDto dto) {
        return repo.save(dto);
    }
}
`
	accepts, returns := jaxrsDTORels(t, src)

	// The paren-in-@Operation method's return DTO must survive (was dropped).
	if !contains(returns, "WidgetList") {
		t.Errorf("RETURNS dto_types = %v, want to contain WidgetList (paren in @Operation must not drop the route DTO)", returns)
	}
	// The paren-in-@Operation POST's implicit body DTO must survive.
	if !contains(accepts, "CreateWidgetDto") {
		t.Errorf("ACCEPTS_INPUT dto_types = %v, want to contain CreateWidgetDto (paren in @Operation must not drop the body DTO)", accepts)
	}
	// Sibling without a paren — proves the binding still works generally and
	// there is no undercount masking the fix.
	wantReturns := 0
	for _, r := range returns {
		if r == "WidgetList" {
			wantReturns++
		}
	}
	if wantReturns < 2 {
		t.Errorf("expected WidgetList returned by BOTH getAll and getSafe (>=2), got %d in %v", wantReturns, returns)
	}
}

// The @Path class binding must also survive a ')' inside the path-template
// string itself (string-internal paren in @Path("...")).
func TestParenDeferred_JAXRS_PathTemplateParen_ClassBinds(t *testing.T) {
	src := `package com.example;

import jakarta.ws.rs.GET;
import jakarta.ws.rs.Path;

@Path("/items/{id:[0-9()]+}")
public class ItemResource {

    @GET
    @Path("/detail")
    public ItemDetail detail() {
        return repo.detail();
    }
}
`
	_, returns := jaxrsDTORels(t, src)
	if !contains(returns, "ItemDetail") {
		t.Errorf("RETURNS dto_types = %v, want to contain ItemDetail (paren in @Path template must not drop the class binding)", returns)
	}
}
