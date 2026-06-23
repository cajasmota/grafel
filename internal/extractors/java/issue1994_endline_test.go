package java_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
)

// Issue #1994 — every entity emitted by the Java extractor MUST carry
// non-zero start_line AND end_line so the docgen source_window helper
// can build a complete excerpt. The W5R1 reproducer was a Quarkus
// AuthController.login Operation entity whose end_line=0 sentinel
// triggered the bundle-side by-name fallback (#1987); the underlying
// extractor contract should also be honored.
//
// This regression covers BOTH primary-pass entities (class / method /
// field / constructor) AND synthesized entities (Lombok / Panache),
// because the synthesizers previously emitted zero line bounds.
func TestJava_EndLine_NonZero_ForOperationsClassesAndFields(t *testing.T) {
	src := `package client_fixture_x.api;

import jakarta.ws.rs.POST;
import jakarta.ws.rs.Path;
import jakarta.inject.Inject;

@Path("/auth")
public class AuthController {

    @Inject
    UsersService usersService;

    @POST
    @Path("/login")
    public Response login(LoginRequest req) {
        if (req == null) {
            return Response.status(400).build();
        }
        return usersService.authenticate(req);
    }

    public Response logout() {
        return Response.ok().build();
    }
}
`

	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "client_fixture_x/api/AuthController.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   parseForTest(t, src),
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	checkedClass := false
	checkedMethod := false
	checkedField := false

	for _, ent := range out {
		// EVERY entity must carry non-zero bounds (#1994 contract).
		if ent.StartLine <= 0 {
			t.Errorf("entity %q (%s/%s): start_line=%d (want > 0)",
				ent.Name, ent.Kind, ent.Subtype, ent.StartLine)
		}
		if ent.EndLine <= 0 {
			t.Errorf("entity %q (%s/%s): end_line=%d (want > 0)",
				ent.Name, ent.Kind, ent.Subtype, ent.EndLine)
		}
		if ent.EndLine > 0 && ent.StartLine > 0 && ent.EndLine < ent.StartLine {
			t.Errorf("entity %q: end_line(%d) < start_line(%d)",
				ent.Name, ent.EndLine, ent.StartLine)
		}

		switch ent.Name {
		case "AuthController":
			checkedClass = true
		case "AuthController.login":
			checkedMethod = true
		case "AuthController.usersService":
			checkedField = true
		}
	}

	if !checkedClass {
		t.Fatalf("class entity AuthController not emitted; out has %d entities", len(out))
	}
	if !checkedMethod {
		t.Fatalf("method entity AuthController.login not emitted; out has %d entities", len(out))
	}
	if !checkedField {
		t.Fatalf("field entity AuthController.usersService not emitted; out has %d entities", len(out))
	}
}

// Issue #1994 — synthesized entities (Lombok @Data getters / @Builder
// methods) also need non-zero line bounds. Anchoring them to the
// declaring class node is the convention.
func TestJava_EndLine_NonZero_OnLombokSynthesizedEntities(t *testing.T) {
	src := `package client_fixture_x.model;

import lombok.Data;
import lombok.Builder;

@Data
@Builder
public class Order {
    private Long id;
    private String name;
}
`

	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "client_fixture_x/model/Order.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   parseForTest(t, src),
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	// We require ALL synthesized Lombok ops/comps to carry non-zero bounds.
	sawSynth := false
	for _, ent := range out {
		isSynth := ent.Properties != nil &&
			(ent.Properties["synthesized_from"] != "")
		if !isSynth {
			continue
		}
		sawSynth = true
		if ent.StartLine <= 0 || ent.EndLine <= 0 {
			t.Errorf("synthesized entity %q (%s/%s): start=%d end=%d (both must be > 0; synthesized_from=%s)",
				ent.Name, ent.Kind, ent.Subtype, ent.StartLine, ent.EndLine,
				ent.Properties["synthesized_from"])
		}
	}
	if !sawSynth {
		t.Fatal("no synthesized Lombok entities found; @Data + @Builder should produce several")
	}
}
