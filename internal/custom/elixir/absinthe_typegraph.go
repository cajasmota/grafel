package elixir

// absinthe_typegraph.go — code-first GraphQL schema type→type graph for Absinthe
// (Elixir), epic #3872, elixir audit #3885, completes #3804 for Elixir. Mirrors
// internal/custom/{python,rust}/graphql_codefirst_typegraph.go.
//
// The existing Absinthe support (internal/engine/elixir_routes.go +
// internal/engine/rules/elixir/frameworks/absinthe.yaml) only matches `object :`
// / `field :` as ROUTING signals — each `field :name` under a query/mutation/
// subscription block synthesizes an http_endpoint. It does NOT emit the GraphQL
// object-type → referenced-type graph: which object type references which other
// object type, and with what cardinality. So Absinthe's
// Schema.type_graph_extraction was HONESTLY missing.
//
// This extractor closes that gap for Absinthe's code-first schema notation:
//
//	object :user do
//	  field :id, :id                      # scalar  -> no edge
//	  field :name, non_null(:string)      # scalar  -> no edge
//	  field :orders, list_of(:order)      # GRAPH_RELATES user->order  to_many
//	  field :account, :account            # GRAPH_RELATES user->account to_one
//	  field :manager, :user               # GRAPH_RELATES user->user  self_ref
//	end
//
//	object :order do
//	  field :total, :decimal
//	end
//
// Each declared output object type (`object :name`, plus `interface :name` and
// `union :name`, which are valid field targets) becomes a SCOPE.Schema/type node
// addressed with the SAME canonical structural ref the SDL pass and the py/rust
// code-first passes use (BuildOperationStructuralRef("graphql", file, name)), so
// identities converge on one node per type. Each object-typed `field` becomes a
// GRAPH_RELATES edge carrying the identical cardinality property contract:
//
//	{field_name, list, nullable, item_nullable, cardinality:to_one|to_many,
//	 self_ref, graphql_field, framework}
//
// HONEST LIMITS:
//   - Field-type resolution is heuristic and same-file: a `field`'s referenced
//     atom must be a GraphQL object/interface/union type declared in the SAME
//     file. A scalar atom (:id/:string/:integer/...) or an atom that is not a
//     known object type in this file produces NO edge. Cross-file type
//     references (multi-module schemas split across `import_types`) and custom
//     scalars are not chased.
//   - `input_object` and `enum` are NOT owners and NOT edge targets — inputs and
//     enums are the flat DTO/value catalog, not the output object→object data
//     relationship graph. (`enum :name` carries members, not type refs.)
//   - The query/mutation/subscription operation roots are already modelled as
//     http_endpoint roots by the routing synthesizer; here they are owners of
//     resolver fields, so a `field :user, :user` under `query do` does emit the
//     root→type edge (the operation's data relation). They are not themselves
//     field targets.

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_elixir_absinthe_typegraph", &absintheTypeGraphExtractor{})
}

// absintheTypeGraphExtractor builds the object-type→type relationship graph for
// Absinthe code-first schemas.
type absintheTypeGraphExtractor struct{}

func (e *absintheTypeGraphExtractor) Language() string {
	return "custom_elixir_absinthe_typegraph"
}

// absintheScalars never make a type→type edge: the GraphQL built-ins plus the
// common Absinthe / custom-scalar atoms used as scalar field types.
var absintheScalars = map[string]bool{
	"id": true, "string": true, "integer": true, "int": true, "float": true,
	"boolean": true, "bool": true,
	"datetime": true, "naive_datetime": true, "date": true, "time": true,
	"decimal": true, "uuid": true, "uuid4": true, "json": true, "map": true,
}

var (
	// `object :name do` / `interface :name do` / `union :name do` — output
	// object-graph types: owners (object) and/or valid field targets (all three).
	// Group 1 = macro (object|interface|union), group 2 = atom type name.
	reAbObjectDecl = regexp.MustCompile(
		`(?m)^[ \t]*(object|interface|union)\s+:([a-z_]\w*)\b`,
	)
	// query do / mutation do / subscription do — operation roots. These are
	// field owners (their resolver fields carry data relations) but are NOT
	// field targets. Group 1 = root keyword.
	reAbRootDecl = regexp.MustCompile(
		`(?m)^[ \t]*(query|mutation|subscription)\s+do\b`,
	)
	// A `field :name, <type-expr>` declaration. Group 1 = field name, group 2 =
	// the type expression up to the line end / a trailing `do` block / `, opts`.
	// The `do` block form `field :x, :t do ... end` is handled by stopping the
	// type expression before the ` do`.
	reAbFieldDecl = regexp.MustCompile(
		`(?m)^[ \t]*field\s+:([a-z_]\w*)\s*,\s*([^\n]+)`,
	)
)

func (e *absintheTypeGraphExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.absinthe_typegraph_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "absinthe"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "elixir" {
		return nil, nil
	}
	src := string(file.Content)

	// File-signal gate: require an Absinthe schema-notation marker plus the
	// object/field constructs this extractor reads.
	if !strings.Contains(src, "Absinthe") && !strings.Contains(src, "object :") {
		return nil, nil
	}
	if !strings.Contains(src, "field :") {
		return nil, nil
	}

	// Pass 1: collect declared object-graph type names (edge targets) and the
	// (owner, kind, line, body) blocks whose fields we walk.
	type ownerBlock struct {
		name string
		kind string // "object" | "interface" | "union" | "root"
		line int
		body string
	}
	var owners []ownerBlock
	known := map[string]bool{} // valid field-target object types

	for _, m := range reAbObjectDecl.FindAllStringSubmatchIndex(src, -1) {
		macro := src[m[2]:m[3]]
		name := src[m[4]:m[5]]
		body := ectoBlockBody(src, m[1])
		owners = append(owners, ownerBlock{name: name, kind: macro, line: lineOf(src, m[0]), body: body})
		known[name] = true
	}
	for _, m := range reAbRootDecl.FindAllStringSubmatchIndex(src, -1) {
		root := src[m[2]:m[3]] // query | mutation | subscription
		// The root regex consumes the block `do`; start the body scan from the
		// match start so ectoBlockBody finds that same `do` (not the next one).
		body := ectoBlockBody(src, m[0])
		owners = append(owners, ownerBlock{name: root, kind: "root", line: lineOf(src, m[0]), body: body})
		// roots are NOT field targets; do not add to `known`.
	}

	if len(owners) == 0 {
		return nil, nil
	}

	nodes := map[string]int{}
	var out []types.EntityRecord
	nodeFor := func(name string, line int) int {
		if idx, ok := nodes[name]; ok {
			return idx
		}
		ent := makeEntity(name, "SCOPE.Schema", "type", file.Path, file.Language, line)
		setProps(&ent,
			"graphql_type", name,
			"framework", "absinthe",
			"code_first", "true",
			"structural_ref", extractor.BuildOperationStructuralRef("graphql", file.Path, name),
			"provenance", "INFERRED_FROM_CODEFIRST_GRAPHQL_OBJECTTYPE",
		)
		out = append(out, ent)
		nodes[name] = len(out) - 1
		return nodes[name]
	}

	seen := map[string]bool{}
	emit := func(owner, fieldName, target string, tc absintheCard) {
		if !known[target] || absintheScalars[target] {
			return
		}
		key := owner + "|" + fieldName + "|" + target
		if seen[key] {
			return
		}
		seen[key] = true
		ownerRef := extractor.BuildOperationStructuralRef("graphql", file.Path, owner)
		targetRef := extractor.BuildOperationStructuralRef("graphql", file.Path, target)
		props := map[string]string{
			"field_name":    fieldName,
			"list":          absintheBool(tc.list),
			"nullable":      absintheBool(tc.nullable),
			"cardinality":   absintheCardLabel(tc),
			"self_ref":      absintheBool(target == owner),
			"graphql_field": owner + "." + fieldName,
			"framework":     "absinthe",
			"provenance":    "INFERRED_FROM_CODEFIRST_GRAPHQL_FIELD",
		}
		if tc.list {
			props["item_nullable"] = absintheBool(tc.itemNullable)
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

	for _, ow := range owners {
		// Only object/interface/union/root carry output fields; enums/inputs are
		// not in `owners`. Interfaces/unions rarely own object-typed fields, but
		// an interface that declares fields is a legitimate owner.
		nodeFor(ow.name, ow.line)
		for _, fm := range reAbFieldDecl.FindAllStringSubmatch(ow.body, -1) {
			fieldName := fm[1]
			typeExpr := strings.TrimSpace(fm[2])
			base, tc := absintheParseTypeExpr(typeExpr)
			if base == "" {
				continue
			}
			emit(ow.name, fieldName, base, tc)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// --- cardinality model (mirrors internal/extractors/graphql/type_graph.go) ---

type absintheCard struct {
	list         bool
	nullable     bool
	itemNullable bool
}

func absintheBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func absintheCardLabel(tc absintheCard) string {
	if tc.list {
		return "to_many"
	}
	return "to_one"
}

var absintheAtomRe = regexp.MustCompile(`:([a-z_]\w*)`)

// absintheParseTypeExpr resolves an Absinthe field type expression into its base
// object-type atom and GraphQL cardinality.
//
//	:order                       -> base=order  (nullable to_one — Absinthe fields
//	                                default nullable)
//	non_null(:order)             -> base=order  nullable=false
//	list_of(:order)              -> base=order  list=true  item nullable
//	non_null(list_of(:order))    -> base=order  list=true  nullable=false (list)
//	list_of(non_null(:order))    -> base=order  list=true  item_nullable=false
//	non_null(list_of(non_null(:order)))
//	                             -> base=order  list=true  nullable=false item non-null
//
// Absinthe semantics: a bare `field :x, :t` is nullable; `non_null/1` removes
// nullability; `list_of/1` is a list (whose elements are nullable unless wrapped
// in non_null). The type expression may be followed by a `do` resolver block or
// `, resolve: ...` option keywords — only the leading type term is read.
//
// Returns base="" when no atom can be recovered (e.g. a `field :x, do: ...`
// virtual field, or a non-atom expression).
func absintheParseTypeExpr(expr string) (string, absintheCard) {
	tc := absintheCard{nullable: true} // Absinthe fields default to nullable.

	// Cut the type expression off before a trailing `do` resolver block or an
	// option-keyword tail (`, resolve: ...`, `, description: ...`). We only need
	// the leading wrapper(s) + the innermost atom; the first top-level atom is
	// the type reference, anything after the closing wrappers is options.
	e := strings.TrimSpace(expr)
	if i := strings.Index(e, " do"); i >= 0 {
		// Only cut if ` do` begins a block (followed by end/newline), not part of
		// an atom name. Atoms are lowercase identifiers, ` do` has a leading space.
		e = strings.TrimSpace(e[:i])
	}

	hasNonNull := strings.Contains(e, "non_null")
	hasList := strings.Contains(e, "list_of")

	// Locate the innermost atom (the referenced type).
	m := absintheAtomRe.FindStringSubmatch(e)
	if m == nil {
		return "", tc
	}
	base := m[1]

	if hasList {
		tc.list = true
		tc.nullable = true // a bare list_of(...) field is itself nullable.
		// Item nullability: list_of(non_null(:t)) -> item non-null; otherwise
		// the list elements are nullable.
		inner := e
		if li := strings.Index(inner, "list_of"); li >= 0 {
			inner = inner[li+len("list_of"):]
		}
		tc.itemNullable = !strings.Contains(inner, "non_null")
		// non_null(list_of(...)) -> the LIST is non-null (outer wrapper before
		// list_of).
		if hasNonNull {
			outer := e
			if li := strings.Index(outer, "list_of"); li >= 0 {
				outer = outer[:li]
			}
			if strings.Contains(outer, "non_null") {
				tc.nullable = false
			}
		}
	} else if hasNonNull {
		tc.nullable = false
	}

	return base, tc
}
