// Package javascript_test — issue #3624 (epic #3607): GraphQL DataLoader
// N+1 batch-loader extraction.
//
// Verifies that the extractor:
//   - emits a SCOPE.DataLoader entity for each statically-named
//     `new DataLoader(...)` (from the "dataloader" package), named by the LHS;
//   - emits a BATCHES edge from the loader to the wrapped batch function;
//   - emits a USES edge from a resolver to the loader at each
//     `<loader>.load(id)` call site, tagged via=graphql_dataloader.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// TestTSExtractor_DataLoader_BatchesAndUses is the value-asserting case from
// the issue:
//
//	const userLoader = new DataLoader(batchUsers)   → loader userLoader, BATCHES batchUsers
//	userLoader.load(id) inside resolveAuthor        → resolveAuthor USES userLoader
func TestTSExtractor_DataLoader_BatchesAndUses(t *testing.T) {
	src := `
import DataLoader from 'dataloader';

async function batchUsers(ids) {
  return ids.map((id) => ({ id }));
}

const userLoader = new DataLoader(batchUsers);

export function resolveAuthor(post) {
  return userLoader.load(post.authorId);
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	// 1. SCOPE.DataLoader entity named "userLoader".
	loader := findByNameRel(ents, "userLoader")
	if loader == nil {
		t.Fatal("expected SCOPE.DataLoader entity 'userLoader' to be emitted")
	}
	if loader.Kind != string(types.EntityKindDataLoader) {
		t.Fatalf("userLoader kind = %q; want %q", loader.Kind, types.EntityKindDataLoader)
	}
	if loader.Properties["via"] != "graphql_dataloader" {
		t.Fatalf("userLoader via = %q; want graphql_dataloader", loader.Properties["via"])
	}

	// 2. BATCHES edge userLoader → batchUsers.
	foundBatches := false
	for _, r := range loader.Relationships {
		if r.Kind == string(types.RelationshipKindBatches) && r.ToID == "batchUsers" {
			if r.Properties["via"] != "graphql_dataloader" {
				t.Fatalf("BATCHES via = %q; want graphql_dataloader", r.Properties["via"])
			}
			foundBatches = true
		}
	}
	if !foundBatches {
		t.Logf("userLoader relationships:")
		for _, r := range loader.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Fatal("expected BATCHES edge userLoader → batchUsers")
	}

	// 3. USES edge resolveAuthor → userLoader.
	resolver := findByNameRel(ents, "resolveAuthor")
	if resolver == nil {
		t.Fatal("expected entity 'resolveAuthor' to be emitted")
	}
	foundUses := false
	for _, r := range resolver.Relationships {
		if r.Kind == string(types.RelationshipKindUses) && r.ToID == "userLoader" {
			if r.Properties["via"] != "graphql_dataloader" {
				t.Fatalf("USES via = %q; want graphql_dataloader", r.Properties["via"])
			}
			foundUses = true
		}
	}
	if !foundUses {
		t.Logf("resolveAuthor relationships:")
		for _, r := range resolver.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Fatal("expected USES edge resolveAuthor → userLoader")
	}
}

// TestTSExtractor_DataLoader_InlineArrowDelegation verifies that an inline
// delegating arrow batch function is resolved through to the named function:
//
//	const postLoader = new DataLoader((ids) => batchPosts(ids))  → BATCHES batchPosts
//
// and that loadMany() also produces a USES edge.
func TestTSExtractor_DataLoader_InlineArrowDelegation(t *testing.T) {
	src := `
import DataLoader from 'dataloader';

function batchPosts(ids) { return ids; }

const postLoader = new DataLoader((ids) => batchPosts(ids));

export function resolvePosts(user) {
  return postLoader.loadMany(user.postIds);
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	loader := findByNameRel(ents, "postLoader")
	if loader == nil {
		t.Fatal("expected SCOPE.DataLoader entity 'postLoader'")
	}
	foundBatches := false
	for _, r := range loader.Relationships {
		if r.Kind == string(types.RelationshipKindBatches) && r.ToID == "batchPosts" {
			foundBatches = true
		}
	}
	if !foundBatches {
		t.Fatal("expected BATCHES edge postLoader → batchPosts (inline arrow delegation)")
	}

	resolver := findByNameRel(ents, "resolvePosts")
	if resolver == nil {
		t.Fatal("expected entity 'resolvePosts'")
	}
	foundUses := false
	for _, r := range resolver.Relationships {
		if r.Kind == string(types.RelationshipKindUses) && r.ToID == "postLoader" {
			foundUses = true
		}
	}
	if !foundUses {
		t.Fatal("expected USES edge resolvePosts → postLoader (loadMany)")
	}
}

// TestTSExtractor_DataLoader_ContextReceiver verifies the idiomatic GraphQL
// resolver shape where loaders live on the context object:
//
//	(parent, args, context) => context.userLoader.load(parent.id)
//
// The trailing receiver property must still resolve to the known loader.
func TestTSExtractor_DataLoader_ContextReceiver(t *testing.T) {
	src := `
import DataLoader from 'dataloader';

function batchUsers(ids) { return ids; }

const userLoader = new DataLoader(batchUsers);

export function resolveOwner(parent, args, context) {
  return context.userLoader.load(parent.ownerId);
}
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	resolver := findByNameRel(ents, "resolveOwner")
	if resolver == nil {
		t.Fatal("expected entity 'resolveOwner'")
	}
	foundUses := false
	for _, r := range resolver.Relationships {
		if r.Kind == string(types.RelationshipKindUses) && r.ToID == "userLoader" &&
			r.Properties["via"] == "graphql_dataloader" {
			foundUses = true
		}
	}
	if !foundUses {
		t.Logf("resolveOwner relationships:")
		for _, r := range resolver.Relationships {
			t.Logf("  %s → %s (props=%v)", r.Kind, r.ToID, r.Properties)
		}
		t.Fatal("expected USES edge resolveOwner → userLoader (context receiver)")
	}
}

// TestTSExtractor_DataLoader_NoImportNoEmit verifies the fast-path: a file that
// constructs `new DataLoader(...)` WITHOUT importing the "dataloader" package
// (some unrelated local class named DataLoader) must NOT emit a loader entity.
func TestTSExtractor_DataLoader_NoImportNoEmit(t *testing.T) {
	src := `
class DataLoader { constructor(fn) {} }
const x = new DataLoader(() => {});
`
	tree := parseTSRel(t, []byte(src))
	ents := runJS(t, src, "typescript", tree)

	for _, e := range ents {
		if e.Kind == string(types.EntityKindDataLoader) {
			t.Fatalf("did not expect a SCOPE.DataLoader entity without a dataloader import; got %q", e.Name)
		}
	}
}
