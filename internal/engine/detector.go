package engine

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// compiledSourcePattern is a SourcePattern with its regex pre-compiled.
type compiledSourcePattern struct {
	regex      *regexp.Regexp
	entityType string
	nameGroup  int
	scope      string
	framework  string
}

// compiledRelationshipRule is a RelationshipRule with its regex pre-compiled.
type compiledRelationshipRule struct {
	regex        *regexp.Regexp
	sourceType   string
	targetType   string
	relationship string
	sourceGroup  int
	targetGroup  int
	framework    string
}

// compiledRuleSet holds all compiled patterns for one framework.
type compiledRuleSet struct {
	sourcePatterns    []compiledSourcePattern
	relationshipRules []compiledRelationshipRule
}

// Detector applies YAML-driven framework extraction rules to source files.
// It is safe for concurrent use.
type Detector struct {
	rules    map[string][]FrameworkRule
	compiled map[string][]compiledRuleSet
	tracer   trace.Tracer
	once     sync.Once
}

// New creates a Detector from a set of loaded rules.
// Regex compilation is deferred to first use (lazy init).
func New(rules map[string][]FrameworkRule) *Detector {
	return &Detector{
		rules:  rules,
		tracer: otel.Tracer("archigraph/engine"),
	}
}

// compile pre-compiles all regex patterns. Called once via sync.Once.
func (d *Detector) compile() {
	d.compiled = make(map[string][]compiledRuleSet, len(d.rules))

	for lang, frameworkRules := range d.rules {
		var sets []compiledRuleSet
		for _, fr := range frameworkRules {
			cs := compiledRuleSet{}

			for _, sp := range fr.SourcePatterns {
				re, err := regexp.Compile(sp.Pattern)
				if err != nil {
					log.Printf("engine: invalid source_pattern regex in %s: %q: %v", lang, sp.Pattern, err)
					continue
				}
				cs.sourcePatterns = append(cs.sourcePatterns, compiledSourcePattern{
					regex:      re,
					entityType: sp.EntityType,
					nameGroup:  sp.NameGroup,
					scope:      sp.Scope,
					framework:  lang,
				})
			}

			for _, rr := range fr.RelationshipRules {
				re, err := regexp.Compile(rr.Pattern)
				if err != nil {
					log.Printf("engine: invalid relationship_rule regex in %s: %q: %v", lang, rr.Pattern, err)
					continue
				}
				cs.relationshipRules = append(cs.relationshipRules, compiledRelationshipRule{
					regex:        re,
					sourceType:   rr.SourceType,
					targetType:   rr.TargetType,
					relationship: rr.Relationship,
					sourceGroup:  rr.SourceGroup,
					targetGroup:  rr.TargetGroup,
					framework:    lang,
				})
			}

			sets = append(sets, cs)
		}
		d.compiled[lang] = sets
	}
}

// DetectResult holds the entities and relationships extracted by the engine.
type DetectResult struct {
	Entities      []types.EntityRecord
	Relationships []types.RelationshipRecord
}

// Detect applies all YAML-driven rules for the file's language and returns
// extracted entities and relationships.
//
// Unknown languages return empty results with no error.
// Invalid regex patterns (caught at compile time) are skipped.
func (d *Detector) Detect(ctx context.Context, file extractor.FileInput) (*DetectResult, error) {
	d.once.Do(d.compile)

	_, span := d.tracer.Start(ctx, "engine.detect",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file.path", file.Path),
		),
	)
	defer span.End()

	// Resolve compiled YAML-rule sets for this language. If no rules are
	// registered we still allow the language through when the synthesis
	// pass below knows how to handle it — that pass scans content
	// directly and can emit framework entities (notably the http_endpoint
	// synthetics from #534) even when no YAML rules are present.
	sets, ok := d.compiled[file.Language]
	if !ok && !synthesisSupportsLanguage(file.Language) {
		span.SetAttributes(
			attribute.Int("entity_count", 0),
			attribute.Int("relationship_count", 0),
		)
		return &DetectResult{}, nil
	}

	content := string(file.Content)
	var entities []types.EntityRecord
	var relationships []types.RelationshipRecord

	// Track seen entities to avoid duplicates from overlapping patterns.
	seenEntities := make(map[string]bool)

	for _, cs := range sets {
		// Extract entities from source patterns.
		for _, sp := range cs.sourcePatterns {
			matches := sp.regex.FindAllStringSubmatch(content, -1)
			for _, match := range matches {
				name := extractGroup(match, sp.nameGroup)
				if name == "" {
					continue
				}

				key := fmt.Sprintf("%s:%s:%s", sp.entityType, name, file.Path)
				if seenEntities[key] {
					continue
				}
				seenEntities[key] = true

				entity := types.EntityRecord{
					Name:       name,
					Kind:       sp.entityType,
					SourceFile: file.Path,
					Language:   file.Language,
					Properties: map[string]string{
						"framework":    sp.framework,
						"pattern_type": "yaml_driven",
					},
					EnrichmentRequired: isComplexEntity(sp.entityType),
					EnrichmentStatus:   types.StatusPending,
					QualityScore:       0.5,
				}
				entities = append(entities, entity)
			}
		}

		// Extract relationships from relationship rules.
		for _, rr := range cs.relationshipRules {
			matches := rr.regex.FindAllStringSubmatch(content, -1)
			for _, match := range matches {
				sourceName := extractGroup(match, rr.sourceGroup)
				targetName := extractGroup(match, rr.targetGroup)
				if sourceName == "" || targetName == "" {
					continue
				}

				rel := types.RelationshipRecord{
					FromID: fmt.Sprintf("%s:%s", rr.sourceType, sourceName),
					ToID:   fmt.Sprintf("%s:%s", rr.targetType, targetName),
					Kind:   rr.relationship,
					Properties: map[string]string{
						"framework":    rr.framework,
						"pattern_type": "yaml_driven",
					},
				}
				relationships = append(relationships, rel)
			}
		}
	}

	// Spring MVC AST pass: compose class-level @RequestMapping prefix with
	// method-level verb annotations into a single Route. The YAML rules
	// above can't see lexical scope, so they emit orphan Route:/api +
	// Route:/orders pairs; this pass replaces them with Route:/api/orders.
	// No-op for non-Java files. Refs #67.
	entities, relationships = applySpringRouteComposition(
		ctx, file.Language, file.Path, file.Content, entities, relationships,
	)

	// Django REST Framework AST pass: compose the parent `path("api/",
	// include(<router>.urls))` prefix with each `<router>.register("name",
	// ViewSet)` call into a single composed Route. The YAML rules above
	// can't see the router-variable binding, so they emit orphan
	// Route:api/ + Route:users pairs; this pass replaces them with
	// Route:/api/users. No-op for non-Python files. Refs #64.
	entities, relationships = applyDjangoRouteComposition(
		ctx, file.Language, file.Path, file.Content, entities, relationships,
	)

	// Synthetic http_endpoint emission for typed-HTTP cross-repo matching.
	// Runs AFTER the Spring + Django composition passes so it can re-use
	// the composed Route entities they emit. Appends new entities/edges
	// only — never modifies or removes existing ones, so this pass cannot
	// regress the surrounding pipeline's bug-rate. Refs #534.
	entities, relationships = applyHTTPEndpointSynthesis(
		file.Language, file.Path, file.Content, entities, relationships,
	)

	// Django models-import suffix rewrite (PR #580 wave-10 Chain-fix A):
	// The YAML rule `from \S+\.models import (\w+)` emits Model:<name>
	// for every captured identifier. In Django/DRF projects, a sibling
	// `models` module routinely re-exports Serializer / ViewSet / View
	// classes. The naive Model: prefix surfaces as kind-mismatch
	// bug-resolver edges (60 instances on client-fixture-a). Rewrite the
	// ToID prefix in-place on suffix heuristics so the IMPORTS edge
	// matches the actual entity kind. Python-only.
	if file.Language == "python" {
		relationships = rewritePythonModelImports(relationships)
	}

	span.SetAttributes(
		attribute.Int("entity_count", len(entities)),
		attribute.Int("relationship_count", len(relationships)),
	)

	return &DetectResult{
		Entities:      entities,
		Relationships: relationships,
	}, nil
}

// extractGroup safely extracts a capture group from a regex match.
// Returns empty string if the group index is out of range.
func extractGroup(match []string, group int) string {
	if group < 0 || group >= len(match) {
		return ""
	}
	return match[group]
}

// isComplexEntity returns true for entity types that warrant LLM enrichment.
// Matches Python behavior: Controllers and Middleware are complex, Routes/Config are not.
func isComplexEntity(entityType string) bool {
	switch entityType {
	case "Controller", "Middleware", "Service", "Repository", "Model":
		return true
	default:
		return false
	}
}

// Languages returns all language names that have loaded rules.
func (d *Detector) Languages() []string {
	langs := make([]string, 0, len(d.rules))
	for lang := range d.rules {
		langs = append(langs, lang)
	}
	return langs
}

// RuleCount returns the total number of framework rules loaded across all languages.
func (d *Detector) RuleCount() int {
	count := 0
	for _, rules := range d.rules {
		count += len(rules)
	}
	return count
}
