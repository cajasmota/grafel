package java_test

// Issue #2023 — Wave-10 retest of #1997 surfaced that @Inject REFERENCES
// edge emission is inconsistent across classes: AuthController works
// (3 edges) but TransfersController returns zero edges despite using
// the same `@Inject` pattern. Possible root causes from the report:
//
//   - Extra class-level annotations (@Secured, @Path, @Tag, ...) break
//     the @Inject scanner
//   - Multiple stacked field-level annotations
//   - Field type uses generic wrappers (Instance<T>, Provider<T>)
//   - Visibility / final / static modifiers on the field
//   - Multi-line @Inject patterns (annotation on its own line)
//   - Inject-with-args annotations (@Inject(qualifier="x"))
//
// This file locks down the contract: every reasonable shape of an
// @Inject-annotated field MUST produce a REFERENCES edge from the
// containing class entity to the injected leaf type. The matrix below
// runs through every variant the W10R2 fixture exercises plus a few
// nasty edge cases discovered while reproducing the bug.
//
// Test fixtures use the neutral `client_fixture_x` package name — no
// client names are leaked in this regression file.

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
)

func TestIssue2023_Inject_Consistency_AcrossControllerShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // sorted list of REFERENCES ToIDs expected on the class
	}{
		{
			name: "transfers-controller-real-shape",
			// Mirrors the W10R2 fixture: stacked class-level annotations
			// (@Secured @RequestScoped @Path @Tag @Consumes @Produces),
			// two @Inject fields on separate lines (annotation on its
			// own line, type+name on the next), ten HTTP handler methods.
			src: `package client_fixture_x.api;

import jakarta.inject.Inject;
import jakarta.ws.rs.core.Response;
import jakarta.enterprise.context.RequestScoped;
import jakarta.ws.rs.*;
import jakarta.ws.rs.core.MediaType;

@Secured
@RequestScoped
@Path("/transfers")
@Tag(name = "Transfers")
@Consumes(MediaType.APPLICATION_JSON)
@Produces(MediaType.APPLICATION_JSON)
public class TransfersController {

    @Inject
    TransfersService transfersService;

    @Inject
    TransferDifferenceReport differenceReport;

    @GET
    public Response all() { return transfersService.list(); }

    @POST
    public Response transfer() { return transfersService.transfer(); }

    @PUT
    public Response confirm() { return transfersService.confirm(); }

    @GET
    public Response incoming() { return transfersService.incoming(); }

    @GET
    public Response outgoing() { return transfersService.outgoing(); }

    @GET
    public Response difference() { return transfersService.difference(); }

    @GET
    public Response report() { return differenceReport.pdf(); }

    @POST
    public Response resolve() { return transfersService.resolve(); }

    @POST
    public Response adjust() { return transfersService.adjust(); }
}
`,
			want: []string{"TransferDifferenceReport", "TransfersService"},
		},
		{
			name: "auth-controller-baseline",
			// Mirrors the W10R1 fixture: smaller class, three @Inject
			// fields, fewer class-level annotations.
			src: `package client_fixture_x.api;

import jakarta.inject.Inject;

@Path("/auth")
public class AuthController {

    @Inject
    UsersService usersService;

    @Inject
    RolesService rolesService;

    @Inject
    TokenService tokenService;

    public Response login() { return null; }
    public Response logout() { return null; }
    public Response refresh() { return null; }
}
`,
			want: []string{"RolesService", "TokenService", "UsersService"},
		},
		{
			name: "all-modifier-permutations",
			src: `package client_fixture_x.api;

@Path("/m") public class ModifiersController {
    @Inject private A a;
    @Inject protected B b;
    @Inject public C c;
    @Inject final D d;
    @Inject D2 d2;
}
`,
			want: []string{"A", "B", "C", "D", "D2"},
		},
		{
			name: "annotation-stacking-orderings",
			src: `package client_fixture_x.api;

@Path("/x") public class StackingController {
    @Inject @NotNull A a;
    @NotNull @Inject B b;
    @Inject @Named("qa") C c;
    @Named("qb") @Inject D d;
}
`,
			want: []string{"A", "B", "C", "D"},
		},
		{
			name: "inject-with-args-and-fully-qualified",
			src: `package client_fixture_x.api;

@Path("/q") public class QualifiedController {
    @Inject(qualifier = "main") A a;
    @jakarta.inject.Inject B b;
    @javax.inject.Inject C c;
}
`,
			want: []string{"A", "B", "C"},
		},
		{
			name: "generic-wrappers-on-injected-types",
			// The leaf type of Instance<UsersService> is "Instance" by
			// design (leafTypeName strips generics). The test locks the
			// current behaviour — the regression we care about is "edge
			// emitted at all", not the specific wrapper-leaf semantics.
			// If a future fix unwraps Instance<T> -> T it will need to
			// update this expectation.
			src: `package client_fixture_x.api;

@Path("/g") public class GenericsController {
    @Inject Instance<UsersService> usersService;
    @Inject Provider<TokenService> tokenService;
    @Inject PlainType plain;
}
`,
			want: []string{"Instance", "PlainType", "Provider"},
		},
		{
			name: "javadoc-and-blank-lines-between-fields",
			src: `package client_fixture_x.api;

@Path("/d") public class DocController {

    /** The users service. */
    @Inject
    UsersService usersService;

    // line comment
    @Inject
    AuditLog auditLog;

    /**
     * Multi-line javadoc.
     */
    @Inject
    NotificationService notificationService;
}
`,
			want: []string{"AuditLog", "NotificationService", "UsersService"},
		},
		{
			name: "many-fields-then-many-methods",
			// Pre-fix hypothesis (now refuted) was that a per-class slice
			// cap could lose later fields. This case exercises a class
			// with five @Inject fields and ten methods.
			src: `package client_fixture_x.api;

@Secured @RequestScoped @Path("/many") @Tag(name="many") @Consumes("application/json") @Produces("application/json")
public class ManyController {
    @Inject AService a;
    @Inject BService b;
    @Inject CService c;
    @Inject DService d;
    @Inject EService e;

    public Response m1() { return null; }
    public Response m2() { return null; }
    public Response m3() { return null; }
    public Response m4() { return null; }
    public Response m5() { return null; }
    public Response m6() { return null; }
    public Response m7() { return null; }
    public Response m8() { return null; }
    public Response m9() { return null; }
    public Response m10() { return null; }
}
`,
			want: []string{"AService", "BService", "CService", "DService", "EService"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext, ok := extractor.Get("java")
			if !ok {
				t.Fatal("java extractor not registered")
			}
			out, err := ext.Extract(context.Background(), extractor.FileInput{
				Path:     "client_fixture_x/api/" + tc.name + ".java",
				Content:  []byte(tc.src),
				Language: "java",
				TSTree:   parseForTest(t, tc.src),
			})
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			// Aggregate REFERENCES targets across all SCOPE.Component
			// entities (each fixture has exactly one outer class).
			var got []string
			seen := map[string]bool{}
			for _, e := range out {
				if e.Kind != "SCOPE.Component" {
					continue
				}
				for _, r := range e.Relationships {
					if r.Kind != "REFERENCES" {
						continue
					}
					if !seen[r.ToID] {
						seen[r.ToID] = true
						got = append(got, r.ToID)
					}
				}
			}
			sort.Strings(got)

			gotSet := strings.Join(got, ",")
			wantSet := strings.Join(tc.want, ",")
			if gotSet != wantSet {
				t.Fatalf("REFERENCES mismatch:\n  got  = [%s]\n  want = [%s]\n\nfixture:\n%s",
					gotSet, wantSet, tc.src)
			}
		})
	}
}

// TestIssue2023_Inject_NonEmptyForControllerWithStackedAnnotations is the
// narrow guard the W10R2 bug report demanded: a TransfersController-shape
// class with stacked class-level annotations MUST emit at least one
// REFERENCES edge. Catches a future regression that breaks the @Inject
// scanner on heavily-annotated classes even if the exact target list
// changes (e.g. leaf-type extraction is reworked).
func TestIssue2023_Inject_NonEmptyForControllerWithStackedAnnotations(t *testing.T) {
	src := `package client_fixture_x.api;

import jakarta.inject.Inject;

@Secured
@RequestScoped
@Path("/transfers")
@Tag(name = "Transfers")
@Consumes("application/json")
@Produces("application/json")
public class TransfersController {

    @Inject
    TransfersService transfersService;

    @Inject
    TransferDifferenceReport differenceReport;

    public Response method() { return null; }
}
`
	ext, _ := extractor.Get("java")
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "client_fixture_x/api/TransfersController.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   parseForTest(t, src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var refCount int
	for _, e := range out {
		if e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" {
				refCount++
			}
		}
	}
	if refCount == 0 {
		t.Fatalf("TransfersController emitted ZERO REFERENCES edges — #2023 regression. " +
			"Stacked class-level annotations broke the @Inject scanner.")
	}
}
