package java_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/java"
)

// Issue #4377 LIVE-REPRO — Spring global cross-cutting wiring.
//
// Runs the ACTUAL registered custom_java_patterns extractor (which dispatches
// ExtractSpringGlobalWiring) + the ACTUAL resolve.BuildIndex symbol table over
// faithful Spring sources, and asserts that the previously-orphan cross-cutting
// classes are now USES-linked from a config/app carrier AND resolve in the
// symbol table:
//
//   - WebMvcConfigurer.addInterceptor(new AuthInterceptor()) →
//     MvcConfig → AuthInterceptor USES (global, di_role=interceptor, path patterns).
//   - @Component @Order(1) class LoggingFilter implements Filter →
//     spring_app → LoggingFilter USES (global, di_role=filter, order=1).
//   - @Bean FilterRegistrationBean<TenantFilter> ... setFilter(new TenantFilter())
//     → SecurityConfig → TenantFilter USES (global, di_role=filter).
//   - @RestControllerAdvice GlobalExceptionHandler with
//     @ExceptionHandler(ResourceNotFoundException.class) →
//     spring_app → GlobalExceptionHandler USES (global, di_role=exception_advice)
//     + GlobalExceptionHandler → ResourceNotFoundException USES (handles_exception).
//
// Pre-fix: none of these classes had any inbound edge from a config/app source —
// every interceptor/filter/advice class was a structural orphan and the app-wide
// scope was invisible.

const springMvcConfigSrc = `package com.example.config;

import org.springframework.context.annotation.Configuration;
import org.springframework.web.servlet.config.annotation.InterceptorRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;

@Configuration
public class MvcConfig implements WebMvcConfigurer {
    @Override
    public void addInterceptors(InterceptorRegistry registry) {
        registry.addInterceptor(new AuthInterceptor()).addPathPatterns("/api/**");
    }
}
`

const springInterceptorSrc = `package com.example.web;

import org.springframework.web.servlet.HandlerInterceptor;
import org.springframework.stereotype.Component;

@Component
public class AuthInterceptor implements HandlerInterceptor {
}
`

const springFilterSrc = `package com.example.web;

import jakarta.servlet.Filter;
import org.springframework.core.annotation.Order;
import org.springframework.stereotype.Component;

@Component
@Order(1)
public class LoggingFilter implements Filter {
}
`

const springTenantFilterSrc = `package com.example.web;

import jakarta.servlet.Filter;

public class TenantFilter implements Filter {
}
`

const springSecurityConfigSrc = `package com.example.config;

import org.springframework.boot.web.servlet.FilterRegistrationBean;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
public class SecurityConfig {
    @Bean
    public FilterRegistrationBean<TenantFilter> tenantFilter() {
        FilterRegistrationBean<TenantFilter> registration = new FilterRegistrationBean<>();
        registration.setFilter(new TenantFilter());
        registration.setOrder(5);
        return registration;
    }
}
`

const springAdviceSrc = `package com.example.web;

import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

@RestControllerAdvice
public class GlobalExceptionHandler {
    @ExceptionHandler(ResourceNotFoundException.class)
    public String handleNotFound(ResourceNotFoundException ex) {
        return "not found";
    }
}
`

const springExceptionSrc = `package com.example.web;

public class ResourceNotFoundException extends RuntimeException {
}
`

func javaExtract4377(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_java_patterns")
	if !ok {
		t.Fatal("custom_java_patterns not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "java", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return ents
}

// globalUse holds one USES edge marked global=true for assertion convenience.
type globalUse struct {
	from, to, role, order, patterns string
}

func collectGlobalUses4377(ents []types.EntityRecord) []globalUse {
	var out []globalUse
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != string(types.RelationshipKindUses) {
				continue
			}
			if r.Properties.Get("global") != "true" {
				continue
			}
			from := r.FromID // explicit FromName override, else empty (carrier)
			out = append(out, globalUse{
				from:     from,
				to:       r.ToID,
				role:     r.Properties.Get("di_role"),
				order:    r.Properties.Get("order"),
				patterns: r.Properties.Get("path_patterns"),
			})
		}
	}
	return out
}

func TestIssue4377_SpringGlobalWiring_LinkedAndResolving(t *testing.T) {
	var all []types.EntityRecord
	all = append(all, javaExtract4377(t, "src/MvcConfig.java", springMvcConfigSrc)...)
	all = append(all, javaExtract4377(t, "src/AuthInterceptor.java", springInterceptorSrc)...)
	all = append(all, javaExtract4377(t, "src/LoggingFilter.java", springFilterSrc)...)
	all = append(all, javaExtract4377(t, "src/TenantFilter.java", springTenantFilterSrc)...)
	all = append(all, javaExtract4377(t, "src/SecurityConfig.java", springSecurityConfigSrc)...)
	all = append(all, javaExtract4377(t, "src/GlobalExceptionHandler.java", springAdviceSrc)...)
	all = append(all, javaExtract4377(t, "src/ResourceNotFoundException.java", springExceptionSrc)...)

	uses := collectGlobalUses4377(all)
	if len(uses) == 0 {
		t.Fatalf("Spring global-wiring USES edges = 0 (orphan cross-cutting classes); want > 0 (#4377)")
	}
	t.Logf("Spring global USES edges (%d):", len(uses))
	for _, u := range uses {
		t.Logf("  -> %s  role=%s order=%s patterns=%s from=%s", u.to, u.role, u.order, u.patterns, u.from)
	}

	// Assert each expected (target, role) is present, with order/patterns where
	// applicable.
	type want struct {
		to, role, order, patterns string
	}
	wants := []want{
		{"Class:AuthInterceptor", "interceptor", "", "/api/**"},
		{"Class:LoggingFilter", "filter", "1", ""},
		{"Class:TenantFilter", "filter", "5", ""},
		{"Class:GlobalExceptionHandler", "exception_advice", "", ""},
		{"Class:ResourceNotFoundException", "handles_exception", "", ""},
	}
	for _, w := range wants {
		found := false
		for _, u := range uses {
			if u.to == w.to && u.role == w.role {
				found = true
				if w.order != "" && u.order != w.order {
					t.Errorf("%s role=%s: order=%q, want %q", w.to, w.role, u.order, w.order)
				}
				if w.patterns != "" && u.patterns != w.patterns {
					t.Errorf("%s role=%s: path_patterns=%q, want %q", w.to, w.role, u.patterns, w.patterns)
				}
				break
			}
		}
		if !found {
			t.Errorf("missing global USES edge -> %s (role=%s) (#4377); got %+v", w.to, w.role, uses)
		}
	}

	// The advice→exception edge must originate from the advice class, not the app
	// carrier — verify the FromName override bound the source.
	advisesExc := false
	for _, u := range uses {
		if u.to == "Class:ResourceNotFoundException" && u.from == "Class:GlobalExceptionHandler" {
			advisesExc = true
		}
	}
	if !advisesExc {
		t.Errorf("advice->exception edge FromID not bound to Class:GlobalExceptionHandler (#4377)")
	}

	// The cross-cutting target classes are NOT emitted as standalone nodes by the
	// custom extractor (they come from the base tree-sitter Java extractor in the
	// real pipeline). Add faithful base class nodes so the USES targets resolve,
	// mirroring the #4367 live-repro convention.
	for _, cls := range []string{
		"AuthInterceptor", "LoggingFilter", "TenantFilter",
		"GlobalExceptionHandler", "ResourceNotFoundException",
	} {
		c := types.EntityRecord{
			Name: cls, Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: "src/" + cls + ".java", Language: "java",
			Properties: map[string]string{"kind": "SCOPE.Component", "subtype": "class"},
		}
		c.ID = c.ComputeID()
		all = append(all, c)
	}

	idx := resolve.BuildIndex(all)
	resolved := 0
	for _, target := range []string{
		"Class:AuthInterceptor", "Class:LoggingFilter", "Class:TenantFilter",
		"Class:GlobalExceptionHandler", "Class:ResourceNotFoundException",
	} {
		if _, ok := idx.Lookup(target); ok {
			resolved++
		} else {
			t.Errorf("symbol table did NOT resolve %q — cross-cutting class stays orphan (#4377)", target)
		}
	}
	t.Logf("#4377: %d/5 cross-cutting target classes resolve via symbol table after fix", resolved)
}
