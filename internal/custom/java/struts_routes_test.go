package java

import (
	"testing"
)

// ============================================================================
// Issue #3089: Struts route/interceptor/auth extractor
// ============================================================================

// ----------------------------------------------------------------------------
// Routing: route_extraction — @Action annotation
// ----------------------------------------------------------------------------

// TestStruts_RouteExtraction_ActionAnnotation_Issue3089 proves that
// @Action(value="/path") annotations are extracted as Route entities.
// Registry target: lang.java.framework.struts Routing/route_extraction → partial.
// Cite: internal/custom/java/struts_routes.go
func TestStruts_RouteExtraction_ActionAnnotation_Issue3089(t *testing.T) {
	source := `
package com.example;

import org.apache.struts2.convention.annotation.Action;
import org.apache.struts2.convention.annotation.Result;
import com.opensymphony.xwork2.ActionSupport;

public class UserAction extends ActionSupport {

    @Action(value="/users/list")
    @Result(name="success", location="/WEB-INF/jsp/users.jsp")
    public String list() {
        return SUCCESS;
    }

    @Action(value="/users/create")
    @Result(name="success", location="/WEB-INF/jsp/createUser.jsp")
    public String create() {
        return SUCCESS;
    }

    @Action(value="/users/{id}/edit")
    public String edit() {
        return SUCCESS;
    }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "UserAction.java",
	})

	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_ACTION_ANNOTATION" {
			routes[e.Properties["path"].(string)] = true
		}
	}

	for _, want := range []string{
		"/users/list",
		"/users/create",
		"/users/{id}/edit",
	} {
		if !routes[want] {
			t.Errorf("[#3089 route_extraction] expected route %q, got %v", want, routes)
		}
	}
}

// TestStruts_RouteExtraction_ActionShorthand_Issue3089 proves that the
// positional @Action("/path") shorthand (no explicit value=) is extracted.
func TestStruts_RouteExtraction_ActionShorthand_Issue3089(t *testing.T) {
	source := `
import org.apache.struts2.convention.annotation.Action;
import com.opensymphony.xwork2.ActionSupport;

public class OrderAction extends ActionSupport {
    @Action("/orders/new")
    public String newOrder() { return SUCCESS; }

    @Action("/orders/save")
    public String save() { return SUCCESS; }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "OrderAction.java",
	})

	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_ACTION_ANNOTATION" {
			routes[e.Properties["path"].(string)] = true
		}
	}

	if !routes["/orders/new"] {
		t.Errorf("[#3089 route_extraction] expected /orders/new, got %v", routes)
	}
	if !routes["/orders/save"] {
		t.Errorf("[#3089 route_extraction] expected /orders/save, got %v", routes)
	}
}

// TestStruts_RouteExtraction_Namespace_Issue3089 proves that @Namespace prefix
// is prepended to @Action paths.
func TestStruts_RouteExtraction_Namespace_Issue3089(t *testing.T) {
	source := `
import org.apache.struts2.convention.annotation.Action;
import org.apache.struts2.convention.annotation.Namespace;
import com.opensymphony.xwork2.ActionSupport;

@Namespace("/api/v1")
public class ProductAction extends ActionSupport {
    @Action("/products")
    public String list() { return SUCCESS; }

    @Action("/products/{id}")
    public String show() { return SUCCESS; }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "ProductAction.java",
	})

	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_ACTION_ANNOTATION" {
			routes[e.Properties["path"].(string)] = true
		}
	}

	if !routes["/api/v1/products"] {
		t.Errorf("[#3089 route_extraction] expected /api/v1/products with namespace prefix, got %v", routes)
	}
	if !routes["/api/v1/products/{id}"] {
		t.Errorf("[#3089 route_extraction] expected /api/v1/products/{id} with namespace prefix, got %v", routes)
	}
}

// TestStruts_RouteExtraction_XMLConfig_Issue3089 proves that struts.xml
// <action> elements are extracted from XML config files.
// Registry target: lang.java.framework.struts Routing/route_extraction → partial.
// Cite: internal/custom/java/struts_routes.go
func TestStruts_RouteExtraction_XMLConfig_Issue3089(t *testing.T) {
	source := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE struts PUBLIC "-//Apache Software Foundation//DTD Struts Configuration 2.5//EN"
    "http://struts.apache.org/dtds/struts-2.5.dtd">
<struts>
    <package name="default" namespace="/app" extends="struts-default">
        <action name="users" class="com.example.UserAction" method="list">
            <result name="success">/WEB-INF/jsp/users.jsp</result>
        </action>
        <action name="createUser" class="com.example.UserAction" method="create">
            <result name="success" type="redirectAction">users</result>
        </action>
        <action name="userDetail" class="com.example.UserAction" method="show">
            <result>/WEB-INF/jsp/userDetail.jsp</result>
        </action>
    </package>
    <package name="admin" namespace="/admin" extends="struts-default">
        <action name="dashboard" class="com.example.AdminAction">
            <result>/WEB-INF/jsp/admin.jsp</result>
        </action>
    </package>
</struts>
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "struts.xml",
	})

	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_XML_ACTION" {
			routes[e.Properties["path"].(string)] = true
		}
	}

	for _, want := range []string{
		"/app/users",
		"/app/createUser",
		"/app/userDetail",
		"/admin/dashboard",
	} {
		if !routes[want] {
			t.Errorf("[#3089 route_extraction] expected route %q, got %v", want, routes)
		}
	}
}

// TestStruts_RouteExtraction_XMLConfig_NoNamespace_Issue3089 proves that
// <action> elements in packages without a namespace are extracted with a
// leading slash.
func TestStruts_RouteExtraction_XMLConfig_NoNamespace_Issue3089(t *testing.T) {
	source := `<?xml version="1.0" encoding="UTF-8"?>
<struts>
    <package name="default" extends="struts-default">
        <action name="login" class="com.example.LoginAction">
            <result>/login.jsp</result>
        </action>
        <action name="logout" class="com.example.LoginAction" method="logout">
            <result type="redirect">/login.action</result>
        </action>
    </package>
</struts>
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "struts.xml",
	})

	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_XML_ACTION" {
			routes[e.Properties["path"].(string)] = true
		}
	}

	if !routes["/login"] {
		t.Errorf("[#3089 route_extraction] expected /login, got %v", routes)
	}
	if !routes["/logout"] {
		t.Errorf("[#3089 route_extraction] expected /logout, got %v", routes)
	}
}

// TestStruts_HandlerAttribution_Annotation_Issue3089 proves that the
// enclosing action class is attributed as the handler for @Action routes.
// Registry target: Routing/handler_attribution is already partial via YAML rule.
func TestStruts_HandlerAttribution_Annotation_Issue3089(t *testing.T) {
	source := `
import org.apache.struts2.convention.annotation.Action;
import com.opensymphony.xwork2.ActionSupport;

public class InvoiceAction extends ActionSupport {
    @Action("/invoices/list")
    public String list() { return SUCCESS; }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "InvoiceAction.java",
	})

	foundRoute := false
	foundHandler := false
	foundRel := false

	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_ACTION_ANNOTATION" {
			foundRoute = true
		}
		if e.Provenance == "INFERRED_FROM_STRUTS_ACTION_HANDLER" {
			foundHandler = true
			if e.Name != "InvoiceAction" {
				t.Errorf("[#3089 handler_attribution] expected handler name=InvoiceAction, got %q", e.Name)
			}
		}
	}
	for _, rel := range r.Relationships {
		if rel.RelationshipType == "HANDLED_BY" && rel.Properties["framework"] == "struts" {
			foundRel = true
		}
	}

	if !foundRoute {
		t.Errorf("[#3089 handler_attribution] expected INFERRED_FROM_STRUTS_ACTION_ANNOTATION entity")
	}
	if !foundHandler {
		t.Errorf("[#3089 handler_attribution] expected INFERRED_FROM_STRUTS_ACTION_HANDLER entity")
	}
	if !foundRel {
		t.Errorf("[#3089 handler_attribution] expected HANDLED_BY relationship")
	}
}

// TestStruts_HandlerAttribution_XML_Issue3089 proves that the action class
// and method from struts.xml are attributed as handlers.
func TestStruts_HandlerAttribution_XML_Issue3089(t *testing.T) {
	source := `<?xml version="1.0" encoding="UTF-8"?>
<struts>
    <package name="default" namespace="/" extends="struts-default">
        <action name="search" class="com.example.SearchAction" method="execute">
            <result>/search.jsp</result>
        </action>
    </package>
</struts>
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "struts.xml",
	})

	foundHandler := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_XML_HANDLER" {
			foundHandler = true
			if e.Properties["class"] != "com.example.SearchAction" {
				t.Errorf("[#3089 handler_attribution] expected class=com.example.SearchAction, got %v", e.Properties["class"])
			}
			if e.Properties["method"] != "execute" {
				t.Errorf("[#3089 handler_attribution] expected method=execute, got %v", e.Properties["method"])
			}
		}
	}
	if !foundHandler {
		t.Errorf("[#3089 handler_attribution] expected INFERRED_FROM_STRUTS_XML_HANDLER entity")
	}
}

// ----------------------------------------------------------------------------
// Middleware: middleware_coverage — interceptor chain
// ----------------------------------------------------------------------------

// TestStruts_Middleware_InterceptorImpl_Issue3089 proves that classes
// implementing the Interceptor interface are extracted as Middleware entities.
// Registry target: lang.java.framework.struts Middleware/middleware_coverage → partial.
// Cite: internal/custom/java/struts_routes.go
func TestStruts_Middleware_InterceptorImpl_Issue3089(t *testing.T) {
	source := `
package com.example;

import com.opensymphony.xwork2.interceptor.Interceptor;
import com.opensymphony.xwork2.ActionInvocation;

public class LoggingInterceptor implements Interceptor {

    @Override
    public void init() {}

    @Override
    public void destroy() {}

    @Override
    public String intercept(ActionInvocation invocation) throws Exception {
        System.out.println("Before action: " + invocation.getAction());
        String result = invocation.invoke();
        System.out.println("After action: " + result);
        return result;
    }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "LoggingInterceptor.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Kind == "Middleware" && e.Provenance == "INFERRED_FROM_STRUTS_INTERCEPTOR" {
			found = true
			if e.Name != "LoggingInterceptor" {
				t.Errorf("[#3089 middleware_coverage] expected name=LoggingInterceptor, got %q", e.Name)
			}
			if e.Properties["middleware_type"] != "interceptor" {
				t.Errorf("[#3089 middleware_coverage] expected middleware_type=interceptor, got %v", e.Properties["middleware_type"])
			}
			if e.Properties["framework"] != "struts" {
				t.Errorf("[#3089 middleware_coverage] expected framework=struts, got %v", e.Properties["framework"])
			}
		}
	}
	if !found {
		t.Errorf("[#3089 middleware_coverage] expected Middleware entity from implements Interceptor")
	}
}

// TestStruts_Middleware_AbstractInterceptor_Issue3089 proves that classes
// extending AbstractInterceptor are extracted as Middleware entities.
func TestStruts_Middleware_AbstractInterceptor_Issue3089(t *testing.T) {
	source := `
import com.opensymphony.xwork2.interceptor.AbstractInterceptor;
import com.opensymphony.xwork2.ActionInvocation;

public class AuthInterceptor extends AbstractInterceptor {
    @Override
    public String intercept(ActionInvocation invocation) throws Exception {
        String user = (String) invocation.getInvocationContext().getSession().get("user");
        if (user == null) {
            return "login";
        }
        return invocation.invoke();
    }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "AuthInterceptor.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Kind == "Middleware" && e.Provenance == "INFERRED_FROM_STRUTS_INTERCEPTOR" {
			found = true
			if e.Name != "AuthInterceptor" {
				t.Errorf("[#3089 middleware_coverage] expected name=AuthInterceptor, got %q", e.Name)
			}
		}
	}
	if !found {
		t.Errorf("[#3089 middleware_coverage] expected Middleware entity from extends AbstractInterceptor")
	}
}

// TestStruts_Middleware_InterceptMethod_Issue3089 proves that a class with
// an intercept(ActionInvocation) override is detected even without an
// explicit implements/extends declaration in the same file.
func TestStruts_Middleware_InterceptMethod_Issue3089(t *testing.T) {
	source := `
import com.opensymphony.xwork2.ActionInvocation;

public class TimingInterceptor extends BaseFrameworkInterceptor {
    @Override
    public String intercept(ActionInvocation invocation) throws Exception {
        long start = System.currentTimeMillis();
        String result = invocation.invoke();
        long elapsed = System.currentTimeMillis() - start;
        System.out.println("Elapsed: " + elapsed + "ms");
        return result;
    }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "TimingInterceptor.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Kind == "Middleware" && e.Provenance == "INFERRED_FROM_STRUTS_INTERCEPTOR" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3089 middleware_coverage] expected Middleware entity from intercept(ActionInvocation) override")
	}
}

// ----------------------------------------------------------------------------
// Auth: auth_coverage — JAAS + Spring Security integration markers
// ----------------------------------------------------------------------------

// TestStruts_Auth_JAAS_Issue3089 proves that JAAS LoginContext usage in a
// Struts interceptor is detected as auth coverage evidence.
// Registry target: lang.java.framework.struts Auth/auth_coverage → partial.
// Cite: internal/custom/java/struts_routes.go
func TestStruts_Auth_JAAS_Issue3089(t *testing.T) {
	source := `
import javax.security.auth.login.LoginContext;
import javax.security.auth.Subject;
import com.opensymphony.xwork2.interceptor.AbstractInterceptor;

public class JaasInterceptor extends AbstractInterceptor {
    @Override
    public String intercept(ActionInvocation invocation) throws Exception {
        LoginContext lc = new LoginContext("appDomain", new SimpleCallbackHandler());
        lc.login();
        Subject subject = lc.getSubject();
        // proceed with authenticated subject
        return invocation.invoke();
    }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "JaasInterceptor.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_AUTH_JAAS" {
			found = true
			if e.Properties["auth_type"] != "jaas" {
				t.Errorf("[#3089 auth_coverage] expected auth_type=jaas, got %v", e.Properties["auth_type"])
			}
			if e.Properties["framework"] != "struts" {
				t.Errorf("[#3089 auth_coverage] expected framework=struts, got %v", e.Properties["framework"])
			}
		}
	}
	if !found {
		t.Errorf("[#3089 auth_coverage] expected INFERRED_FROM_STRUTS_AUTH_JAAS entity")
	}
}

// TestStruts_Auth_SpringSecurity_Issue3089 proves that Spring Security markers
// (SecurityContextHolder, @PreAuthorize) are detected as auth evidence in a
// Struts+Spring integration scenario.
func TestStruts_Auth_SpringSecurity_Issue3089(t *testing.T) {
	source := `
import org.springframework.security.core.context.SecurityContextHolder;
import org.springframework.security.core.Authentication;
import com.opensymphony.xwork2.ActionSupport;

public class SecureAction extends ActionSupport {
    public String execute() {
        Authentication auth = SecurityContextHolder.getContext().getAuthentication();
        if (auth == null || !auth.isAuthenticated()) {
            return LOGIN;
        }
        return SUCCESS;
    }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "SecureAction.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_AUTH_SPRING_SECURITY" {
			found = true
			if e.Properties["auth_type"] != "spring_security" {
				t.Errorf("[#3089 auth_coverage] expected auth_type=spring_security, got %v", e.Properties["auth_type"])
			}
		}
	}
	if !found {
		t.Errorf("[#3089 auth_coverage] expected INFERRED_FROM_STRUTS_AUTH_SPRING_SECURITY entity")
	}
}

// TestStruts_Auth_SpringPreAuthorize_Issue3089 proves that @PreAuthorize
// annotation is detected as Spring Security auth evidence.
func TestStruts_Auth_SpringPreAuthorize_Issue3089(t *testing.T) {
	source := `
import org.springframework.security.access.prepost.PreAuthorize;
import com.opensymphony.xwork2.ActionSupport;

public class AdminAction extends ActionSupport {
    @PreAuthorize("hasRole('ADMIN')")
    public String adminOnly() { return SUCCESS; }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "AdminAction.java",
	})

	found := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_AUTH_SPRING_SECURITY" {
			found = true
		}
	}
	if !found {
		t.Errorf("[#3089 auth_coverage] expected INFERRED_FROM_STRUTS_AUTH_SPRING_SECURITY for @PreAuthorize")
	}
}

// ----------------------------------------------------------------------------
// Gating: extractor must not fire for wrong framework or absent signal
// ----------------------------------------------------------------------------

// TestStruts_Gating_WrongFramework_Issue3089 confirms the extractor does not
// fire for non-struts frameworks.
func TestStruts_Gating_WrongFramework_Issue3089(t *testing.T) {
	source := `
import org.apache.struts2.convention.annotation.Action;
import com.opensymphony.xwork2.ActionSupport;

public class UserAction extends ActionSupport {
    @Action("/users/list")
    public String list() { return SUCCESS; }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "spring_boot", // wrong framework
		FilePath:  "UserAction.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3089 gating] expected 0 entities for framework=spring_boot, got %d", len(r.Entities))
	}
}

// TestStruts_Gating_NoSignal_Issue3089 confirms the extractor no-ops on Java
// files with no Struts signals.
func TestStruts_Gating_NoSignal_Issue3089(t *testing.T) {
	source := `
public class OrderService {
    public Order findById(long id) { return null; }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "OrderService.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3089 gating] expected 0 entities for file without Struts signal, got %d", len(r.Entities))
	}
}

// ----------------------------------------------------------------------------
// AOP: not_applicable — Struts uses interceptors, not AspectJ AOP
// ----------------------------------------------------------------------------

// TestStruts_AOP_NotApplicable_Issue3089 confirms that the Spring AOP
// extractor does NOT fire for framework=struts.
// Registry target: AOP/advice_attribution, AOP/aspect_extraction,
//
//	AOP/pointcut_resolution → not_applicable.
func TestStruts_AOP_NotApplicable_Issue3089(t *testing.T) {
	source := `
import org.aspectj.lang.annotation.Aspect;
import org.aspectj.lang.annotation.Before;
import com.opensymphony.xwork2.ActionSupport;

// Note: @Aspect is NOT a Struts concept — Spring AOP extractor must not
// fire for framework=struts. Struts uses its interceptor chain for AOP-like
// cross-cutting concerns, not AspectJ.
@Aspect
public class LoggingAspect {
    @Before("execution(* com.example.*.*(..))")
    public void logBefore() {}
}
`
	r := ExtractSpringAOP(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "LoggingAspect.java",
	})
	if len(r.Entities) != 0 {
		t.Errorf("[#3089 aop-not-applicable] Spring AOP extractor must not fire for framework=struts, got %d entities", len(r.Entities))
	}
}

// ----------------------------------------------------------------------------
// Comprehensive: full Struts app with routes, interceptors, and auth
// ----------------------------------------------------------------------------

// TestStruts_FullApp_Issue3089 verifies a realistic Struts app with @Action
// routes, custom interceptors, and auth integration all extracted correctly.
// This is the comprehensive proving test that justifies partial status for
// route_extraction, middleware_coverage, and auth_coverage.
func TestStruts_FullApp_Issue3089(t *testing.T) {
	source := `
package com.example;

import org.apache.struts2.convention.annotation.Action;
import org.apache.struts2.convention.annotation.Namespace;
import org.apache.struts2.convention.annotation.Result;
import com.opensymphony.xwork2.ActionSupport;
import com.opensymphony.xwork2.interceptor.AbstractInterceptor;
import com.opensymphony.xwork2.ActionInvocation;
import org.springframework.security.core.context.SecurityContextHolder;

@Namespace("/api")
public class CustomerAction extends ActionSupport {

    @Action(value="/customers/list")
    @Result(name="success", location="customers.jsp")
    public String list() {
        return SUCCESS;
    }

    @Action(value="/customers/new")
    public String create() {
        return SUCCESS;
    }

    @Action(value="/customers/{id}")
    public String show() {
        return SUCCESS;
    }

    @Action(value="/customers/{id}/delete")
    public String delete() {
        return SUCCESS;
    }
}

class SecurityInterceptor extends AbstractInterceptor {
    @Override
    public String intercept(ActionInvocation invocation) throws Exception {
        var auth = SecurityContextHolder.getContext().getAuthentication();
        if (auth == null || !auth.isAuthenticated()) {
            return LOGIN;
        }
        return invocation.invoke();
    }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "CustomerAction.java",
	})

	// Validate routes (namespace + action paths)
	routes := make(map[string]bool)
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_ACTION_ANNOTATION" {
			routes[e.Properties["path"].(string)] = true
		}
	}
	for _, want := range []string{
		"/api/customers/list",
		"/api/customers/new",
		"/api/customers/{id}",
		"/api/customers/{id}/delete",
	} {
		if !routes[want] {
			t.Errorf("[#3089 full-app] expected route %q, got %v", want, routes)
		}
	}

	// Validate interceptor middleware
	foundInterceptor := false
	for _, e := range r.Entities {
		if e.Kind == "Middleware" && e.Provenance == "INFERRED_FROM_STRUTS_INTERCEPTOR" {
			foundInterceptor = true
		}
	}
	if !foundInterceptor {
		t.Errorf("[#3089 full-app] expected Middleware entity for SecurityInterceptor")
	}

	// Validate auth (Spring Security marker in the interceptor)
	foundAuth := false
	for _, e := range r.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_AUTH_SPRING_SECURITY" {
			foundAuth = true
		}
	}
	if !foundAuth {
		t.Errorf("[#3089 full-app] expected INFERRED_FROM_STRUTS_AUTH_SPRING_SECURITY entity")
	}
}

// TestStruts_XMLAndAnnotation_Coexist_Issue3089 ensures that annotation-based
// and XML-based route extraction can coexist in the same test run.
func TestStruts_XMLAndAnnotation_Coexist_Issue3089(t *testing.T) {
	xmlSource := `<?xml version="1.0" encoding="UTF-8"?>
<struts>
    <package name="default" namespace="/legacy" extends="struts-default">
        <action name="home" class="com.example.HomeAction">
            <result>/home.jsp</result>
        </action>
    </package>
</struts>
`
	rXML := ExtractStruts(PatternContext{
		Source:    xmlSource,
		Language:  "java",
		Framework: "struts2",
		FilePath:  "struts.xml",
	})

	javaSource := `
import org.apache.struts2.convention.annotation.Action;
import com.opensymphony.xwork2.ActionSupport;

public class HomeAction extends ActionSupport {
    @Action("/home/index")
    public String index() { return SUCCESS; }
}
`
	rJava := ExtractStruts(PatternContext{
		Source:    javaSource,
		Language:  "java",
		Framework: "struts2",
		FilePath:  "HomeAction.java",
	})

	xmlRoutes := 0
	for _, e := range rXML.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_XML_ACTION" {
			xmlRoutes++
		}
	}
	if xmlRoutes == 0 {
		t.Errorf("[#3089 coexist] expected XML action routes, got 0")
	}

	annotationRoutes := 0
	for _, e := range rJava.Entities {
		if e.Provenance == "INFERRED_FROM_STRUTS_ACTION_ANNOTATION" {
			annotationRoutes++
		}
	}
	if annotationRoutes == 0 {
		t.Errorf("[#3089 coexist] expected annotation-based routes, got 0")
	}
}

// TestStruts_RouteProperties_Issue3089 confirms that route entities carry
// the expected properties (framework, route_type, http_verb, path).
func TestStruts_RouteProperties_Issue3089(t *testing.T) {
	source := `
import org.apache.struts2.convention.annotation.Action;
import com.opensymphony.xwork2.ActionSupport;

public class ItemAction extends ActionSupport {
    @Action("/items/list")
    public String list() { return SUCCESS; }
}
`
	r := ExtractStruts(PatternContext{
		Source:    source,
		Language:  "java",
		Framework: "struts",
		FilePath:  "ItemAction.java",
	})

	for _, e := range r.Entities {
		if e.Provenance != "INFERRED_FROM_STRUTS_ACTION_ANNOTATION" {
			continue
		}
		if e.Properties["framework"] != "struts" {
			t.Errorf("[#3089] expected framework=struts, got %v", e.Properties["framework"])
		}
		if e.Properties["route_type"] != "annotation" {
			t.Errorf("[#3089] expected route_type=annotation, got %v", e.Properties["route_type"])
		}
		if e.Properties["path"] != "/items/list" {
			t.Errorf("[#3089] expected path=/items/list, got %v", e.Properties["path"])
		}
		return
	}
	t.Errorf("[#3089] expected at least one INFERRED_FROM_STRUTS_ACTION_ANNOTATION entity")
}
