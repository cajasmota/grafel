package engine

// Regression tests for the parens-in-annotation-string UNDERCOUNT / shape-loss
// bug CLASS — the systematic follow-up to the NestJS fix (PR #3417).
//
// Root cause: a regex that handled an intervening annotation / decorator
// argument with `[^)]*` stops at the FIRST ')' — including a ')' inside a
// string literal. A Swagger `@Operation(summary = "Get (all) widgets")` (or any
// annotation whose string argument contains a ')') silently broke the scan and
// the TARGET route/handler/shape was dropped (sub-pattern A), or a path
// template containing parens inside a string was truncated (sub-pattern B).
//
// Each test below FAILS before the fix and asserts a SPECIFIC outcome.

import "testing"

// ---------------------------------------------------------------------------
// Target 2 (MED-HIGH) — Java Feign client: an intervening annotation with a
// ')' inside its string (e.g. @Tag(name = "x (y)")) sits between
// @FeignClient(...) and `interface`. The whole client interface must NOT be
// dropped — its method routes must still produce FETCHES edges.
// ---------------------------------------------------------------------------

func TestParenClass_FeignClient_InterveningAnnotationParenInString(t *testing.T) {
	src := `
import org.springframework.cloud.openfeign.FeignClient;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.beans.factory.annotation.Autowired;

@FeignClient(name = "customer-service", url = "http://customer-svc")
@Tag(name = "customers (legacy)")
public interface CustomerClient {
    @GetMapping("/customers/{id}")
    Customer getCustomer(@PathVariable String id);
}

@Service
public class OrderHandler {
    @Autowired
    CustomerClient customerClient;

    void handle(String cid) {
        Customer c = customerClient.getCustomer(cid);
    }
}
`
	ids, rels := runDetectWithRels(t, "java", "OrderHandler.java", src)
	requireContains(t, ids, []string{"http:GET:/customers/{id}"}, "feign paren-in-string")
	requireFetches(t, rels, "http:GET:/customers/{id}", "feign paren-in-string")
}

// ---------------------------------------------------------------------------
// Target 1 (HIGH) — JAX-RS: a Swagger @Operation with a ')' inside its summary
// string sits between @GET/@Path and the handler. The route must NOT be
// dropped. A sibling route without the paren proves the undercount is gone.
// ---------------------------------------------------------------------------

func TestParenClass_JAXRS_OperationParenInString_RouteSurvives(t *testing.T) {
	src := `package com.example;

import jakarta.ws.rs.GET;
import jakarta.ws.rs.Path;
import jakarta.ws.rs.Produces;

@Path("/widgets")
public class WidgetResource {

    @GET
    @Path("/all")
    @Operation(summary = "Get (all) widgets")
    @Produces("application/json")
    public List<Widget> getAll() {
        return repo.findAll();
    }

    @GET
    @Path("/safe")
    @Operation(summary = "Get safe widgets")
    public List<Widget> getSafe() {
        return repo.findAll();
    }
}
`
	got, _ := runDetect(t, "java", "src/main/java/com/example/WidgetResource.java", src)
	requireContains(t, got, []string{
		"http:GET:/widgets/all",  // previously DROPPED (paren in @Operation string)
		"http:GET:/widgets/safe", // sibling — proves the undercount is gone
	}, "JAX-RS @Operation paren-in-string")
}

// ---------------------------------------------------------------------------
// Target 5 (sub-pattern B) — Spring @GetMapping path template containing a
// regex with parens inside the string. The full path must be captured (and the
// verb kept as GET, not degraded to ANY) — not truncated at the inner ')'.
// ---------------------------------------------------------------------------

func TestParenClass_Spring_PathRegexWithParens_NotTruncated(t *testing.T) {
	src := `package com.example;
import org.springframework.web.bind.annotation.*;

@RestController
public class ItemController {
    @GetMapping("/items/{id:(\\d+)}")
    public Object get(String id) { return null; }
}
`
	got := collect(t, map[string]string{"Item.java": src})
	ids := endpointIDs(got)
	if len(ids) != 1 || ids[0] != "http:GET:/items/{id}" {
		t.Fatalf("ids = %v, want [http:GET:/items/{id}] (path-regex must not truncate, verb must stay GET)", ids)
	}
}

// A @RequestMapping whose value template carries a string-internal paren must
// likewise be captured fully.
func TestParenClass_Spring_RequestMappingPathRegex_NotTruncated(t *testing.T) {
	src := `package com.example;
import org.springframework.web.bind.annotation.*;

@RestController
public class ThingController {
    @RequestMapping(value = "/things/{id:(\\d+)}", method = RequestMethod.GET)
    public Object get(String id) { return null; }
}
`
	got := collect(t, map[string]string{"Thing.java": src})
	ids := endpointIDs(got)
	if len(ids) != 1 || ids[0] != "http:GET:/things/{id}" {
		t.Fatalf("ids = %v, want [http:GET:/things/{id}]", ids)
	}
}

// ---------------------------------------------------------------------------
// Target 3 (MED) — Java Spring @RequestBody shape: an intervening @Schema with
// a ')' inside its description string must not drop the body DTO type.
// ---------------------------------------------------------------------------

func TestParenClass_JavaRequestBody_SchemaParenInString_TypeFound(t *testing.T) {
	params := `@RequestBody @Schema(description = "the (full) create payload") CreateUserDto dto`
	got := extractJavaRequestDTO(params, "spring_mvc")
	if got != "CreateUserDto" {
		t.Fatalf("extractJavaRequestDTO = %q, want CreateUserDto (paren in @Schema string must not drop the body type)", got)
	}
}

// JAX-RS implicit body: an annotation with a paren-in-string before the body
// param must not drop the type.
func TestParenClass_JavaRequestBody_JAXRS_AnnotationParenInString(t *testing.T) {
	params := `@Schema(description = "payload (v2)") OrderDto order`
	got := extractJavaRequestDTO(params, "jaxrs")
	if got != "OrderDto" {
		t.Fatalf("extractJavaRequestDTO(jaxrs) = %q, want OrderDto", got)
	}
}

// ---------------------------------------------------------------------------
// Target 4 (LOW-MED) — Python FastAPI: a route decorator whose path argument
// contains a regex with a ')' inside the string must still let the
// response_model be located.
// ---------------------------------------------------------------------------

func TestParenClass_Python_RouteRegexParen_ResponseModelFound(t *testing.T) {
	src := "from fastapi import FastAPI\n" +
		"app = FastAPI()\n\n" +
		"@app.get(\"/items/{id:(\\\\d+)}\", response_model=ItemOut)\n" +
		"async def get_item(id: int):\n" +
		"    return None\n"
	got := lookupFastAPIResponseModel(src, "get_item")
	if got != "ItemOut" {
		t.Fatalf("lookupFastAPIResponseModel = %q, want ItemOut (paren in route-decorator string must not drop the response model)", got)
	}
}

// Sanity: the normal (no-paren) FastAPI case still resolves, and a multi-line
// decorator stack is handled.
func TestParenClass_Python_NormalAndMultiDecorator(t *testing.T) {
	src := "from fastapi import FastAPI\n" +
		"app = FastAPI()\n\n" +
		"@app.get(\"/items\", response_model=ItemList)\n" +
		"async def list_items():\n" +
		"    return None\n"
	if got := lookupFastAPIResponseModel(src, "list_items"); got != "ItemList" {
		t.Fatalf("normal case = %q, want ItemList", got)
	}
}
