// End-to-end test for #1936 Phase 1 — assert that ApplyJavaAnnotationRoutes
// emits a `parameters` JSON property on the synthetic endpoint entity that
// covers ALL parameter locations (path / query / header / cookie / form /
// matrix / body) on a representative JAX-RS + Spring controller pair.
package engine

import (
	"strings"
	"testing"
)

// TestApplyJavaAnnotationRoutes_Issue1936_JAXRSAllLocations exercises a
// fictional client-fixture-X JAX-RS resource that uses every supported
// parameter location plus a body row.
func TestApplyJavaAnnotationRoutes_Issue1936_JAXRSAllLocations(t *testing.T) {
	src := `package com.client_fixture_x.banking;
import jakarta.ws.rs.*;
import jakarta.ws.rs.core.Response;
import jakarta.validation.Valid;

@Path("/transfers")
public class TransfersResource {

    @PUT
    @Path("/confirm/{transferId}")
    public Response confirm(
            @PathParam("transferId") String id,
            @QueryParam("dryRun") @DefaultValue("false") boolean dryRun,
            @HeaderParam("X-Request-ID") String requestId,
            @CookieParam("session") String session,
            @MatrixParam("priority") String priority,
            @Valid ConfirmRequest body) {
        return Response.ok().build();
    }
}
`
	got := collect(t, map[string]string{"TransfersResource.java": src})
	var put *recordLike
	for i := range got {
		if got[i].ID == "http:PUT:/transfers/confirm/{transferId}" {
			put = &got[i]
			break
		}
	}
	if put == nil {
		t.Fatalf("[#1936] PUT endpoint missing; ids=%v", endpointIDs(got))
	}
	raw := put.Props["parameters"]
	if raw == "" {
		t.Fatalf("[#1936] expected parameters JSON, got empty")
	}
	params := DecodeJavaParameters(raw)
	if len(params) < 6 {
		t.Fatalf("[#1936] expected at least 6 param rows, got %d (%s)", len(params), raw)
	}

	wantByIn := map[string]string{
		"path":   "transferId",
		"query":  "dryRun",
		"header": "X-Request-ID",
		"cookie": "session",
		"matrix": "priority",
		"body":   "body",
	}
	for in, wantName := range wantByIn {
		var found *JavaParam
		for i := range params {
			if params[i].In == in {
				found = &params[i]
				break
			}
		}
		if found == nil {
			t.Errorf("[#1936] missing %s param row", in)
			continue
		}
		if found.Name != wantName {
			t.Errorf("[#1936] %s row name = %q, want %q", in, found.Name, wantName)
		}
	}
	// Default value captured.
	for _, p := range params {
		if p.Name == "dryRun" && p.DefaultValue != "false" {
			t.Errorf("[#1936] dryRun default = %q, want false", p.DefaultValue)
		}
	}
}

// TestApplyJavaAnnotationRoutes_Issue1936_SpringQueryHeaderRequired covers
// Spring annotations (@RequestParam / @RequestHeader / @PathVariable /
// @CookieValue) including the required-vs-optional semantics.
func TestApplyJavaAnnotationRoutes_Issue1936_SpringQueryHeaderRequired(t *testing.T) {
	src := `package com.client_fixture_x.api;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/orders")
public class OrderController {

    @GetMapping("/{id}")
    public OrderResponse get(
            @PathVariable("id") String id,
            @RequestParam(value = "expand", defaultValue = "false") boolean expand,
            @RequestHeader("X-Tenant") String tenant,
            @RequestHeader(value = "X-Trace", required = false) String trace,
            @CookieValue("session") String session) {
        return null;
    }
}
`
	got := collect(t, map[string]string{"OrderController.java": src})
	var ep *recordLike
	for i := range got {
		if got[i].ID == "http:GET:/api/orders/{id}" {
			ep = &got[i]
			break
		}
	}
	if ep == nil {
		t.Fatalf("[#1936] GET endpoint missing; ids=%v", endpointIDs(got))
	}
	raw := ep.Props["parameters"]
	if raw == "" {
		t.Fatalf("[#1936] expected parameters JSON, got empty")
	}
	params := DecodeJavaParameters(raw)

	byName := map[string]JavaParam{}
	for _, p := range params {
		byName[p.Name] = p
	}
	if p, ok := byName["id"]; !ok || p.In != "path" || !p.Required {
		t.Errorf("[#1936] id row wrong: %+v", p)
	}
	if p, ok := byName["expand"]; !ok || p.In != "query" || p.Required || p.DefaultValue != "false" {
		t.Errorf("[#1936] expand row wrong: %+v", p)
	}
	if p, ok := byName["X-Tenant"]; !ok || p.In != "header" || !p.Required {
		t.Errorf("[#1936] X-Tenant row wrong: %+v", p)
	}
	if p, ok := byName["X-Trace"]; !ok || p.In != "header" || p.Required {
		t.Errorf("[#1936] X-Trace should be optional: %+v", p)
	}
	if p, ok := byName["session"]; !ok || p.In != "cookie" {
		t.Errorf("[#1936] session row wrong: %+v", p)
	}
}

// TestApplyJavaAnnotationRoutes_Issue1936_BodyRowExcludedFromGet asserts that
// a single controller class with both GET and POST handlers does not leak
// the POST body row into the GET endpoint's parameter list when the body
// param is an unannotated DTO that the GET would otherwise misclassify.
func TestApplyJavaAnnotationRoutes_Issue1936_BodyRowExcludedFromGet(t *testing.T) {
	src := `package com.client_fixture_x.api;
import jakarta.ws.rs.*;

@Path("/items")
public class ItemsResource {

    @GET
    public Object list(@QueryParam("q") String q, FilterDTO filter) { return null; }

    @POST
    public Object create(CreateItemRequest req) { return null; }
}
`
	got := collect(t, map[string]string{"ItemsResource.java": src})
	for _, r := range got {
		raw := r.Props["parameters"]
		if raw == "" {
			continue
		}
		params := DecodeJavaParameters(raw)
		if strings.HasPrefix(r.ID, "http:GET:") {
			for _, p := range params {
				if p.In == "body" {
					t.Errorf("[#1936] GET endpoint %s should not have body param: %+v", r.ID, p)
				}
			}
		}
		if strings.HasPrefix(r.ID, "http:POST:") {
			hasBody := false
			for _, p := range params {
				if p.In == "body" {
					hasBody = true
					if p.Type != "CreateItemRequest" {
						t.Errorf("[#1936] POST body type = %q, want CreateItemRequest", p.Type)
					}
				}
			}
			if !hasBody {
				t.Errorf("[#1936] POST endpoint %s missing body param row", r.ID)
			}
		}
	}
}
