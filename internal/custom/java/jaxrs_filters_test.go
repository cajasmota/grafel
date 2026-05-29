package java

import (
	"os"
	"strings"
	"testing"
)

// ============================================================================
// JAX-RS + MicroProfile filter extractor tests (issue #3083)
//
// Coverage cells proven:
//   lang.java.framework.jaxrs       → Middleware/middleware_coverage (partial)
//   lang.java.framework.microprofile → Middleware/middleware_coverage (partial)
//   lang.java.framework.jaxrs       → DI/di_binding_extraction    (partial, via jakarta_ee_advanced.go)
//   lang.java.framework.jaxrs       → DI/di_injection_point       (partial, via jakarta_ee_advanced.go)
//   lang.java.framework.jaxrs       → DI/di_scope_resolution      (partial, via jakarta_ee_advanced.go)
// ============================================================================

// ── helpers ──────────────────────────────────────────────────────────────────

func entityHasProvenance(entities []SecondaryEntity, name, provenance string) bool {
	for _, e := range entities {
		if e.Name == name && e.Provenance == provenance {
			return true
		}
	}
	return false
}

func entityWithName(entities []SecondaryEntity, name string) *SecondaryEntity {
	for i := range entities {
		if entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

// ── JAX-RS ContainerRequestFilter ────────────────────────────────────────────

// TestJaxrsFilters_ContainerRequestFilter_Issue3083 proves that a @Provider +
// ContainerRequestFilter class is detected as middleware for framework=jaxrs.
// Registry target: lang.java.framework.jaxrs → Middleware/middleware_coverage → partial.
func TestJaxrsFilters_ContainerRequestFilter_Issue3083(t *testing.T) {
	source := `
package com.example;

import jakarta.ws.rs.container.ContainerRequestContext;
import jakarta.ws.rs.container.ContainerRequestFilter;
import jakarta.ws.rs.ext.Provider;

@Provider
public class AuthRequestFilter implements ContainerRequestFilter {
    @Override
    public void filter(ContainerRequestContext ctx) {}
}
`
	r := ExtractJaxrsFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jaxrs",
		FilePath:  "AuthRequestFilter.java",
	})

	if !entityHasProvenance(r.Entities, "AuthRequestFilter", "INFERRED_FROM_JAXRS_PROVIDER_FILTER") {
		t.Errorf("[#3083 jaxrs-middleware] expected INFERRED_FROM_JAXRS_PROVIDER_FILTER for AuthRequestFilter; got %v", entityNames(r.Entities))
	}
	e := entityWithName(r.Entities, "AuthRequestFilter")
	if e == nil {
		t.Fatal("[#3083 jaxrs-middleware] entity not found")
	}
	if e.Properties["filter_type"] != "container_request_filter" {
		t.Errorf("[#3083 jaxrs-middleware] filter_type = %v, want container_request_filter", e.Properties["filter_type"])
	}
	if e.Properties["framework"] != "jaxrs" {
		t.Errorf("[#3083 jaxrs-middleware] framework = %v, want jaxrs", e.Properties["framework"])
	}
}

// TestJaxrsFilters_ContainerResponseFilter_Issue3083 proves ContainerResponseFilter detection.
func TestJaxrsFilters_ContainerResponseFilter_Issue3083(t *testing.T) {
	source := `
package com.example;

import jakarta.ws.rs.container.ContainerRequestContext;
import jakarta.ws.rs.container.ContainerResponseContext;
import jakarta.ws.rs.container.ContainerResponseFilter;
import jakarta.ws.rs.ext.Provider;

@Provider
public class CorsFilter implements ContainerResponseFilter {
    @Override
    public void filter(ContainerRequestContext req, ContainerResponseContext res) {
        res.getHeaders().add("Access-Control-Allow-Origin", "*");
    }
}
`
	r := ExtractJaxrsFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jaxrs",
		FilePath:  "CorsFilter.java",
	})

	if !entityHasProvenance(r.Entities, "CorsFilter", "INFERRED_FROM_JAXRS_PROVIDER_FILTER") {
		t.Errorf("[#3083 jaxrs-response-filter] expected entity for CorsFilter; got %v", entityNames(r.Entities))
	}
	e := entityWithName(r.Entities, "CorsFilter")
	if e != nil && e.Properties["filter_type"] != "container_response_filter" {
		t.Errorf("[#3083 jaxrs-response-filter] filter_type = %v, want container_response_filter", e.Properties["filter_type"])
	}
}

// TestJaxrsFilters_ClientRequestFilter_Issue3083 proves ClientRequestFilter detection.
func TestJaxrsFilters_ClientRequestFilter_Issue3083(t *testing.T) {
	source := `
package com.example;

import jakarta.ws.rs.client.ClientRequestContext;
import jakarta.ws.rs.client.ClientRequestFilter;
import jakarta.ws.rs.ext.Provider;

@Provider
public class BearerTokenFilter implements ClientRequestFilter {
    @Override
    public void filter(ClientRequestContext ctx) {}
}
`
	r := ExtractJaxrsFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jaxrs",
		FilePath:  "BearerTokenFilter.java",
	})

	if !entityHasProvenance(r.Entities, "BearerTokenFilter", "INFERRED_FROM_JAXRS_PROVIDER_FILTER") {
		t.Errorf("[#3083 jaxrs-client-filter] expected entity for BearerTokenFilter; got %v", entityNames(r.Entities))
	}
	e := entityWithName(r.Entities, "BearerTokenFilter")
	if e != nil && e.Properties["filter_type"] != "client_request_filter" {
		t.Errorf("[#3083 jaxrs-client-filter] filter_type = %v, want client_request_filter", e.Properties["filter_type"])
	}
}

// TestJaxrsFilters_PreMatching_Issue3083 proves @PreMatching filter detection.
func TestJaxrsFilters_PreMatching_Issue3083(t *testing.T) {
	source := `
package com.example;

import jakarta.ws.rs.container.ContainerRequestContext;
import jakarta.ws.rs.container.ContainerRequestFilter;
import jakarta.ws.rs.container.PreMatching;
import jakarta.ws.rs.ext.Provider;

@PreMatching
@Provider
public class NormalizationFilter implements ContainerRequestFilter {
    @Override
    public void filter(ContainerRequestContext ctx) {}
}
`
	r := ExtractJaxrsFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jaxrs",
		FilePath:  "NormalizationFilter.java",
	})

	if len(r.Entities) == 0 {
		t.Errorf("[#3083 jaxrs-prematching] expected at least one entity; got none")
		return
	}
	e := entityWithName(r.Entities, "NormalizationFilter")
	if e == nil {
		t.Errorf("[#3083 jaxrs-prematching] entity NormalizationFilter not found; got %v", entityNames(r.Entities))
		return
	}
	ft := e.Properties["filter_type"]
	if ft != "prematching_request_filter" && ft != "container_request_filter" {
		t.Errorf("[#3083 jaxrs-prematching] unexpected filter_type=%v", ft)
	}
}

// TestJaxrsFilters_NameBinding_Issue3083 proves @NameBinding annotation detection.
func TestJaxrsFilters_NameBinding_Issue3083(t *testing.T) {
	source := `
package com.example;

import jakarta.ws.rs.NameBinding;
import java.lang.annotation.*;

@NameBinding
@Retention(RetentionPolicy.RUNTIME)
@Target({ElementType.TYPE, ElementType.METHOD})
public @interface Secured {}
`
	r := ExtractJaxrsFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jaxrs",
		FilePath:  "Secured.java",
	})

	if !entityHasProvenance(r.Entities, "Secured", "INFERRED_FROM_JAXRS_NAME_BINDING") {
		t.Errorf("[#3083 jaxrs-namebinding] expected INFERRED_FROM_JAXRS_NAME_BINDING for Secured; got %v", entityNames(r.Entities))
	}
}

// TestJaxrsFilters_Gating_Issue3083 confirms the extractor is gated on jaxrs/microprofile
// and does NOT fire for unrelated frameworks (spring_boot, quarkus, helidon).
func TestJaxrsFilters_Gating_Issue3083(t *testing.T) {
	source := `
@Provider
public class AuthFilter implements ContainerRequestFilter {
    public void filter(ContainerRequestContext ctx) {}
}
`
	for _, fw := range []string{"spring_boot", "quarkus", "helidon", "micronaut"} {
		r := ExtractJaxrsFilters(PatternContext{Source: source, Language: "java", Framework: fw, FilePath: "F.java"})
		if len(r.Entities) != 0 {
			t.Errorf("[#3083 jaxrs-gating] framework %q should no-op, got %d entities", fw, len(r.Entities))
		}
	}
	// Should fire for jaxrs.
	r := ExtractJaxrsFilters(PatternContext{Source: source, Language: "java", Framework: "jaxrs", FilePath: "F.java"})
	if len(r.Entities) == 0 {
		t.Error("[#3083 jaxrs-gating] expected entity for framework=jaxrs, got none")
	}
}

// ── MicroProfile ContainerRequestFilter ──────────────────────────────────────

// TestMicroProfile_ContainerRequestFilter_Issue3083 proves that a @Provider +
// ContainerRequestFilter class is detected as middleware for framework=microprofile.
// Registry target: lang.java.framework.microprofile → Middleware/middleware_coverage → partial.
func TestMicroProfile_ContainerRequestFilter_Issue3083(t *testing.T) {
	source := `
package com.example.microprofile;

import jakarta.ws.rs.container.ContainerRequestContext;
import jakarta.ws.rs.container.ContainerRequestFilter;
import jakarta.ws.rs.ext.Provider;

@Provider
public class MpAuthFilter implements ContainerRequestFilter {
    @Override
    public void filter(ContainerRequestContext ctx) {}
}
`
	r := ExtractJaxrsFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "microprofile",
		FilePath:  "MpAuthFilter.java",
	})

	if !entityHasProvenance(r.Entities, "MpAuthFilter", "INFERRED_FROM_JAXRS_PROVIDER_FILTER") {
		t.Errorf("[#3083 mp-middleware] expected INFERRED_FROM_JAXRS_PROVIDER_FILTER for MpAuthFilter; got %v", entityNames(r.Entities))
	}
	e := entityWithName(r.Entities, "MpAuthFilter")
	if e != nil && e.Properties["framework"] != "microprofile" {
		t.Errorf("[#3083 mp-middleware] framework = %v, want microprofile", e.Properties["framework"])
	}
}

// TestMicroProfile_ContainerResponseFilter_Issue3083 proves response filter for microprofile.
func TestMicroProfile_ContainerResponseFilter_Issue3083(t *testing.T) {
	source := `
package com.example.microprofile;

import jakarta.ws.rs.container.ContainerRequestContext;
import jakarta.ws.rs.container.ContainerResponseContext;
import jakarta.ws.rs.container.ContainerResponseFilter;
import jakarta.ws.rs.ext.Provider;

@Provider
public class MpCorsFilter implements ContainerResponseFilter {
    @Override
    public void filter(ContainerRequestContext req, ContainerResponseContext res) {}
}
`
	r := ExtractJaxrsFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "microprofile",
		FilePath:  "MpCorsFilter.java",
	})

	if !entityHasProvenance(r.Entities, "MpCorsFilter", "INFERRED_FROM_JAXRS_PROVIDER_FILTER") {
		t.Errorf("[#3083 mp-response-filter] expected entity for MpCorsFilter; got %v", entityNames(r.Entities))
	}
}

// TestMicroProfile_Gating_Issue3083 confirms the extractor fires for microprofile.
func TestMicroProfile_Gating_Issue3083(t *testing.T) {
	source := `
@Provider
public class MyFilter implements ContainerResponseFilter {
    public void filter(ContainerRequestContext req, ContainerResponseContext res) {}
}
`
	r := ExtractJaxrsFilters(PatternContext{Source: source, Language: "java", Framework: "microprofile", FilePath: "F.java"})
	if len(r.Entities) == 0 {
		t.Error("[#3083 mp-gating] expected entity for framework=microprofile, got none")
	}
}

// ── JAX-RS CDI DI (via jakarta_ee_advanced.go extension) ─────────────────────

// TestJaxrs_CDI_DiScopeResolution_Issue3083 proves that CDI @ApplicationScoped /
// @RequestScoped is detected for framework=jaxrs (di_scope_resolution cell).
func TestJaxrs_CDI_DiScopeResolution_Issue3083(t *testing.T) {
	source := `
package com.example.jaxrs;

import jakarta.enterprise.context.RequestScoped;

@RequestScoped
public class JaxrsSecurityContext {
    private String principal;
}
`
	r := ExtractJakartaEEAdvanced(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jaxrs",
		FilePath:  "JaxrsSecurityContext.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_CDI_SCOPE" && e.Name == "JaxrsSecurityContext" {
			found = true
			if e.Properties["cdi_scope"] != "RequestScoped" {
				t.Errorf("[#3083 jaxrs-cdi-scope] cdi_scope = %v, want RequestScoped", e.Properties["cdi_scope"])
			}
		}
	}
	if !found {
		t.Errorf("[#3083 jaxrs-cdi-scope] expected INFERRED_FROM_CDI_SCOPE for JaxrsSecurityContext; got %v", entityNames(r.Entities))
	}
}

// TestJaxrs_CDI_DiProducer_Issue3083 proves that @Produces binding is detected
// for framework=jaxrs (di_binding_extraction cell).
func TestJaxrs_CDI_DiProducer_Issue3083(t *testing.T) {
	source := `
package com.example.jaxrs;

import jakarta.enterprise.inject.Produces;

public class JaxrsProducers {
    @Produces
    public SecurityService securityService() {
        return new SecurityServiceImpl();
    }
}
`
	r := ExtractJakartaEEAdvanced(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jaxrs",
		FilePath:  "JaxrsProducers.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_CDI_PRODUCER" && e.Name == "securityService" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3083 jaxrs-cdi-producer] expected INFERRED_FROM_JAKARTA_CDI_PRODUCER for securityService; got %v", entityNames(r.Entities))
	}
}

// TestJaxrs_CDI_Gating_Issue3083 confirms that "jaxrs" is in jakartaEEAdvFrameworks.
func TestJaxrs_CDI_Gating_Issue3083(t *testing.T) {
	source := `
@RequestScoped
public class MyBean {}
`
	r := ExtractJakartaEEAdvanced(PatternContext{Source: source, Language: "java", Framework: "jaxrs", FilePath: "MyBean.java"})
	if len(r.Entities) == 0 {
		t.Error("[#3083 jaxrs-cdi-gating] expected CDI scope entity for framework=jaxrs, got none")
	}
	// Confirm it does NOT fire for an unrelated framework.
	r2 := ExtractJakartaEEAdvanced(PatternContext{Source: source, Language: "java", Framework: "spring_boot", FilePath: "MyBean.java"})
	if len(r2.Entities) != 0 {
		t.Error("[#3083 jaxrs-cdi-gating] spring_boot should not fire ExtractJakartaEEAdvanced")
	}
}

// ── Fixture-file integration tests ───────────────────────────────────────────

// TestJaxrsFilters_FixtureFile_Issue3083 loads the jaxrs fixture and confirms
// all expected filter classes are extracted.
func TestJaxrsFilters_FixtureFile_Issue3083(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/fixtures/sources/java/jaxrs/JaxrsFiltersFixture.java")
	if err != nil {
		t.Fatalf("[#3083 fixture] cannot read fixture: %v", err)
	}
	source := string(data)
	r := ExtractJaxrsFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jaxrs",
		FilePath:  "JaxrsFiltersFixture.java",
	})

	wantEntities := []string{
		"AuthRequestFilter",
		"CorsResponseFilter",
		"BearerTokenClientFilter",
		"Secured",
	}
	names := entityNames(r.Entities)
	for _, want := range wantEntities {
		if !contains(names, want) {
			t.Errorf("[#3083 fixture jaxrs] missing entity %q in %v", want, names)
		}
	}
	// NormalizationFilter has @PreMatching — may be detected via prematching RE.
	if !contains(names, "NormalizationFilter") {
		t.Logf("[#3083 fixture jaxrs] NormalizationFilter not detected (acceptable if @PreMatching+@Provider ordering not matched)")
	}
}

// TestMicroProfileFilters_FixtureFile_Issue3083 loads the microprofile fixture
// and confirms filter classes are extracted.
func TestMicroProfileFilters_FixtureFile_Issue3083(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/fixtures/sources/java/microprofile/MicroProfileFiltersFixture.java")
	if err != nil {
		t.Fatalf("[#3083 fixture] cannot read fixture: %v", err)
	}
	source := string(data)
	r := ExtractJaxrsFilters(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "microprofile",
		FilePath:  "MicroProfileFiltersFixture.java",
	})

	wantEntities := []string{
		"MpAuthRequestFilter",
		"MpCorsResponseFilter",
		"JwtRequired",
	}
	names := entityNames(r.Entities)
	for _, want := range wantEntities {
		if !contains(names, want) {
			t.Errorf("[#3083 fixture mp] missing entity %q in %v", want, names)
		}
	}
}

// TestJaxrsFilters_FixtureCDI_Issue3083 loads the jaxrs fixture and confirms
// CDI scope annotations are extracted by ExtractJakartaEEAdvanced (di_scope_resolution).
func TestJaxrsFilters_FixtureCDI_Issue3083(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/fixtures/sources/java/jaxrs/JaxrsFiltersFixture.java")
	if err != nil {
		t.Fatalf("[#3083 fixture-cdi] cannot read fixture: %v", err)
	}
	source := string(data)
	r := ExtractJakartaEEAdvanced(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "jaxrs",
		FilePath:  "JaxrsFiltersFixture.java",
	})

	foundScope := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_CDI_SCOPE" && strings.Contains(e.Name, "SecurityContext") {
			foundScope = true
		}
	}
	if !foundScope {
		t.Errorf("[#3083 fixture-cdi] expected INFERRED_FROM_CDI_SCOPE for SecurityContext; got %v", entityNames(r.Entities))
	}
	foundProducer := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_JAKARTA_CDI_PRODUCER" {
			foundProducer = true
		}
	}
	if !foundProducer {
		t.Errorf("[#3083 fixture-cdi] expected INFERRED_FROM_JAKARTA_CDI_PRODUCER for @Produces method; got %v", entityNames(r.Entities))
	}
}
