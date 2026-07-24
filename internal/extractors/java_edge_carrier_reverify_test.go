package extractors

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// java_edge_carrier_reverify_test.go — live-path re-verification for #3605 (epic
// #3584).
//
// #3589 had to leave the jaxrs/CDI di_injection_point cell `missing` and the
// bean-validation nested_model_extraction cell `partial` because the dispatcher
// (`patternResultToRecords` in internal/custom/java/patterns_dispatch.go) DROPPED
// any Relationship whose SourceRef had no emitted carrier entity. The
// di_injection_point DEPENDS_ON edge (SourceRef `scope:dependency:jakarta:…`, the
// injecting class) and the nested-@Valid VALIDATES edge (SourceRef
// `scope:class:bean_validation:…`, the owning DTO) both encode their carrier
// purely in the ref and never emit a standalone entity, so both edges vanished.
//
// #3605 synthesises a minimal carrier from the structured SourceRef. These tests
// drive the FULL live dispatch (RunCustomExtractors → CustomExtractorsFor("java")
// → custom_java_patterns.Extract) and assert the SPECIFIC restored edge now
// materialises with a real carrier — never len > 0.

// relWithProp reports whether rec carries a relationship of the given kind to the
// given ToID whose property key==val.
func relWithProp(rec *types.EntityRecord, kind, toID, key, val string) bool {
	if rec == nil {
		return false
	}
	for _, r := range rec.Relationships {
		if r.Kind == kind && r.ToID == toID && r.Properties.Get(key) == val {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// JAX-RS / CDI — DI.di_injection_point
// (cite: internal/custom/java/jakarta_ee.go @EJB / @Resource injection;
//  carrier synthesised by internal/custom/java/patterns_dispatch.go #3605)
// ─────────────────────────────────────────────────────────────────────────────

func TestReverifyJaxrsInjectionPointCarrier(t *testing.T) {
	path := "src/main/java/com/example/api/UserResource.java"
	src := `
package com.example.api;

import jakarta.ws.rs.Path;
import jakarta.ws.rs.GET;
import jakarta.ejb.EJB;

@Path("/users")
public class UserResource {

    @EJB
    private UserService userService;

    @GET
    public String list() {
        return userService.findAll();
    }
}
`
	recs := runJavaPatterns(t, path, src)

	// The injecting class is the carrier: the @EJB DEPENDS_ON edge's SourceRef is
	// scope:dependency:jakarta:<fp>:UserResource, which #3605 now materialises as a
	// SCOPE.Class carrier so the edge survives instead of being dropped.
	carrierRef := "scope:dependency:jakarta:" + path + ":UserResource"
	carrier := findByKindProp(recs, "SCOPE.Class", "ref", carrierRef)
	if carrier == nil {
		t.Fatalf("expected a synthesised SCOPE.Class carrier at ref %q for the @EJB injection point; got %v",
			carrierRef, names(recs))
	}
	if got := carrier.Name; got != "UserResource" {
		t.Errorf("carrier Name = %q, want UserResource", got)
	}
	if got := carrier.Properties["synthesized_carrier"]; got != "true" {
		t.Errorf("carrier synthesized_carrier = %q, want true", got)
	}

	// The restored di_injection_point edge: DEPENDS_ON from UserResource to the
	// injected UserService bean, tagged kind=ejb_inject.
	targetRef := "scope:dependency:jakarta:" + path + ":UserService"
	if !relWithProp(carrier, "DEPENDS_ON", targetRef, "kind", "ejb_inject") {
		t.Fatalf("expected restored di_injection_point DEPENDS_ON edge "+
			"UserResource -> UserService (kind=ejb_inject); got rels %v", carrier.Relationships)
	}
	if !hasRel(carrier, "DEPENDS_ON", targetRef) {
		t.Errorf("carrier missing DEPENDS_ON ToID %q", targetRef)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Bean Validation — Validation.nested_model_extraction
// (cite: internal/custom/java/bean_validation.go @Valid recursion;
//  carrier synthesised by internal/custom/java/patterns_dispatch.go #3605)
// ─────────────────────────────────────────────────────────────────────────────

func TestReverifyBeanValidationNestedValidCarrier(t *testing.T) {
	path := "src/main/java/com/example/dto/OrderRequest.java"
	src := `
package com.example.dto;

import jakarta.validation.Valid;
import jakarta.validation.constraints.NotNull;

public class OrderRequest {

    @NotNull
    private String orderId;

    @Valid
    @NotNull
    private Address shippingAddress;
}
`
	recs := runJavaPatterns(t, path, src)

	// The owning DTO is the carrier: the nested @Valid VALIDATES edge's SourceRef
	// is scope:class:bean_validation:<fp>:OrderRequest, which #3605 now materialises
	// as a SCOPE.Class carrier so the VALIDATES edge survives.
	carrierRef := "scope:class:bean_validation:" + path + ":OrderRequest"
	carrier := findByKindProp(recs, "SCOPE.Class", "ref", carrierRef)
	if carrier == nil {
		t.Fatalf("expected a synthesised SCOPE.Class carrier at ref %q for the nested @Valid edge; got %v",
			carrierRef, names(recs))
	}
	if got := carrier.Name; got != "OrderRequest" {
		t.Errorf("carrier Name = %q, want OrderRequest", got)
	}

	// The restored nested_model_extraction edge: VALIDATES from OrderRequest to the
	// nested Address type via the @Valid annotation on shippingAddress.
	targetRef := "scope:dependency:bean_validation:" + path + ":Address"
	if !relWithProp(carrier, "VALIDATES", targetRef, "via", "valid_annotation") {
		t.Fatalf("expected restored nested_model_extraction VALIDATES edge "+
			"OrderRequest -> Address (via=valid_annotation); got rels %v", carrier.Relationships)
	}
	if !relWithProp(carrier, "VALIDATES", targetRef, "field", "shippingAddress") {
		t.Errorf("VALIDATES edge missing field=shippingAddress; got rels %v", carrier.Relationships)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Negative — no phantom carrier for unrelated refs.
//
// A source whose only framework signal yields entities but NO edge with an
// unmaterialised SourceRef must not gain any synthesised carrier. Synthesis fires
// ONLY for a SourceRef an actual Relationship references.
// ─────────────────────────────────────────────────────────────────────────────

func TestReverifyNoPhantomCarrierForUnrelatedRefs(t *testing.T) {
	path := "src/main/java/com/example/dto/SimpleDto.java"
	src := `
package com.example.dto;

import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Size;

public class SimpleDto {

    @NotNull
    @Size(min = 1, max = 64)
    private String name;
}
`
	recs := runJavaPatterns(t, path, src)

	// No @Valid here, so no VALIDATES edge and therefore no bean_validation class
	// carrier should ever be synthesised.
	for i := range recs {
		if recs[i].Properties["synthesized_carrier"] == "true" {
			t.Fatalf("synthesised a phantom carrier %q (%s) for a source with no carrier-less edge; got %v",
				recs[i].Name, recs[i].Properties["ref"], names(recs))
		}
	}
}
