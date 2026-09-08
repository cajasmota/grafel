package golang

import (
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"github.com/cajasmota/grafel/internal/types"
)

// field_type_refs.go — the field→declared-type edge for Go (issue #6912, arm D).
// Arm A is internal/extractors/csharp/field_type_refs.go (#6984), arm B is
// internal/extractors/proto/field_type_refs.go (#6991), arm C is
// internal/extractors/rust/field_type_refs.go (#7000).
//
// THE GAP, AND WHY GO IS ARM B'S SHAPE RATHER THAN ARM C'S. Go is the ONLY
// language on this issue that already ships a same-file-gated declared-type
// edge: extractStructFieldDependencies emits DEPENDS_ON per struct field type,
// gated on collectDeclaredTypeNames. But it anchors that edge on the STRUCT
// (`FromID: ownerName`), so the struct is wired and the FIELD is not. Every
// SCOPE.Schema/field entity extractStructFieldEntities emits (#4850) is a leaf
// on its outbound side, which is the #6912 population: #6726 measures
// SCOPE.Schema at 99.3% orphan across their corpus, and Go is one of their
// languages.
//
// So this pass adds a SECOND edge, anchored on the field, and leaves the
// struct-anchored DEPENDS_ON exactly as it was. That is arm B's ruling: the
// duplicated thing is the ANCHOR, not the information. "Which FIELDS point at
// Order?" is what the new anchor answers and the old one cannot — DEPENDS_ON
// names no field at all, not even in a property, so after this pass the two
// edges carry different facts rather than the same fact twice.
//
// DOES THE SECOND ANCHOR DOUBLE-COUNT IN internal/quality? This was arm B's
// open question and it lands here because Go is the only language shipping both
// edges. MEASURED, by reading the arithmetic rather than reasoning about it —
// answer: no metric counts the same fact twice, because no metric counts
// ENDPOINTS; but two rates move, both in the flattering direction, and neither
// dedups.
//
//   - internal/quality/audit/audit.go:424 OrphanRate is DEFLATED. `touched`
//     (audit.go:363-368) admits any non-CONTAINS/DECLARES edge in EITHER
//     direction, so a field that was previously reachable only by its CONTAINS
//     edge flips to non-orphan the moment it is the FromID here. That is the
//     #6912 gap closing and is the intended effect — but it is worth naming that
//     the connectivity is derived from a fact the graph already recorded at the
//     struct anchor. Note internal/quality/fitness/rule.go:511-526 computes a
//     SECOND, inbound-only orphan rate that will NOT move for the field, so the
//     two published orphan numbers diverge further after this arm.
//   - audit.go:433 ReferencesPerFunction is INFLATED, and this one is a genuine
//     distortion rather than a gain. Its numerator counts REFERENCES EDGES
//     (audit.go:382-384) while its denominator counts FUNCTION ENTITIES, and a
//     SCOPE.Schema/field is not function-like (audit.go:480-493) — so every edge
//     added here raises the ratio with no denominator movement. DEPENDS_ON is
//     counted by nothing, so the old anchor contributed zero and the new one
//     contributes one. It feeds refHealth/RiskScore (audit.go:584) and the
//     "REFERENCES emission missing" gate at audit.go:680, which it can therefore
//     mask. The honest fix is for that metric to exclude ref_kind=field_target_type
//     — nothing in internal/quality reads `ref_kind` today — but that is a
//     quality-metric change touching every arm A–D at once, not a Go-only one,
//     so it is reported rather than smuggled in here.
//
// THREE PLACES THIS ARM DELIBERATELY DOES NOT COPY ITS NEIGHBOUR
//
//  1. THE ADDRESS IS STRUCTURAL, NOT A BARE NAME. The adjacent DEPENDS_ON uses
//     `ToID: t` — the bare type name. That is not the address arms A–C settled
//     on, and adjacency is not a reason to inherit it: a bare name is resolved
//     by name-family lookup across the whole graph and is what #6986 measured at
//     91.6% dangling for the custom lane's `Class:<Name>` form. This pass uses
//     BuildComponentStructuralRef — `scope:component:class:go:<file>:<Name>` —
//     which is FILE-SCOPED, so a second .go file declaring a same-named type
//     cannot make it ambiguous. Post-resolution the ToID is the target entity's
//     ID, so no query sees the dialect. The existing DEPENDS_ON keeps its bare
//     name; re-spelling it is a graph-wide change with no bearing on #6912.
//
//  2. THE ALLOW-LIST ADMITS SCOPE.Schema, BECAUSE IN GO A TYPE ALIAS IS ONE.
//     Arm C's guard is `Kind != "SCOPE.Component"`, which is correct for Rust
//     (buildRustTypeAlias emits a Component) and WRONG here: extractTypes emits
//     `type Celsius float64`, `type Handler func()`, `type UserID string` and
//     the Go-1.9 `type T = U` form as Kind SCOPE.Schema, Subtype type_alias
//     (extractor.go:11-13, and the two type_alias branches). Inheriting arm C's
//     kind check verbatim would have silently dropped every alias target — the
//     single largest non-struct declared-type population in idiomatic Go. So the
//     allow-list is written on Go's own buildComponent behaviour:
//
//     SCOPE.Component + struct     → admitted (type T struct{…})
//     SCOPE.Component + interface  → admitted (type T interface{…})
//     SCOPE.Schema    + type_alias → admitted (type T U / type T = U)
//
//     Everything else is refused, and THE ALLOW-LIST IS A LIVE FILTER WITH A
//     REAL KILL. An earlier draft of this comment claimed the opposite — that
//     the excluded records were refused by name shape before the subtype check
//     saw them, making it belt-and-braces — and offered a compound mutant
//     (allow-list removed TOGETHER with rule 2) as evidence. That experiment
//     established nothing: rule 2's mutant is independently lethal, so the
//     compound killed exactly the tests rule 2 alone kills and carried no
//     information about the allow-list. Recorded because the reasoning error is
//     more portable than the fix: a compound mutant grades its second half only
//     if its first half is NOT lethal on its own.
//
//     The distinguishing input, and the reason the claim was false:
//
//     - IMPORT ENTITIES are SCOPE.Component with an EMPTY Subtype and Name set
//     to the whole import path (extractImportEntities), never truncated to a
//     segment. A single-segment path IS a bare name sharing this file's
//     namespace, and it is reachable twice over, because `import _ "embed"`
//     does not bind the identifier:
//
//     SAME-FILE (`type embed struct{…}` here too): two records, both
//     SCOPE.Component/embed, hence ONE graph node under rule 2 — so admitting
//     the import would address the same node by the same ToID and change no
//     output. Pinned by TestGoFieldTypeRefs_BlankImportSharingATypeNameBinds,
//     which is really rule 2's test: it proves the count must be over KINDS,
//     since under a record count this edge disappears.
//
//     SIBLING-FILE (`type embed struct{…}` in another file of the package —
//     the ORDINARY case, since a Go package is a directory): the import
//     placeholder is then the ONLY record named `embed` in this file. One node,
//     so rule 2 is silent; a bare name, so "refused by name shape" does not
//     apply. Without the allow-list the field binds TO THE IMPORT PLACEHOLDER —
//     a wrong binding, and wrong bindings never reach bug-extractor precisely
//     because they bind. This is the case the allow-list exists for, and it is
//     graded by TestGoFieldTypeRefs_ImportPlaceholderIsNeverATarget.
//
//     Enumerated rather than hand-picked: of the records emitted before this
//     pass runs that carry a bare (non-dotted, non-path) Name, package-level
//     funcs are excluded by Go's own package-scope uniqueness, enum value-sets
//     always present a second kind (they require a same-file named type), and
//     the import placeholder is the only one that can stand alone.
//     - SCOPE.Schema + field is refused so a field never targets a field. This
//     one IS unreachable by name shape: a field entity's Name is dotted
//     (`Order.Buyer`) and a candidate is a single type_identifier, which cannot
//     contain a dot. Refused anyway, but claimed as nothing more.
//     - SCOPE.Component + file (#577) is refused; its Name is the file path.
//
//  3. THE CANDIDATE COLLECTOR IS NOT resolveTypeReferences, and this is the one
//     correctness fix in the arm. resolveTypeReferences collects EVERY
//     descendant type_identifier, which for Go's `qualified_type` node
//     (`other.Order`) is the TRAILING SEGMENT — so it strips `other.Order` to
//     `Order`. In a file that also declares its own `Order`, the struct-anchored
//     DEPENDS_ON therefore binds to the WRONG entity today, silently, because a
//     bound edge never reaches `bug-extractor`. Arms B and C both carry a
//     forbidden row for exactly that mutation. goFieldTypeCandidates skips
//     `qualified_type` WHOLE rather than taking its tail, so the new edge is
//     deliberately narrower than its neighbour on qualified names. Pinned in
//     both directions by TestGoFieldTypeRefs_QualifiedTypeIsNotStrippedToASameFileName,
//     which also pins that DEPENDS_ON's behaviour is UNCHANGED by this arm —
//     narrowing resolveTypeReferences is a separate fix with its own blast
//     radius, not a rider on this one.
//
// A NON-BINDING EDGE IS WORSE THAN NO EDGE, AND GO HAS A PARALLEL POPULATION
// THAT MAKES THIS SHARPER THAN IT WAS FOR RUST. An unresolved stub is kept
// verbatim and dangling and classified `bug-extractor`, so every miss is a full
// hit. The Go-specific hazard is extractGoEnums: a `type Status int` beside a
// const block typed by it produces a SCOPE.Enum value-set named `Status`
// (extractor.EnumEntity) IN ADDITION to the SCOPE.Schema/type_alias of the same
// name in the same file. The resolver's byLocation fallback is what binds a
// component-space ref to a Schema entity, and that fallback consults
// ambigLocation FIRST — so a name carried by two same-file entities returns
// statusAmbiguous and the edge dangles.
//
// Arm C's collision check only looked for duplicates WITHIN its own admitted
// set and would not have seen this: the alias is admitted and the enum
// value-set is not. So goInFileTypeTargets counts across EVERY same-file record
// and drops any name denoting more than one graph node, whether or not the
// second node is a legal target. That is strictly the resolver's own ambiguity
// rule, mirrored at the emit site so the pass declines to emit rather than
// emitting a stub that cannot bind.
//
// The rule is graded by a POSITIVE CONTROL IN THE SAME FIXTURE rather than by
// the shadowed case alone: `type Status int` carries a typed const block and
// gets no edge, while `type Tier int` — the same declaration form with no const
// block, hence no value-set and one node — DOES get one. Without that pair the
// test cannot tell "the enum shadow is respected" from "a type_alias target
// never works at all", which is the failure mode a recall-only assertion has.
// See TestGoFieldTypeRefs_TypeShadowedByAnEnumValueSetGetsNoEdge.
//
// WHAT THIS PASS IS NOT
//
//   - SAME FILE ONLY. A field whose type is declared in another file of the
//     same package gets nothing, and for Go — where a package is a directory of
//     files and one-type-per-file is not the convention — that is the dominant
//     miss, larger than it was for C# or Rust. Closing it needs the cross-file
//     package type view that pass 1 has none of; binding a bare type name
//     across files is the exact hazard #6976/#6369 are about. Separate arm, not
//     a widening of this one.
//   - IT DOES NOT UNDERSTAND TYPE PARAMETERS. `type Holder[T any] struct { Item
//     T }` yields candidate `T`, which is dropped only because nothing in the
//     file happens to be DECLARED `T`. A file that also declares `type T
//     struct{}` gets a wrong edge. Known-wrong and pinned as such by
//     TestGoFieldTypeRefs_KnownOverFire_TypeParameterShadowedByASameFileType, a
//     case a real fix is expected to break.
//   - AN ANONYMOUS NESTED STRUCT OR INTERFACE IS NOT A TARGET AND CONTRIBUTES
//     NONE. `Inner struct { A Order }` yields nothing: `Order` is the declared
//     type of `Inner.A`, not of `Inner`, and emitting `Inner → Order` with
//     target_type=Order would assert something false. struct_type and
//     interface_type are therefore skipped whole. This costs no reachable
//     recall — `topLevelFieldDeclarations` already refuses to treat nested
//     struct members as the outer struct's fields (#4850), so the nested field
//     has no entity of its own to carry the edge either.

// goFieldTypeRefsMetaKey is the per-field stash written by
// extractStructFieldEntities and consumed — and deleted — by
// attachGoFieldTypeRefs.
//
// The candidates cannot become edges during the walk. Two independent reasons,
// either sufficient: `type Order struct { Buyer Customer }` may be declared
// BEFORE `type Customer struct{}` in the same file, so the in-file declaration
// set is complete only once the walk has finished; and the collision rule above
// must see the enum value-sets and import records, which are appended to
// `records` after extractTypes returns.
const goFieldTypeRefsMetaKey = "field_type_refs"

// goFieldTargetRefKind is the value of the `ref_kind` edge property. It matches
// arm A's csFieldTargetRefKind, arm B's protoFieldTargetRefKind, arm C's
// rustFieldTargetRefKind and the custom lane's referencesClassEdge verbatim —
// the discriminator is what makes the edge queryable as a field→declared-type
// edge across languages, so it must be one string.
//
// It is NOT the spelling the pre-existing struct-anchored DEPENDS_ON uses (that
// edge carries no properties at all). That is a different relation with a
// different anchor and is left exactly as it was.
const goFieldTargetRefKind = "field_target_type"

// goFieldTypeCandidates returns every bare type name written in a field's
// declared type expression, in source order and without deduplication.
//
// It descends the type node and collects `type_identifier` leaves, which unwraps
// pointer (`*Order`), slice (`[]Order`), array (`[3]Order`), map
// (`map[Key]Order` — BOTH halves, each judged independently), channel (`chan
// Order`), variadic, function (`func(Order) error`) and generic (`Holder[Order]`)
// syntax for free, with no wrapper list to keep in sync.
//
// A Go PRIMITIVE needs no blocklist here and, unlike arm B's protoScalars, a
// blocklist would be actively wrong rather than merely redundant. `int`,
// `string` and `bool` are not keywords in Go's grammar — tree-sitter-go emits
// them as `type_identifier`, exactly like a user type, because Go's predeclared
// identifiers live in the universe scope and are legally shadowable. So the
// primitive is collected as a candidate and then refused by the in-file
// declaration check, which is the correct order of operations: a file that
// writes `type string struct { v int }` (legal Go — the predeclared identifier
// is shadowed at package scope) genuinely does mean THAT type in field position,
// and a blocklist would refuse the one edge that should exist. This is the exact
// inverse of arm B's case, where protoc's grammar binds `string` to the scalar
// before it ever considers a same-named message, and is why that guard is not
// portable. Both directions graded:
// TestGoFieldTypeRefs_PrimitiveFieldsProduceNoEdge enumerates the predeclared
// set against an independent literal, and
// TestGoFieldTypeRefs_ShadowedPredeclaredIdentifierIsATarget constructs the
// shadowing declaration.
//
// TWO node kinds are skipped WHOLE, without descending:
//
//   - `qualified_type` — `other.Order`, `time.Time`. The trailing segment is
//     never taken; see point 3 of the header block. This is the one place the
//     field edge is narrower than the struct-anchored DEPENDS_ON.
//   - `struct_type` and `interface_type` — an anonymous nested struct or
//     interface written in field position. Not a named target, and its members
//     are not this field's declared type; see the header block.
func goFieldTypeCandidates(typ ts.Node, src []byte) []string {
	var out []string
	var walkType func(n ts.Node)
	walkType = func(n ts.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "qualified_type", "struct_type", "interface_type":
			return
		case "type_identifier":
			out = append(out, nodeText(n, src))
			return
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walkType(n.NamedChild(i))
		}
	}
	walkType(typ)
	return out
}

// goFieldTypeTarget is one in-file type declaration a field can point at: the
// structural ToID that binds to it, and the bare name for the `target_type`
// property.
type goFieldTypeTarget struct {
	toID string
	name string
}

// goInFileTypeTargets indexes every TYPE DECLARED in this file that a field-type
// edge may address, keyed by bare name.
//
// Two rules, and the second is the Go-specific one:
//
//  1. The record must be a type declaration this package emits as an entity, per
//     the three-line allow-list in the header block. This is a check on the
//     EMITTED RECORD, not on collectDeclaredTypeNames, and the reason is NOT the
//     one it would be natural to give.
//
//     The natural argument is that knownTypeNames is a superset: it takes the
//     name of every type_spec node, while extractTypes `continue`s without
//     emitting anything when it recognises none of the body node kinds, so
//     gating on it could address an entity that does not exist. THAT ARGUMENT IS
//     UNSUPPORTED — the body-kind list is wide enough that no Go type_spec form
//     tried here escapes it. `Stack[int]`, `pkg.Stack[int]`, `[...]int`,
//     `func(...int)`, `[][]map[string]*J`, `(int)`, `interface{}`,
//     `interface{ int | string }` and `struct{}` ALL produce an entity. The
//     superset is real in the code and empty in fact, and it is recorded as an
//     unconstructed case rather than a demonstrated one.
//
//     The load-bearing reason is the other one: a NAME SET CANNOT EXPRESS EITHER
//     RULE THIS PASS NEEDS. knownTypeNames is a map[string]bool and carries no
//     Kind and no Subtype, so it cannot separate an admitted type declaration
//     from a value-set that merely shares its name (rule 2), and it cannot
//     express the allow-list at all. Reading the records is what makes the
//     enum-shadow rule possible; using knownTypeNames would have forced the
//     dangling edge described in the header block.
//
//  2. A name that denotes MORE THAN ONE DISTINCT GRAPH NODE in this file is
//     dropped, and the count spans every record in the file rather than only the
//     admitted ones. The resolver's ambigLocation check runs before the
//     byLocation fallback this address depends on, so a name shared with a
//     NON-target node — a SCOPE.Enum value-set over a `type Status int`, the
//     overwhelmingly common Go enum idiom — cannot bind either. Counting only
//     admitted records (arm C's rule) would emit a stub guaranteed to dangle.
//
//     "EVERY same-file record" means every record that exists AT STEP 4b-bis,
//     which is where this pass runs — not every record Extract eventually
//     returns. Steps 4c-6b append more (SCOPE.Pattern, SCOPE.ExceptionType,
//     config keys, constant sets) and buildSymbolIndex sees those too, so a
//     collision invisible here would reach the resolver as a dangling edge.
//     It holds today only because every one of those producers emits a PREFIXED
//     name (`error_handling:…`, `exception:…`, `config:…`), which no bare
//     type_identifier candidate can equal; verified empirically at review, with
//     all 168 corpus edges re-checked against the POST-Extract record set —
//     0 target names carrying more than one kind, 0 targets without a same-file
//     declaration. A future producer emitting a BARE-named entity would
//     silently reintroduce dangling here, so that is the invariant to preserve
//     rather than an accident to rely on.
//     The unit is the KIND rather than the record, mirroring the resolver
//     exactly; see the comment on nameKinds below for why, and why counting
//     records instead would refuse edges that bind.
func goInFileTypeTargets(records []types.EntityRecord, filePath string) map[string]goFieldTypeTarget {
	// Pass 1 — how many DISTINCT GRAPH NODES each same-file name denotes,
	// target or not.
	//
	// The unit is the KIND, not the record, and that is the resolver's own rule
	// rather than a convenience. buildSymbolIndex marks (file, name) ambiguous
	// only when it meets a SECOND DISTINCT ENTITY ID there (`existing != e.ID`,
	// internal/resolve/refs.go:1308), and graph.EntityID hashes
	// (repo, Kind, Name, SourceFile) — so within one file two records collide
	// into ONE node exactly when they share a Kind. Counting RECORDS instead
	// would refuse names the resolver binds perfectly well: `import _ "embed"`
	// beside `type embed struct{…}` is two records and ONE node, both
	// SCOPE.Component/embed in this file. Counting kinds keeps that edge and
	// still drops the type_alias-plus-enum case, which is two kinds and
	// genuinely two nodes.
	nameKinds := make(map[string]map[string]bool)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" {
			continue
		}
		if nameKinds[r.Name] == nil {
			nameKinds[r.Name] = make(map[string]bool)
		}
		nameKinds[r.Name][r.Kind] = true
	}

	// Pass 2 — admit the type declarations whose name is unambiguous.
	targets := make(map[string]goFieldTypeTarget)
	for i := range records {
		r := &records[i]
		if r.SourceFile != filePath || r.Name == "" {
			continue
		}
		switch {
		case r.Kind == "SCOPE.Component" && (r.Subtype == "struct" || r.Subtype == "interface"):
		case r.Kind == "SCOPE.Schema" && r.Subtype == "type_alias":
		default:
			continue
		}
		if len(nameKinds[r.Name]) > 1 {
			continue
		}
		targets[r.Name] = goFieldTypeTarget{
			toID: extractor.BuildComponentStructuralRef("go", filePath, r.Name),
			name: r.Name,
		}
	}
	return targets
}

// attachGoFieldTypeRefs appends one REFERENCES edge per (field, in-file declared
// type) pair and clears the stash it consumed.
//
// FromID is left empty so graph assembly anchors the edge on the field record
// that carries it — the Django precedent at
// internal/extractors/python/django_relational.go:246-252, and arms B and C.
// (Hibernate sets an explicit structural FromID because it hangs its edge off
// its own parallel SCOPE.Component node; that is a different shape and not the
// one to copy — #6912's own body was corrected on this point.)
//
// Relationships is APPENDED to, never assigned: a Go field record carries no
// outbound edge today, but assigning would silently clobber one added later.
//
// The `field_name` property is the field entity's own leaf name, recovered by
// stripping the `<owner>.` prefix extractStructFieldEntities built the Name
// from. That leaf is the JSON WIRE NAME when a `json:"…"` tag renamed the field
// — the same value the Name and the CONTAINS structural-ref already carry, so
// the property agrees with the endpoints rather than introducing a third
// spelling of the same field.
func attachGoFieldTypeRefs(records []types.EntityRecord, filePath string) []types.EntityRecord {
	targets := goInFileTypeTargets(records, filePath)
	for i := range records {
		r := &records[i]
		if r.Metadata == nil {
			continue
		}
		cands, _ := r.Metadata[goFieldTypeRefsMetaKey].([]string)
		delete(r.Metadata, goFieldTypeRefsMetaKey)
		if len(cands) == 0 || r.Kind != "SCOPE.Schema" || r.Subtype != "field" {
			continue
		}
		owner, _ := r.Metadata["owner"].(string)
		fieldName := r.Name
		if owner != "" {
			fieldName = strings.TrimPrefix(r.Name, owner+".")
		}
		emitted := make(map[string]bool)
		for _, cand := range cands {
			t, ok := targets[cand]
			if !ok || emitted[t.toID] {
				continue
			}
			// A field never targets its own owning struct: `type Node struct {
			// Next *Node }` is a self-reference the struct-anchored DEPENDS_ON
			// also declines (`t == ownerName`), and the CONTAINS edge already
			// relates the two records.
			if owner != "" && t.name == owner {
				continue
			}
			emitted[t.toID] = true
			r.Relationships = append(r.Relationships, types.RelationshipRecord{
				ToID: t.toID,
				Kind: string(types.RelationshipKindReferences),
				Properties: types.Props{
					{K: "field_name", V: fieldName},
					{K: "ref_kind", V: goFieldTargetRefKind},
					{K: "target_type", V: t.name},
				},
			})
		}
	}
	return records
}
