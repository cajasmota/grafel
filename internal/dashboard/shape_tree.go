// shape_tree.go — GET /api/v2/groups/{id}/shape
//
// Refs #1935 Phase 1 — ShapeTree subtree resolution.
//
// When a path-detail parameter or response carries a user-defined object
// type (e.g. `TransferRequest`, `LoginResponse`), the dashboard's
// ShapeTree component lazily fetches that type's field list via this
// endpoint and renders them as an indented expandable subtree. The
// frontend asks for one level at a time (`depth=1` is the default and
// the supported cap) and re-requests as the user drills further in.
//
// The resolver walks CONTAINS edges from the requested class entity to
// SCOPE.Schema/field children, parses each field's signature into
// `(type, annotations[])`, and reports whether each field's type
// itself resolves to a known class (which the frontend then renders
// as expandable). For unresolvable types (primitives, unknown DTOs,
// container types like List<X>) `has_children` is false so the row
// renders as a terminal leaf.
//
// The endpoint is mounted at the group level — types are resolved within
// the group's repo set. Cross-repo type references are supported.
package dashboard

import (
	"net/http"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// v2ShapeRow is one row in a ShapeTree response. Each row corresponds to
// a field of the requested type. `name`, `type`, `annotations`, and
// `nullable` come from the field entity's signature; `type_entity_id`
// + `has_children` indicate whether the frontend can drill further.
type v2ShapeRow struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Annotations  []string `json:"annotations,omitempty"`
	// Validations are per-field constraint chips (#4858) parsed from the
	// field entity's `validations` metadata property — e.g. for a NestJS DTO
	// field `@IsString() @MaxLength(120) @IsOptional() name?: string` this is
	// ["IsString","MaxLength:120","IsOptional"]. Empty for fields without
	// validation decorators.
	Validations  []string `json:"validations,omitempty"`
	Nullable     bool     `json:"nullable,omitempty"`
	TypeEntityID string   `json:"type_entity_id,omitempty"`
	HasChildren  bool     `json:"has_children"`
}

// v2ShapeResponse is the wire shape of GET /api/v2/groups/{id}/shape.
// Frontend uses TypeName + Subtype to render the subtree header
// (e.g. "record TransferRequest" vs "class LoginResponse").
type v2ShapeResponse struct {
	TypeEntityID string       `json:"type_entity_id"`
	TypeName     string       `json:"type_name"`
	Subtype      string       `json:"subtype,omitempty"`
	Rows         []v2ShapeRow `json:"rows"`
}

// shapeTreeDepthCap is the maximum traversal depth a single request may
// ask for. The frontend currently only ever requests depth=1 (lazy
// expansion), but the parameter is accepted so a future "expand all"
// affordance can request more without changing the wire contract.
const shapeTreeDepthCap = 3

// handleV2Shape — GET /api/v2/groups/{id}/shape?type_entity_id=<id>&depth=1
//
// Resolves the requested class entity, walks CONTAINS to its field
// children, and returns one ShapeRow per field with type + annotation
// metadata + a `has_children` flag the frontend uses to render the
// expander glyph.
func (s *Server) handleV2Shape(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeV2Err(w, http.StatusBadRequest, "group_required", "group id required")
		return
	}
	grp, err := s.graphs.GetGroup(id)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "group_not_found", err.Error())
		return
	}

	typeRef := strings.TrimSpace(r.URL.Query().Get("type_entity_id"))
	if typeRef == "" {
		// Accept `type` as a convenience alias: the frontend may receive
		// only the bare type name (e.g. "TransferRequest") from the
		// path-detail payload when the indexer could not embed the full
		// prefixed entity id. Resolve by name.
		typeRef = strings.TrimSpace(r.URL.Query().Get("type"))
	}
	if typeRef == "" {
		writeV2Err(w, http.StatusBadRequest, "type_required", "type_entity_id query param required")
		return
	}

	_, ent := findEntity(grp, typeRef)
	if ent == nil {
		// Fallback: scan all repos by short name (e.g. `TransferRequest`).
		ent = findClassEntityByName(grp, typeRef)
	}
	if ent == nil {
		writeV2Err(w, http.StatusNotFound, "type_not_found", "no class entity found for "+typeRef)
		return
	}

	// Locate the owning repo so we can walk CONTAINS edges from this
	// entity to its field children (Relationships live on Doc, indexed
	// by FromID).
	repoSlug, repo := findRepoForEntity(grp, ent.ID)
	if repo == nil {
		writeV2Err(w, http.StatusNotFound, "type_not_found", "type "+typeRef+" present in group but no owning repo")
		return
	}

	rows := collectShapeRows(grp, repo, ent)

	writeV2JSON(w, http.StatusOK, v2OK(v2ShapeResponse{
		TypeEntityID: dashPrefixedID(repoSlug, ent.ID),
		TypeName:     ent.Name,
		Subtype:      ent.Subtype,
		Rows:         rows,
	}))
}

// collectShapeRows returns one v2ShapeRow per CONTAINS field of the
// given class entity. Field rows include parsed annotations + type +
// nullable inference + a HasChildren flag so the frontend can render
// the expansion glyph.
func collectShapeRows(grp *DashGroup, repo *DashRepo, class *graph.Entity) []v2ShapeRow {
	var rows []v2ShapeRow
	seen := map[string]bool{}
	collectShapeRowsInto(grp, repo, class, &rows, seen, map[string]bool{})
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

// collectShapeRowsInto appends the CONTAINS field rows of `class` into rows,
// then recurses into the class's EXTENDS base(s) so inherited / mapped-type
// fields are projected onto the requested DTO (#4845). visited guards against
// inheritance cycles; seen de-dupes field names so a subclass field overrides
// (shadows) the inherited one. A child field with the same name as an inherited
// one wins because subclasses are walked before their bases.
func collectShapeRowsInto(grp *DashGroup, repo *DashRepo, class *graph.Entity, rows *[]v2ShapeRow, seen, visited map[string]bool) {
	if repo == nil || repo.Doc == nil || class == nil {
		return
	}
	if visited[class.ID] {
		return
	}
	visited[class.ID] = true

	// Map field entities by ID for the FromID=class.ID CONTAINS walk.
	byID := make(map[string]*graph.Entity, len(repo.Doc.Entities))
	for i := range repo.Doc.Entities {
		byID[repo.Doc.Entities[i].ID] = &repo.Doc.Entities[i]
	}

	for _, rel := range repo.Doc.Relationships {
		if rel.Kind != "CONTAINS" || rel.FromID != class.ID {
			continue
		}
		child, ok := byID[rel.ToID]
		if !ok {
			continue
		}
		if !isFieldEntity(child) {
			continue
		}
		row := buildShapeRow(grp, child)
		// De-dupe by field name (Lombok synthesis can emit duplicates;
		// a subclass field shadows the inherited one walked later).
		key := row.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		*rows = append(*rows, row)
	}

	// #4845 — recurse into EXTENDS base classes so a mapped-type DTO
	// (`extends PartialType(CreateThingBody)`) or a `extends BaseDto` DTO,
	// which owns no fields of its own, still renders the inherited field-set.
	for _, base := range extendsBaseEntities(grp, repo, class) {
		baseRepo := repo
		if base.ID != "" {
			if _, r := findRepoForEntity(grp, base.ID); r != nil {
				baseRepo = r
			}
		}
		collectShapeRowsInto(grp, baseRepo, base, rows, seen, visited)
	}
}

// extendsBaseEntities returns the resolved base-class entities a class
// EXTENDS, resolving each edge first by its (resolved) ToID entity, then by
// the bare base name carried in Properties["to"] (the JS/Java heritage edges
// keep the source/target names there). Returns only entities that look like a
// class/record/schema shape so EXTENDS edges to external framework interfaces
// contribute nothing. (#4845)
func extendsBaseEntities(g *DashGroup, repo *DashRepo, class *graph.Entity) []*graph.Entity {
	if repo == nil || repo.Doc == nil || class == nil {
		return nil
	}
	byID := make(map[string]*graph.Entity, len(repo.Doc.Entities))
	for i := range repo.Doc.Entities {
		byID[repo.Doc.Entities[i].ID] = &repo.Doc.Entities[i]
	}
	var out []*graph.Entity
	seen := map[string]bool{}
	for _, rel := range repo.Doc.Relationships {
		if !strings.EqualFold(rel.Kind, "EXTENDS") || rel.FromID != class.ID {
			continue
		}
		var base *graph.Entity
		if e, ok := byID[rel.ToID]; ok && e.ID != class.ID {
			base = e
		} else if name := relTargetName(rel); name != "" {
			base = findClassEntityByName(g, name)
		}
		if base == nil || base.ID == class.ID || seen[base.ID] {
			continue
		}
		seen[base.ID] = true
		out = append(out, base)
	}
	return out
}

// relTargetName returns the bare base/target name an EXTENDS edge carries in
// its Properties (the heritage extractors store it under "to"), or "".
func relTargetName(rel graph.Relationship) string {
	if rel.Properties == nil {
		return ""
	}
	if to := rel.Properties["to"]; to != "" {
		return to
	}
	return ""
}

// isFieldEntity reports whether the entity represents a class field
// the ShapeTree should render. Java/Python both emit `SCOPE.Schema`
// entities with `subtype="field"` for class fields.
func isFieldEntity(e *graph.Entity) bool {
	if e == nil {
		return false
	}
	if e.Kind == "SCOPE.Schema" && e.Subtype == "field" {
		return true
	}
	// Bare "Schema" kind without the SCOPE prefix is occasionally
	// emitted by older extractors; accept that shape too.
	if (e.Kind == "Schema" || dashStripScopePrefix(e.Kind) == "schema") &&
		e.Subtype == "field" {
		return true
	}
	return false
}

// buildShapeRow parses a field entity into the wire shape consumed by
// the frontend. The field signature is the canonical source of type +
// annotation info — Java field entities carry a signature of the form
// `[@Annotation ...] Type name`.
func buildShapeRow(grp *DashGroup, field *graph.Entity) v2ShapeRow {
	// Field entity Name is qualified "<Class>.<field>" — strip the prefix
	// so the row shows just the field token.
	name := field.Name
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	annotations, typ := parseFieldSignature(field.Signature, name)

	row := v2ShapeRow{
		Name:        name,
		Type:        typ,
		Annotations: annotations,
		Validations: fieldValidations(field),
		Nullable:    inferNullable(annotations, typ),
	}
	// Resolve the field's element type to a class entity so the frontend
	// can render the expansion glyph. Container element types (List<X>,
	// Map<K,V>, Optional<X>) are unwrapped to the inner reference type.
	resolveType := unwrapElementType(typ)
	if resolveType != "" {
		if target := findClassEntityByName(grp, resolveType); target != nil {
			tgtSlug, _ := findRepoForEntity(grp, target.ID)
			row.TypeEntityID = dashPrefixedID(tgtSlug, target.ID)
			row.HasChildren = true
		}
	}
	return row
}

// fieldValidations reads the per-field validation constraints stamped onto a
// field entity by the extractor (#4858) under Properties["validations"]
// (comma-joined). Returns nil when the field carries none. The list is rendered
// by the dashboard as small constraint chips next to the field type — e.g.
// ["IsString","MaxLength:120","IsOptional"] for a class-validator DTO field.
func fieldValidations(field *graph.Entity) []string {
	if field == nil || field.Properties == nil {
		return nil
	}
	raw := strings.TrimSpace(field.Properties["validations"])
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseFieldSignature splits a Java field signature like
// `@NotBlank @Min(0) BigDecimal qty` into ([@NotBlank, @Min(0)], "BigDecimal")
// using the field name as the right-anchor token.
func parseFieldSignature(sig, fieldName string) (annotations []string, typ string) {
	sig = strings.TrimSpace(sig)
	if sig == "" {
		return nil, ""
	}
	// Walk left-to-right collecting @Annotation tokens (with optional
	// (...) argument lists). Stop at the first non-annotation token —
	// the type starts there.
	i := 0
	for i < len(sig) {
		// Skip whitespace.
		for i < len(sig) && (sig[i] == ' ' || sig[i] == '\t') {
			i++
		}
		if i >= len(sig) || sig[i] != '@' {
			break
		}
		start := i
		i++
		for i < len(sig) && (isIdentChar(sig[i]) || sig[i] == '.') {
			i++
		}
		// Optional (...) — capture matched-paren depth.
		if i < len(sig) && sig[i] == '(' {
			depth := 1
			i++
			for i < len(sig) && depth > 0 {
				switch sig[i] {
				case '(':
					depth++
				case ')':
					depth--
				}
				i++
			}
		}
		annotations = append(annotations, strings.TrimSpace(sig[start:i]))
	}
	rest := strings.TrimSpace(sig[i:])
	// rest is "Type name" — strip the trailing name token, which equals
	// fieldName for well-formed sigs. If the sig lacks the field name
	// (some older extractor paths), keep the whole rest as the type.
	if fieldName != "" && strings.HasSuffix(rest, " "+fieldName) {
		typ = strings.TrimSpace(strings.TrimSuffix(rest, " "+fieldName))
	} else if fieldName != "" && rest == fieldName {
		typ = ""
	} else {
		// Heuristic: last whitespace-delimited token is the name.
		fields := strings.Fields(rest)
		if len(fields) >= 2 {
			typ = strings.Join(fields[:len(fields)-1], " ")
		} else {
			typ = rest
		}
	}
	return annotations, typ
}

// isIdentChar reports whether b is a valid Java identifier character.
func isIdentChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') || b == '_' || b == '$'
}

// inferNullable returns true when the type or annotation set indicates
// the field accepts null. `@Nullable` is the explicit signal; Optional<X>
// is the structural signal. `@NotNull`/`@NotBlank`/`@NotEmpty` are
// explicit non-null markers (returns false even if the type looks
// nullable). Primitive types (lowercase) are non-nullable in Java.
func inferNullable(annotations []string, typ string) bool {
	for _, a := range annotations {
		if strings.HasPrefix(a, "@NotNull") ||
			strings.HasPrefix(a, "@NotBlank") ||
			strings.HasPrefix(a, "@NotEmpty") {
			return false
		}
	}
	for _, a := range annotations {
		if strings.HasPrefix(a, "@Nullable") {
			return true
		}
	}
	if strings.HasPrefix(typ, "Optional<") {
		return true
	}
	// Lowercase primitive (byte/short/int/long/float/double/boolean/char).
	switch typ {
	case "byte", "short", "int", "long", "float", "double", "boolean", "char":
		return false
	}
	return false
}

// unwrapElementType returns the inner reference type from a Java
// container/wrapper generic. `List<Foo>` → `Foo`; `Map<String, Bar>` →
// `Bar`; `Optional<Foo>` → `Foo`. For bare reference types the input
// is returned unchanged. For container types where the element is
// primitive (e.g. `List<String>`) the inner token is returned but
// findClassEntityByName will not resolve it, so no expander renders.
func unwrapElementType(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	for _, w := range []string{
		"List", "Set", "Collection", "Iterable", "Optional", "Stream",
		"ArrayList", "LinkedList", "HashSet", "TreeSet",
	} {
		prefix := w + "<"
		if strings.HasPrefix(t, prefix) && strings.HasSuffix(t, ">") {
			return strings.TrimSpace(t[len(prefix) : len(t)-1])
		}
	}
	// Map<K,V> → return V (the value type, since maps typically carry
	// the user-defined payload as the value).
	if strings.HasPrefix(t, "Map<") && strings.HasSuffix(t, ">") {
		inner := t[len("Map<") : len(t)-1]
		parts := splitTopLevelComma(inner)
		if len(parts) == 2 {
			return strings.TrimSpace(parts[1])
		}
	}
	return t
}

// splitTopLevelComma splits on commas at generic-depth zero.
func splitTopLevelComma(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// findClassEntityByName scans the group's repos for a class-like model
// entity whose simple name matches `name`. The match is case-sensitive;
// the first hit (by sorted-repo iteration) wins. Returns nil when no
// model matches — primitives, JDK types, and container element types
// with no in-group DTO definition all fall through.
//
// Two entity shapes resolve:
//
//   - SCOPE.Component (class/interface/record/enum) — the OO class shape
//     emitted for Java/TS/etc. classes.
//   - SCOPE.Schema object/model nodes — the shape emitted for DTOs and
//     ORM/GraphQL models (NestJS response DTOs under dto/response/, Mongoose
//     @Schema classes, Prisma/Drizzle/Mongoose models, GraphQL types, …).
//     #4569: a `@Get() …(): Promise<ProposalCountsResponse>` handler emits a
//     RETURNS/response_type of `ProposalCountsResponse`, but that DTO is
//     indexed as kind SCOPE.Schema (not SCOPE.Component), so the Response row
//     resolved the NAME yet found no entity to expand and rendered "(none)".
//     Resolving the Schema model node lets the field-set (its SCOPE.Schema/
//     field CONTAINS children) render. Sub-field nodes (subtype
//     field/column/property) are excluded — only the object shape itself.
//
// Primitive / framework names are short-circuited so we do not pay
// the entity scan cost for unresolvable types.
func findClassEntityByName(g *DashGroup, name string) *graph.Entity {
	if name == "" || isJavaPrimitiveLikeName(name) {
		return nil
	}
	for _, r := range sortedRepos(g) {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			e := &r.Doc.Entities[i]
			if e.Name != name {
				continue
			}
			switch e.Kind {
			case "SCOPE.Component":
				switch e.Subtype {
				case "class", "interface", "record", "enum", "":
					return e
				}
			case "SCOPE.Schema":
				// A Schema object/model shape (DTO, ORM/GraphQL model) — its
				// fields render as the response/body field-set. Exclude the
				// per-field/column/property sub-nodes, which are children, not
				// the shape itself.
				switch e.Subtype {
				case "field", "column", "property":
					// sub-field node — not an object shape.
				default:
					return e
				}
			}
		}
	}
	return nil
}

// isJavaPrimitiveLikeName returns true for type tokens that the
// ShapeTree resolver will never find in the entity index (Java
// primitives, common JDK / Jakarta scalar types, etc.). Used as a
// cheap pre-filter to avoid an O(N) entity scan per field.
func isJavaPrimitiveLikeName(name string) bool {
	switch name {
	case "byte", "short", "int", "long", "float", "double",
		"boolean", "char", "void",
		"Byte", "Short", "Integer", "Long", "Float", "Double",
		"Boolean", "Character", "Number",
		"String", "CharSequence",
		"Object", "Class",
		"BigDecimal", "BigInteger",
		"Date", "Instant", "LocalDate", "LocalDateTime", "LocalTime",
		"OffsetDateTime", "ZonedDateTime", "Duration", "Period",
		"UUID", "URI", "URL":
		return true
	}
	return false
}

// classHasFieldChildren reports whether the class entity has at least
// one CONTAINS edge to a field child within its owning repo. Used by
// the path-detail handler to decide HasChildren on the request body /
// response shape row without making the frontend pay a probe request.
func classHasFieldChildren(g *DashGroup, class *graph.Entity) bool {
	return classHasFieldChildrenRec(g, class, map[string]bool{})
}

// classHasFieldChildrenRec is classHasFieldChildren with cycle protection,
// returning true if the class or any of its EXTENDS bases owns a field child
// (#4845: a mapped-type / `extends BaseDto` DTO inherits its fields).
func classHasFieldChildrenRec(g *DashGroup, class *graph.Entity, visited map[string]bool) bool {
	if class == nil || visited[class.ID] {
		return false
	}
	visited[class.ID] = true
	_, repo := findRepoForEntity(g, class.ID)
	if repo == nil || repo.Doc == nil {
		return false
	}
	byID := make(map[string]*graph.Entity, len(repo.Doc.Entities))
	for i := range repo.Doc.Entities {
		byID[repo.Doc.Entities[i].ID] = &repo.Doc.Entities[i]
	}
	for _, rel := range repo.Doc.Relationships {
		if rel.Kind != "CONTAINS" || rel.FromID != class.ID {
			continue
		}
		if child, ok := byID[rel.ToID]; ok && isFieldEntity(child) {
			return true
		}
	}
	for _, base := range extendsBaseEntities(g, repo, class) {
		if classHasFieldChildrenRec(g, base, visited) {
			return true
		}
	}
	return false
}

// findRepoForEntity returns the (slug, *DashRepo) owning the entity ID,
// or ("", nil) when no repo contains it.
func findRepoForEntity(g *DashGroup, entityID string) (string, *DashRepo) {
	for _, r := range sortedRepos(g) {
		if r.Doc == nil {
			continue
		}
		for i := range r.Doc.Entities {
			if r.Doc.Entities[i].ID == entityID {
				return r.Slug, r
			}
		}
	}
	return "", nil
}
