package com.example.microprofile.filter;

import jakarta.ws.rs.container.ContainerRequestContext;
import jakarta.ws.rs.container.ContainerRequestFilter;
import jakarta.ws.rs.container.ContainerResponseContext;
import jakarta.ws.rs.container.ContainerResponseFilter;
import jakarta.ws.rs.ext.Provider;
import jakarta.ws.rs.NameBinding;
import jakarta.inject.Inject;
import jakarta.enterprise.context.ApplicationScoped;
import jakarta.enterprise.inject.Produces;
import java.lang.annotation.*;

/**
 * Fixture covering MicroProfile JAX-RS filter patterns detected by
 * internal/custom/java/jaxrs_filters.go (issue #3083).
 *
 * MicroProfile shares the JAX-RS filter model — @Provider-annotated
 * ContainerRequestFilter / ContainerResponseFilter implementations are the
 * canonical middleware registration mechanism.
 */

// ── Server-side request filter ───────────────────────────────────────────────

@Provider
public class MpAuthRequestFilter implements ContainerRequestFilter {

    @Inject
    private MpJwtValidator jwtValidator;

    @Override
    public void filter(ContainerRequestContext requestContext) {
        // MP-JWT validation
    }
}

// ── Server-side response filter ──────────────────────────────────────────────

@Provider
public class MpCorsResponseFilter implements ContainerResponseFilter {

    @Override
    public void filter(ContainerRequestContext req, ContainerResponseContext res) {
        res.getHeaders().add("Access-Control-Allow-Origin", "*");
    }
}

// ── @NameBinding meta-annotation ─────────────────────────────────────────────

@NameBinding
@Retention(RetentionPolicy.RUNTIME)
@Target({ElementType.TYPE, ElementType.METHOD})
public @interface JwtRequired {}

// ── CDI scoped bean (di_scope_resolution via jakarta_ee_advanced.go) ─────────

@ApplicationScoped
public class TokenCache {

    @Inject
    private ConfigProvider configProvider;

    @Produces
    public JwtConfiguration jwtConfig() {
        return configProvider.getConfig(JwtConfiguration.class);
    }
}
