package python_test

// graphql_dataloader_test.go — value-asserting tests for the
// python_graphql_dataloader extractor (#3624, epic #3607). Asserts the actual
// loader entity, BATCHES edge, and resolver→loader USES edge — not len>0.

import (
	"context"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/python"
	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func runPyDataLoader(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extractor.Get("python_graphql_dataloader")
	if !ok {
		t.Fatal("python_graphql_dataloader not registered")
	}
	ents, err := e.Extract(context.Background(),
		extractor.FileInput{Path: "resolvers.py", Language: "python", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

func findEdge(ents []types.EntityRecord, kind, toID string) *types.RelationshipRecord {
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == kind && r.ToID == toID {
				return r
			}
		}
	}
	return nil
}

// TestPyDataLoader_LoadFnKwarg is the value-asserting case:
//
//	user_loader = DataLoader(load_fn=batch_users)   → loader user_loader BATCHES batch_users
//	await user_loader.load(self.author_id)           → resolver author USES user_loader
func TestPyDataLoader_LoadFnKwarg(t *testing.T) {
	src := `
from aiodataloader import DataLoader

async def batch_users(keys):
    return keys

user_loader = DataLoader(load_fn=batch_users)

@strawberry.field
async def author(self, info):
    return await user_loader.load(self.author_id)
`
	ents := runPyDataLoader(t, src)

	// 1. Loader entity.
	var loader *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == string(types.EntityKindDataLoader) && ents[i].Name == "user_loader" {
			loader = &ents[i]
			break
		}
	}
	if loader == nil {
		t.Fatal("expected SCOPE.DataLoader entity 'user_loader'")
	}
	if loader.Properties["via"] != "graphql_dataloader" {
		t.Fatalf("loader via = %q; want graphql_dataloader", loader.Properties["via"])
	}

	// 2. BATCHES edge user_loader → batch_users.
	if e := findEdge(ents, string(types.RelationshipKindBatches), "batch_users"); e == nil {
		t.Fatal("expected BATCHES edge user_loader → batch_users")
	} else if e.Properties["via"] != "graphql_dataloader" {
		t.Fatalf("BATCHES via = %q; want graphql_dataloader", e.Properties["via"])
	}

	// 3. USES edge author resolver → user_loader.
	usesEdge := findEdge(ents, string(types.RelationshipKindUses), "user_loader")
	if usesEdge == nil {
		t.Fatal("expected USES edge resolver → user_loader")
	}
	if usesEdge.Properties["via"] != "graphql_dataloader" {
		t.Fatalf("USES via = %q; want graphql_dataloader", usesEdge.Properties["via"])
	}
	// The carrier must be the enclosing resolver 'author'.
	var carrierOK bool
	for i := range ents {
		if ents[i].Subtype == "resolver" && ents[i].Properties["resolver_name"] == "author" {
			carrierOK = true
		}
	}
	if !carrierOK {
		t.Fatal("expected USES carrier to be the enclosing resolver 'author'")
	}
}

// TestPyDataLoader_PositionalBatchFn covers DataLoader(batch_users) (positional)
// and a load_many() call site.
func TestPyDataLoader_PositionalBatchFn(t *testing.T) {
	src := `
from aiodataloader import DataLoader

async def batch_posts(keys):
    return keys

post_loader = DataLoader(batch_posts)

async def resolve_posts(self, info):
    return await post_loader.load_many(self.post_ids)
`
	ents := runPyDataLoader(t, src)

	if e := findEdge(ents, string(types.RelationshipKindBatches), "batch_posts"); e == nil {
		t.Fatal("expected BATCHES edge post_loader → batch_posts (positional)")
	}
	if e := findEdge(ents, string(types.RelationshipKindUses), "post_loader"); e == nil {
		t.Fatal("expected USES edge resolve_posts → post_loader (load_many)")
	}
}

// TestPyDataLoader_NoAioImportNoEmit verifies the fast-path: a file that
// constructs DataLoader without importing aiodataloader (an unrelated local
// class) must NOT emit a loader entity.
func TestPyDataLoader_NoAioImportNoEmit(t *testing.T) {
	src := `
class DataLoader:
    def __init__(self, fn): ...

x = DataLoader(some_fn)
`
	ents := runPyDataLoader(t, src)
	for _, e := range ents {
		if e.Kind == string(types.EntityKindDataLoader) {
			t.Fatalf("did not expect a SCOPE.DataLoader entity without aiodataloader import; got %q", e.Name)
		}
	}
}
