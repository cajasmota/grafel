package java_test

// Issue #2111 — JAX-RS endpoint methods with method bodies that call injected
// services were emitting 0 CALLS edges when the callee method shares the same
// leaf name as the enclosing endpoint method (e.g. UsersController.create
// calling UsersService.create). The self-recursion guard in
// extractCallRelationships was using a bare-leaf comparison that
// incorrectly treated "UsersService.create" as a self-call from
// "UsersController.create" because both share the leaf "create".
//
// Root cause: the guard applied `leaf == callerName` to ALL targets including
// dotted (typed-receiver) targets. Dotted targets like "UsersService.create"
// are cross-type calls and must never be filtered by a bare-name match.
//
// Fix: restrict the self-recursion skip to bare-name targets only
// (strings.IndexByte(target, '.') < 0).
//
// Affected patterns in the wild:
//   - JAX-RS @POST create → usersService.create(req)    blocked as "self-call"
//   - @GET getSummary     → ordersService.getSummary()  blocked as "self-call"
//   - @GET getInventoryReport → inventoryService.getInventoryReport() blocked
//
// Any controller endpoint whose method name matches an injected service's
// method name would produce 0 CALLS edges from the extractor.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
)

// TestJAXRS_MethodBody_EmitsCALLS_SameLeafName is the primary regression
// for #2111: a JAX-RS @POST controller method named "create" calling
// usersService.create(req) MUST emit a CALLS edge to "UsersService.create".
// The @Inject + @POST + @Path annotations must not suppress body extraction.
func TestJAXRS_MethodBody_EmitsCALLS_SameLeafName(t *testing.T) {
	src := `package client_fixture_x.api;

import jakarta.ws.rs.POST;
import jakarta.ws.rs.Path;
import jakarta.inject.Inject;

@Path("/users")
public class UsersController {

    @Inject
    UsersService usersService;

    @POST
    @Path("/")
    public Response create(CreateUserRequest req) {
        return usersService.create(req);
    }
}
`
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "client_fixture_x/api/UsersController.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   parseForTest(t, src),
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	for _, ent := range out {
		if ent.Name == "UsersController.create" {
			for _, rel := range ent.Relationships {
				if rel.Kind == "CALLS" && rel.ToID == "UsersService.create" {
					return // pass
				}
			}
			t.Errorf("UsersController.create has no CALLS edge to UsersService.create; got: %+v",
				ent.Relationships)
			return
		}
	}
	t.Fatal("entity UsersController.create not found in output")
}

// TestJAXRS_MethodBody_EmitsCALLS_GetEndpoints covers the @GET variants
// reported in #2111: getSummary and getInventoryReport similarly blocked.
func TestJAXRS_MethodBody_EmitsCALLS_GetEndpoints(t *testing.T) {
	src := `package client_fixture_x.api;

import jakarta.ws.rs.GET;
import jakarta.ws.rs.Path;
import jakarta.inject.Inject;

@Path("/orders")
public class OrdersController {

    @Inject
    OrdersService ordersService;

    @Inject
    InventoryService inventoryService;

    @GET
    @Path("/summary")
    public Response getSummary() {
        return ordersService.getSummary();
    }

    @GET
    @Path("/inventory-report")
    public Response getInventoryReport() {
        return inventoryService.getInventoryReport();
    }
}
`
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "client_fixture_x/api/OrdersController.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   parseForTest(t, src),
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	cases := []struct {
		method string
		toID   string
	}{
		{"OrdersController.getSummary", "OrdersService.getSummary"},
		{"OrdersController.getInventoryReport", "InventoryService.getInventoryReport"},
	}
	for _, tc := range cases {
		var entity *struct{ rels []struct{ kind, toID string } }
		_ = entity
		found := false
		for _, ent := range out {
			if ent.Name == tc.method {
				for _, rel := range ent.Relationships {
					if rel.Kind == "CALLS" && rel.ToID == tc.toID {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: no CALLS edge to %s; got: %+v", tc.method, tc.toID, ent.Relationships)
				}
				break
			}
		}
	}
}

// TestJAXRS_MethodBody_NoFalsePositiveSelfRecursion ensures the fix does NOT
// break the true self-recursion guard: a bare method call to the same-named
// method (without a receiver) should still be filtered.
func TestJAXRS_MethodBody_NoFalsePositiveSelfRecursion(t *testing.T) {
	src := `package client_fixture_x.api;

public class Helper {
    public int compute(int n) {
        if (n <= 0) return 0;
        return compute(n - 1);
    }
}
`
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "client_fixture_x/api/Helper.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   parseForTest(t, src),
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	for _, ent := range out {
		if ent.Name == "Helper.compute" {
			for _, rel := range ent.Relationships {
				if rel.Kind == "CALLS" && rel.ToID == "compute" {
					t.Errorf("Helper.compute emitted a bare self-CALLS edge (self-recursion guard broken)")
				}
			}
			return
		}
	}
	t.Fatal("entity Helper.compute not found")
}

// TestJAXRS_MethodBody_MultipleServiceCalls ensures an endpoint body that
// calls multiple services emits CALLS edges for each, even when some share
// the same leaf name as the enclosing endpoint.
func TestJAXRS_MethodBody_MultipleServiceCalls(t *testing.T) {
	src := `package client_fixture_x.api;

import jakarta.ws.rs.POST;
import jakarta.ws.rs.Path;
import jakarta.inject.Inject;

@Path("/orders")
public class OrdersController {

    @Inject
    OrdersService ordersService;

    @Inject
    InventoryService inventoryService;

    @Inject
    NotificationService notificationService;

    @POST
    @Path("/")
    public Response create(CreateOrderRequest req) {
        Response r = ordersService.create(req);
        inventoryService.reserve(req.getItemId());
        notificationService.send(req.getUserId());
        return r;
    }
}
`
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "client_fixture_x/api/OrdersController.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   parseForTest(t, src),
	})
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	wantCALLS := map[string]bool{
		"OrdersService.create":     false,
		"InventoryService.reserve": false,
		"NotificationService.send": false,
	}

	for _, ent := range out {
		if ent.Name == "OrdersController.create" {
			for _, rel := range ent.Relationships {
				if rel.Kind == "CALLS" {
					if _, ok := wantCALLS[rel.ToID]; ok {
						wantCALLS[rel.ToID] = true
					}
				}
			}
			for toID, found := range wantCALLS {
				if !found {
					t.Errorf("OrdersController.create: missing CALLS edge to %s; got: %+v",
						toID, ent.Relationships)
				}
			}
			return
		}
	}
	t.Fatal("entity OrdersController.create not found")
}
