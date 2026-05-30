package kotlin_test

import (
	"testing"
)

// spring_middleware_test.go: tests for custom_kotlin_spring_middleware extractor.
// Registry target: lang.kotlin.framework.spring-boot Middleware/middleware_coverage → partial.

const springMwSecurityFilterChainSrc = `
package com.example.security

import org.springframework.context.annotation.Bean
import org.springframework.security.config.annotation.web.builders.HttpSecurity
import org.springframework.security.web.SecurityFilterChain

@EnableWebSecurity
class SecurityConfig {
    @Bean
    fun securityFilterChain(http: HttpSecurity): SecurityFilterChain {
        http.authorizeHttpRequests { auth ->
            auth.requestMatchers("/public/**").permitAll()
                .anyRequest().authenticated()
        }
        return http.build()
    }
}
`

const springMwOncePerRequestFilterSrc = `
package com.example.filter

import org.springframework.web.filter.OncePerRequestFilter
import javax.servlet.http.HttpServletRequest
import javax.servlet.http.HttpServletResponse

class JwtAuthFilter : OncePerRequestFilter() {
    override fun doFilterInternal(
        request: HttpServletRequest,
        response: HttpServletResponse,
        filterChain: javax.servlet.FilterChain
    ) {
        // validate JWT
        filterChain.doFilter(request, response)
    }
}
`

const springMwHandlerInterceptorSrc = `
package com.example.interceptor

import org.springframework.web.servlet.HandlerInterceptor
import javax.servlet.http.HttpServletRequest

class LoggingInterceptor : HandlerInterceptor {
    override fun preHandle(request: HttpServletRequest, response: javax.servlet.http.HttpServletResponse, handler: Any): Boolean {
        return true
    }
}

class WebConfig : org.springframework.web.servlet.config.annotation.WebMvcConfigurer {
    override fun addInterceptors(registry: InterceptorRegistry) {
        registry.addInterceptor(LoggingInterceptor())
    }
}
`

func TestKotlinSpringMiddleware_SecurityFilterChain(t *testing.T) {
	// Registry target: lang.kotlin.framework.spring-boot Middleware/middleware_coverage → partial
	ents := extract(t, "custom_kotlin_spring_middleware", fi("SecurityConfig.kt", "kotlin", springMwSecurityFilterChainSrc))
	if len(ents) == 0 {
		t.Fatal("[spring_middleware] expected at least one entity from SecurityFilterChain @Bean, got none")
	}
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Name == "securityFilterChain" {
			found = true
		}
	}
	if !found {
		t.Errorf("[spring_middleware] expected SecurityFilterChain entity 'securityFilterChain', got: %v", ents)
	}
}

func TestKotlinSpringMiddleware_OncePerRequestFilter(t *testing.T) {
	ents := extract(t, "custom_kotlin_spring_middleware", fi("JwtAuthFilter.kt", "kotlin", springMwOncePerRequestFilterSrc))
	if len(ents) == 0 {
		t.Fatal("[spring_middleware] expected entity from OncePerRequestFilter, got none")
	}
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Name == "JwtAuthFilter" {
			found = true
		}
	}
	if !found {
		t.Errorf("[spring_middleware] expected entity 'JwtAuthFilter', got: %v", ents)
	}
}

func TestKotlinSpringMiddleware_HandlerInterceptor(t *testing.T) {
	ents := extract(t, "custom_kotlin_spring_middleware", fi("LoggingInterceptor.kt", "kotlin", springMwHandlerInterceptorSrc))
	if len(ents) == 0 {
		t.Fatal("[spring_middleware] expected entity from HandlerInterceptor, got none")
	}
	foundInterceptor := false
	foundAddInterceptors := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Name == "LoggingInterceptor" {
			foundInterceptor = true
		}
		if e.Kind == "SCOPE.Pattern" && e.Name == "addInterceptors" {
			foundAddInterceptors = true
		}
	}
	if !foundInterceptor {
		t.Errorf("[spring_middleware] expected 'LoggingInterceptor', got: %v", ents)
	}
	if !foundAddInterceptors {
		t.Errorf("[spring_middleware] expected 'addInterceptors', got: %v", ents)
	}
}

func TestKotlinSpringMiddleware_EmptyFile(t *testing.T) {
	ents := extract(t, "custom_kotlin_spring_middleware", fi("Empty.kt", "kotlin", ""))
	if len(ents) != 0 {
		t.Errorf("[spring_middleware] expected no entities for empty file, got %d", len(ents))
	}
}

func TestKotlinSpringMiddleware_NoSpringSource(t *testing.T) {
	src := `
package com.example
class Foo {
    fun bar() = "hello"
}
`
	ents := extract(t, "custom_kotlin_spring_middleware", fi("Foo.kt", "kotlin", src))
	if len(ents) != 0 {
		t.Errorf("[spring_middleware] expected no entities for non-security file, got %d", len(ents))
	}
}
