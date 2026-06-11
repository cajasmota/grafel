package java

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
	sitter "github.com/smacker/go-tree-sitter"
)

// nosql_model.go — Spring Data NoSQL schema/model extraction (#4283, follow-up
// from #4271).
//
// Spring Data's mapping annotations are simultaneously the query-attribution
// signal (handled in internal/engine/orm_queries_java_mongo_agg.go) AND the
// schema/model definition. This pass emits the model side: a SCOPE.Schema
// model entity (Subtype "schema") per annotated class, mirroring the existing
// ORM model_extraction shape (the Ecto/Django/SQLAlchemy SCOPE.Schema "schema"
// node) and reusing the field-membership model (#4365/#4367): the model
// CONTAINS each SCOPE.Schema/field child the Java extractor already emits for
// the class's `field_declaration`s.
//
// Stores recognised (each by its class-level mapping annotation):
//
//   - Spring Data MongoDB:   @Document(collection="users")  → store "mongodb",
//     collection name carried as the `collection` property.
//   - Spring Data Cassandra: @Table("users")                → store "cassandra",
//     table name carried as the `table` property.
//   - Spring Data Redis:     @RedisHash("users")            → store "redis",
//     keyspace name carried as the `keyspace` property.
//
// The model entity Name is the class's simple name (so it lines up with the
// query-topology `Class:<Entity>` attribution and the dashboard ShapeTree),
// and its Subtype is "schema" — the same Kind/Subtype every other ORM
// model_extraction emitter uses. A plain non-annotated class emits no model
// entity (it stays a bare SCOPE.Component).
//
// Field overrides are carried on the per-field SCOPE.Schema/field signature
// the base extractor already emits (@Field("e") String email → the raw
// declaration with its annotation is preserved in the field signature). The
// model entity additionally records, in its Properties, the storage-name
// override + @Id/@Indexed flags per field so consumers don't have to re-parse
// the source (keys "field.<name>.column", "field.<name>.id",
// "field.<name>.indexed").

// nosqlStore identifies which Spring Data store a class maps to.
type nosqlStore struct {
	store    string // "mongodb" | "cassandra" | "redis"
	nameKey  string // property key for the collection/table/keyspace name
	collName string // the resolved collection/table/keyspace literal (may be "")
}

var (
	// @Document, @Document("c"), @Document(collection="c"),
	// @Document(indexName="c") (spring-data-elastic shares @Document).
	reDocumentAnn = regexp.MustCompile(`@Document\b\s*(?:\(([^)]*)\))?`)
	// @Table, @Table("t"), @Table(value="t"), @Table(name="t").
	reTableAnn = regexp.MustCompile(`@Table\b\s*(?:\(([^)]*)\))?`)
	// @RedisHash, @RedisHash("k"), @RedisHash(value="k"), @RedisHash(timeToLive=..).
	reRedisHashAnn = regexp.MustCompile(`@RedisHash\b\s*(?:\(([^)]*)\))?`)
	// JPA @Entity marker — disambiguates JPA @Table from Cassandra @Table.
	reEntityAnn = regexp.MustCompile(`@Entity\b`)

	// Inside the annotation arg list: a `key = "value"` named attribute.
	reNamedAttr = regexp.MustCompile(`(\w+)\s*=\s*"([^"]*)"`)
	// Inside the annotation arg list: a bare leading `"value"` positional.
	rePositionalStr = regexp.MustCompile(`^\s*"([^"]*)"`)

	// Per-field storage-name overrides: @Field("e") / @Field(name="e") /
	// @Field(value="e"), and the Cassandra @Column equivalents.
	reFieldAnn  = regexp.MustCompile(`@Field\b\s*(?:\(([^)]*)\))?`)
	reColumnAnn = regexp.MustCompile(`@Column\b\s*(?:\(([^)]*)\))?`)
)

// detectNoSQLStore inspects a class declaration header (annotations +
// declaration tokens, excluding the body) and returns the Spring Data store it
// maps to, or ok=false when the class carries none of the recognised mapping
// annotations.
//
// Cassandra's @Table and JPA's @Table share a spelling; this pass treats any
// class-level @Table as a Cassandra table model. JPA @Entity classes are not
// claimed here (they have no @Table-less mapping idiom recognised by this
// pass), keeping the NoSQL model_extraction credit honest to Spring Data.
func detectNoSQLStore(classDeclSrc string) (nosqlStore, bool) {
	if m := reDocumentAnn.FindStringSubmatch(classDeclSrc); m != nil {
		return nosqlStore{
			store:    "mongodb",
			nameKey:  "collection",
			collName: annotationName(m[1], "collection", "indexName"),
		}, true
	}
	if m := reTableAnn.FindStringSubmatch(classDeclSrc); m != nil {
		// Cassandra @Table stands alone; JPA's @Table is paired with @Entity
		// (a relational model owned by the jpa/hibernate model_extraction
		// arm, not this NoSQL pass). Only claim @Table classes that are not
		// JPA entities so we don't mis-credit relational tables as NoSQL.
		if !reEntityAnn.MatchString(classDeclSrc) {
			return nosqlStore{
				store:    "cassandra",
				nameKey:  "table",
				collName: annotationName(m[1], "value", "name"),
			}, true
		}
	}
	if m := reRedisHashAnn.FindStringSubmatch(classDeclSrc); m != nil {
		return nosqlStore{
			store:    "redis",
			nameKey:  "keyspace",
			collName: annotationName(m[1], "value"),
		}, true
	}
	return nosqlStore{}, false
}

// annotationName resolves the collection/table/keyspace literal from an
// annotation's argument list. A bare positional string (@Document("users"))
// wins; otherwise the first matching named attribute key (e.g. "collection",
// "value", "name") is used. Returns "" when the name is dynamic/absent.
func annotationName(args string, namedKeys ...string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	if m := rePositionalStr.FindStringSubmatch(args); m != nil {
		return m[1]
	}
	for _, m := range reNamedAttr.FindAllStringSubmatch(args, -1) {
		for _, k := range namedKeys {
			if m[1] == k {
				return m[2]
			}
		}
	}
	return ""
}

// emitNoSQLModel emits a SCOPE.Schema model entity for an annotated Spring Data
// class plus CONTAINS edges to the field children the base extractor already
// emitted into the [fieldStart, fieldEnd) region of *out. It returns the model
// entity index in *out (or -1 when the class is not an annotated NoSQL model).
//
// fieldStart/fieldEnd bound the slice region holding this class's directly
// emitted children (operations + SCOPE.Schema/field entities); only the
// SCOPE.Schema/field entries whose Name is "<Class>.<field>" are wired.
func emitNoSQLModel(
	node *sitter.Node,
	file extractor.FileInput,
	className, classDeclSrc, classBodySrc, pkgName string,
	fieldStart, fieldEnd int,
	out *[]types.EntityRecord,
) int {
	store, ok := detectNoSQLStore(classDeclSrc)
	if !ok || className == "" {
		return -1
	}

	qn := className
	if pkgName != "" {
		qn = pkgName + "." + className
	}

	props := map[string]string{
		"store": store.store,
	}
	if store.collName != "" {
		props[store.nameKey] = store.collName
	}

	// Per-field storage-name override + @Id/@Indexed flags, scanned from the
	// class body so consumers don't re-parse source.
	for name, fp := range scanNoSQLFields(classBodySrc) {
		if fp.column != "" {
			props["field."+name+".column"] = fp.column
		}
		if fp.isID {
			props["field."+name+".id"] = "true"
		}
		if fp.indexed {
			props["field."+name+".indexed"] = "true"
		}
	}

	model := types.EntityRecord{
		Name:          className,
		QualifiedName: qn,
		Kind:          "SCOPE.Schema",
		Subtype:       "schema",
		SourceFile:    file.Path,
		Language:      "java",
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		Signature:     "@" + storeAnnotation(store.store) + " " + className,
		Properties:    props,
	}

	modelIdx := len(*out)
	*out = append(*out, model)

	// Wire CONTAINS edges from the model to each field child already emitted
	// for this class, reusing the same Format A schema-field structural ref
	// the class→field membership model uses (#690).
	prefix := className + "."
	for i := fieldStart; i < fieldEnd && i < len(*out); i++ {
		c := (*out)[i]
		if c.Kind == "SCOPE.Schema" && c.Subtype == "field" && strings.HasPrefix(c.Name, prefix) {
			(*out)[modelIdx].Relationships = append((*out)[modelIdx].Relationships,
				types.RelationshipRecord{
					ToID: extractor.BuildSchemaFieldStructuralRef("java", file.Path, c.Name),
					Kind: "CONTAINS",
				})
		}
	}
	return modelIdx
}

// storeAnnotation returns the canonical class-level annotation spelling for a
// store, used only to build a readable model signature.
func storeAnnotation(store string) string {
	switch store {
	case "mongodb":
		return "Document"
	case "cassandra":
		return "Table"
	case "redis":
		return "RedisHash"
	default:
		return "Document"
	}
}

// nosqlFieldProps captures per-field NoSQL mapping metadata.
type nosqlFieldProps struct {
	column  string // @Field("e") / @Column("e") storage-name override
	isID    bool   // @Id present
	indexed bool   // @Indexed present
}

// fieldDeclLineRe matches a Java field declaration's trailing `<name>;` (or
// `<name> = ...;`) so we can key per-field metadata by the declared name.
var fieldDeclLineRe = regexp.MustCompile(`(\w+)\s*(?:=[^;]*)?;`)

// scanNoSQLFields walks the class body line-buffer and associates pending
// field annotations (@Field/@Column storage-name override, @Id, @Indexed) with
// the next field declaration. This is a lightweight line scanner — it does not
// need the AST because the base extractor already emits the field entities; we
// only enrich the model's Properties.
func scanNoSQLFields(classBodySrc string) map[string]nosqlFieldProps {
	out := map[string]nosqlFieldProps{}
	var pending nosqlFieldProps
	hasPending := false

	flushInto := func(name string) {
		if name == "" {
			return
		}
		if hasPending {
			out[name] = pending
		}
		pending = nosqlFieldProps{}
		hasPending = false
	}

	for _, raw := range strings.Split(classBodySrc, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		annotationOnly := strings.HasPrefix(line, "@")

		// Accumulate annotations (they may sit on their own line or be
		// inlined before the field type).
		if strings.Contains(line, "@Id") {
			pending.isID = true
			hasPending = true
		}
		if strings.Contains(line, "@Indexed") {
			pending.indexed = true
			hasPending = true
		}
		if m := reFieldAnn.FindStringSubmatch(line); m != nil {
			if n := annotationName(m[1], "name", "value"); n != "" {
				pending.column = n
				hasPending = true
			}
		}
		if m := reColumnAnn.FindStringSubmatch(line); m != nil {
			if n := annotationName(m[1], "value", "name"); n != "" {
				pending.column = n
				hasPending = true
			}
		}

		if annotationOnly {
			continue
		}

		// A declaration line: bind any pending annotations to this field name.
		// Skip method declarations (contain a '(' before the ';').
		if strings.Contains(line, "(") {
			// Reset pending — method, not a field.
			pending = nosqlFieldProps{}
			hasPending = false
			continue
		}
		if m := fieldDeclLineRe.FindStringSubmatch(line); m != nil {
			flushInto(m[1])
		}
	}
	return out
}
