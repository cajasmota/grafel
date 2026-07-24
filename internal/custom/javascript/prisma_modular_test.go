package javascript_test

import (
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/javascript"
)

// prismaFileInput reads a `.prisma` file from testdata and returns a FileInput
// whose Path is repo-root-relative and whose RepoRoot points at the fixture
// root, so the extractor can discover sibling `.prisma` files (the modular
// split-schema layout).
func prismaFileInput(t *testing.T, fixtureRoot, relPath string) extreg.FileInput {
	t.Helper()
	abs, err := filepath.Abs(fixtureRoot)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(abs, filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relPath, err)
	}
	return extreg.FileInput{
		Path:     relPath,
		Language: "prisma",
		Content:  content,
		RepoRoot: abs,
	}
}

func referencesEdge(ents []types.EntityRecord, target, fieldName string) *types.RelationshipRecord {
	for ei := range ents {
		for ri := range ents[ei].Relationships {
			r := &ents[ei].Relationships[ri]
			if r.Kind == string(types.RelationshipKindReferences) &&
				r.ToID == "Class:"+target &&
				r.Properties.Get("field_name") == fieldName {
				return r
			}
		}
	}
	return nil
}

func hasModelEntity(ents []types.EntityRecord, name string) bool {
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "model" && e.Name == name {
			return true
		}
	}
	return false
}

// TestPrismaModularSplitSchema verifies that under the `prismaSchemaFolder`
// modular layout (prisma/schema/*.prisma, one domain per file), cross-file
// model relations and enum references resolve as one logical schema.
//
//	user.prisma:  model User  { role Role; posts Post[] }
//	post.prisma:  model Post  { status Status; author User @relation(...) }
//	enums.prisma: enum Role / enum Status
func TestPrismaModularSplitSchema(t *testing.T) {
	const root = "testdata/prisma_modular"

	// post.prisma — the file with the cross-file @relation to User.
	postEnts := extractEnts(t, "custom_js_prisma",
		prismaFileInput(t, root, "prisma/schema/post.prisma"))

	if !hasModelEntity(postEnts, "Post") {
		t.Fatal("expected model Post extracted from post.prisma")
	}

	// Post.author User @relation — the singular FK-owning side → many_to_one,
	// resolving cross-file to User (declared in user.prisma).
	assertCardinality(t,
		graphRelatesEdge(postEnts, "Class:Post", "Class:User"),
		"Class:Post", "Class:User", "many_to_one")

	// The field-level REFERENCES edge to the cross-file User model.
	if referencesEdge(postEnts, "User", "author") == nil {
		t.Error("expected cross-file REFERENCES edge author → User")
	}

	// Post.status Status — enum declared in enums.prisma resolves cross-file.
	if referencesEdge(postEnts, "Status", "status") == nil {
		t.Error("expected cross-file REFERENCES edge status → Status (enum)")
	}

	// user.prisma — model User with posts Post[] (one_to_many) to the
	// cross-file Post model, plus role Role (cross-file enum).
	userEnts := extractEnts(t, "custom_js_prisma",
		prismaFileInput(t, root, "prisma/schema/user.prisma"))

	if !hasModelEntity(userEnts, "User") {
		t.Fatal("expected model User extracted from user.prisma")
	}
	assertCardinality(t,
		graphRelatesEdge(userEnts, "Class:User", "Class:Post"),
		"Class:User", "Class:Post", "one_to_many")
	if referencesEdge(userEnts, "Role", "role") == nil {
		t.Error("expected cross-file REFERENCES edge role → Role (enum)")
	}

	// Models keep their real source file (no synthetic relocation).
	for _, e := range userEnts {
		if e.Name == "User" && e.SourceFile != "prisma/schema/user.prisma" {
			t.Errorf("User source file: want prisma/schema/user.prisma, got %q", e.SourceFile)
		}
	}
}

// TestPrismaSingleFileSchemaNoRegression verifies the single-`schema.prisma`
// case (one file = the union of one) still resolves intra-file relations and
// does NOT trigger sibling-folder unioning (a lone .prisma file in its folder).
func TestPrismaSingleFileSchemaNoRegression(t *testing.T) {
	const root = "testdata/prisma_single"

	ents := extractEnts(t, "custom_js_prisma",
		prismaFileInput(t, root, "prisma/schema.prisma"))

	if !hasModelEntity(ents, "User") || !hasModelEntity(ents, "Order") {
		t.Fatal("expected User and Order models from single schema.prisma")
	}
	assertCardinality(t,
		graphRelatesEdge(ents, "Class:User", "Class:Order"),
		"Class:User", "Class:Order", "one_to_many")
	assertCardinality(t,
		graphRelatesEdge(ents, "Class:Order", "Class:User"),
		"Class:Order", "Class:User", "many_to_one")
	// Intra-file enum reference still resolves.
	if referencesEdge(ents, "Role", "role") == nil {
		t.Error("expected intra-file REFERENCES edge role → Role (enum)")
	}
}
