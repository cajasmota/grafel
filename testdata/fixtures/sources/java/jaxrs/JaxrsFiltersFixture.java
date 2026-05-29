package com.example.jaxrs.filter;

import jakarta.inject.Inject;
import jakarta.inject.Named;
import jakarta.enterprise.context.RequestScoped;
import jakarta.enterprise.inject.Produces;
import jakarta.ws.rs.container.ContainerRequestContext;
import jakarta.ws.rs.container.ContainerRequestFilter;
import jakarta.ws.rs.container.ContainerResponseContext;
import jakarta.ws.rs.container.ContainerResponseFilter;
import jakarta.ws.rs.client.ClientRequestContext;
import jakarta.ws.rs.client.ClientRequestFilter;
import jakarta.ws.rs.ext.Provider;
import jakarta.ws.rs.NameBinding;
import java.lang.annotation.*;

/**
 * Fixture covering the JAX-RS middleware filter patterns detected by
 * internal/custom/java/jaxrs_filters.go (issue #3083).
 */

// ── Server-side request filter ───────────────────────────────────────────────

@Provider
public class AuthRequestFilter implements ContainerRequestFilter {

    @Inject
    private SecurityService securityService;

    @Override
    public void filter(ContainerRequestContext requestContext) {
        // token validation
    }
}

// ── Server-side response filter ──────────────────────────────────────────────

@Provider
public class CorsResponseFilter implements ContainerResponseFilter {

    @Override
    public void filter(ContainerRequestContext req, ContainerResponseContext res) {
        res.getHeaders().add("Access-Control-Allow-Origin", "*");
    }
}

// ── Client-side request filter ───────────────────────────────────────────────

@Provider
public class BearerTokenClientFilter implements ClientRequestFilter {

    @Inject
    @Named("tokenStore")
    private TokenStore tokenStore;

    @Override
    public void filter(ClientRequestContext requestContext) {
        requestContext.getHeaders().add("Authorization", "Bearer " + tokenStore.getToken());
    }
}

// ── @PreMatching filter ───────────────────────────────────────────────────────

@PreMatching
@Provider
public class NormalizationFilter implements ContainerRequestFilter {

    @Override
    public void filter(ContainerRequestContext ctx) {
        // normalize path
    }
}

// ── @NameBinding annotation ───────────────────────────────────────────────────

@NameBinding
@Retention(RetentionPolicy.RUNTIME)
@Target({ElementType.TYPE, ElementType.METHOD})
public @interface Secured {}

// ── CDI scoped bean (di_scope_resolution) ────────────────────────────────────

@RequestScoped
public class SecurityContext {

    @Inject
    private UserRepository userRepository;

    @Produces
    public Principal currentPrincipal() {
        return userRepository.findCurrent();
    }
}
