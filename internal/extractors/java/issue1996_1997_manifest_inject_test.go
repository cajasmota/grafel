package java_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
)

// Issue #1996 — Java class entities MUST emit EXTENDS / IMPLEMENTS edges
// so the docgen ClassManifest can populate the `bases` and `interfaces`
// fields with cross-language parity. Pre-fix Java emitted zero structural
// edges for class hierarchy; the manifest's bases array was always empty.
//
// Issue #1997 — @Inject-annotated fields MUST also produce a REFERENCES
// edge from the containing class entity to the injected type, matching
// the Python convention. The Schema/CONTAINS edge for the field stays
// (option B in #1997).
func TestJava_ClassManifest_EmitsExtendsImplementsAndInjectReferences(t *testing.T) {
	src := `package client_fixture_x.api;

import jakarta.inject.Inject;

@Secured
@RequestScoped
@Path("/api/transfers")
@Tag(name = "transfers")
public class TransfersController extends BaseController implements ApiService, Auditable {

    @Inject
    UsersService usersService;

    @Inject
    AuditLog auditLog;

    public Response list() {
        return Response.ok().build();
    }
}
`

	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "client_fixture_x/api/TransfersController.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   parseForTest(t, src),
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	var classRels []string // ToID:Kind tokens for the class entity
	var classSig string
	for _, ent := range out {
		if ent.Name != "TransfersController" || ent.Kind != "SCOPE.Component" {
			continue
		}
		classSig = ent.Signature
		for _, r := range ent.Relationships {
			classRels = append(classRels, r.Kind+":"+r.ToID)
		}
	}
	if len(classRels) == 0 {
		t.Fatalf("TransfersController class entity not found or has no relationships; out has %d entities", len(out))
	}

	hasRel := func(kind, toID string) bool {
		needle := kind + ":" + toID
		for _, r := range classRels {
			if r == needle {
				return true
			}
		}
		return false
	}

	// #1996 — EXTENDS edge.
	if !hasRel("EXTENDS", "BaseController") {
		t.Errorf("expected EXTENDS BaseController edge; got rels=%v", classRels)
	}
	// #1996 — IMPLEMENTS edges (both interfaces).
	if !hasRel("IMPLEMENTS", "ApiService") {
		t.Errorf("expected IMPLEMENTS ApiService edge; got rels=%v", classRels)
	}
	if !hasRel("IMPLEMENTS", "Auditable") {
		t.Errorf("expected IMPLEMENTS Auditable edge; got rels=%v", classRels)
	}

	// #1997 — REFERENCES edges to injected types.
	if !hasRel("REFERENCES", "UsersService") {
		t.Errorf("expected REFERENCES UsersService edge (from @Inject); got rels=%v", classRels)
	}
	if !hasRel("REFERENCES", "AuditLog") {
		t.Errorf("expected REFERENCES AuditLog edge (from @Inject); got rels=%v", classRels)
	}

	// #1996 — class signature must retain the @-prefixed annotation tokens
	// so the docgen ClassManifest decorator regex can extract them. We
	// don't assert exact text (whitespace varies) but each annotation
	// must appear as an @Name token in the signature.
	for _, ann := range []string{"@Secured", "@RequestScoped", "@Path", "@Tag"} {
		if !contains(classSig, ann) {
			t.Errorf("expected class signature to contain %q (for ClassManifest decorators); got %q",
				ann, classSig)
		}
	}
}

// contains is strings.Contains without the import (the test file already
// pulls heavy package deps; this avoids adding another import line).
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
