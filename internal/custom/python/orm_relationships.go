package python

// orm_relationships.go — relationship/FK extraction for Peewee, Pony ORM,
// Beanie, MongoEngine, and Tortoise ORM.
//
// Issue #3070 — ORM relationship extractor for peewee/pony/beanie/mongoengine/tortoise.
// Pattern: mirrors saRelationshipRe / saForeignKeyRe from sqlalchemy.go.

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
	extractor.Register("python_peewee_rel", &PeeweeRelExtractor{})
	extractor.Register("python_pony_rel", &PonyRelExtractor{})
	extractor.Register("python_beanie_rel", &BeanieRelExtractor{})
	extractor.Register("python_mongoengine_rel", &MongoEngineRelExtractor{})
	extractor.Register("python_tortoise_rel", &TortoiseRelExtractor{})
}

// ============================================================================
// Peewee
// ============================================================================

// PeeweeRelExtractor extracts ForeignKeyField and ManyToManyField relationships
// from Peewee ORM model classes.
type PeeweeRelExtractor struct{}

func (e *PeeweeRelExtractor) Language() string { return "python_peewee_rel" }

var (
	// peeweeModelRe matches class definitions that extend a Peewee Model.
	peeweeModelRe = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*:`)

	// peeweeFKFieldRe matches:  attr = ForeignKeyField(TargetModel, ...)
	peeweeFKFieldRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*=\s*ForeignKeyField\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)

	// peeweeMTMFieldRe matches: attr = ManyToManyField(TargetModel, ...)
	peeweeMTMFieldRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*=\s*ManyToManyField\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)

	// peeweeBaseIndicators are common Peewee base class names.
	peeweeBaseIndicators = []string{"Model", "peewee.Model", "pw.Model"}
)

func isPeeweeModel(bases string) bool {
	for _, ind := range peeweeBaseIndicators {
		if strings.Contains(bases, ind) {
			return true
		}
	}
	return false
}

func (e *PeeweeRelExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_peewee_rel")
	_, span := tracer.Start(ctx, "custom.python_peewee_rel")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	// Gate: file must import/reference peewee
	source := string(file.Content)
	if !strings.Contains(source, "peewee") {
		return nil, nil
	}

	var out []types.EntityRecord

	for _, idx := range allMatchesIndex(peeweeModelRe, source) {
		className := source[idx[2]:idx[3]]
		bases := source[idx[4]:idx[5]]
		if !isPeeweeModel(bases) {
			continue
		}

		classLine := lineOf(source, idx[0])
		body := extractClassBody(source, idx[0])

		// ForeignKeyField → foreign_key / relationship
		for _, fkIdx := range allMatchesIndex(peeweeFKFieldRe, body) {
			attr := body[fkIdx[2]:fkIdx[3]]
			target := body[fkIdx[4]:fkIdx[5]]
			relLine := classLine + strings.Count(body[:fkIdx[0]], "\n")
			props := map[string]string{
				"framework":    "peewee",
				"pattern_type": "foreign_key",
				"target_model": target,
				"parent_class": className,
			}
			out = append(out, entity(className+"."+attr, "SCOPE.Schema", "", file.Path, relLine, props))
		}

		// ManyToManyField → association / relationship
		for _, mtmIdx := range allMatchesIndex(peeweeMTMFieldRe, body) {
			attr := body[mtmIdx[2]:mtmIdx[3]]
			target := body[mtmIdx[4]:mtmIdx[5]]
			relLine := classLine + strings.Count(body[:mtmIdx[0]], "\n")
			props := map[string]string{
				"framework":    "peewee",
				"pattern_type": "many_to_many",
				"target_model": target,
				"parent_class": className,
			}
			out = append(out, entity(className+"."+attr, "SCOPE.Schema", "", file.Path, relLine, props))
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// ============================================================================
// Pony ORM
// ============================================================================

// PonyRelExtractor extracts Required/Optional/Set relationships that reference
// other entities in Pony ORM model classes.
type PonyRelExtractor struct{}

func (e *PonyRelExtractor) Language() string { return "python_pony_rel" }

var (
	// ponyEntityRe matches class definitions that extend db.Entity or Entity.
	ponyEntityRe = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*:`)

	// ponyRelRe matches Required/Optional/Set fields that reference another entity.
	// Handles both unquoted class refs (Required(Department)) and quoted string refs
	// (Optional("Employee"), Set("Project")).  The three capture groups after (2) are:
	//   group 3: double-quoted ref  group 4: single-quoted ref  group 5: bare class ref
	ponyRelRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*=\s*(Required|Optional|Set)\s*\(\s*(?:"([A-Za-z_][A-Za-z0-9_]*)"|'([A-Za-z_][A-Za-z0-9_]*)'|([A-Z][A-Za-z0-9_]*))`)

	// ponyBaseIndicators are common Pony ORM base class names.
	ponyBaseIndicators = []string{"db.Entity", "Entity"}
)

func isPonyEntity(bases string) bool {
	for _, ind := range ponyBaseIndicators {
		if strings.Contains(bases, ind) {
			return true
		}
	}
	return false
}

func (e *PonyRelExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_pony_rel")
	_, span := tracer.Start(ctx, "custom.python_pony_rel")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	if !strings.Contains(source, "pony") && !strings.Contains(source, "db.Entity") {
		return nil, nil
	}

	var out []types.EntityRecord

	for _, idx := range allMatchesIndex(ponyEntityRe, source) {
		className := source[idx[2]:idx[3]]
		bases := source[idx[4]:idx[5]]
		if !isPonyEntity(bases) {
			continue
		}

		classLine := lineOf(source, idx[0])
		body := extractClassBody(source, idx[0])

		for _, rIdx := range allMatchesIndex(ponyRelRe, body) {
			attr := body[rIdx[2]:rIdx[3]]
			relKind := body[rIdx[4]:rIdx[5]] // Required | Optional | Set
			// target may come from group 3 (double-quoted), 4 (single-quoted), or 5 (bare class)
			target := ""
			for _, pair := range [][2]int{{rIdx[6], rIdx[7]}, {rIdx[8], rIdx[9]}, {rIdx[10], rIdx[11]}} {
				if pair[0] >= 0 {
					target = body[pair[0]:pair[1]]
					break
				}
			}
			if target == "" {
				continue
			}
			relLine := classLine + strings.Count(body[:rIdx[0]], "\n")

			patternType := "relationship"
			if relKind == "Set" {
				patternType = "many_to_many"
			}

			props := map[string]string{
				"framework":    "pony",
				"pattern_type": patternType,
				"rel_kind":     relKind,
				"target_model": target,
				"parent_class": className,
			}
			out = append(out, entity(className+"."+attr, "SCOPE.Schema", "", file.Path, relLine, props))
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// ============================================================================
// Beanie
// ============================================================================

// BeanieRelExtractor extracts Link[Model] type-hint relationships from Beanie
// ODM document classes (async MongoDB).
type BeanieRelExtractor struct{}

func (e *BeanieRelExtractor) Language() string { return "python_beanie_rel" }

var (
	// beanieDocRe matches class definitions that extend Document.
	beanieDocRe = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*:`)

	// beanieLinkRe matches: attr: Link[Model], List[Link[Model]], Optional[Link[Model]],
	// or List[Link["Model"]] (quoted forward-reference form).
	beanieLinkRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*:\s*(?:(?:Optional|List)\s*\[\s*)*Link\s*\[\s*"?([A-Za-z_][A-Za-z0-9_]*)"?`)

	// beanieBackLinkRe matches: attr: BackLink[TargetModel]
	beanieBackLinkRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*:\s*BackLink\s*\[\s*([A-Za-z_][A-Za-z0-9_]*)`)

	// beanieFetchLinksRe detects use of fetch_links=True (lazy_loading_recognition)
	beanieFetchLinksRe = regexp.MustCompile(`fetch_links\s*=\s*True`)

	// beanieBaseIndicators are common Beanie base class names.
	beanieBaseIndicators = []string{"Document", "beanie.Document"}
)

func isBeanieDocument(bases string) bool {
	for _, ind := range beanieBaseIndicators {
		if strings.Contains(bases, ind) {
			return true
		}
	}
	return false
}

func (e *BeanieRelExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_beanie_rel")
	_, span := tracer.Start(ctx, "custom.python_beanie_rel")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	if !strings.Contains(source, "beanie") && !strings.Contains(source, "Link[") {
		return nil, nil
	}

	var out []types.EntityRecord

	// Detect fetch_links=True for lazy_loading_recognition at file level
	hasFetchLinks := beanieFetchLinksRe.MatchString(source)

	for _, idx := range allMatchesIndex(beanieDocRe, source) {
		className := source[idx[2]:idx[3]]
		bases := source[idx[4]:idx[5]]
		if !isBeanieDocument(bases) {
			continue
		}

		classLine := lineOf(source, idx[0])
		body := extractClassBody(source, idx[0])

		// Link[Model] fields
		for _, lIdx := range allMatchesIndex(beanieLinkRe, body) {
			attr := body[lIdx[2]:lIdx[3]]
			target := body[lIdx[4]:lIdx[5]]
			relLine := classLine + strings.Count(body[:lIdx[0]], "\n")
			props := map[string]string{
				"framework":    "beanie",
				"pattern_type": "link",
				"target_model": target,
				"parent_class": className,
			}
			if hasFetchLinks {
				props["lazy_loading"] = "fetch_links"
			}
			out = append(out, entity(className+"."+attr, "SCOPE.Schema", "", file.Path, relLine, props))
		}

		// BackLink[Model] fields
		for _, blIdx := range allMatchesIndex(beanieBackLinkRe, body) {
			attr := body[blIdx[2]:blIdx[3]]
			target := body[blIdx[4]:blIdx[5]]
			relLine := classLine + strings.Count(body[:blIdx[0]], "\n")
			props := map[string]string{
				"framework":    "beanie",
				"pattern_type": "back_link",
				"target_model": target,
				"parent_class": className,
			}
			out = append(out, entity(className+"."+attr, "SCOPE.Schema", "", file.Path, relLine, props))
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// ============================================================================
// MongoEngine
// ============================================================================

// MongoEngineRelExtractor extracts ReferenceField and EmbeddedDocumentField
// relationships from MongoEngine document classes.
//
// Note: MongoEngine is a MongoDB ODM. "foreign key" as an RDBMS concept is
// not applicable — ReferenceField is the closest analog (a DBRef/ObjectId
// pointer) but it is NOT a SQL FK with integrity constraints. We record
// association_extraction and relationship_extraction as partial, and treat
// foreign_key_extraction as not_applicable for MongoDB-based ORMs.
type MongoEngineRelExtractor struct{}

func (e *MongoEngineRelExtractor) Language() string { return "python_mongoengine_rel" }

var (
	// meDocRe matches class definitions that extend Document or EmbeddedDocument.
	meDocRe = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*:`)

	// meReferenceFieldRe matches: attr = ReferenceField(TargetModel, ...)
	meReferenceFieldRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*=\s*ReferenceField\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)

	// meEmbeddedFieldRe matches: attr = EmbeddedDocumentField(TargetDoc, ...)
	meEmbeddedFieldRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*=\s*EmbeddedDocumentField\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)

	// meEmbeddedListFieldRe matches: attr = EmbeddedDocumentListField(TargetDoc, ...)
	meEmbeddedListFieldRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*=\s*EmbeddedDocumentListField\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)

	// meLazyReferenceRe matches: attr = LazyReferenceField(TargetModel, ...)
	meLazyReferenceRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*=\s*LazyReferenceField\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)`)

	// meBaseIndicators are common MongoEngine base class names.
	meBaseIndicators = []string{"Document", "EmbeddedDocument", "mongoengine.Document", "me.Document"}
)

func isMongoEngineDoc(bases string) bool {
	for _, ind := range meBaseIndicators {
		if strings.Contains(bases, ind) {
			return true
		}
	}
	return false
}

func (e *MongoEngineRelExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_mongoengine_rel")
	_, span := tracer.Start(ctx, "custom.python_mongoengine_rel")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	if !strings.Contains(source, "mongoengine") && !strings.Contains(source, "ReferenceField") {
		return nil, nil
	}

	var out []types.EntityRecord

	type fieldPattern struct {
		re          *regexp.Regexp
		patternType string
		isLazy      bool
	}
	fieldPatterns := []fieldPattern{
		{meReferenceFieldRe, "reference", false},
		{meEmbeddedFieldRe, "embedded", false},
		{meEmbeddedListFieldRe, "embedded_list", false},
		{meLazyReferenceRe, "lazy_reference", true},
	}

	for _, idx := range allMatchesIndex(meDocRe, source) {
		className := source[idx[2]:idx[3]]
		bases := source[idx[4]:idx[5]]
		if !isMongoEngineDoc(bases) {
			continue
		}

		classLine := lineOf(source, idx[0])
		body := extractClassBody(source, idx[0])

		for _, fp := range fieldPatterns {
			for _, fIdx := range allMatchesIndex(fp.re, body) {
				attr := body[fIdx[2]:fIdx[3]]
				target := body[fIdx[4]:fIdx[5]]
				relLine := classLine + strings.Count(body[:fIdx[0]], "\n")
				props := map[string]string{
					"framework":    "mongoengine",
					"pattern_type": fp.patternType,
					"target_model": target,
					"parent_class": className,
				}
				if fp.isLazy {
					props["lazy_loading"] = "lazy_reference"
				}
				out = append(out, entity(className+"."+attr, "SCOPE.Schema", "", file.Path, relLine, props))
			}
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

// ============================================================================
// Tortoise ORM
// ============================================================================

// TortoiseRelExtractor extracts fields.ForeignKeyField and
// fields.ManyToManyField relationships from Tortoise ORM model classes.
type TortoiseRelExtractor struct{}

func (e *TortoiseRelExtractor) Language() string { return "python_tortoise_rel" }

var (
	// tortoiseModelRe matches class definitions that extend tortoise Model.
	tortoiseModelRe = regexp.MustCompile(`(?m)^class\s+([A-Z][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*:`)

	// tortoiseFKRe matches:
	//   attr = fields.ForeignKeyField("app.Model", ...)
	//   attr: fields.ForeignKeyRelation[Model]  (type annotation form)
	tortoiseFKRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*(?:=\s*fields\.ForeignKeyField\s*\(\s*["']([^"']+)["']|:\s*fields\.ForeignKeyRelation\s*\[\s*([A-Za-z_][A-Za-z0-9_]*))`)

	// tortoiseMTMRe matches:
	//   attr = fields.ManyToManyField("app.Model", ...)
	//   attr: fields.ManyToManyRelation[Model]
	tortoiseMTMRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*(?:=\s*fields\.ManyToManyField\s*\(\s*["']([^"']+)["']|:\s*fields\.ManyToManyRelation\s*\[\s*([A-Za-z_][A-Za-z0-9_]*))`)

	// tortoiseO2OFieldRe matches: attr = fields.OneToOneField("app.Model", ...)
	tortoiseO2OFieldRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*=\s*fields\.OneToOneField\s*\(\s*["']([^"']+)["']`)

	// tortoiseBackRefRe matches: attr: fields.ReverseRelation[Model] or ["Model"]
	tortoiseBackRefRe = regexp.MustCompile(
		`(?m)^\s+(\w+)\s*:\s*fields\.ReverseRelation\s*\[\s*"?([A-Za-z_][A-Za-z0-9_]*)"?`)

	// tortoiseBaseIndicators are common Tortoise ORM base class names.
	tortoiseBaseIndicators = []string{"Model", "tortoise.Model", "tortoise.models.Model"}
)

func isTortoiseModel(bases string) bool {
	for _, ind := range tortoiseBaseIndicators {
		if strings.Contains(bases, ind) {
			return true
		}
	}
	return false
}

func (e *TortoiseRelExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_tortoise_rel")
	_, span := tracer.Start(ctx, "custom.python_tortoise_rel")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	if !strings.Contains(source, "tortoise") && !strings.Contains(source, "fields.ForeignKeyField") {
		return nil, nil
	}

	var out []types.EntityRecord

	for _, idx := range allMatchesIndex(tortoiseModelRe, source) {
		className := source[idx[2]:idx[3]]
		bases := source[idx[4]:idx[5]]
		if !isTortoiseModel(bases) {
			continue
		}

		classLine := lineOf(source, idx[0])
		body := extractClassBody(source, idx[0])

		// ForeignKeyField / ForeignKeyRelation
		for _, fkIdx := range allMatchesIndex(tortoiseFKRe, body) {
			attr := body[fkIdx[2]:fkIdx[3]]
			// group 2 is assignment form "app.Model" string, group 3 is annotation form
			target := ""
			if fkIdx[4] >= 0 {
				target = body[fkIdx[4]:fkIdx[5]]
			} else if fkIdx[6] >= 0 {
				target = body[fkIdx[6]:fkIdx[7]]
			}
			relLine := classLine + strings.Count(body[:fkIdx[0]], "\n")
			props := map[string]string{
				"framework":    "tortoise",
				"pattern_type": "foreign_key",
				"target_model": target,
				"parent_class": className,
			}
			out = append(out, entity(className+"."+attr, "SCOPE.Schema", "", file.Path, relLine, props))
		}

		// ManyToManyField / ManyToManyRelation
		for _, mtmIdx := range allMatchesIndex(tortoiseMTMRe, body) {
			attr := body[mtmIdx[2]:mtmIdx[3]]
			target := ""
			if mtmIdx[4] >= 0 {
				target = body[mtmIdx[4]:mtmIdx[5]]
			} else if mtmIdx[6] >= 0 {
				target = body[mtmIdx[6]:mtmIdx[7]]
			}
			relLine := classLine + strings.Count(body[:mtmIdx[0]], "\n")
			props := map[string]string{
				"framework":    "tortoise",
				"pattern_type": "many_to_many",
				"target_model": target,
				"parent_class": className,
			}
			out = append(out, entity(className+"."+attr, "SCOPE.Schema", "", file.Path, relLine, props))
		}

		// OneToOneField
		for _, o2oIdx := range allMatchesIndex(tortoiseO2OFieldRe, body) {
			attr := body[o2oIdx[2]:o2oIdx[3]]
			target := body[o2oIdx[4]:o2oIdx[5]]
			relLine := classLine + strings.Count(body[:o2oIdx[0]], "\n")
			props := map[string]string{
				"framework":    "tortoise",
				"pattern_type": "one_to_one",
				"target_model": target,
				"parent_class": className,
			}
			out = append(out, entity(className+"."+attr, "SCOPE.Schema", "", file.Path, relLine, props))
		}

		// ReverseRelation (back-references)
		for _, brIdx := range allMatchesIndex(tortoiseBackRefRe, body) {
			attr := body[brIdx[2]:brIdx[3]]
			target := body[brIdx[4]:brIdx[5]]
			relLine := classLine + strings.Count(body[:brIdx[0]], "\n")
			props := map[string]string{
				"framework":    "tortoise",
				"pattern_type": "reverse_relation",
				"target_model": target,
				"parent_class": className,
			}
			out = append(out, entity(className+"."+attr, "SCOPE.Schema", "", file.Path, relLine, props))
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
