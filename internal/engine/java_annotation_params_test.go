// Tests for #1936 Phase 1 — Java JAX-RS / Spring full parameter-location
// coverage. Each test focuses on one annotation kind so a regression is
// easy to localise. End-to-end coverage (`parameters` property attached to
// the emitted endpoint entity) is exercised in
// java_annotation_routes_params_test.go.
package engine

import (
	"strings"
	"testing"
)

// findParamByIn returns the first parameter with the given `in` location, or
// nil. Used by every test below to assert specific rows.
func findParamByIn(ps []JavaParam, in string) *JavaParam {
	for i := range ps {
		if ps[i].In == in {
			return &ps[i]
		}
	}
	return nil
}

// findParamByName returns the first parameter with the given wire name.
func findParamByName(ps []JavaParam, name string) *JavaParam {
	for i := range ps {
		if ps[i].Name == name {
			return &ps[i]
		}
	}
	return nil
}

func TestExtractJavaParameters_JAXRSQueryParam(t *testing.T) {
	frag := `@QueryParam("limit") @DefaultValue("20") int limit, @QueryParam("offset") @DefaultValue("0") int offset)`
	got := extractJavaParameters(frag, []string{"GET"})
	if len(got) != 2 {
		t.Fatalf("want 2 query params, got %d (%+v)", len(got), got)
	}
	for _, p := range got {
		if p.In != "query" {
			t.Errorf("want in=query, got %q for %q", p.In, p.Name)
		}
		if p.Required {
			t.Errorf("@DefaultValue should mark %q optional, got required=true", p.Name)
		}
	}
	limit := findParamByName(got, "limit")
	if limit == nil {
		t.Fatalf("missing limit param")
	}
	if limit.DefaultValue != "20" {
		t.Errorf("want default=20, got %q", limit.DefaultValue)
	}
	if limit.Type != "int" {
		t.Errorf("want type=int, got %q", limit.Type)
	}
}

func TestExtractJavaParameters_JAXRSHeaderParam(t *testing.T) {
	frag := `@HeaderParam("X-Request-ID") String requestId, @HeaderParam("Authorization") @NotNull String auth)`
	got := extractJavaParameters(frag, []string{"GET"})
	if len(got) != 2 {
		t.Fatalf("want 2 header params, got %d", len(got))
	}
	req := findParamByName(got, "X-Request-ID")
	if req == nil || req.In != "header" {
		t.Fatalf("want X-Request-ID as header, got %+v", req)
	}
	if !req.Required {
		t.Errorf("@HeaderParam is required by default — got required=false")
	}
	auth := findParamByName(got, "Authorization")
	if auth == nil || !auth.Required {
		t.Errorf("@NotNull should keep Authorization required, got %+v", auth)
	}
}

func TestExtractJavaParameters_JAXRSCookieParam(t *testing.T) {
	frag := `@CookieParam("session") String session)`
	got := extractJavaParameters(frag, []string{"GET"})
	if len(got) != 1 {
		t.Fatalf("want 1 cookie param, got %d", len(got))
	}
	if got[0].In != "cookie" {
		t.Errorf("want in=cookie, got %q", got[0].In)
	}
	if got[0].Name != "session" {
		t.Errorf("want name=session, got %q", got[0].Name)
	}
}

func TestExtractJavaParameters_JAXRSFormParam(t *testing.T) {
	frag := `@FormParam("username") String username, @FormParam("password") String password)`
	got := extractJavaParameters(frag, []string{"POST"})
	if len(got) != 2 {
		t.Fatalf("want 2 form params, got %d", len(got))
	}
	for _, p := range got {
		if p.In != "form" {
			t.Errorf("want in=form, got %q for %q", p.In, p.Name)
		}
	}
}

func TestExtractJavaParameters_JAXRSMatrixParam(t *testing.T) {
	frag := `@MatrixParam("category") String category)`
	got := extractJavaParameters(frag, []string{"GET"})
	if len(got) != 1 || got[0].In != "matrix" {
		t.Fatalf("want 1 matrix param, got %+v", got)
	}
	if got[0].Name != "category" {
		t.Errorf("want name=category, got %q", got[0].Name)
	}
}

func TestExtractJavaParameters_JAXRSMixedPathQueryBody(t *testing.T) {
	// Realistic JAX-RS handler — path + query + implicit body.
	frag := `@PathParam("id") String id, @QueryParam("limit") @DefaultValue("10") int limit, UpdateRequest payload)`
	got := extractJavaParameters(frag, []string{"PUT"})
	if len(got) != 3 {
		t.Fatalf("want 3 params, got %d (%+v)", len(got), got)
	}
	if p := findParamByIn(got, "path"); p == nil || p.Name != "id" {
		t.Errorf("want path id, got %+v", p)
	}
	if p := findParamByIn(got, "query"); p == nil || p.Name != "limit" {
		t.Errorf("want query limit, got %+v", p)
	}
	if p := findParamByIn(got, "body"); p == nil || p.Type != "UpdateRequest" {
		t.Errorf("want body UpdateRequest, got %+v", p)
	}
}

func TestExtractJavaParameters_SpringRequestParamQuery(t *testing.T) {
	frag := `@RequestParam("page") int page, @RequestParam(value = "size", defaultValue = "20") int size)`
	got := extractJavaParameters(frag, []string{"GET"})
	if len(got) != 2 {
		t.Fatalf("want 2 query params, got %d", len(got))
	}
	page := findParamByName(got, "page")
	if page == nil || page.In != "query" {
		t.Fatalf("want page query, got %+v", page)
	}
	if !page.Required {
		t.Errorf("Spring @RequestParam without defaultValue should be required, got %+v", page)
	}
	size := findParamByName(got, "size")
	if size == nil || size.DefaultValue != "20" {
		t.Errorf("want size default=20, got %+v", size)
	}
	if size.Required {
		t.Errorf("Spring @RequestParam with defaultValue should be optional, got required=true")
	}
}

func TestExtractJavaParameters_SpringRequestHeader(t *testing.T) {
	frag := `@RequestHeader("X-Tenant") String tenant, @RequestHeader(value = "X-Trace", required = false) String trace)`
	got := extractJavaParameters(frag, []string{"GET"})
	if len(got) != 2 {
		t.Fatalf("want 2 header params, got %d", len(got))
	}
	tenant := findParamByName(got, "X-Tenant")
	if tenant == nil || !tenant.Required {
		t.Errorf("want X-Tenant required, got %+v", tenant)
	}
	trace := findParamByName(got, "X-Trace")
	if trace == nil || trace.Required {
		t.Errorf("want X-Trace optional (required=false), got %+v", trace)
	}
}

func TestExtractJavaParameters_SpringCookieValue(t *testing.T) {
	frag := `@CookieValue("session") String session)`
	got := extractJavaParameters(frag, []string{"GET"})
	if len(got) != 1 || got[0].In != "cookie" || got[0].Name != "session" {
		t.Fatalf("want cookie session, got %+v", got)
	}
}

func TestExtractJavaParameters_SpringRequestBodyExplicit(t *testing.T) {
	frag := `@RequestBody @Valid CreateOrderRequest req)`
	got := extractJavaParameters(frag, []string{"POST"})
	if len(got) != 1 {
		t.Fatalf("want 1 body param, got %d", len(got))
	}
	if got[0].In != "body" {
		t.Errorf("want in=body, got %q", got[0].In)
	}
	if got[0].Type != "CreateOrderRequest" {
		t.Errorf("want type=CreateOrderRequest, got %q", got[0].Type)
	}
	if !got[0].Required {
		t.Errorf("@RequestBody is required, got required=false")
	}
}

func TestExtractJavaParameters_ContextInjectionsSkipped(t *testing.T) {
	frag := `@Context UriInfo uriInfo, @Context HttpServletRequest req, @QueryParam("q") String q)`
	got := extractJavaParameters(frag, []string{"GET"})
	if len(got) != 1 {
		t.Fatalf("want @Context params skipped, got %d (%+v)", len(got), got)
	}
	if got[0].In != "query" {
		t.Errorf("want query q, got %+v", got[0])
	}
}

func TestExtractJavaParameters_GetVerbDropsImplicitBody(t *testing.T) {
	// A GET handler with an unannotated DTO param — must NOT surface as body.
	frag := `@QueryParam("q") String q, FilterDTO filter)`
	got := extractJavaParameters(frag, []string{"GET"})
	if len(got) != 1 {
		t.Fatalf("want only 1 param (no body on GET), got %d (%+v)", len(got), got)
	}
}

func TestExtractJavaParameters_DefaultValueAndAnnotationsRecorded(t *testing.T) {
	frag := `@QueryParam("limit") @DefaultValue("10") @Min(1) @Max(100) int limit)`
	got := extractJavaParameters(frag, []string{"GET"})
	if len(got) != 1 {
		t.Fatalf("want 1 param, got %d", len(got))
	}
	p := got[0]
	if p.DefaultValue != "10" {
		t.Errorf("want default=10, got %q", p.DefaultValue)
	}
	heads := strings.Join(p.Annotations, ",")
	for _, want := range []string{"@QueryParam", "@DefaultValue", "@Min", "@Max"} {
		if !strings.Contains(heads, want) {
			t.Errorf("annotations missing %s: %s", want, heads)
		}
	}
}

func TestEncodeDecodeJavaParameters_RoundTrip(t *testing.T) {
	in := []JavaParam{
		{Name: "limit", In: "query", Type: "int", Required: false, DefaultValue: "10", Annotations: []string{"@QueryParam", "@DefaultValue"}},
		{Name: "X-Trace", In: "header", Type: "String", Required: true, Annotations: []string{"@HeaderParam"}},
	}
	enc := EncodeJavaParameters(in)
	if enc == "" {
		t.Fatalf("encode produced empty string")
	}
	out := DecodeJavaParameters(enc)
	if len(out) != len(in) {
		t.Fatalf("round trip length mismatch: in=%d out=%d", len(in), len(out))
	}
	for i := range in {
		if in[i].Name != out[i].Name || in[i].In != out[i].In || in[i].Required != out[i].Required {
			t.Errorf("round trip mismatch at %d: in=%+v out=%+v", i, in[i], out[i])
		}
	}
}

func TestEncodeJavaParameters_EmptyReturnsEmpty(t *testing.T) {
	if got := EncodeJavaParameters(nil); got != "" {
		t.Errorf("empty input should encode to \"\", got %q", got)
	}
}
