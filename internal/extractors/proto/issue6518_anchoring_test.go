package proto_test

import (
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// issue6518_anchoring_test.go — every proto CONTAINS edge must be anchored on
// the record that OWNS it, not on the source file's path.
//
// THE SITE (#6518, allow-list entry proto:fileContainsRel:CONTAINS{1} in
// internal/extractors/file_anchored_rels_guard_test.go): fileContainsRel built
// `FromID: filePath` for all three file-level emissions — file → service
// (buildService), file → message (buildMessage) and file → enum (buildEnum).
//
// WHAT WAS MEASURED, not inferred. The fixture below is extracted, id-stamped
// and pushed through the production resolver pipeline (ResolveImports →
// ReferencesEmbedded). Unlike hcl (#6367), proto emitted NO entity named for
// the containing file — not the full path and not the basename — so the
// path-valued FromID DANGLED at every path, root and nested alike:
//
//	nested "api/v1/order.proto"  → 3 of 3 file-level CONTAINS DANGLING
//	root   "order.proto"         → 3 of 3 file-level CONTAINS DANGLING
//
// There is no root-path accident to hide behind here, which is why the defect
// never surfaced as a "misanchored" count anywhere: internal/resolve/refs.go's
// sourceFileExtensions does not list ".proto", so looksLikeSourceFilePath does
// not even recognise the FromID as a path and the edge is dispositioned
// DispositionBugExtractor — a generic bucket, pointing at the wrong thing.
//
// THE FIX IS NOT "empty FromID" ALONE. These records were appended to the
// CONTAINED entity, so clearing FromID there would stamp the message's own id
// and the edge would die as a self-loop (internal/graph/orientation.go:206) —
// the very trap the old fileContainsRel comment described. They are re-homed
// onto a SCOPE.Component/file entity (extractor.FileEntity, the entity ~25
// other extractors already emit per #577) which is the owner the edge always
// meant, and FromID is left empty there, exactly as hcl's file component does.
//
// IMPORTS is deliberately untouched: #120 keeps the file path on IMPORTS edges.

// anchorSrc6518 exercises all three file-level emission sites (service,
// message, enum) plus their children, so a fix that repairs one arm and not
// the others cannot pass.
const anchorSrc6518 = `syntax = "proto3";

import "common.proto";

message Order {
  string id = 1;
  Status status = 2;
}

enum Status {
  STATUS_UNKNOWN = 0;
  STATUS_PAID = 1;
}

service OrderService {
  rpc Get(Order) returns (Order);
}
`

// anchorEdge6518 is one CONTAINS edge, with the endpoint it actually lands on
// after graph assembly's substitution rule and the endpoint it should have.
type anchorEdge6518 struct {
	// ownerName, fromLabel and wantLabel exist for the FAILURE MESSAGE only.
	// A label is Subtype+":"+Name, which is NOT unique; the assertion compares
	// fromID against wantID — resolved identities — and uses the labels purely
	// to say WHICH nodes those ids are.
	ownerName string
	fromLabel string
	wantLabel string
	fromID    string
	wantID    string
	resolved  bool
	rawFromID string
}

// measureAnchoring6518 extracts anchorSrc6518 at path, replays graph
// assembly's "substitute the owner's id only when FromID is empty" rule, and
// returns one anchorEdge6518 per CONTAINS edge, plus the stamped records.
func measureAnchoring6518(t *testing.T, path string) ([]anchorEdge6518, []types.EntityRecord) {
	t.Helper()

	recs := extract(t, path, anchorSrc6518)
	if len(recs) == 0 {
		t.Fatalf("extract %s: no records", path)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6518", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}
	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))

	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	label := func(id string) string {
		if e := byID[id]; e != nil {
			return e.Subtype + ":" + e.Name
		}
		return "<UNRESOLVED:" + id + ">"
	}

	var out []anchorEdge6518
	for i := range recs {
		owner := &recs[i]
		for _, r := range owner.Relationships {
			if r.Kind != "CONTAINS" {
				continue // IMPORTS keeps the file path on purpose (#120).
			}
			// Replay graph assembly: cmd/grafel/index.go and
			// relRecordToGraphRel in internal/extractors/incremental.go
			// substitute the owning record's own id ONLY when FromID is empty.
			from := r.FromID
			if from == "" {
				from = owner.ID
			}
			_, ok := byID[from]
			out = append(out, anchorEdge6518{
				ownerName: owner.Name,
				fromLabel: label(from),
				wantLabel: label(owner.ID),
				fromID:    from,
				wantID:    owner.ID,
				resolved:  ok,
				rawFromID: r.FromID,
			})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ownerName < out[b].ownerName })
	return out, recs
}

// TestProto_ContainsAnchoredOnOwner_6518 is the fix's behavioural test: on BOTH
// a nested and a root path, every CONTAINS edge must land on the record that
// carries it.
func TestProto_ContainsAnchoredOnOwner_6518(t *testing.T) {
	// BOTH paths are load-bearing. proto dangled at both before the fix (no
	// entity carried the file's name in any spelling), but a future change that
	// reintroduces a basename-shaped anchor would resolve at the root path and
	// dangle nested — the hcl failure mode (#6367) — and only the nested path
	// would catch it.
	for _, path := range []string{"api/v1/order.proto", "order.proto"} {
		t.Run(path, func(t *testing.T) {
			edges, recs := measureAnchoring6518(t, path)

			dangling, misanchored := 0, 0
			for _, e := range edges {
				// IDENTITY, not label: two records could share Subtype+Name.
				if e.fromID == e.wantID {
					continue
				}
				if !e.resolved {
					dangling++
				} else {
					misanchored++
				}
				t.Errorf("CONTAINS owned by %q: FROM = %s (id %q), want %s (id %q) "+
					"(FromID=%q must be empty so assembly stamps the owning record)",
					e.ownerName, e.fromLabel, e.fromID, e.wantLabel, e.wantID, e.rawFromID)
			}
			if len(edges) == 0 {
				t.Fatalf("no CONTAINS edges produced by the fixture — the measurement is vacuous")
			}
			if dangling != 0 || misanchored != 0 {
				t.Logf("MEASURED CONTAINS at %s: %d dangling, %d misanchored, of %d",
					path, dangling, misanchored, len(edges))
			}

			// Anchoring the edges on their owner must not be achieved by
			// DELETING them: the file-level CONTAINS edges still have to exist,
			// and their owner has to be the FILE — not the contained entity,
			// which is what an empty FromID at the old site would have produced
			// (a self-loop, discarded by orientation.go:206).
			assertFileOwnsContains6518(t, path, recs)
		})
	}
}

// assertFileOwnsContains6518 requires a SCOPE.Component/file entity named for
// path that carries exactly one CONTAINS edge to each of the three top-level
// entities, with an empty FromID.
func assertFileOwnsContains6518(t *testing.T, path string, recs []types.EntityRecord) {
	t.Helper()

	var fileEnt *types.EntityRecord
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Subtype == "file" && recs[i].Name == path {
			fileEnt = &recs[i]
			break
		}
	}
	if fileEnt == nil {
		t.Fatalf("no SCOPE.Component/file entity named %q — the file-level CONTAINS "+
			"edges have no owner to be anchored on (#6518)", path)
	}

	svc := findRec6422(t, recs, "SCOPE.Service", "service", "OrderService")
	msg := findRec6422(t, recs, "SCOPE.Schema", "message", "Order")
	enum := findRec6422(t, recs, "SCOPE.Schema", "enum", "Status")

	for _, want := range []struct {
		what string
		id   string
	}{
		{"service OrderService", svc.ID},
		{"message Order", msg.ID},
		{"enum Status", enum.ID},
	} {
		n := 0
		for _, r := range fileEnt.Relationships {
			if r.Kind == "CONTAINS" && r.FromID == "" && r.ToID == want.id {
				n++
			}
		}
		if n != 1 {
			t.Errorf("file → %s CONTAINS edges owned by the file entity = %d, want 1 "+
				"(each file-level arm must survive the re-anchoring, #6518)", want.what, n)
		}
	}
}

// findFileEntity6518 returns the per-file SCOPE.Component carrier for path, or
// nil, and fails if more than one record claims to be it.
func findFileEntity6518(t *testing.T, recs []types.EntityRecord, path string) *types.EntityRecord {
	t.Helper()
	var out *types.EntityRecord
	for i := range recs {
		if recs[i].Kind != "SCOPE.Component" || recs[i].Subtype != "file" {
			continue
		}
		if recs[i].Name != path {
			t.Errorf("file entity named %q, want %q", recs[i].Name, path)
			continue
		}
		if out != nil {
			t.Fatalf("two SCOPE.Component/file records for %q", path)
		}
		out = &recs[i]
	}
	return out
}

// importsOnlySrc6518 is a valid .proto with no top-level DEFINITION at all —
// the shape a `foo_deps.proto` aggregator file takes.
const importsOnlySrc6518 = `syntax = "proto3";

import "common.proto";
import public "other.proto";
`

// emptySrc6518 parses cleanly and yields NOTHING to anchor: no definition, no
// import. A `syntax`-only stub, or the residue of a file whose whole body the
// grammar rejected.
const emptySrc6518 = `syntax = "proto3";
`

// TestProto_FileEntityCarriesImportsOnlyFiles_6518 pins the emission guard's
// INCLUSIVE half.
//
// The carrier is emitted when the file has something for it to carry —
// file-level CONTAINS edges OR imports. An imports-only .proto has no
// containment at all, but it is precisely the file where IMPORTS is the ONLY
// content, and #577's whole purpose is to let ReferencesEmbedded rewrite an
// IMPORTS FromID from the raw path onto this entity's stamped id so the
// cross-repo linker (#566) can map the edge back to its repo. Denying the
// carrier to that file would withhold the entity from the one shape it was
// introduced for. (An earlier revision of this fix did exactly that, on a
// "mirror hcl's emitFileLevelRelationships" argument that does not survive
// contact with #577's stated reason for the entity.)
func TestProto_FileEntityCarriesImportsOnlyFiles_6518(t *testing.T) {
	const path = "deps.proto"
	recs := extract(t, path, importsOnlySrc6518)

	if findFileEntity6518(t, recs, path) == nil {
		t.Errorf("imports-only %s emitted NO SCOPE.Component/file carrier — the "+
			"IMPORTS FromID has nothing to be rewritten onto, which is the entity's "+
			"stated purpose (#577, #6518)", path)
	}

	imports := 0
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == "IMPORTS" {
				imports++
			}
		}
	}
	if imports != 2 {
		t.Fatalf("IMPORTS edges = %d, want 2 — the fixture must still be extracted, "+
			"or the assertion above is vacuous", imports)
	}
}

// TestProto_NoFileEntityWithoutAnythingToCarry_6518 pins the guard's EXCLUSIVE
// half, which is the permissive direction and is covered nowhere else.
//
// Widening the guard to emit unconditionally mints a bare SCOPE.Component with
// zero relationships for every content-free .proto — a new orphan node per
// file, invisible to every count-based assertion in the suite because the whole
// point of the record is that it carries nothing.
func TestProto_NoFileEntityWithoutAnythingToCarry_6518(t *testing.T) {
	const path = "empty.proto"
	recs := extract(t, path, emptySrc6518)

	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Subtype == "file" {
			t.Errorf("content-free %s emitted a SCOPE.Component/file entity with %d "+
				"relationships — the carrier exists to hold file-level CONTAINS or "+
				"IMPORTS edges, and this file has neither (#6518)",
				path, len(recs[i].Relationships))
		}
	}
}

// selfImportSrc6518 is a file that imports ITSELF, beside an ordinary import.
//
// protoc rejects this ("File recursively imports itself"), but grafel never
// runs protoc and this package says so explicitly: an import string "is not a
// path in any enforced sense" (service_ref_e2e_6492_test.go), which is exactly
// why `import "Foo";` beside `service Foo` is already a pinned fixture. The
// same argument admits `import "<own path>";` as input the extractor must not
// corrupt the graph on.
const selfImportSrc6518 = `syntax = "proto3";

import "self.proto";
import "common.proto";

message M { string id = 1; }
`

// TestProto_SelfImportDoesNotCollideWithTheFileEntity_6518 is the collision
// guard.
//
// graph.EntityID hashes (repo, kind, name, sourceFile) and NOT Subtype. The
// import placeholder is {Name: <the quoted string>, Kind: SCOPE.Component,
// SourceFile: <importing file>} and the #6518 carrier is {Name: <path>, Kind:
// SCOPE.Component, SourceFile: <path>} — so in a file that imports its own
// path the two records hash to ONE id. That is a defect introduced by the
// carrier: on the pre-#6518 head only one of the two records existed.
//
// The cross-file case is the control and is benign: `a.proto` importing
// `b.proto` mints a placeholder named "b.proto" whose SourceFile is "a.proto",
// which is a different tuple from b.proto's own carrier.
func TestProto_SelfImportDoesNotCollideWithTheFileEntity_6518(t *testing.T) {
	const path = "self.proto"
	recs := extract(t, path, selfImportSrc6518)
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6518", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}

	byID := map[string][]string{}
	for i := range recs {
		if recs[i].ID == "" {
			continue
		}
		byID[recs[i].ID] = append(byID[recs[i].ID],
			recs[i].Kind+"/"+recs[i].Subtype+"/"+recs[i].Name)
	}
	for id, members := range byID {
		if len(members) > 1 {
			t.Errorf("DUPLICATE EntityID %s shared by %v — a file that imports its own "+
				"path mints an import placeholder with the same (kind, name, sourceFile) "+
				"tuple as the #6518 carrier, and EntityID does not hash Subtype", id, members)
		}
	}

	// The carrier must still be there: suppressing the COLLIDING record is the
	// fix, not suppressing the carrier.
	if findFileEntity6518(t, recs, path) == nil {
		t.Errorf("no SCOPE.Component/file carrier for %q — the self-import must "+
			"suppress the PLACEHOLDER, not the carrier (#6518)", path)
	}

	// The ordinary import in the same file is the control: a blanket "drop the
	// placeholders" would pass the duplicate check and fail here.
	foundCommon, selfImports := false, 0
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Subtype == "import" {
			switch recs[i].Name {
			case "common.proto":
				foundCommon = true
			case path:
				selfImports++
			}
		}
	}
	if !foundCommon {
		t.Errorf("the ordinary `import \"common.proto\";` placeholder is gone — only " +
			"the SELF-import may be suppressed (#6518)")
	}
	if selfImports != 0 {
		t.Errorf("self-import placeholder entities = %d, want 0", selfImports)
	}
}

// sameBasenameImportSrc6518 imports a DIFFERENT file that happens to share the
// importing file's basename — `api/v1/self.proto` importing
// `vendor/self.proto`. Ordinary proto: package layouts routinely repeat a
// filename under different directories.
const sameBasenameImportSrc6518 = `syntax = "proto3";

import "vendor/self.proto";

message M { string id = 1; }
`

// TestProto_SameBasenameImportKeepsItsPlaceholder_6518 is the PERMISSIVE
// direction of the self-import guard, and nothing else covers it.
//
// The guard suppresses a placeholder only when the quoted string equals the
// importing file's OWN path. Widen it to compare BASENAMES —
// filepath.Base(path) == filepath.Base(self), a one-token change that reads as
// a normalisation — and this file silently loses a legitimate import
// placeholder and its IMPORTS edge, because `vendor/self.proto` and
// `api/v1/self.proto` share a basename and nothing else. The whole package's
// suite stays green under that mutation without this test: every other fixture
// is at a root path where the basename IS the path.
func TestProto_SameBasenameImportKeepsItsPlaceholder_6518(t *testing.T) {
	const path = "api/v1/self.proto"
	recs := extract(t, path, sameBasenameImportSrc6518)

	found, imports := false, 0
	for i := range recs {
		if recs[i].Kind == "SCOPE.Component" && recs[i].Subtype == "import" &&
			recs[i].Name == "vendor/self.proto" {
			found = true
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "IMPORTS" {
				imports++
			}
		}
	}
	if !found {
		t.Errorf("the placeholder for `import \"vendor/self.proto\";` is gone from "+
			"%s — only an import of the file's OWN PATH may be suppressed, and a "+
			"shared basename is not that (#6518)", path)
	}
	if imports != 1 {
		t.Errorf("IMPORTS edges = %d, want 1 — suppressing the placeholder takes the "+
			"edge with it, so this is the same defect counted twice", imports)
	}
}
