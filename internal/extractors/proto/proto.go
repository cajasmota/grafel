// Package proto implements the tree-sitter–based extractor for Protocol Buffer source files.
//
// Extracted entities:
//   - service    → Kind="SCOPE.Service",   Subtype="service"
//   - rpc        → Kind="SCOPE.Operation", Subtype="endpoint" (Properties["type"]="rpc")
//   - message    → Kind="SCOPE.Schema",    Subtype="message"
//   - field      → Kind="SCOPE.Schema",    Subtype="field" (message fields,
//     map fields, and oneof members alike)
//   - enum       → Kind="SCOPE.Schema",    Subtype="enum"
//   - enum value → Kind="SCOPE.Schema",    Subtype="enum_value"
//
// # Syntax support: proto3 only
//
// The bundled smacker/go-tree-sitter protobuf grammar accepts proto3 and
// nothing else. `syntax = "proto2"` (and a file with no syntax statement,
// which IS proto2), proto2-only keywords (`optional`, `required`, `group`),
// and `edition = "2023"` are all hard rejections: the whole file body
// collapses into a single ERROR node, so the parse pins at an error ratio
// around 0.20-0.28 regardless of file size and never clears
// treesitter.maxErrorRatio (0.10). Such files are dropped whole, every time —
// no entities, no relationships, no partial output. This is a syntax-level
// gap, not an options-level one: custom options such as
// `option (gogoproto.marshaler_all) = true;` parse cleanly (measured
// error_ratio 0.0000).
//
// Issue #377 — relationship parity:
//
//   - IMPORTS edges are emitted from file.Path → import target for every
//     `import "x.proto";` and `import public "x.proto";` directive, and for
//     nothing else. `import public` carries Properties["public"]="true".
//     Until #6359 buildRPC also emitted IMPORTS, for rpc request/response
//     types, conflating two unrelated edge families under one verb.
//   - CONTAINS edges:
//   - file → service / message / enum (top-level definitions),
//   - service → rpc,
//   - message → field (including every member of a `oneof` block, #6358 —
//     the oneof GROUP itself carries no entity, so mutual exclusivity is not
//     modelled),
//   - enum → enum value.
//     ToIDs use BuildOperationStructuralRef("proto", file, name) for entity
//     children (service/message/enum/rpc) and the table#column-style ref
//     `scope:schema:column:proto:<file>:<parent>#<member>` for fields and
//     enum values, mirroring SQL Format B. Every CONTAINS target is backed by
//     an entity this package also emits, so no edge resolves to a phantom.
//   - REFERENCES edges: message → the message/enum type of each non-scalar
//     field, and rpc → its request and response message types (#6359,
//     Properties["direction"]="request"/"response"/"request,response"). Both
//     address the target through messageTypeRef — the schema address space,
//     kept separate from the operation one so an rpc and a message of the same
//     name in one file do not collide. Both are restricted to types defined in
//     the SAME file — see dropUnresolvableTypeRefs for why a cross-file type
//     carries no edge yet.
//
// Uses the protobuf grammar from smacker/go-tree-sitter.
// Registers itself via init() and is imported by registry_gen.go.
package proto

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func init() {
	// Registration key MUST equal the language token the classifier emits for
	// .proto files ("protobuf", classifier.go extensionLanguageMap), because
	// extractors.Extract dispatches on FileInput.Language. Registering under
	// "proto" (as this package did until #6356) made extractors.Get("protobuf")
	// miss, so every .proto file in production was counted at
	// daemon/extract/subproc.go as stats.Skipped++ and produced nothing —
	// while the package's own unit tests, which call the extractor directly,
	// stayed green.
	extractor.Register("protobuf", &Extractor{})
}

// Extractor implements extractor.Extractor for Protocol Buffers.
type Extractor struct{}

// Language returns the canonical language name. It matches the classifier
// token and the Language field stamped on every entity this package emits
// (#6356). The literal "proto" still appears inside structural-ref builders
// below: that is the ref-namespace segment, which is a separate, already-
// persisted identifier and is deliberately not renamed here.
func (e *Extractor) Language() string { return "protobuf" }

// Extract walks the tree-sitter CST and returns entity records.
func (e *Extractor) Extract(_ context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	if file.TSTree == nil || len(file.Content) == 0 {
		return nil, nil
	}

	var entities []types.EntityRecord
	walkProto(file.TSTree.RootNode(), file, &entities)

	// #6357: drop field-type REFERENCES edges whose target is not defined in
	// this file. See dropUnresolvableTypeRefs.
	dropUnresolvableTypeRefs(file, entities)

	// Append IMPORTS stub entities, one per `import "..."` directive.
	importEntities := buildImportEntities(file)
	if len(importEntities) > 0 {
		entities = append(entities, importEntities...)
	}

	return entities, nil
}

// dropUnresolvableTypeRefs removes every message→type REFERENCES edge whose
// target message/enum is not defined in this same file, mutating entities in
// place.
//
// #6357. namedTypeRefs strips the package qualifier from a field type, and
// buildMessage then builds the structural ref against file.Path. For a field
// whose type lives in an *imported* file that produces a ref into the wrong
// file — etcd's lease.proto has
// `etcdserverpb.LeaseTimeToLiveRequest LeaseTimeToLiveRequest = 1;` and emitted
// scope:operation:method:proto:<lease.proto>:LeaseTimeToLiveRequest, a target
// that cannot exist because the message is defined in
// etcd/api/etcdserverpb/rpc.proto. resolve/refs.go materialises such a ref as a
// phantom grey node.
//
// Correct cross-file resolution needs a proto package index (map
// proto-package + type name → defining file, built across the import closure),
// which this extractor does not have: Extract sees one file at a time and the
// import directives carry file paths, not package names. Until that index
// exists the honest behaviour is to emit nothing rather than a known-dangling
// target, so same-file REFERENCES (the `repeated Order orders = 3;` case the
// edge was written for) keep working and cross-file ones are simply absent.
// Tracked as follow-up work; see the #6357 review notes.
func dropUnresolvableTypeRefs(file extractor.FileInput, entities []types.EntityRecord) {
	local := make(map[string]bool, len(entities))
	for i := range entities {
		if entities[i].Kind != "SCOPE.Schema" {
			continue
		}
		if entities[i].Subtype == "message" || entities[i].Subtype == "enum" {
			local[messageTypeRef(file.Path, entities[i].Name)] = true
		}
	}
	for i := range entities {
		rels := entities[i].Relationships
		if len(rels) == 0 {
			continue
		}
		kept := rels[:0]
		for _, r := range rels {
			if r.Kind == "REFERENCES" && !local[r.ToID] {
				continue
			}
			kept = append(kept, r)
		}
		if len(kept) == len(rels) {
			continue
		}
		entities[i].Relationships = kept
	}
}

func nodeText(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}

func childByType(node ts.Node, types_ ...string) ts.Node {
	set := make(map[string]bool, len(types_))
	for _, t := range types_ {
		set[t] = true
	}
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && set[ch.Type()] {
			return ch
		}
	}
	return nil
}

// fieldMemberRef returns the Format B structural-ref for a parent#member edge
// inside a proto file (message field, enum value). Mirrors
// BuildSchemaColumnStructuralRef but tagged with the "proto" language.
func fieldMemberRef(filePath, parent, member string) string {
	return "scope:schema:column:proto:" + filePath + ":" + parent + "#" + member
}

// messageTypeRef returns the structural-ref used to ADDRESS a message/enum as
// the TARGET of a type reference (an rpc's request/response type, a message
// field's declared type). Shape:
//
//	scope:schema:message:proto:<file>:<Name>
//
// It is deliberately NOT BuildOperationStructuralRef. rpc entities are
// SCOPE.Operation and message/enum entities are SCOPE.Schema, but the
// operation ref addresses purely by (file, name): for
// `service S { rpc User(User) returns (User); }` the rpc and the message
// collide on ONE address, internal/resolve/refs.go resolves the ref through
// operationKindFamily straight back to the rpc, assembly stamps FromID with
// that same id, and the edge is discarded as a self-loop
// (internal/graph/orientation.go:206) — the rpc → message link silently lost.
//
// The schema scope-kind gives type targets their own address space:
// structuralKindFamilies("schema") returns schemaKindFamily
// (internal/resolve/refs.go:1807), which contains SCOPE.Schema and NOT
// SCOPE.Operation, so lookupLocationKind binds the message even when a
// same-named rpc shares the file. Pinned by
// TestRPC_RPCAndMessageOfTheSameNameDoNotSelfLoop.
func messageTypeRef(filePath, name string) string {
	// filepath.ToSlash mirrors every extractor.Build*StructuralRef helper, so
	// a Windows-separated path mints ONE spelling of the address rather than
	// two. lookupStructural calls normalizePath defensively and resolved the
	// backslash form anyway, but this package's whole defect class (#6359,
	// #6419, #6422) is "one entity, two disagreeing address spellings".
	return "scope:schema:message:proto:" + filepath.ToSlash(filePath) + ":" + name
}

// fileContainsRel builds a CONTAINS edge from file.Path → a top-level entity,
// addressing the target through the ref form that matches its ENTITY KIND.
//
// #6422. It used to address every top-level entity through
// BuildOperationStructuralRef — right for a service, wrong for a message or an
// enum, and invisible until a name collides. graph.EntityID hashes
// (repo, kind, name, sourceFile) and NOT Subtype, and the operation ref
// addresses purely by (file, name), so for
//
//	message User {…}; service S { rpc User(User) returns (User); }
//
// the file → message ref resolved through operationKindFamily
// (internal/resolve/refs.go) onto the SCOPE.Operation RPC: the edge became a
// duplicate of the existing service → rpc CONTAINS and the message was left
// with NO inbound CONTAINS at all. Measured end to end in
// TestFileContains_MessageIsNotReparentedOntoTheRPC_6422.
//
// Suppressing the duplicate would have fixed the visible half and kept the
// worse half, so the addressing is what changed: message and enum take
// messageTypeRef — the schema address space #6419's type REFERENCES already
// use — and service keeps the operation form.
//
// FromID stays the file path on purpose. It is a KNOWN OFFENDER in
// internal/extractors/file_anchored_rels_guard_test.go (dangling on the FROM
// side, tracked separately under #6298), but the usual remedy — leave FromID
// empty and let assembly stamp the owner — is WRONG here: this record is
// appended to the CONTAINED entity, so an empty FromID would stamp the
// message's own id and the edge would die as a self-loop. #6422 is the TO
// side only.
func fileContainsRel(filePath, toRef string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID: filePath,
		ToID:   toRef,
		Kind:   "CONTAINS",
	}
}

// fileContainsOperationRel is the file → service form, and the form the
// service → rpc edges already use.
//
// A PRECISE STATEMENT OF WHAT THIS RESOLVES, because two earlier revisions of
// this comment were wrong in opposite directions.
//
// The first claimed "SCOPE.Service and SCOPE.Operation both resolve through
// the operation address space", which was FALSE at the time: operationKindFamily
// held only {"Operation", "Function", "Method", "SCOPE.Operation"}, so a service
// reached its entity solely by falling through to the kind-agnostic byLocation
// path — which drops any (file, name) that is not unique. The second revision
// recorded that gap accurately and left it open as #6459.
//
// #6459 has since closed it FOR THE `message Foo` + `service Foo` SHAPE — as an
// ORDERED TIER, not by widening any kind family. That scope is the whole claim;
// the residual it does not cover is stated at the bottom of this comment. SCOPE.Service is in NO family: not the shared operationKindFamily
// (that slice also feeds hintKinds and the leaf-name family mask, and ~60
// non-proto sites emit SCOPE.Service beside a same-named function or class, so
// a global admission destroys those unique matches instead of adding any), and
// not a proto-only variant of it either. A proto-only widening fails for a
// reason specific to THIS file: buildService addresses every rpc child through
// the same BuildOperationStructuralRef this function uses, so rpcs and services
// occupy ONE address space. Put SCOPE.Service into the family that space is
// filtered by, and
//
//	service User  { rpc Get(Foo)  returns (Foo); }
//	service Admin { rpc User(Foo) returns (Foo); }
//
// — ordinary proto — makes the rpc User ref match two family members, and the
// service Admin → rpc User CONTAINS edge that used to resolve now dangles.
//
// internal/resolve/refs.go's lookupProtoServiceTier instead consults
// SCOPE.Service only when the unmodified operation family matched NOTHING at
// all at that (file, name), and only for the proto LANGUAGE segment this very
// function stamps into the ref. On
//
//	message Foo {…}; service Foo { rpc Go(Foo) returns (Foo); }
//
// in one file — the collision that made the old byLocation fallback fail —
// there is no operation-family candidate named Foo, the tier runs, and the
// file → service Foo CONTAINS resolves to the SCOPE.Service entity instead of
// being dropped as ambiguous. (Scoped claim: what the old fallback dropped was
// this ref's BINDING; a synthetic index with no rpc present can still bind it
// by luck, so the pre-fix damage is only observable once the ambiguity exists —
// which is why the guard fixture carries the colliding message.)
//
// WHAT IS STILL NOT RESOLVED, stated as plainly as the fix. When a service and
// one of its OWN rpcs share a name —
//
//	service Foo { rpc Foo(Bar) returns (Bar); }
//
// — this function and buildService produce the BYTE-IDENTICAL ref
// scope:operation:method:proto:<file>:Foo for two different entities. The tier
// cannot help and must not try: its precondition sees the rpc in the operation
// family and bails, because the alternative is a service outranking a real rpc.
// Measured end-to-end through this extractor at the head that added the tier:
// the service ends with ZERO inbound CONTAINS and the rpc carries TWO — its own
// parent edge plus the file → service edge mis-bound onto it. That is #6459's
// title symptom surviving in this one shape. It is a MIS-BINDING, not a dangle,
// so nothing is left unresolved and no ref-integrity check sees it. Closing it
// needs this file to stop addressing a service and its rpc identically — a ref
// FORMAT change, not a resolver change — and belongs in its own issue with its
// own measurement. The same applies to two rpcs in one service sharing a name.
//
// The guards are internal/resolve/proto_service_family_6459_test.go (the
// service half) and internal/resolve/proto_rpc_service_collision_6492_test.go
// (the rpc half — the two-service fixture above). The counter-guard, that a
// non-proto operation-space ref (celery task, Spring stereotype) is unaffected
// and that the language boundary admits proto and nothing else, is
// internal/resolve/service_family_scope_6492_test.go. The residual above is
// pinned as a characterisation test:
// TestSelfNamedRpcLeavesTheServiceOrphaned6459Residual in
// service_ref_e2e_6492_test.go.
func fileContainsOperationRel(filePath, name string) types.RelationshipRecord {
	return fileContainsRel(filePath, extractor.BuildOperationStructuralRef("proto", filePath, name))
}

// fileContainsSchemaRel is the file → message/enum form. Both are SCOPE.Schema
// entities and must be addressed in the schema address space.
func fileContainsSchemaRel(filePath, name string) types.RelationshipRecord {
	return fileContainsRel(filePath, messageTypeRef(filePath, name))
}

func walkProto(node ts.Node, file extractor.FileInput, out *[]types.EntityRecord) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "service":
		if rec, ok := buildService(node, file); ok {
			*out = append(*out, rec)
		}
		// Walk inside service for rpc nodes.
		for i := range node.ChildCount() {
			ch := node.Child(int(i))
			if ch != nil && ch.Type() == "rpc" {
				if rec, ok := buildRPC(ch, file); ok {
					*out = append(*out, rec)
				}
			}
		}
		return // Don't recurse further into service — already handled.
	case "message":
		if recs, ok := buildMessage(node, file); ok {
			*out = append(*out, recs...)
		}
	case "enum":
		if recs, ok := buildEnum(node, file); ok {
			*out = append(*out, recs...)
		}
	}

	for i := range node.ChildCount() {
		walkProto(node.Child(int(i)), file, out)
	}
}

func buildService(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	nameNode := childByType(node, "service_name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := strings.TrimSpace(nodeText(nameNode, file.Content))
	// service_name wraps identifier
	if ident := childByType(nameNode, "identifier"); ident != nil {
		name = nodeText(ident, file.Content)
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	// CONTAINS edges: service → each rpc child + file → service.
	var rels []types.RelationshipRecord
	rels = append(rels, fileContainsOperationRel(file.Path, name))
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil || ch.Type() != "rpc" {
			continue
		}
		rpcNameNode := childByType(ch, "rpc_name")
		if rpcNameNode == nil {
			continue
		}
		rpcName := strings.TrimSpace(nodeText(rpcNameNode, file.Content))
		if ident := childByType(rpcNameNode, "identifier"); ident != nil {
			rpcName = nodeText(ident, file.Content)
		}
		if rpcName == "" {
			continue
		}
		rels = append(rels, types.RelationshipRecord{
			ToID: extractor.BuildOperationStructuralRef("proto", file.Path, rpcName),
			Kind: "CONTAINS",
		})
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Service",
		Subtype:            "service",
		SourceFile:         file.Path,
		Language:           "protobuf",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "service " + name,
		EnrichmentRequired: false,
		Relationships:      rels,
	}, true
}

func buildRPC(node ts.Node, file extractor.FileInput) (types.EntityRecord, bool) {
	// rpc: rpc <rpc_name> ( <msg_type> ) returns ( <msg_type> ) ;
	nameNode := childByType(node, "rpc_name")
	if nameNode == nil {
		return types.EntityRecord{}, false
	}
	name := strings.TrimSpace(nodeText(nameNode, file.Content))
	if ident := childByType(nameNode, "identifier"); ident != nil {
		name = nodeText(ident, file.Content)
	}
	if name == "" {
		return types.EntityRecord{}, false
	}

	// Collect request and response types.
	var msgTypes []string
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch != nil && ch.Type() == "message_or_enum_type" {
			msgTypes = append(msgTypes, nodeText(ch, file.Content))
		}
	}
	reqType, respType := "?", "?"
	if len(msgTypes) >= 1 {
		reqType = msgTypes[0]
	}
	if len(msgTypes) >= 2 {
		respType = msgTypes[1]
	}

	sig := fmt.Sprintf("rpc %s(%s) returns (%s)", name, reqType, respType)

	// #6359: request/response type edges.
	//
	// These were emitted as `{FromID: file.Path, ToID: <bare type name>,
	// Kind: "IMPORTS"}` — four defects in one literal:
	//
	//   - WRONG VERB. An rpc's request/response type is not a file import. The
	//     verb collided with the genuine file-level `import "x.proto"` edges
	//     from buildImportEntities, so IMPORTS meant two unrelated things and
	//     no consumer could tell them apart. It also mis-anchored the edge:
	//     the #120 IMPORTS exemption in
	//     internal/extractors/file_anchored_rels_guard_test.go is the only
	//     reason a file-anchored FromID passed the guard at all.
	//   - WRONG SOURCE. FromID was the FILE, not the rpc, so every rpc in a
	//     service collapsed onto one origin and N rpcs sharing a request type
	//     produced N indistinguishable duplicates.
	//   - UNRESOLVABLE TARGET. ToID was the bare, unqualified type text — no
	//     structural ref, no import path — so nothing could ever bind to it
	//     and resolve/refs.go materialised a phantom grey node per rpc arm.
	//   - LITERAL "?". With no guard before emission, an rpc whose types the
	//     grammar did not yield emitted an edge to the string "?".
	//
	// The old comment at this site described that shape as deliberate. It is
	// not; it is the bug, and it is replaced.
	//
	// New shape: REFERENCES, anchored on the rpc entity itself (empty FromID,
	// the same convention buildService uses for its service→rpc CONTAINS
	// edges), targeting messageTypeRef — the SAME address a message→field-type
	// REFERENCES edge uses. That means rpc type refs flow through
	// dropUnresolvableTypeRefs like every other type ref in this package: a
	// type defined in this file keeps its edge, one that is not is dropped
	// rather than dangled, pending the cross-file proto package index. One
	// convention, applied once.
	//
	// The target ref is messageTypeRef and NOT BuildOperationStructuralRef
	// because the rpc itself occupies the operation address space of this
	// file: `rpc User(User) returns (User)` would otherwise address the rpc's
	// own id and lose the edge to the self-loop filter. See messageTypeRef.
	var rpcRels []types.RelationshipRecord
	dir := make(map[string]string, 2)
	var order []string
	for i, raw := range msgTypes {
		which := "request"
		if i > 0 {
			which = "response"
		}
		for _, ref := range namedTypeRefs(raw) {
			to := messageTypeRef(file.Path, ref)
			if prev, ok := dir[to]; ok {
				// Same type on both arms (`rpc Ping(Empty) returns (Empty)`):
				// one edge, both roles recorded, instead of a duplicate pair.
				if prev != which {
					dir[to] = prev + "," + which
				}
				continue
			}
			dir[to] = which
			order = append(order, to)
		}
	}
	for _, to := range order {
		rpcRels = append(rpcRels, types.RelationshipRecord{
			ToID: to,
			Kind: "REFERENCES",
			// types.Props is a binary-searched, KEY-SORTED slice (internal/types/props.go:40):
			// "direction" must precede "via_rpc" or Get() cannot find either.
			Properties: types.Props{{K: "direction", V: dir[to]}, {K: "via_rpc", V: name}},
		})
	}

	return types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Operation",
		Subtype:            "endpoint",
		SourceFile:         file.Path,
		Language:           "protobuf",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		EnrichmentRequired: false,
		// Set type=rpc explicitly so buildOutputDoc doesn't override with "endpoint".
		// Python golden uses type=rpc, subtype=endpoint for RPC entities.
		Properties: map[string]string{"type": "rpc"},
		// rpc → request/response type REFERENCES edges (#6359). File-level
		// `import "..."` IMPORTS edges are emitted separately as stub entities
		// by buildImportEntities and are now the ONLY IMPORTS this package
		// produces.
		Relationships: rpcRels,
	}, true
}

func buildMessage(node ts.Node, file extractor.FileInput) ([]types.EntityRecord, bool) {
	nameNode := childByType(node, "message_name")
	if nameNode == nil {
		return nil, false
	}
	name := strings.TrimSpace(nodeText(nameNode, file.Content))
	if ident := childByType(nameNode, "identifier"); ident != nil {
		name = nodeText(ident, file.Content)
	}
	if name == "" {
		return nil, false
	}

	// CONTAINS edges: file → message + message → each field.
	// REFERENCES edges: message → each named (non-scalar) field type, so a
	// field `repeated Order orders = 3;` yields message User → message Order.
	var rels []types.RelationshipRecord
	rels = append(rels, fileContainsSchemaRel(file.Path, name))
	var fieldEnts []types.EntityRecord
	if body := childByType(node, "message_body"); body != nil {
		// seen is keyed on the field's SIMPLE name and is deliberately scoped
		// to the whole MESSAGE, not to the enclosing oneof (#6358). proto3
		// requires field names to be unique across the entire message —
		// including across separate oneof blocks — so two members sharing a
		// name is invalid proto, not a case to support: both would mint the
		// same Format-B member ref (…:<file>:<Msg>#<name>) and resolve/refs.go
		// would un-bind the pair as ambiguous anyway. Keeping one dedupe per
		// message means the address space and the dedupe agree.
		seen := make(map[string]bool)
		refSeen := make(map[string]bool)
		// addMember runs one field-shaped node (a top-level `field`, a
		// `map_field`, or a `oneof_field` nested inside a `oneof`) through the
		// shared name/type/entity/REFERENCES path.
		addMember := func(ch ts.Node, isMap bool) {
			fname := fieldName(ch, file.Content)
			if fname == "" || seen[fname] {
				return
			}
			seen[fname] = true
			var ftype, label string
			if isMap {
				ftype, label = mapFieldTypeAndLabel(ch, file.Content)
			} else {
				ftype, label = fieldTypeAndLabel(ch, file.Content)
			}
			fieldEnts = append(fieldEnts, buildField(file, name, fname, ftype, label, ch))
			rels = append(rels, types.RelationshipRecord{
				ToID: fieldMemberRef(file.Path, name, fname),
				Kind: "CONTAINS",
			})
			// REFERENCES edge to a named (non-scalar) message/enum type. Scalars
			// (string, int32, …) carry no edge. The map type's value component is
			// also followed (map<string, Order> → Order).
			for _, ref := range namedTypeRefs(ftype) {
				if refSeen[ref] {
					continue
				}
				refSeen[ref] = true
				rels = append(rels, types.RelationshipRecord{
					ToID:       messageTypeRef(file.Path, ref),
					Kind:       "REFERENCES",
					Properties: types.Props{{K: "type", V: ref}, {K: "via_field", V: fname}},
				})
			}
		}
		for i := range body.ChildCount() {
			ch := body.Child(int(i))
			if ch == nil {
				continue
			}
			switch ch.Type() {
			case "field":
				addMember(ch, false)
			case "map_field":
				addMember(ch, true)
			case "oneof":
				// #6358: the grammar nests oneof members one level deeper as
				// `oneof_field` children of a `oneof` node, so the flat
				// message_body scan never reached them and every field inside
				// every oneof was silently dropped — no entity, no edge, no
				// warning, on files that parse at error_ratio 0.0000.
				//
				// Members only: the oneof GROUP itself gets no entity here, so
				// the mutual-exclusivity semantics of the tagged union are not
				// modelled. That is a deliberate scope line (see #6358) — the
				// group needs a new 4-part ID form; recovering the dropped
				// members does not.
				for j := range ch.ChildCount() {
					m := ch.Child(int(j))
					if m == nil || m.Type() != "oneof_field" {
						continue
					}
					// A oneof member cannot be repeated/optional and cannot be
					// a map, so it always takes the plain field path.
					addMember(m, false)
				}
			}
		}
	}

	msg := types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Schema",
		Subtype:            "message",
		SourceFile:         file.Path,
		Language:           "protobuf",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "message " + name,
		EnrichmentRequired: false,
		Relationships:      rels,
	}
	out := make([]types.EntityRecord, 0, 1+len(fieldEnts))
	out = append(out, msg)
	out = append(out, fieldEnts...)
	return out, true
}

// buildField emits a per-message-field SCOPE.Field entity. The entity ID reuses
// the Format-B member ref (scope:schema:column:proto:<file>:<parent>#<field>) so
// it coincides with the message→field CONTAINS edge target. Properties carry the
// resolved field type and the proto label (repeated/optional/required) when one
// is present.
func buildField(file extractor.FileInput, parent, fname, ftype, label string, node ts.Node) types.EntityRecord {
	props := map[string]string{"type": ftype}
	if label != "" {
		props["label"] = label
	}
	sig := ftype + " " + fname
	if label != "" {
		sig = label + " " + sig
	}
	return types.EntityRecord{
		// Name is "<parent>.<field>" so the Format-B member resolver
		// (Index.byMember[file][parent][field], internal/resolve/refs.go) binds
		// the message→field CONTAINS edge — the dotted name splits into
		// scope=<parent>, member=<field>. Mirrors the SQL table.column and ORM
		// Model.field conventions.
		Name:               parent + "." + fname,
		Kind:               "SCOPE.Schema",
		Subtype:            "field",
		SourceFile:         file.Path,
		Language:           "protobuf",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		QualifiedName:      parent + "." + fname,
		Properties:         props,
		EnrichmentRequired: false,
	}
}

func buildEnum(node ts.Node, file extractor.FileInput) ([]types.EntityRecord, bool) {
	nameNode := childByType(node, "enum_name")
	if nameNode == nil {
		return nil, false
	}
	name := strings.TrimSpace(nodeText(nameNode, file.Content))
	if ident := childByType(nameNode, "identifier"); ident != nil {
		name = nodeText(ident, file.Content)
	}
	if name == "" {
		return nil, false
	}

	// CONTAINS edges: file → enum + enum → each enum value.
	//
	// #6357: every enum→value CONTAINS edge is paired with an actual
	// SCOPE.Schema/enum_value entity at the same Format-B member ref. Until
	// #6357 the edge was emitted and the entity was not, so resolve/refs.go
	// materialised an unresolvable stub — one phantom grey node per enum value
	// in every indexed .proto file. Emitting (rather than dropping the edge)
	// is the direction the package doc already promised and the direction
	// buildField already takes for message fields; an enum's values are the
	// enum's only content, so suppressing them would leave enums contentless.
	var rels []types.RelationshipRecord
	rels = append(rels, fileContainsSchemaRel(file.Path, name))
	var valueEnts []types.EntityRecord
	if body := childByType(node, "enum_body"); body != nil {
		seen := make(map[string]bool)
		for i := range body.ChildCount() {
			ch := body.Child(int(i))
			if ch == nil || ch.Type() != "enum_field" {
				continue
			}
			vname := enumValueName(ch, file.Content)
			if vname == "" || seen[vname] {
				continue
			}
			seen[vname] = true
			rels = append(rels, types.RelationshipRecord{
				ToID: fieldMemberRef(file.Path, name, vname),
				Kind: "CONTAINS",
			})
			valueEnts = append(valueEnts, buildEnumValue(file, name, vname, enumValueNumber(ch, file.Content), ch))
		}
	}

	enumEnt := types.EntityRecord{
		Name:               name,
		Kind:               "SCOPE.Schema",
		Subtype:            "enum",
		SourceFile:         file.Path,
		Language:           "protobuf",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          "enum " + name,
		EnrichmentRequired: false,
		Relationships:      rels,
	}
	out := make([]types.EntityRecord, 0, 1+len(valueEnts))
	out = append(out, enumEnt)
	out = append(out, valueEnts...)
	return out, true
}

// buildEnumValue emits a per-enum-value SCOPE.Schema entity, mirroring
// buildField. The entity's dotted Name (<enum>.<value>) is what makes the
// Format-B member resolver (Index.byMember[file][scope][member],
// internal/resolve/refs.go) bind the enum→value CONTAINS edge whose ToID is
// scope:schema:column:proto:<file>:<enum>#<value>. Without this entity that
// edge resolves to nothing and becomes a phantom node (#6357).
//
// Known address collision. The Format-B member ref is keyed on the IMMEDIATE
// parent's simple name, not on the full proto scope chain, so a message field
// and an enum value can collide when their parents share a simple name:
//
//	message Foo { string bar = 1; }              → …:c.proto:Foo#bar
//	message M { enum Foo { bar = 0; } }          → …:c.proto:Foo#bar
//
// Both are legal proto3 in one file and both mint the same address. This is
// not a mis-bind: internal/resolve/refs.go (the ambiguity branch around
// refs.go:1307-1315) sees two candidates for one key and writes the blank
// ambiguity sentinel, so the pair UN-binds — degrading to exactly the
// pre-#6357 phantom for those two members and nothing worse. Fixing it means
// putting the full nesting path into the member ref, which changes the address
// scheme for every field in every language that uses Format-B; it is recorded
// here rather than done, because the collision needs two same-simple-named
// containers of different kinds in one file.
func buildEnumValue(file extractor.FileInput, parent, vname, number string, node ts.Node) types.EntityRecord {
	props := map[string]string{}
	if number != "" {
		props["number"] = number
	}
	sig := parent + "." + vname
	if number != "" {
		sig += " = " + number
	}
	return types.EntityRecord{
		Name:               parent + "." + vname,
		Kind:               "SCOPE.Schema",
		Subtype:            "enum_value",
		SourceFile:         file.Path,
		Language:           "protobuf",
		StartLine:          int(node.StartPoint().Row) + 1,
		EndLine:            int(node.EndPoint().Row) + 1,
		Signature:          sig,
		QualifiedName:      parent + "." + vname,
		Properties:         props,
		EnrichmentRequired: false,
	}
}

// fieldName returns the message-field's identifier (the second `identifier`
// child after the `type` node — the first `identifier` under `type` is the
// type name, not the field name). The grammar lays a `field` node out as:
//
//	field
//	  type
//	    (string|identifier|message_or_enum_type ...)
//	  identifier   ← field name
//	  =
//	  field_number
func fieldName(node ts.Node, src []byte) string {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil {
			continue
		}
		if ch.Type() == "identifier" {
			return nodeText(ch, src)
		}
	}
	return ""
}

// protoScalars is the set of protobuf built-in scalar types. A field whose
// resolved type is one of these carries no REFERENCES edge.
var protoScalars = map[string]bool{
	"double": true, "float": true, "int32": true, "int64": true,
	"uint32": true, "uint64": true, "sint32": true, "sint64": true,
	"fixed32": true, "fixed64": true, "sfixed32": true, "sfixed64": true,
	"bool": true, "string": true, "bytes": true,
}

// fieldTypeAndLabel returns the resolved field type and the proto label
// (repeated / optional / required) for a `field` node. The grammar lays the
// field out as an optional label keyword, a `type` node, the field-name
// identifier, `=`, and the field number. The `type` node wraps either a scalar
// keyword, a `message_or_enum_type`, or a `map_type` (`map<K, V>`); we return
// the textual type (e.g. "string", "Order", "map<string, Order>").
func fieldTypeAndLabel(node ts.Node, src []byte) (ftype, label string) {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "repeated", "optional", "required":
			label = ch.Type()
		case "type", "map_type":
			ftype = strings.TrimSpace(nodeText(ch, src))
		}
	}
	// Some grammars surface a map field as a `map_field` node whose `type`
	// child already includes the `map<...>` text — covered by the `type` case
	// above. As a fallback, if no `type` child was found, scan for a
	// message_or_enum_type directly.
	if ftype == "" {
		if t := childByType(node, "message_or_enum_type"); t != nil {
			ftype = strings.TrimSpace(nodeText(t, src))
		}
	}
	return ftype, label
}

// mapFieldTypeAndLabel renders a `map_field` node's type as "map<K, V>" so
// namedTypeRefs follows the value (and key) component to any named message type.
// Map fields carry no repeated/optional label in proto3, so the label is empty.
// Grammar shape:
//
//	map_field
//	  map  <  key_type  ,  type  >  identifier  =  field_number  ;
func mapFieldTypeAndLabel(node ts.Node, src []byte) (ftype, label string) {
	var key, val string
	if k := childByType(node, "key_type"); k != nil {
		key = strings.TrimSpace(nodeText(k, src))
	}
	if v := childByType(node, "type"); v != nil {
		val = strings.TrimSpace(nodeText(v, src))
	}
	return "map<" + key + ", " + val + ">", ""
}

// namedTypeRefs extracts the named (non-scalar) type names referenced by a field
// type and returns one structural-ref per referenced type. Scalars yield none.
// A `map<K, V>` yields a ref for each of K and V that is itself a named type
// (the key is constrained to scalars in proto, so in practice only V matters,
// but both are checked for robustness). Leading dots and package qualifiers are
// stripped to the trailing type segment so the ref binds to the local message
// name (e.g. `.foo.bar.Order` → `Order`).
func namedTypeRefs(ftype string) []string {
	ftype = strings.TrimSpace(ftype)
	if ftype == "" {
		return nil
	}
	var raws []string
	if strings.HasPrefix(ftype, "map<") && strings.HasSuffix(ftype, ">") {
		inner := ftype[len("map<") : len(ftype)-1]
		for _, part := range strings.Split(inner, ",") {
			raws = append(raws, strings.TrimSpace(part))
		}
	} else {
		raws = append(raws, ftype)
	}
	var out []string
	for _, r := range raws {
		// Strip leading dot and package qualifier to the trailing segment.
		r = strings.TrimPrefix(r, ".")
		if idx := strings.LastIndex(r, "."); idx >= 0 {
			r = r[idx+1:]
		}
		if r == "" || protoScalars[r] {
			continue
		}
		out = append(out, r)
	}
	return out
}

// enumValueName returns the first identifier child of an `enum_field` node.
//
//	enum_field
//	  identifier   ← value name (e.g. UNKNOWN)
//	  =
//	  int_lit      ← value number
func enumValueName(node ts.Node, src []byte) string {
	if id := childByType(node, "identifier"); id != nil {
		return nodeText(id, src)
	}
	return ""
}

// enumValueNumber returns the enum value's assigned tag number, or "" when the
// grammar produced no int_lit child.
//
// The sign is a SEPARATE sibling token, not part of int_lit — the grammar
// shreds `E_NEG = -1;` into
//
//	enum_field
//	  identifier "E_NEG"
//	  =
//	  -            ← sign, its own token
//	  int_lit "1"
//
// so reading int_lit alone reported -1 as "1", indistinguishable from a
// genuine 1. Enum values are int32 and negatives are legal proto3, so the sign
// must be carried onto Properties["number"]. A leading `+` is normalised away.
func enumValueNumber(node ts.Node, src []byte) string {
	sign := ""
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "-":
			sign = "-"
		case "+":
			sign = ""
		case "int_lit":
			return sign + strings.TrimSpace(nodeText(ch, src))
		}
	}
	return ""
}

// buildImportEntities scans top-level `import "..."` and `import public "..."`
// directives and returns one stub SCOPE.Component entity per import target,
// each carrying an IMPORTS edge from file.Path → target. `import public`
// imports carry Properties["public"]="true" on the relationship.
func buildImportEntities(file extractor.FileInput) []types.EntityRecord {
	root := file.TSTree.RootNode()
	var entities []types.EntityRecord
	for i := range root.ChildCount() {
		ch := root.Child(int(i))
		if ch == nil || ch.Type() != "import" {
			continue
		}
		path, public := parseImport(ch, file.Content)
		if path == "" {
			continue
		}
		rel := types.RelationshipRecord{
			FromID: file.Path,
			ToID:   path,
			Kind:   "IMPORTS",
		}
		if public {
			rel.Properties = types.Props{{K: "public", V: "true"}}
		}
		entities = append(entities, types.EntityRecord{
			Name:               path,
			Kind:               "SCOPE.Component",
			Subtype:            "import",
			SourceFile:         file.Path,
			Language:           "protobuf",
			StartLine:          int(ch.StartPoint().Row) + 1,
			EndLine:            int(ch.EndPoint().Row) + 1,
			Signature:          nodeText(ch, file.Content),
			EnrichmentRequired: false,
			Relationships:      []types.RelationshipRecord{rel},
		})
	}
	return entities
}

// parseImport extracts the quoted path and the `public` modifier from an
// `import` node. The grammar shape is:
//
//	import
//	  import          (keyword)
//	  [public|weak]   (optional modifier)
//	  string          ("path/to.proto")
//	  ;
func parseImport(node ts.Node, src []byte) (path string, public bool) {
	for i := range node.ChildCount() {
		ch := node.Child(int(i))
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "public":
			public = true
		case "string":
			raw := nodeText(ch, src)
			path = strings.Trim(raw, "\"'")
		}
	}
	return path, public
}
