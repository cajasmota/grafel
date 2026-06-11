package java_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #4410 LIVE-REPRO — Spring Security global filter-chain wiring.
//
// #4377 covered WebMvcConfigurer interceptors, servlet @Component filters, and
// @ControllerAdvice. Spring Security registers its OWN global filter chain that
// #4377 did not wire. #4410 adds it via extractSpringSecurityWiring:
//
//   - modern @Bean SecurityFilterChain filterChain(HttpSecurity http) with
//     http.addFilterBefore(new JwtAuthFilter(), UsernamePasswordAuthenticationFilter.class)
//     → SecurityConfig → JwtAuthFilter USES
//       (global, di_role=security_filter, relative_order=before,
//        relative_to=UsernamePasswordAuthenticationFilter).
//   - legacy class extends WebSecurityConfigurerAdapter with the same idiom.
//   - @EnableMethodSecurity posture + AuthenticationProvider/UserDetailsService
//     @Bean providers wired into the chain.
//
// Pre-fix: the custom security filter class had no inbound edge — it was a
// structural orphan and the security chain was invisible to the graph.

const springSecurityFilterChainSrc = `package com.example.security;

import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.security.config.annotation.method.configuration.EnableMethodSecurity;
import org.springframework.security.config.annotation.web.builders.HttpSecurity;
import org.springframework.security.web.SecurityFilterChain;
import org.springframework.security.web.authentication.UsernamePasswordAuthenticationFilter;
import org.springframework.security.core.userdetails.UserDetailsService;

@Configuration
@EnableMethodSecurity
public class SecurityConfig {

    @Bean
    public SecurityFilterChain filterChain(HttpSecurity http) throws Exception {
        http.addFilterBefore(new JwtAuthFilter(), UsernamePasswordAuthenticationFilter.class)
            .addFilterAfter(new AuditFilter(), JwtAuthFilter.class);
        return http.build();
    }

    @Bean
    public UserDetailsService userDetailsService() {
        return new CustomUserDetailsService();
    }
}
`

const springLegacyAdapterSrc = `package com.example.security;

import org.springframework.security.config.annotation.web.builders.HttpSecurity;
import org.springframework.security.config.annotation.web.configuration.WebSecurityConfigurerAdapter;
import org.springframework.security.web.authentication.UsernamePasswordAuthenticationFilter;

public class LegacySecurityConfig extends WebSecurityConfigurerAdapter {
    @Override
    protected void configure(HttpSecurity http) throws Exception {
        http.addFilterBefore(new TenantAuthFilter(), UsernamePasswordAuthenticationFilter.class);
    }
}
`

// Faithful target classes so the Class:<X> USES stubs resolve, mirroring the
// #4377 live-repro convention.
const springSecurityTargetsSrc = `package com.example.security;

import jakarta.servlet.Filter;

class JwtAuthFilter implements Filter {}
class AuditFilter implements Filter {}
class TenantAuthFilter implements Filter {}
class CustomUserDetailsService {}
`

func TestIssue4410_SpringSecurityFilterChain_Wired(t *testing.T) {
	var all []types.EntityRecord
	all = append(all, javaExtract4377(t, "src/SecurityConfig.java", springSecurityFilterChainSrc)...)
	all = append(all, javaExtract4377(t, "src/LegacySecurityConfig.java", springLegacyAdapterSrc)...)
	all = append(all, javaExtract4377(t, "src/SecurityTargets.java", springSecurityTargetsSrc)...)

	uses := collectGlobalUses4377(all)
	if len(uses) == 0 {
		t.Fatalf("Spring Security global-wiring USES edges = 0 (orphan filter chain); want > 0 (#4410)")
	}
	for _, u := range uses {
		t.Logf("  -> %s  role=%s from=%s", u.to, u.role, u.from)
	}

	// Helper: find a security_filter edge for a target and assert its relative
	// position metadata.
	findFilter := func(to string) (types.RelationshipRecord, bool) {
		for _, e := range all {
			for _, r := range e.Relationships {
				if r.ToID == to && r.Properties["di_role"] == "security_filter" &&
					r.Properties["global"] == "true" {
					return r, true
				}
			}
		}
		return types.RelationshipRecord{}, false
	}

	// --- modern @Bean SecurityFilterChain: addFilterBefore + addFilterAfter ---
	if r, ok := findFilter("Class:JwtAuthFilter"); !ok {
		t.Error("missing security_filter USES edge -> Class:JwtAuthFilter (#4410)")
	} else {
		if r.Properties["relative_order"] != "before" {
			t.Errorf("JwtAuthFilter: relative_order=%q, want before", r.Properties["relative_order"])
		}
		if r.Properties["relative_to"] != "UsernamePasswordAuthenticationFilter" {
			t.Errorf("JwtAuthFilter: relative_to=%q, want UsernamePasswordAuthenticationFilter", r.Properties["relative_to"])
		}
	}
	if r, ok := findFilter("Class:AuditFilter"); !ok {
		t.Error("missing security_filter USES edge -> Class:AuditFilter (#4410)")
	} else if r.Properties["relative_order"] != "after" {
		t.Errorf("AuditFilter: relative_order=%q, want after", r.Properties["relative_order"])
	}

	// --- legacy WebSecurityConfigurerAdapter ---
	if r, ok := findFilter("Class:TenantAuthFilter"); !ok {
		t.Error("missing security_filter USES edge -> Class:TenantAuthFilter (legacy adapter) (#4410)")
	} else if r.Properties["relative_to"] != "UsernamePasswordAuthenticationFilter" {
		t.Errorf("TenantAuthFilter: relative_to=%q", r.Properties["relative_to"])
	}

	// --- @EnableMethodSecurity posture ---
	foundMethodSec := false
	// --- UserDetailsService provider bean ---
	foundProvider := false
	for _, e := range all {
		for _, r := range e.Relationships {
			if r.Properties["di_role"] == "method_security" {
				foundMethodSec = true
			}
			if r.Properties["di_role"] == "security_provider" && r.ToID == "Class:UserDetailsService" {
				foundProvider = true
			}
		}
	}
	if !foundMethodSec {
		t.Error("missing method_security USES edge for @EnableMethodSecurity (#4410)")
	}
	if !foundProvider {
		t.Error("missing security_provider USES edge for UserDetailsService @Bean (#4410)")
	}

	// The security_filter edges must be owned by a security_config carrier (one
	// stable owner per security config), not a colliding duplicate.
	var carrierRefs = map[string]bool{}
	for _, e := range all {
		if e.Subtype == "security_config" {
			carrierRefs[e.Name] = true
		}
	}
	if !carrierRefs["SecurityConfig"] {
		t.Error("expected a security_config carrier entity named SecurityConfig (#4410)")
	}
	if !carrierRefs["LegacySecurityConfig"] {
		t.Error("expected a security_config carrier entity named LegacySecurityConfig (#4410)")
	}
}
