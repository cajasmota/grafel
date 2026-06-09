package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_mongoose", &mongooseExtractor{})
}

type mongooseExtractor struct{}

func (e *mongooseExtractor) Language() string { return "custom_js_mongoose" }

var (
	reMongooseSchemaAssign = regexp.MustCompile(
		`(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)(?:\s*:\s*\w+(?:<[^>]*>)?)?\s*=\s*new\s+(?:mongoose\.)?Schema\s*\(`,
	)
	reMongooseModelCall = regexp.MustCompile(
		`(?:mongoose\.)?model\s*(?:<[^>]*>)?\s*\(\s*['"]([A-Za-z0-9_]+)['"]\s*(?:,\s*([A-Za-z_][A-Za-z0-9_]*)\s*)?`,
	)
	reMongooseMiddleware = regexp.MustCompile(
		`([A-Za-z_][A-Za-z0-9_]*)\.(pre|post)\s*\(\s*['"]` +
			`(save|validate|remove|deleteOne|deleteMany|find|findOne|findOneAndDelete` +
			`|findOneAndUpdate|updateOne|updateMany|init|count|countDocuments` +
			`|estimatedDocumentCount|aggregate|insertMany)\s*['"]`,
	)
	reMongooseMiddlewareArray = regexp.MustCompile(
		`([A-Za-z_][A-Za-z0-9_]*)\.(pre|post)\s*\(\s*\[([^\]]+)\]`,
	)
	reMongooseVirtual = regexp.MustCompile(
		`([A-Za-z_][A-Za-z0-9_]*)\.virtual\s*\(\s*['"]([A-Za-z0-9_.]+)['"]`,
	)
	reMongoosePopulate = regexp.MustCompile(
		`\.populate\s*\(\s*['"]([A-Za-z0-9_.]+)['"]`,
	)
	reMongooseMethod = regexp.MustCompile(
		`([A-Za-z_][A-Za-z0-9_]*)\.methods\.([A-Za-z_][A-Za-z0-9_]*)\s*=`,
	)
	reMongooseStatic = regexp.MustCompile(
		`([A-Za-z_][A-Za-z0-9_]*)\.statics\.([A-Za-z_][A-Za-z0-9_]*)\s*=`,
	)
	reMongooseIndex = regexp.MustCompile(
		`([A-Za-z_][A-Za-z0-9_]*)\.index\s*\(\s*\{([^}]*)\}`,
	)

	// NestJS `@nestjs/mongoose` decorator style (issue #4328): `@Schema(...)
	// class Foo { @Prop(...) field: type; }`. mongoose.go previously only handled
	// the classic `new Schema()` form, so the entire decorator-based schema class
	// — and every @Prop field — was unextracted, orphaning the document shape.
	reMongooseSchemaClass = regexp.MustCompile(
		`@Schema\s*\([^@]*?\)\s*(?:export\s+)?(?:abstract\s+)?class\s+([A-Z][A-Za-z0-9_]*)`,
	)
	// @Prop(...) field: type;  — captures field name (group 1). The optional
	// decorator run handles companion decorators between @Prop and the property.
	reMongooseProp = regexp.MustCompile(
		`@Prop\s*\(([^@]*?)\)\s*(?:@\w+\s*(?:\([^@]*?\))?\s*)*([A-Za-z_]\w*)\s*[!?]?\s*:`,
	)
	// Thunk / array target type inside a @Prop options object:
	// `type: () => Foo`, `type: [Foo]`, `ref: 'Foo'`, `type: Foo`.
	reMongoosePropThunk = regexp.MustCompile(
		`(?:type\s*:\s*\(\s*\)\s*=>\s*([A-Z][A-Za-z0-9_]*))` +
			`|(?:type\s*:\s*\[\s*([A-Z][A-Za-z0-9_]*)\s*\])` +
			`|(?:ref\s*:\s*['"]([A-Z][A-Za-z0-9_]*)['"])`,
	)
)

// mongoosePropPrimitive is the set of mongoose @Prop primitive constructor
// targets that coerce a scalar rather than reference a document class.
var mongoosePropPrimitive = map[string]bool{
	"String": true, "Number": true, "Boolean": true, "Date": true,
	"Buffer": true, "Mixed": true, "ObjectId": true, "Map": true, "Array": true,
}

func (e *mongooseExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.mongoose_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "mongoose"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.Subtype)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Schema assignments
	schemaVars := make(map[string]bool)
	for _, m := range reMongooseSchemaAssign.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		schemaVars[varName] = true
		ent := makeEntity(varName, "SCOPE.Schema", "", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mongoose", "provenance", "INFERRED_FROM_MONGOOSE_SCHEMA")
		addEntity(ent)
	}

	// mongoose.model("Name", schemaVar)
	modelVarToSchema := make(map[string]string)
	for _, m := range reMongooseModelCall.FindAllStringSubmatchIndex(src, -1) {
		modelName := src[m[2]:m[3]]
		schemaVar := ""
		if m[4] >= 0 {
			schemaVar = src[m[4]:m[5]]
			modelVarToSchema[modelName] = schemaVar
		}
		ent := makeEntity(modelName, "SCOPE.Schema", "model", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mongoose", "schema_var", schemaVar,
			"provenance", "INFERRED_FROM_MONGOOSE_MODEL")
		addEntity(ent)
	}

	// Schema middleware (pre/post hooks)
	for _, m := range reMongooseMiddleware.FindAllStringSubmatchIndex(src, -1) {
		schemaVar := src[m[2]:m[3]]
		hookType := src[m[4]:m[5]]
		hookName := src[m[6]:m[7]]
		name := fmt.Sprintf("%s.%s(%s)", schemaVar, hookType, hookName)
		ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mongoose", "schema_var", schemaVar,
			"hook_type", hookType, "hook_name", hookName,
			"provenance", "INFERRED_FROM_MONGOOSE_MIDDLEWARE")
		addEntity(ent)
	}

	// Array-form hooks
	for _, m := range reMongooseMiddlewareArray.FindAllStringSubmatchIndex(src, -1) {
		schemaVar := src[m[2]:m[3]]
		hookType := src[m[4]:m[5]]
		hooks := src[m[6]:m[7]]
		name := fmt.Sprintf("%s.%s([%s])", schemaVar, hookType, strings.TrimSpace(hooks))
		ent := makeEntity(name, "SCOPE.Pattern", "middleware", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mongoose", "schema_var", schemaVar,
			"hook_type", hookType, "hooks", hooks,
			"provenance", "INFERRED_FROM_MONGOOSE_MIDDLEWARE")
		addEntity(ent)
	}

	// Virtual properties
	for _, m := range reMongooseVirtual.FindAllStringSubmatchIndex(src, -1) {
		schemaVar := src[m[2]:m[3]]
		virtualName := src[m[4]:m[5]]
		name := fmt.Sprintf("%s.virtual.%s", schemaVar, virtualName)
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mongoose", "schema_var", schemaVar, "virtual_name", virtualName,
			"provenance", "INFERRED_FROM_MONGOOSE_VIRTUAL")
		addEntity(ent)
	}

	// .populate() traversals
	for _, m := range reMongoosePopulate.FindAllStringSubmatchIndex(src, -1) {
		field := src[m[2]:m[3]]
		name := "populate:" + field
		ent := makeEntity(name, "SCOPE.Operation", "query", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mongoose", "field", field,
			"provenance", "INFERRED_FROM_MONGOOSE_POPULATE")
		addEntity(ent)
	}

	// Instance methods
	for _, m := range reMongooseMethod.FindAllStringSubmatchIndex(src, -1) {
		schemaVar := src[m[2]:m[3]]
		methodName := src[m[4]:m[5]]
		name := fmt.Sprintf("%s.methods.%s", schemaVar, methodName)
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mongoose", "schema_var", schemaVar, "method_name", methodName,
			"provenance", "INFERRED_FROM_MONGOOSE_METHOD")
		addEntity(ent)
	}

	// Static methods
	for _, m := range reMongooseStatic.FindAllStringSubmatchIndex(src, -1) {
		schemaVar := src[m[2]:m[3]]
		staticName := src[m[4]:m[5]]
		name := fmt.Sprintf("%s.statics.%s", schemaVar, staticName)
		ent := makeEntity(name, "SCOPE.Operation", "function", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mongoose", "schema_var", schemaVar, "static_name", staticName,
			"provenance", "INFERRED_FROM_MONGOOSE_STATIC")
		addEntity(ent)
	}

	// Index definitions
	for _, m := range reMongooseIndex.FindAllStringSubmatchIndex(src, -1) {
		schemaVar := src[m[2]:m[3]]
		indexFields := strings.TrimSpace(src[m[4]:m[5]])
		name := fmt.Sprintf("%s.index:{%s}", schemaVar, indexFields)
		ent := makeEntity(name, "SCOPE.Pattern", "index", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "mongoose", "schema_var", schemaVar, "fields", indexFields,
			"provenance", "INFERRED_FROM_MONGOOSE_INDEX")
		addEntity(ent)
	}

	// NestJS @Schema()/@Prop() decorator-style schemas (issue #4328). Each
	// @Schema class becomes a SCOPE.Schema model; each @Prop field is emitted as
	// a member entity with a CONTAINS edge from the owning schema class, and a
	// `type: () => X` / `type: [X]` / `ref: 'X'` thunk yields a REFERENCES edge
	// to the target document class so embedded/referenced docs are not orphans.
	schemaClassMatches := reMongooseSchemaClass.FindAllStringSubmatchIndex(src, -1)
	for i, m := range schemaClassMatches {
		className := src[m[2]:m[3]]
		classEnt := makeEntity(className, "SCOPE.Schema", "schema_class", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&classEnt, "framework", "mongoose", "provenance", "INFERRED_FROM_NESTJS_MONGOOSE_SCHEMA")

		// Class body: from this @Schema to the next @Schema class (or EOF).
		bodyEnd := len(src)
		if i+1 < len(schemaClassMatches) {
			bodyEnd = schemaClassMatches[i+1][0]
		}
		body := src[m[0]:bodyEnd]

		classIdx := len(entities)
		addEntity(classEnt)
		// addEntity may have skipped a duplicate class name; only attach members
		// when this class was actually appended at classIdx.
		appended := classIdx < len(entities) && entities[classIdx].Name == className

		for _, pm := range reMongooseProp.FindAllStringSubmatchIndex(body, -1) {
			optsBlob := body[pm[2]:pm[3]]
			fieldName := body[pm[4]:pm[5]]
			propEnt := makeEntity(fmt.Sprintf("%s.%s", className, fieldName),
				"SCOPE.Component", "prop", file.Path, file.Language, lineOf(src, m[0]+pm[0]))
			setProps(&propEnt, "framework", "mongoose", "field_name", fieldName,
				"owner_class", className, "provenance", "INFERRED_FROM_NESTJS_MONGOOSE_PROP")

			// Thunk / array / ref target document class.
			if tm := reMongoosePropThunk.FindStringSubmatch(optsBlob); tm != nil {
				target := tm[1]
				if target == "" {
					target = tm[2]
				}
				if target == "" {
					target = tm[3]
				}
				if target != "" && !mongoosePropPrimitive[target] {
					setProps(&propEnt, "target_type", target)
					propEnt.Relationships = append(propEnt.Relationships,
						referencesClassEdge(propEnt.ID, target, "mongoose", fieldName))
				}
			}

			if appended {
				entities[classIdx].Relationships = append(entities[classIdx].Relationships,
					containsFieldEdge(className, propEnt.ID, fieldName, "mongoose"))
			}
			addEntity(propEnt)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
