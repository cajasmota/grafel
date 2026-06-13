package rust

// juniper_typegraph.go — code-first GraphQL schema type→type graph for juniper
// (#5007, follow-up from #4964). The juniper sibling of
// internal/custom/rust/graphql_codefirst_typegraph.go (async-graphql, #3983) and
// internal/custom/{python,javascript}/graphql_codefirst_typegraph.go.
//
// internal/custom/rust/juniper.go already emits the flat DTO catalog (a
// SCOPE.Schema/dto per #[derive(GraphQLObject/GraphQLInputObject/GraphQLEnum)]
// type) and the synthetic GRAPHQL endpoint per #[graphql_object] resolver
// method. What it does NOT emit is the typed field→type relationship graph (the
// #3804 lane): which object type references which other object type, with what
// cardinality. juniper's Schema.type_graph_extraction was HONESTLY missing.
//
// This extractor closes that gap for juniper:
//
//	#[derive(GraphQLObject)]
//	struct User {
//	    id: i32,                  // scalar -> no edge
//	    name: String,             // scalar -> no edge
//	    orders: Vec<Order>,       // GRAPH_RELATES User->Order  to_many
//	    account: Option<Account>, // GRAPH_RELATES User->Account nullable to_one
//	    manager: Option<User>,    // self_ref
//	}
//
//	struct Query;
//	#[graphql_object]
//	impl Query {
//	    // resolver return type -> GRAPH_RELATES Query->User
//	    fn user(&self, id: i32) -> FieldResult<User> { ... }
//	    fn orders(&self) -> Vec<Order> { ... }
//	}
//
// Each object type (a #[derive(GraphQLObject)] struct, the resolver root of a
// #[graphql_object] / #[graphql_subscription] impl) becomes a SCOPE.Schema/type
// node addressed with the SAME canonical structural ref the SDL pass and the
// async-graphql / py / jsts code-first passes use
// (BuildOperationStructuralRef("graphql", file, TypeName)), so identities
// converge on one node per type across passes/repos. Each object-typed struct
// field and each resolver return type becomes a GRAPH_RELATES edge carrying the
// identical cardinality property contract as the SDL / async-graphql / py / jsts
// emitters:
//
//	{field_name, list, nullable, item_nullable, cardinality:to_one|to_many,
//	 self_ref, graphql_field, framework:juniper}
//
// #5109 DEEPENS this pass along three axes (the deferred scope from #5007):
//
//	(a) FIELD/TYPE RENAME — #[graphql(name = "...")] on a field, resolver
//	    method, struct, or interface trait now overrides the Rust ident for the
//	    GraphQL-facing name. The recorded graphql_type / graphql_field /
//	    field_name properties use the GraphQL name; the structural-ref node
//	    identity also keys on the GraphQL name so it converges with the SDL pass
//	    (which names types/fields by their schema name, not the Rust ident).
//
//	(b) CROSS-FILE TARGETS — a field/resolver whose innermost object type is a
//	    capitalized non-scalar ident that is NOT declared in the SAME file now
//	    emits a GRAPH_RELATES edge whose ToID is a by-name stub
//	    ("Kind:SCOPE.Schema:<Type>") that the resolver binds via its global
//	    by-name index to the matching SCOPE.Schema/type node emitted by the
//	    target file's own pass. Such edges carry cross_file=true. If no such
//	    node exists anywhere (or the name is ambiguous), the stub stays
//	    unresolved — honestly no false edge is fabricated.
//
//	(c) #[graphql_interface] TRAITS — a #[graphql_interface(for = [A, B])] trait
//	    becomes an interface type node (graphql_kind=interface). Each type listed
//	    in `for = [...]` (and each `impl Trait for Object`) emits a GRAPH_RELATES
//	    edge Object->Interface carrying relation=implements. A field/resolver
//	    typed as an interface trait is a recognized edge target like any object.
//
// HONEST LIMITS:
//   - Cross-file resolution is by-NAME only: a same-named non-GraphQL
//     SCOPE.Schema type in another file is indistinguishable, but the resolver
//     drops the edge on any name ambiguity, so no wrong edge is produced.
//   - GraphQLInputObject and GraphQLEnum types are intentionally NOT owners —
//     inputs/enums are the flat DTO catalog (already credited in #4964) and carry
//     no object→object data relations in the output type graph.
//   - A plain `struct` with no juniper derive emits no type node and no edge; a
//     non-GraphQL `impl` (no #[graphql_object]/#[graphql_subscription]) emits no
//     resolver edge.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_rust_juniper_typegraph", &rustJuniperTypeGraphExtractor{})
}

// rustJuniperTypeGraphExtractor builds the object-type→type relationship graph
// for juniper code-first schemas.
type rustJuniperTypeGraphExtractor struct{}

func (e *rustJuniperTypeGraphExtractor) Language() string {
	return "custom_rust_juniper_typegraph"
}

var (
	// #[derive(... GraphQLObject ...)] struct Name { ... } — juniper output
	// object types. These are the owners of the field graph and edge targets.
	// Negative lookahead is not available in RE2; GraphQLInputObject is filtered
	// out below by requiring the derive token NOT to be the input/enum form.
	reJunTGObjectStruct = regexp.MustCompile(
		`#\[derive\s*\(([^)]*\bGraphQLObject\b[^)]*)\)\]\s*(?:#\[[^\]]*\]\s*)*(?:pub\s+)?struct\s+([A-Za-z_]\w*)`,
	)
	// #[graphql_object ...] / #[graphql_subscription ...] (resolver impl)
	// immediately preceding `impl <Root>`. Group 1 = the resolver root type name.
	reJunTGObjectImpl = regexp.MustCompile(
		`#\[graphql_(?:object|subscription)[^\]]*\]\s*(?:#\[[^\]]*\]\s*)*impl\s+(?:<[^>]*>\s*)?([A-Za-z_]\w*)`,
	)
	// #[graphql_interface(...)] trait Name { ... } — juniper models GraphQL
	// interfaces as Rust traits (unlike async-graphql's Interface enums). The
	// whole attribute (group 1) carries the optional name-rename and the
	// `for = [...]` implementor list; group 2 is the Rust trait ident.
	reJunTGInterfaceTrait = regexp.MustCompile(
		`#\[graphql_interface(\s*\([^)]*\))?\s*\]\s*(?:#\[[^\]]*\]\s*)*(?:pub\s+)?trait\s+([A-Za-z_]\w*)`,
	)
	// `impl <Trait> for <Object>` — the per-object interface implementation form
	// (an alternative to the `for = [...]` attribute list). Group 1 = trait,
	// group 2 = object.
	reJunTGImplFor = regexp.MustCompile(
		`\bimpl\s+(?:<[^>]*>\s*)?([A-Za-z_]\w*)\s+for\s+([A-Za-z_]\w*)`,
	)
	// A #[graphql(name = "GqlName")] / #[graphql_interface(name = "...")] /
	// #[graphql_object(name = "...")] rename attribute. Group 1 = the GraphQL
	// name. Only the explicit `name = "..."` form changes the schema-facing
	// identifier.
	reJunTGRename = regexp.MustCompile(
		`#\[graphql(?:_object|_interface|_subscription)?\s*\([^)]*\bname\s*=\s*"([^"]+)"`,
	)
	// `for = [A, B, C]` implementor list inside a #[graphql_interface(...)]
	// attribute. Group 1 = the bracketed body.
	reJunTGForList = regexp.MustCompile(
		`\bfor\s*=\s*\[([^\]]*)\]`,
	)
)

func (e *rustJuniperTypeGraphExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.rust_juniper_typegraph")
	_, span := tracer.Start(ctx, "custom.rust_juniper_typegraph")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}
	src := string(file.Content)

	// File-signal gate: require a juniper marker (mirrors juniper.go).
	if !strings.Contains(src, "#[graphql_object") &&
		!strings.Contains(src, "#[graphql_subscription") &&
		!strings.Contains(src, "#[graphql_interface") &&
		!strings.Contains(src, "GraphQLObject") {
		return nil, nil
	}

	type ownerBlock struct {
		name    string // GraphQL-facing name (rename-honoured)
		rust    string // Rust ident (for impl-for matching)
		kind    string // "struct" | "resolver" | "interface"
		line    int
		body    string
	}
	var owners []ownerBlock
	// known maps a target type's GraphQL name -> true (same-file declared object
	// / interface types). gqlName maps a Rust ident -> its GraphQL name so a
	// field/return typed by Rust ident resolves to the renamed target.
	known := map[string]bool{}
	gqlName := map[string]string{}

	register := func(rust, gql string) {
		gqlName[rust] = gql
		known[gql] = true
	}

	// #[derive(GraphQLObject)] structs are both owners and edge targets.
	// GraphQLInputObject also matches the substring GraphQLObject, so require the
	// derive group to NOT contain the input/enum forms.
	for _, m := range reJunTGObjectStruct.FindAllStringSubmatchIndex(src, -1) {
		derives := src[m[2]:m[3]]
		if strings.Contains(derives, "GraphQLInputObject") {
			continue
		}
		rustName := src[m[4]:m[5]]
		// The rename may sit on a stacked line above the derive OR between the
		// derive and the struct keyword (inside this match span m[0]:m[5]).
		name := junTGTypeRenameSpan(src, m[0], m[5], rustName)
		bodyStart, bodyEnd := agqlBlockBody(src, m[5])
		body := ""
		if bodyStart >= 0 {
			body = src[bodyStart:bodyEnd]
		}
		owners = append(owners, ownerBlock{name: name, rust: rustName, kind: "struct", line: lineOf(src, m[0]), body: body})
		register(rustName, name)
	}
	// #[graphql_interface] traits are interface type nodes + owners of their
	// trait-method field graph; the trait ident is also an edge target.
	for _, m := range reJunTGInterfaceTrait.FindAllStringSubmatchIndex(src, -1) {
		rustName := src[m[4]:m[5]]
		// The rename may live in the attribute paren group (group 1) or in a
		// preceding/stacked #[graphql(...)] line; check the whole match span plus
		// the preceding attribute run.
		name := junTGTypeRenameSpan(src, m[0], m[5], rustName)
		bodyStart, bodyEnd := agqlBlockBody(src, m[5])
		body := ""
		if bodyStart >= 0 {
			body = src[bodyStart:bodyEnd]
		}
		owners = append(owners, ownerBlock{name: name, rust: rustName, kind: "interface", line: lineOf(src, m[0]), body: body})
		register(rustName, name)
	}
	// #[graphql_object]/#[graphql_subscription] impl <Root> resolver blocks are
	// owners; their return types are edge targets.
	for _, m := range reJunTGObjectImpl.FindAllStringSubmatchIndex(src, -1) {
		rustName := src[m[2]:m[3]]
		name := rustName
		if g, ok := gqlName[rustName]; ok {
			name = g
		}
		bodyStart, bodyEnd := agqlBlockBody(src, m[1])
		body := ""
		if bodyStart >= 0 {
			body = src[bodyStart:bodyEnd]
		}
		owners = append(owners, ownerBlock{name: name, rust: rustName, kind: "resolver", line: lineOf(src, m[0]), body: body})
	}

	if len(owners) == 0 {
		return nil, nil
	}

	nodes := map[string]int{}
	var out []types.EntityRecord
	nodeFor := func(name string, line int, isInterface bool) {
		if _, ok := nodes[name]; ok {
			return
		}
		ent := makeEntity(name, "SCOPE.Schema", "type", file.Path, file.Language, line)
		setProps(&ent,
			"graphql_type", name,
			"framework", "juniper",
			"code_first", "true",
			"structural_ref", extractor.BuildOperationStructuralRef("graphql", file.Path, name),
			"provenance", "INFERRED_FROM_CODEFIRST_GRAPHQL_OBJECTTYPE",
		)
		if isInterface {
			setProps(&ent, "graphql_kind", "interface")
		}
		out = append(out, ent)
		nodes[name] = len(out) - 1
	}

	// targetRefFor resolves the ToID structural-ref for an edge target.
	//   - Same-file declared type -> the canonical file-keyed operation ref the
	//     SDL / py / jsts passes use, so identities converge in-file (and
	//     crossFile=false).
	//   - Cross-file capitalized non-scalar -> a by-name stub the resolver binds
	//     to the matching SCOPE.Schema/type node emitted by the target file's own
	//     pass via its global by-name index (crossFile=true).
	targetRefFor := func(target string) (ref string, crossFile bool) {
		if known[target] {
			return extractor.BuildOperationStructuralRef("graphql", file.Path, target), false
		}
		// By-name stub: "Kind:SCOPE.Schema:<Type>". splitStub keys this on the
		// SCOPE.Schema byKind bucket, falling back to the kind-agnostic byName
		// index; an ambiguous name leaves it unresolved (no false edge).
		return "Kind:SCOPE.Schema:" + target, true
	}

	seen := map[string]bool{}
	// emit records a GRAPH_RELATES field/resolver edge. target is the GraphQL
	// name of the destination type (same-file rename already applied by the
	// caller). For cross-file targets (not same-file declared) the edge is still
	// emitted with a by-name ToID and cross_file=true.
	emit := func(owner, fieldName, target string, tc rustGqlCard) {
		if target == "" || rustGqlScalars[target] {
			return
		}
		// Cross-file gate: only resolvable-looking identifiers (capitalized) are
		// candidate object types. A same-file unknown lowercase ident is not a
		// type. This keeps the no-match no-op honest.
		if !known[target] && !junTGIsTypeIdent(target) {
			return
		}
		key := owner + "|" + fieldName + "|" + target
		if seen[key] {
			return
		}
		seen[key] = true
		ownerRef := extractor.BuildOperationStructuralRef("graphql", file.Path, owner)
		targetRef, crossFile := targetRefFor(target)
		props := map[string]string{
			"field_name":    fieldName,
			"list":          rustGqlBool(tc.list),
			"nullable":      rustGqlBool(tc.nullable),
			"cardinality":   rustGqlCardLabel(tc),
			"self_ref":      rustGqlBool(target == owner),
			"graphql_field": owner + "." + fieldName,
			"framework":     "juniper",
			"provenance":    "INFERRED_FROM_CODEFIRST_GRAPHQL_FIELD",
		}
		if tc.list {
			props["item_nullable"] = rustGqlBool(tc.itemNullable)
		}
		if crossFile {
			props["cross_file"] = "true"
			props["provenance"] = "INFERRED_FROM_CODEFIRST_GRAPHQL_FIELD_CROSSFILE"
		}
		idx := nodes[owner]
		out[idx].Relationships = append(out[idx].Relationships,
			types.RelationshipRecord{
				FromID:     ownerRef,
				ToID:       targetRef,
				Kind:       string(types.RelationshipKindGraphRelates),
				Properties: props,
			})
	}

	// resolveTarget maps a parsed base Rust type ident to its GraphQL-facing
	// name (honouring a same-file rename) for use as the edge target.
	resolveTarget := func(base string) string {
		if g, ok := gqlName[base]; ok {
			return g
		}
		return base
	}

	for _, ow := range owners {
		nodeFor(ow.name, ow.line, ow.kind == "interface")
		switch ow.kind {
		case "struct", "interface":
			for _, fm := range reGqlTGStructField.FindAllStringSubmatchIndex(ow.body, -1) {
				fieldRust := ow.body[fm[2]:fm[3]]
				typeExpr := strings.TrimSpace(ow.body[fm[4]:fm[5]])
				base, tc := rustParseTypeExpr(typeExpr)
				if base == "" {
					continue
				}
				fieldName := junTGFieldRename(ow.body, fm[0], fieldRust)
				emit(ow.name, fieldName, resolveTarget(base), tc)
			}
			if ow.kind == "interface" {
				// Interface trait methods are also fields (juniper interfaces
				// declare resolvers as trait methods).
				for _, fm := range reGqlTGResolverFn.FindAllStringSubmatchIndex(ow.body, -1) {
					fieldRust := ow.body[fm[2]:fm[3]]
					ret := rustResolverReturnType(ow.body, fm[0])
					if ret == "" {
						continue
					}
					base, tc := rustParseTypeExpr(ret)
					if base == "" {
						continue
					}
					fieldName := junTGFieldRename(ow.body, fm[0], fieldRust)
					emit(ow.name, fieldName, resolveTarget(base), tc)
				}
			}
		case "resolver":
			for _, fm := range reGqlTGResolverFn.FindAllStringSubmatchIndex(ow.body, -1) {
				fieldRust := ow.body[fm[2]:fm[3]]
				ret := rustResolverReturnType(ow.body, fm[0])
				if ret == "" {
					continue
				}
				base, tc := rustParseTypeExpr(ret)
				if base == "" {
					continue
				}
				fieldName := junTGFieldRename(ow.body, fm[0], fieldRust)
				emit(ow.name, fieldName, resolveTarget(base), tc)
			}
		}
	}

	// #[graphql_interface] implementation edges: each object type that
	// implements an interface trait gets a GRAPH_RELATES Object->Interface edge
	// with relation=implements. Two source idioms: the `for = [A, B]` attribute
	// list and the `impl Trait for Object` form.
	implSeen := map[string]bool{}
	emitImplements := func(objRust, ifaceGql string) {
		objGql := resolveTarget(objRust)
		if objGql == "" || ifaceGql == "" || objGql == ifaceGql {
			return
		}
		key := objGql + "=>" + ifaceGql
		if implSeen[key] {
			return
		}
		implSeen[key] = true
		// Ensure the implementing object has a source node (it may be a
		// cross-file or not-yet-emitted owner).
		objRef, objCross := targetRefFor(objGql)
		ifaceRef := extractor.BuildOperationStructuralRef("graphql", file.Path, ifaceGql)
		props := map[string]string{
			"relation":   "implements",
			"framework":  "juniper",
			"provenance": "INFERRED_FROM_CODEFIRST_GRAPHQL_INTERFACE",
		}
		var fromIdx int
		if idx, ok := nodes[objGql]; ok {
			fromIdx = idx
		} else {
			// The object node is cross-file; anchor the edge on the interface
			// node so it is still carried, with the object as a by-name FromID.
			fromIdx = nodes[ifaceGql]
			if objCross {
				props["cross_file"] = "true"
			}
		}
		out[fromIdx].Relationships = append(out[fromIdx].Relationships,
			types.RelationshipRecord{
				FromID:     objRef,
				ToID:       ifaceRef,
				Kind:       string(types.RelationshipKindGraphRelates),
				Properties: props,
			})
	}

	for _, ow := range owners {
		if ow.kind != "interface" {
			continue
		}
		// `for = [A, B, C]` list in the trait's #[graphql_interface(...)] attr.
		// Re-scan the attribute run preceding the trait declaration.
		attr := junTGAttrRun(src, ow.rust)
		if fm := reJunTGForList.FindStringSubmatch(attr); fm != nil {
			for _, raw := range strings.Split(fm[1], ",") {
				obj := strings.TrimSpace(raw)
				if id := rustTGIdentRe.FindString(obj); id != "" {
					emitImplements(id, ow.name)
				}
			}
		}
	}
	// `impl Trait for Object` form, matched anywhere in the file.
	for _, m := range reJunTGImplFor.FindAllStringSubmatch(src, -1) {
		traitRust, objRust := m[1], m[2]
		if g, ok := gqlName[traitRust]; ok {
			// Only when the trait is a known interface owner.
			isIface := false
			for _, ow := range owners {
				if ow.kind == "interface" && ow.rust == traitRust {
					isIface = true
					break
				}
			}
			if isIface {
				emitImplements(objRust, g)
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// junTGIsTypeIdent reports whether an identifier looks like an object type name
// (capitalized first letter) rather than a scalar / lowercase ident. Used as the
// cross-file edge gate so a lowercase same-file-unknown token never fabricates a
// cross-file edge.
func junTGIsTypeIdent(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// junTGTypeRenameSpan returns the GraphQL-facing name for a type declaration
// whose macro+keyword match spans [matchStart, matchEnd). It honours a
// #[graphql(name = "...")] / #[graphql_interface(name = "...")] rename appearing
// EITHER in the attribute run immediately preceding the match OR anywhere inside
// the match span (e.g. a #[graphql(...)] line stacked between the derive and the
// struct keyword). Falls back to the Rust ident.
func junTGTypeRenameSpan(src string, matchStart, matchEnd int, rustName string) string {
	span := src[matchStart:matchEnd]
	if fm := reJunTGRename.FindStringSubmatch(span); fm != nil {
		return fm[1]
	}
	if attr := junTGAttrRunAt(src, matchStart); attr != "" {
		if fm := reJunTGRename.FindStringSubmatch(attr); fm != nil {
			return fm[1]
		}
	}
	return rustName
}

// junTGFieldRename returns the GraphQL-facing field name for a struct field or
// resolver method whose match starts at byte offset declStart within body. It
// honours a #[graphql(name = "...")] attribute on the line(s) immediately
// preceding the field/method. Falls back to the Rust ident.
func junTGFieldRename(body string, declStart int, rustName string) string {
	attr := junTGAttrRunAt(body, declStart)
	if fm := reJunTGRename.FindStringSubmatch(attr); fm != nil {
		return fm[1]
	}
	return rustName
}

// junTGAttrRunAt returns the contiguous run of attribute lines (#[...]) and the
// declaration's own opening that precede byte offset declStart, back to the
// previous blank line / non-attribute statement. This captures both the inline
// attribute group on the declaration line and stacked attribute lines above it,
// so a #[graphql(name="...")] placed on its own line is still associated.
func junTGAttrRunAt(src string, declStart int) string {
	// Walk back over preceding lines while they are attribute or whitespace.
	start := declStart
	lineStart := strings.LastIndexByte(src[:start], '\n') + 1
	for {
		prevEnd := lineStart - 1
		if prevEnd <= 0 {
			break
		}
		prevStart := strings.LastIndexByte(src[:prevEnd], '\n') + 1
		line := strings.TrimSpace(src[prevStart:prevEnd])
		if strings.HasPrefix(line, "#[") || line == "" {
			lineStart = prevStart
			continue
		}
		break
	}
	// Include the declaration line itself (inline attributes / paren group).
	end := strings.IndexByte(src[declStart:], '\n')
	if end < 0 {
		end = len(src) - declStart
	}
	return src[lineStart : declStart+end]
}

// junTGAttrRun returns the attribute run preceding the trait declaration named
// rustName near the given source line. It is a convenience wrapper that locates
// the trait by ident and delegates to junTGAttrRunAt — used to recover the
// `for = [...]` implementor list from a #[graphql_interface(...)] attribute.
func junTGAttrRun(src, rustName string) string {
	for _, mm := range reJunTGInterfaceTrait.FindAllStringSubmatchIndex(src, -1) {
		if src[mm[4]:mm[5]] == rustName {
			return junTGAttrRunAt(src, mm[0])
		}
	}
	return ""
}
