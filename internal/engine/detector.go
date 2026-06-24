package engine

import (
	"context"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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

// compiledFileConvention is a FileConvention whose Glob has been validated and
// normalised for matching with path.Match.
type compiledFileConvention struct {
	// glob is the normalised (forward-slash) glob pattern.
	glob       string
	entityType string
	nameFrom   string
	framework  string
}

// matchesFile reports whether the convention's glob matches filePath.
// filePath is expected to be repo-relative with forward slashes.
// We try an exact path.Match first; if the glob has no slash we also
// test against just the base filename (for patterns like "*.py").
func (c compiledFileConvention) matchesFile(filePath string) bool {
	// Normalise to forward slashes (path.Match requires this).
	normalized := filepath.ToSlash(filePath)
	matched, err := path.Match(c.glob, normalized)
	if err == nil && matched {
		return true
	}
	// Also try matching against just the base so globs like "models.py" work.
	if !strings.Contains(c.glob, "/") {
		base := path.Base(normalized)
		matched2, err2 := path.Match(c.glob, base)
		return err2 == nil && matched2
	}
	return false
}

// compiledRuleSet holds all compiled patterns for one framework.
type compiledRuleSet struct {
	sourcePatterns    []compiledSourcePattern
	relationshipRules []compiledRelationshipRule
	fileConventions   []compiledFileConvention
}

// dormantBucketAliases maps a flavor-named rule bucket (the directory key the
// loader uses) to the concrete classifier language(s) its files actually carry,
// so the bucket's compiled rule sets are appended onto those language keys and
// fire on real files. See the aliasing block in compile() for the rationale
// (#3593). Only buckets whose frameworks/*.yaml carry engine schema keys are
// listed; doc-only buckets (html_templates) are omitted because aliasing them
// would add no extraction.
//
//   - cicd        → yaml       (.github/workflows, .gitlab-ci.yml, …)
//   - ansible     → yaml       (playbooks / roles / inventory .yml)
//   - kubernetes  → yaml       (manifest .yaml/.yml)
//   - docker      → dockerfile + yaml
//     (dockerfile.yaml rules target Dockerfiles → `dockerfile`; the sibling
//     docker_compose.yaml rules target compose files → `yaml`. The bucket is
//     indivisible at load time, so it is aliased onto both; non-matching
//     patterns are inert.)
var dormantBucketAliases = map[string][]string{
	"cicd":       {"yaml"},
	"ansible":    {"yaml"},
	"kubernetes": {"yaml"},
	"docker":     {"dockerfile", "yaml"},
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
		tracer: otel.Tracer("grafel/engine"),
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

			// Compile file_conventions: validate the glob pattern using path.Match
			// (a dry-run against an empty string surfaces syntax errors).
			for _, fc := range fr.FileConventions {
				if fc.Glob == "" {
					continue
				}
				// Validate glob syntax by attempting a dummy match.
				if _, err := path.Match(fc.Glob, ""); err != nil {
					log.Printf("engine: invalid file_convention glob in %s: %q: %v", lang, fc.Glob, err)
					continue
				}
				cs.fileConventions = append(cs.fileConventions, compiledFileConvention{
					glob:       fc.Glob,
					entityType: fc.EntityType,
					nameFrom:   fc.NameFrom,
					framework:  lang,
				})
			}

			sets = append(sets, cs)
		}
		d.compiled[lang] = sets
	}

	// Language-key aliasing (#2865). The JS/TS framework YAML rules live under
	// the directory key `javascript_typescript`, but the indexer tags real
	// source files with the concrete extension language — `typescript`
	// (.ts/.tsx) or `javascript` (.js/.jsx) — and Detect resolves compiled
	// rule sets by `file.Language`. Without this alias, none of the 48 JS/TS
	// framework rule sets (langchain, electron, trpc client, react, vue, …)
	// would ever fire on real files: the lookup `d.compiled["typescript"]`
	// missed the `javascript_typescript` bucket entirely. Mirror the bucket
	// onto both concrete keys so the YAML source_patterns / relationship_rules
	// run on production-tagged files exactly as the per-language Go synthesis
	// passes already do (synthesisSupportsLanguage covers typescript/javascript
	// directly). Only fill a concrete key when it has no rules of its own, so
	// a future dedicated `typescript`-keyed rule set is never shadowed.
	if jsts, ok := d.compiled["javascript_typescript"]; ok {
		for _, alias := range []string{"javascript", "typescript"} {
			if len(d.compiled[alias]) == 0 {
				d.compiled[alias] = jsts
			}
		}
	}

	// Dormant CI/infra-bucket aliasing (#3593). The loader buckets every
	// `rules/<dir>/**` tree under its top-level dirname, but Detect resolves
	// compiled rule sets by `file.Language` — the concrete vocabulary the
	// classifier emits. Several rule buckets are named for a *flavor*
	// (`cicd`, `ansible`, `kubernetes`, `docker`) that the classifier never
	// produces as a `file.Language`: CI/Ansible/K8s manifests are all tagged
	// `yaml` (.yaml/.yml), Dockerfiles `dockerfile`, and docker-compose files
	// `yaml`. Without an alias these buckets' source_patterns /
	// relationship_rules / file_conventions never fire on real files — the
	// lookup `d.compiled["yaml"]` missed the `cicd`/`ansible`/`kubernetes`
	// buckets entirely (the same class of latent bug the jsts alias above
	// fixed for #2865).
	//
	// Unlike the jsts case — where javascript/typescript have no bucket of
	// their own and a single source overwrites — multiple flavor buckets
	// target the SAME concrete language (cicd + ansible + kubernetes +
	// docker-compose all → yaml), so we APPEND each bucket's compiled sets
	// onto the target key rather than overwrite. Rule sets are regex-gated,
	// so a bucket whose patterns don't match a given file is a no-op; firing
	// the union on every file of the target language is safe and is exactly
	// how the per-language Go synthesis passes already operate.
	//
	// `html_templates` is intentionally NOT aliased here: none of its
	// frameworks/*.yaml files carry engine schema keys (source_patterns /
	// file_conventions / relationship_rules) — they are documentation-only
	// descriptors — so aliasing it onto `html` would add zero extraction.
	for bucket, targets := range dormantBucketAliases {
		sets, ok := d.compiled[bucket]
		if !ok || len(sets) == 0 {
			continue
		}
		for _, target := range targets {
			d.compiled[target] = append(d.compiled[target], sets...)
		}
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
		// Precompute all file_convention globs that match this file (for this
		// ruleset). This is evaluated once per (ruleset, file) pair — O(conventions)
		// — and reused for every source-pattern entity emitted below, so there is
		// no per-entity re-scan. The result is the comma-joined glob string used to
		// annotate source-pattern entities (bridge for issue #2383).
		var matchedConventionGlobs []string
		for _, fc := range cs.fileConventions {
			if fc.matchesFile(file.Path) {
				matchedConventionGlobs = append(matchedConventionGlobs, fc.glob)
			}
		}
		conventionAnnotation := strings.Join(matchedConventionGlobs, ",")

		// Extract entities from source patterns.
		// Issue #1413 — use FindAllStringSubmatchIndex so we have byte offsets
		// for computing StartLine. Also derives QualifiedName for Python entities.
		for _, sp := range cs.sourcePatterns {
			idxMatches := sp.regex.FindAllStringSubmatchIndex(content, -1)
			for _, idxMatch := range idxMatches {
				if len(idxMatch) < 2 {
					continue
				}
				name := extractGroupFromIndex(content, idxMatch, sp.nameGroup)
				if name == "" {
					// nameGroup 0 means the full match.
					if sp.nameGroup == 0 {
						name = content[idxMatch[0]:idxMatch[1]]
					}
				}
				if name == "" {
					continue
				}
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}

				key := fmt.Sprintf("%s:%s:%s", sp.entityType, name, file.Path)
				if seenEntities[key] {
					continue
				}
				seenEntities[key] = true

				startLine := matchStartLine(content, idxMatch[0])

				// Derive qualified_name for Python entities (issue #1413).
				qn := ""
				if file.Language == "python" {
					if mod := detectorFilePathToModule(file.Path); mod != "" {
						qn = mod + "." + name
					} else {
						qn = name
					}
				}

				props := map[string]string{
					"framework":    sp.framework,
					"pattern_type": "yaml_driven",
				}
				// Bridge #2383: when a file_convention glob also matches this file,
				// annotate the source-pattern entity so queries can find "all entities
				// from convention Y" or "entities identified by both rule X and convention Y".
				// Convention-derived entities (pattern_type=file_convention) already
				// encode their origin; this annotation is for source-pattern entities only.
				if conventionAnnotation != "" {
					props["file_convention"] = conventionAnnotation
				}

				entity := types.EntityRecord{
					Name:               name,
					QualifiedName:      qn,
					Kind:               sp.entityType,
					SourceFile:         file.Path,
					Language:           file.Language,
					StartLine:          startLine,
					Properties:         props,
					EnrichmentRequired: isComplexEntity(sp.entityType),
					EnrichmentStatus:   types.StatusPending,
					QualityScore:       0.5,
				}
				entities = append(entities, entity)
			}
		}

		// Apply file_conventions: glob-based entity dispatch. For each
		// convention whose Glob matches this file and whose NameFrom is
		// file-driven ("filename" or "parent_dir"), emit one entity tagged
		// with the convention's EntityType. Conventions with NameFrom =
		// "class_name" are informational only — source_patterns supply the
		// name; we tag the file type via a property rather than emitting a
		// separate entity.
		//
		// NOTE: For entity types that require rich semantic extraction (e.g.
		// "Migration" → extractMigrationEntity), the YAML convention identifies
		// the file while the language extractor does the detailed extraction.
		// This is intentional: source_patterns cannot express multi-field
		// property bags. The YAML-driven entity emitted here is a lightweight
		// file-tag; the extractor's entity is the authoritative one.
		for _, fc := range cs.fileConventions {
			if !fc.matchesFile(file.Path) {
				continue
			}
			// Tag the file as matching this convention regardless of name_from.
			// For file-driven names we emit a standalone entity.
			switch fc.nameFrom {
			case "filename":
				name := fileConventionName(file.Path, "filename")
				key := fmt.Sprintf("%s:%s:%s", fc.entityType, name, file.Path)
				if !seenEntities[key] {
					seenEntities[key] = true
					entities = append(entities, types.EntityRecord{
						Name:       name,
						Kind:       fc.entityType,
						SourceFile: file.Path,
						Language:   file.Language,
						StartLine:  1,
						Properties: map[string]string{
							"framework":          fc.framework,
							"pattern_type":       "file_convention",
							"file_convention":    fc.glob,
							"file_convention_op": "filename",
						},
						EnrichmentStatus: types.StatusPending,
						QualityScore:     0.5,
					})
				}
			case "parent_dir":
				name := fileConventionName(file.Path, "parent_dir")
				key := fmt.Sprintf("%s:%s:%s", fc.entityType, name, file.Path)
				if !seenEntities[key] {
					seenEntities[key] = true
					entities = append(entities, types.EntityRecord{
						Name:       name,
						Kind:       fc.entityType,
						SourceFile: file.Path,
						Language:   file.Language,
						StartLine:  1,
						Properties: map[string]string{
							"framework":          fc.framework,
							"pattern_type":       "file_convention",
							"file_convention":    fc.glob,
							"file_convention_op": "parent_dir",
						},
						EnrichmentStatus: types.StatusPending,
						QualityScore:     0.5,
					})
				}
			default:
				// "class_name" and unknown values: convention is informational.
				// The source_patterns layer emits the named entities; we only
				// annotate any existing entity for this file with the convention
				// tag via a property on the first entity found, to preserve
				// graph signal without creating phantom nodes.
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

	// Issue #3172 — Python DRF ViewSet double-emit dedup.
	//
	// Every Python framework's YAML rules fire on every Python file because
	// they all share the "python" language key.  Falcon's catch-all class
	// source_pattern (class \w+ ...) emits a `Controller` entity for every
	// class definition it sees, including DRF ViewSet classes in Django
	// files that the Django source_pattern already emits as `View`.  The
	// result is two framework-typed nodes — View + Controller — for the
	// same (Name, SourceFile), where the Controller carries zero edges
	// (dead phantom, ~72 per Upvate bench).
	//
	// Resolution: after all rule-sets have run, drop any `Controller` entity
	// whose (Name, SourceFile) pair is also covered by a `View` entity in
	// the same result set.  `View` carries CALLS/ROUTES_TO edges and is the
	// semantically correct node for a Django/DRF class; `Controller` is the
	// phantom.  Order-independent — we scan the final slice rather than
	// relying on rule-set processing order.
	if file.Language == "python" {
		entities = deduplicateViewControllerForPython(entities)
	}

	// Build the base args struct shared across all engine passes.
	// Each pass reads the fields it needs; unused fields are ignored.
	// Pass-specific fields (Pass1Entities, RepoRoot) are populated once here
	// and forwarded opaquely — passes that don't need them pay no call-site
	// cost. Entities and Relationships are updated after each pass via the
	// returned DetectorPassResult. Refs #2446.
	passArgs := DetectorPassArgs{
		Ctx:             ctx,
		Lang:            file.Language,
		Path:            file.Path,
		RepoRoot:        file.RepoRoot,
		Content:         file.Content,
		Pass1Entities:   file.Pass1Entities,
		CrossFileFields: file.CrossFileFields,
		Entities:        entities,
		Relationships:   relationships,
	}

	// applyPass is a tiny closure that applies a DetectorPassArgs→DetectorPassResult
	// pass and threads the updated (entities, relationships) back into passArgs so
	// the next pass sees the accumulated results.
	applyPass := func(fn func(DetectorPassArgs) DetectorPassResult) {
		res := fn(passArgs)
		passArgs.Entities = res.Entities
		passArgs.Relationships = res.Relationships
	}

	// Spring MVC AST pass: compose class-level @RequestMapping prefix with
	// method-level verb annotations into a single Route. The YAML rules
	// above can't see lexical scope, so they emit orphan Route:/api +
	// Route:/orders pairs; this pass replaces them with Route:/api/orders.
	// No-op for non-Java files. Refs #67.
	applyPass(applySpringRouteComposition)

	// Spring MVC AST pass for Kotlin: same prefix-composition logic as the
	// Java pass above, but adapted for the Kotlin tree-sitter CST shape.
	// Emits http_endpoint_definition entities directly (no intermediate Route
	// layer) because there is no YAML rule layer for Kotlin Spring controllers.
	// No-op for non-Kotlin files. Refs #1421.
	applyPass(applySpringRouteCompositionKotlin)

	// Django REST Framework AST pass: compose the parent `path("api/",
	// include(<router>.urls))` prefix with each `<router>.register("name",
	// ViewSet)` call into a single composed Route. The YAML rules above
	// can't see the router-variable binding, so they emit orphan
	// Route:api/ + Route:users pairs; this pass replaces them with
	// Route:/api/users. No-op for non-Python files. Refs #64.
	applyPass(applyDjangoRouteComposition)

	// Go HTTP route binding pass: rewrites YAML-emitted
	// `Route:<path> -ROUTES_TO-> Controller:<receiverVar>` edges to point at
	// the qualified handler method (`Controller:<Type>.<Method>`). Covers
	// chi, gin, echo, fiber, gorilla_mux — every framework whose YAML rule
	// captures only the bare receiver identifier with `(\w+)`. Edits ToID
	// only; never adds/removes entities. No-op for non-Go files. Refs #613.
	applyPass(applyGoRouteComposition)

	// Synthetic http_endpoint emission for typed-HTTP cross-repo matching.
	// Runs AFTER the Spring + Django composition passes so it can re-use
	// the composed Route entities they emit. Appends new entities/edges
	// only — never modifies or removes existing ones, so this pass cannot
	// regress the surrounding pipeline's bug-rate. Refs #534.
	applyPass(applyHTTPEndpointSynthesis)

	// ORM QUERIES edge synthesis (#723): for every detectable ORM call
	// site, emit a directed QUERIES edge from the enclosing function to
	// the targeted model class. Runs AFTER http_endpoint_synthesis so
	// the per-file entity index already includes any synthetic class
	// entities emitted earlier. Append-only — never modifies existing
	// entities or edges, so this pass cannot regress bug-rate on files
	// without ORM calls.
	applyPass(applyORMQueries)

	// Django ORM field-access edges (#2279). Lifts the filter_keys
	// property bag recorded on every QUERIES edge by the pass above
	// into first-class READS_FIELD / WRITES_FIELD edges between the
	// call site and the SCOPE.Schema(subtype=field) entity emitted by
	// the Python extractor. Runs AFTER applyORMQueries because it
	// pivots off the QUERIES edges that pass emits. Append-only —
	// cannot regress the surrounding pipeline's bug-rate.
	applyPass(applyORMFieldEdges)

	// Feature-flag gating topology (#3628 area #17). For every flag-check
	// call site detected via a flag-management SDK (LaunchDarkly variation,
	// Unleash isEnabled, OpenFeature getBooleanValue, Ruby Flipper.enabled?,
	// Flagsmith has_feature), emits a synthetic SCOPE.FeatureFlag entity
	// (`feature:<key>`) and a GATED_BY edge from the enclosing function so
	// the graph can answer flag blast-radius ("what code is gated by flag X").
	// Only literal flag keys are emitted; dynamic keys are skipped. Reuses
	// the orm_queries enclosing-function index. Append-only — cannot regress
	// the surrounding pipeline's bug-rate on files without flag checks.
	applyPass(applyFeatureFlagEdges)

	// Plugin / extension-system registration topology (#3628 area #25). For
	// every recognised build/config file (webpack/vite/rollup config, Babel,
	// ESLint, pytest_plugins, setuptools entry_points, pom.xml, build.gradle),
	// emits a synthetic SCOPE.Plugin entity (`plugin:<ecosystem>:<name>`) and
	// a REGISTERS_PLUGIN edge from the declaring file so the graph can answer
	// "which plugins does this build/app register". Only literal plugin names
	// are emitted; dynamic names are skipped. No-op on non-config files.
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	applyPass(applyPluginSystemEdges)

	// Kafka producer/consumer cross-repo edges (wave 1 of #726). Emits
	// synthetic MessageTopic entities + PUBLISHES_TO / SUBSCRIBES_TO edges
	// using the same cross-repo matching strategy as #534: identical
	// topic IDs on both sides naturally link via the existing import-
	// channel linker. Append-only — cannot regress the surrounding
	// pipeline's bug-rate.
	applyPass(applyKafkaEdges)

	// Kafka wrapper + transport idiom detection (#1467). Extends topic
	// extraction with four new families: Python KafkaBus wrapper
	// (bus.publish / bus.consumer), Spring RedisTemplate.convertAndSend,
	// Java Kafka Streams (builder.stream / kStream.to), and AWS SNS
	// publish (boto3, aws-sdk-java, aws-sdk-js, aws-sdk-go-v2). Emits
	// MessageTopic entities + PUBLISHES_TO / SUBSCRIBES_TO edges.
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	applyPass(applyKafkaWrapperEdges)

	// RabbitMQ producer/consumer cross-repo edges (wave 2 of #726). Emits
	// SCOPE.Queue entities + PUBLISHES_TO / SUBSCRIBES_TO edges for pika
	// (Python), amqplib (Node), Spring AMQP / direct RabbitMQ client (Java),
	// amqp091-go (Go), Quarkus RabbitMQ connector, and Celery with AMQP
	// broker. Append-only — cannot regress the surrounding pipeline's bug-rate.
	applyPass(applyRabbitMQEdges)

	// C/C++ ZeroMQ + MQTT producer/consumer cross-repo edges (#3559,
	// epic #3505). Emits SCOPE.MessageTopic entities + PUBLISHES_TO /
	// SUBSCRIBES_TO edges for libzmq/cppzmq sockets (keyed by endpoint) and
	// Paho/Mosquitto MQTT topics (keyed by topic), using the same identical-ID
	// cross-repo matching strategy as the Kafka pass. librdkafka C/C++ topics
	// are handled inside applyKafkaEdges. Append-only — cannot regress the
	// surrounding pipeline's bug-rate.
	applyPass(applyCppMessagingEdges)

	// AWS SQS producer/consumer cross-repo edges (wave 2 of #726). Emits
	// SCOPE.Queue entities + PUBLISHES_TO / SUBSCRIBES_TO edges for boto3
	// (Python), aws-sdk v2/v3 (Node), aws-sdk-go-v2 (Go), AWS SDK v2
	// (Java), and Lambda SQS triggers. Also detects SNS→SQS fanout.
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	applyPass(applySQSEdges)

	// IaC-declared SNS → SQS fan-out edges (#1596). Reads SNS→SQS
	// subscriptions declared in AWS CDK (TS), Terraform (HCL), and
	// CloudFormation (YAML) and emits a synthetic SNS topic (SCOPE.Queue,
	// broker=sns) plus a SUBSCRIBES_TO edge per SQS subscriber. Subscriptions
	// for the same topic name declared across different IaC tools collapse
	// onto a single SNS topic node, surfacing the fan-out in /topology.
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	applyPass(applyIaCSNSEdges)

	// IaC-declared messaging infrastructure → Topology channels (#4496,
	// ref epic #4493). Scans already-extracted IaC resource entities (any of
	// Terraform/HCL, CDK, Pulumi, CloudFormation, Bicep) whose cross-tool
	// resource_category is a messaging primitive (queue/topic/stream) and
	// APPENDS a synthetic SCOPE.Queue / SCOPE.MessageTopic channel keyed by the
	// canonical broker+name. Covers AWS SQS/SNS/Kinesis/MSK/EventBridge, GCP
	// Pub/Sub, and Azure Service Bus / Event Hubs / Event Grid uniformly. The
	// synthetic channel reuses the code-side sqs:/sns: IDs so a declared queue
	// collapses onto any code publisher/consumer of the same name (free
	// code-join); brokers with no code-side convention appear as a
	// declared-but-unwired channel — surfacing IaC queues in /topology even
	// when no SDK publisher/subscriber was detected. Runs AFTER the code-side
	// and iac_sns passes so it dedupes against the IDs they already emit.
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	applyPass(applyIaCTopologyChannels)

	// Kubernetes cross-resource edge synthesis (#3517 / epic #3512). The yaml
	// extractor already extracts K8s resources + sub-resources well but emits no
	// edges BETWEEN resources. This pass re-reads the manifest (file-scoped) and
	// synthesises: Service.spec.selector → matching workload (ROUTES_TO via pod-
	// template-label superset match), container env/envFrom + volume references →
	// ConfigMap/Secret/PVC (USES), Ingress backend → Service (ROUTES_TO), HPA
	// scaleTargetRef → workload and ownerReferences → parent (DEPENDS_ON). All
	// endpoints use the same QualifiedName the extractor minted so the resolver's
	// byQualifiedName index binds them. Append-only — cannot regress the yaml
	// extractor's output or the surrounding pipeline's bug-rate.
	applyPass(applyKubernetesEdges)
	// AWS CDK (TypeScript) resource + dependency extraction (part of #3512).
	// Emits one SCOPE.InfraResource per construct (`new s3.Bucket(this,'Id',…)`),
	// NAMED by its 'LogicalId' string literal and tagged with the construct type
	// + a coarse scope class, plus DEPENDS_ON edges for grant calls
	// (`bucket.grantRead(fn)`), Lambda event sources (`fn.addEventSource(new
	// SqsEventSource(queue))`), and construct variables passed through props.
	// Mirrors the hcl extractor's depends_on → DEPENDS_ON edge kind so CDK and
	// Terraform dependency edges are uniform. JS/TS only (CDK-Python/Java/Go/C#
	// follow under their own language buckets). Append-only — cannot regress the
	// surrounding pipeline's bug-rate.
	applyPass(applyCDKEdges)
	// CloudFormation / SAM resource modeling + dependency edges (#3518,
	// epic #3512). Detects CloudFormation and SAM templates (YAML or JSON)
	// and emits one SCOPE.* entity per `LogicalId: { Type: AWS::*, ... }`
	// resource (Kind mapped via cfnResourceKind), plus Parameter / Mapping /
	// Output-Export nodes. The core gap it closes is dependency attribution:
	// `!Ref`/`Ref`, `!GetAtt`/`Fn::GetAtt`, `DependsOn`, and `!Sub ${X}`
	// references become DEPENDS_ON / USES edges between resources (and to
	// Parameters). Cross-stack `Fn::ImportValue` / `Outputs.Export` collapse
	// onto a shared `cfn-export:<name>` node; SAM `AWS::Serverless::Function`
	// Events become ROUTES_TO (Api) / SUBSCRIBES_TO (SQS/SNS) / TRIGGERS
	// (Schedule) and join the serverless_edges.go `aws-lambda:` synthetic;
	// nested `AWS::CloudFormation::Stack` TemplateURL → IMPORTS. Append-only —
	// cannot regress the surrounding pipeline's bug-rate.
	applyPass(applyCloudFormationEdges)
	// Pulumi (TypeScript + Python) resource + dependency extraction (#3528,
	// epic #3512). Emits one SCOPE.InfraResource per resource constructor
	// (`new aws.s3.Bucket("name",…)` TS / `aws.s3.Bucket("name",…)` Python),
	// NAMED by its logical-name string literal and tagged with the resource type
	// + a coarse scope class, plus DEPENDS_ON edges for output references
	// (`other.arn`/`.id` passed into args), explicit dependsOn/depends_on lists,
	// and `StackReference("org/proj/stack")` cross-stack nodes (collapsed onto a
	// shared pulumi-stack:<ref> node). ComponentResource subclasses become a
	// component-scoped resource node. Mirrors the hcl extractor's depends_on →
	// DEPENDS_ON edge kind so Pulumi, CDK and Terraform dependency edges are
	// uniform. TS/JS + Python (Go/C#/Java follow under their own buckets).
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	applyPass(applyPulumiEdges)

	// Debezium / Kafka-Connect CDC connector edges (#1708). Parses the
	// JSON config of a CDC connector and emits a SCOPE.Component
	// (cdc_connector) plus CAPTURES → captured-table and PUBLISHES_TO →
	// produced kafka:<topic> edges. Topic IDs use the same canonical
	// "kafka:<topic>" form as the Kafka synthesis pass, so downstream
	// SUBSCRIBES_TO edges from regular Kafka consumers attach to the same
	// node without an explicit cross-pass handoff. Append-only — cannot
	// regress the surrounding pipeline's bug-rate. Only fires for files
	// whose path the classifier narrowed to language="json" (cdc/,
	// debezium/, kafka-connect/, *-connector.json, …), then content-
	// sniffed for `connector.class` / `io.debezium`.
	applyPass(applyDebeziumCDCEdges)

	// Google Cloud Pub/Sub producer/consumer cross-repo edges (wave 3 of #726).
	// Emits SCOPE.Queue entities + PUBLISHES_TO / SUBSCRIBES_TO edges for
	// google-cloud-pubsub (Python/Node/Go/Java), Pub/Sub Lite, and
	// Eventarc / Cloud Run trigger consumers.
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	applyPass(applyPubSubEdges)

	// NATS producer/consumer cross-repo edges (wave 3 of #726). Emits
	// SCOPE.Queue entities + PUBLISHES_TO / SUBSCRIBES_TO edges for
	// nats.go / nats.js / nats.py / nats.java, JetStream, and NATS
	// Streaming (STAN). Wildcard subjects and request/reply pattern tracked.
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	applyPass(applyNATSEdges)

	// Apache Pulsar producer/consumer cross-repo edges (#936). Emits
	// SCOPE.MessageTopic entities (broker=pulsar) + PUBLISHES_TO /
	// SUBSCRIBES_TO edges for pulsar-client (Python/Java/Kotlin),
	// pulsar-client-go (Go), and pulsar-client (Node/TS). Topic names are
	// canonicalised to the full persistent://tenant/namespace/topic URI so
	// the cross-repo linker matches producer and consumer sides on the same
	// entity ID. Append-only — cannot regress the surrounding pipeline.
	applyPass(applyPulsarEdges)

	// Azure Service Bus / Event Hubs producer/consumer cross-repo edges
	// (#3674, #3628 area #2). Completes azure broker topology: before this
	// pass azure had NO messaging emitter, so its producer→consumer topology
	// could never form even though the broker-agnostic topic_pass join was
	// ready. Emits SCOPE.MessageTopic entities keyed `azure:<name>` +
	// PUBLISHES_TO / SUBSCRIBES_TO edges for Azure.Messaging.ServiceBus /
	// EventHubs (C#), @azure/service-bus + @azure/event-hubs (JS/TS), and
	// azure-servicebus + azure-eventhub (Python). Dynamic names are skipped
	// (honest-partial). Azure EventGrid stays with event_bus_edges.go.
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	applyPass(applyAzureMessagingEdges)

	// #727: Real-time event channel synthesis. Three append-only passes
	// for WebSocket, Server-Sent Events, and GraphQL subscriptions. Each
	// scans the file directly and emits ChannelEvent / Stream /
	// Subscription entities plus the WS_SUBSCRIBES_TO / WS_EMITS /
	// WS_CONNECTS / STREAMS_{TO,FROM} / GRAPHQL_{PUBLISHES,SUBSCRIBES}
	// edges. Same architectural shape as applyHTTPEndpointSynthesis: no
	// existing entity or edge is touched, so these passes cannot regress
	// the surrounding pipeline.
	applyPass(applyWebSocketSynthesis)
	applyPass(applySSESynthesis)
	applyPass(applyGraphQLSubscriptionSynthesis)
	// #3682 (epic #3628 area #7): realtime-endpoint synthesis. Emits the
	// endpoint-shaped, queryable, cross-linkable view of WebSocket / SSE /
	// SignalR / Phoenix Channels handlers — http_endpoint_definition entities
	// with verb=WS|SSE + route_path + a HANDLES edge to the handler — so the
	// `endpoints` / `find` tools surface realtime endpoints alongside REST
	// routes. Additive on top of the ChannelEvent/Stream passes above.
	applyPass(applyRealtimeEndpointSynthesis)
	// [realtime] WS room/channel grouping (child of #3628). Emits the grouping
	// layer ABOVE the per-event WS endpoints: a SCOPE.Channel node per
	// real-time room/channel/group/topic + JOINS_CHANNEL / BROADCASTS_TO edges
	// from the enclosing function. A join and a broadcast on the same literal
	// room converge on one node, so the graph answers "who joins / broadcasts
	// to room X?" for Socket.IO rooms (JS/TS), Rails ActionCable (ruby), Django
	// Channels groups (python), and Phoenix channel topics (elixir). Dynamic
	// room names are skipped (honest-partial). Append-only — cannot regress
	// surrounding passes.
	applyPass(applyWSChannelGrouping)
	// #728: Scheduled-job entry-point detection. Emits SCOPE.ScheduledJob
	// entities + TRIGGERS edges for every major scheduler framework across
	// Python, Node, Java/Kotlin, Go; plus Kubernetes CronJob YAML and
	// GitHub Actions schedule triggers (path-driven, not language-gated).
	// Append-only — cannot regress surrounding passes.
	applyPass(applyScheduledJobEdges)
	// CLI command entry-point detection (epic #3628). Emits SCOPE.Command
	// entities + HANDLES_COMMAND edges (the CLI sibling of an HTTP endpoint's
	// route -> handler) for click/argparse/typer (Python), commander/yargs/
	// oclif (Node), cobra (Go), picocli/Spring Shell (Java), and Thor/Rake
	// (Ruby). Dynamic command names / handler refs are skipped, and every
	// detector is gated behind a framework-import pre-filter so a non-CLI
	// .command() / .action() on an unrelated object mints nothing.
	// Append-only - cannot regress surrounding passes.
	applyPass(applyCLICommandEdges)
	// #3628 area: ORM model lifecycle-hook / signal → handler TRIGGERS.
	// Emits SCOPE.ModelEvent:<Model>.<event> nodes + TRIGGERS edges to the
	// handler for Django signals, SQLAlchemy events, ActiveRecord callbacks,
	// TypeORM entity listeners, Sequelize hooks, and Mongoose middleware.
	// Answers "what runs after a User is saved?". Append-only — cannot regress
	// surrounding passes.
	applyPass(applyORMLifecycleHookEdges)
	// #728: Webhook endpoint detection. Tags HTTP endpoints that verify
	// inbound callbacks from external providers (Stripe, GitHub, Twilio,
	// Slack, Mailgun, Svix, generic) with is_webhook=true +
	// webhook_provider and emits SUBSCRIBES_TO edges to SCOPE.External
	// entities. Append-only — cannot regress surrounding passes.
	applyPass(applyWebhookEdges)
	// gRPC service definitions + client/server cross-repo edges (#725).
	// Emits SCOPE.GrpcService + SCOPE.GrpcMethod entities and
	// GRPC_IMPLEMENTS / GRPC_HANDLES edges for Java/Kotlin, Go, Python,
	// and Node/TypeScript. Cross-repo matching: both sides emit GrpcMethod
	// entities keyed by `grpc:ServiceName/MethodName`; the import-channel
	// linker joins them without any new linker code. Append-only — cannot
	// regress surrounding passes.
	applyPass(applyGRPCEdges)
	// Serverless function invocation edges (#925). Emits
	// SCOPE.ServerlessFunction entities + CALLS / HANDLES edges for AWS Lambda
	// (boto3, AWS SDK v2/v3, Go SDK v2, Java RequestHandler), Google Cloud
	// Functions (functions-framework Python/Node), and Azure Functions (durable
	// Python/Node/C#, [FunctionName] C# attribute). Cross-repo: both sides emit
	// the same provider-prefixed entity ID so the import-channel linker joins
	// them without new linker code. Append-only — cannot regress surrounding passes.
	// Lays groundwork for #927 EventBridge (Lambda synthetics as anchor targets).
	applyPass(applyServerlessEdges)
	// Serverless Framework (serverless.yml) topology extraction (#3519, epic
	// #3512). Parses a serverless.yml manifest and emits first-class graph
	// entities/edges: functions.<name> → SCOPE.ServerlessFunction (keyed
	// aws-lambda:<name>, collapsing with the code-side handler synthetics from
	// applyServerlessEdges); http/httpApi events → http_endpoint_definition +
	// SERVES; sqs/sns/stream/kinesis events → queue/topic + TRIGGERS; schedule
	// events → SCOPE.ScheduledJob + TRIGGERS; functions.<name>.handler →
	// HANDLES edge to the resolved code symbol. Also populates
	// serverlessYMLHandlerIndex so resolveServerlessYMLName performs the real
	// topology join (previously a #927-deferred stub). Append-only — cannot
	// regress surrounding passes. resources: is left to the CFN pass.
	applyPass(applyServerlessFrameworkEdges)
	// Deployment / request-flow topology edges (#3633, epic #3625). Restores the
	// previously-orphaned deployment_topology enricher as a live pass. Parses the
	// reverse-proxy / API-gateway / container-orchestration configs that sit in
	// FRONT of the application — nginx (upstream/proxy_pass), Caddy
	// (reverse_proxy), docker-compose (services + depends_on), Kong (declarative
	// services/routes), and Traefik (dynamic routers→services) — and emits
	// canonical SCOPE.Service nodes plus DEPENDS_ON / ROUTES_TO edges modelling
	// the request flow. Backend services are keyed `service:<name>` so a proxy
	// upstream and a compose service of the same name collapse onto one node.
	// K8s Ingress→Service and serverless.yml topology are handled by their own
	// dedicated passes (applyKubernetesEdges, applyServerlessFrameworkEdges).
	// Append-only — cannot regress surrounding passes.
	applyPass(applyDeploymentTopologyEdges)
	// Explicit infra↔code DEPLOYMENT edges (#4983, Topology Model 2/3 epic
	// #4810). The K8s / compose / serverless passes above model infra↔infra and
	// request-flow topology but never connect an IaC COMPUTE resource to the
	// CODE service it runs. This pass mints a first-class DEPLOYS edge from a
	// Kubernetes workload (via its container image repo), a docker-compose
	// service (via its first-party image / build), and a serverless function
	// (via its handler) to the canonical code `service:<name>` node — the SAME
	// key applyDeploymentTopologyEdges/applyAPIGatewayRoutingEdges use, so the
	// edge collapses onto the real code/compose service node. Public base/
	// sidecar images (postgres, redis, nginx…) and templated refs are filtered,
	// not guessed; every edge carries inferred=true so Model 2/3 can style the
	// deploy-time mapping distinctly. Append-only — cannot regress surrounding
	// passes.
	applyPass(applyDeploymentCodeEdges)
	// API-gateway route topology edges (#3723, epic #3628 area #21). Complements
	// applyDeploymentTopologyEdges (which owns the reverse-proxy / infra gateways
	// nginx/Caddy/Kong/Traefik) by modelling the APPLICATION-FRAMEWORK API
	// gateways whose config declares a route + the upstream service it forwards
	// to: Spring Cloud Gateway (application.yml spring.cloud.gateway.routes +
	// Java RouteLocatorBuilder DSL), Ocelot (ocelot.json Routes[]), Express
	// Gateway (gateway.config.yml apiEndpoints+serviceEndpoints), and
	// http-proxy-middleware (createProxyMiddleware({target})). Mints a
	// SCOPE.Route node per gateway route and a ROUTES_TO edge to the upstream
	// `service:<name>` SCOPE.Service node — the SAME canonical key the
	// deployment-topology pass uses, so a gateway route to `lb://user-service`
	// and a compose service `user-service` collapse onto one node. Honest-partial:
	// dynamic/templated upstreams (${...}) are omitted, not guessed. Append-only
	// — cannot regress surrounding passes.
	applyPass(applyAPIGatewayRoutingEdges)
	// Frontend route -> component graph (epic #3628). Complements the BACKEND
	// routing passes above by modelling the CLIENT-SIDE routing table: which
	// component a single-page-app router (React Router, Vue Router, Angular)
	// renders for a URL path. Mints a SCOPE.Route node keyed `feroute:<file>:
	// <path>` (synthesis="frontend_routing", scope="client") — DISTINCT from a
	// backend SCOPE.Endpoint or api-gateway SCOPE.Route even when the path
	// coincides — and a ROUTES_TO edge to the rendered component (bare name,
	// bound by the cross-file resolver). Honest-partial: dynamic paths and
	// unresolvable component refs are dropped. JS/TS only; gated; append-only.
	applyPass(applyFrontendRouteEdges)
	// Workflow orchestration edges (#934). Emits SCOPE.Workflow, SCOPE.Activity,
	// and SCOPE.StateMachine entities plus STARTS_WORKFLOW, EXECUTES_ACTIVITY,
	// and STEPFUNCTION_STEP_INVOKES edges for Temporal (Python, Go, Java, Node),
	// Cadence (Java), and AWS Step Functions (ASL JSON, Terraform, CloudFormation,
	// CDK). Step Functions Task states referencing Lambda ARNs are linked to the
	// SCOPE.ServerlessFunction entities emitted by #940 (serverless_edges.go)
	// without new linker code. Append-only — cannot regress surrounding passes.
	applyPass(applyWorkflowEdges)
	applyPass(applySFNStartExecutionEdges)
	// Workflow/orchestration DAG topology (#3628 area #12). Extends the
	// SCOPE.Workflow / SCOPE.Activity entity shape to the task-dependency DAG
	// orchestrators that workflow_edges.go did not cover: Airflow (Python
	// operators + `>>` / set_downstream chains and @task TaskFlow), Celery
	// canvas (chain/group/chord), and Argo Workflows (YAML dag.tasks
	// dependencies + sequential steps). Emits EXECUTES_ACTIVITY (DAG→task) and
	// TASK_DEPENDS_ON (upstream→downstream) edges. Append-only — cannot regress
	// surrounding passes.
	applyPass(applyWorkflowDAGEdges)
	// Finite-state-machine (FSM) topology (#3704, epic #3628 area #20).
	// Emits SCOPE.State entities + TRANSITIONS_TO edges (carrying the
	// triggering event) for the dominant application-level FSM libraries:
	// XState (JS/TS), Ruby AASM, Spring StateMachine (Java/Kotlin), and the
	// Python transitions library. Distinct from the AWS Step Functions
	// SCOPE.StateMachine whole-machine model in workflow_edges.go - this
	// models the individual states and the state-to-state transition graph.
	// Append-only - cannot regress surrounding passes.
	applyPass(applyStateMachineEdges)
	// Redis pub/sub + Streams channel discovery (#930). Emits SCOPE.Queue
	// entities keyed by channel:redis-pubsub:<name> or stream:redis:<name>,
	// plus PUBLISHES_TO / SUBSCRIBES_TO edges. Covers Python (redis-py /
	// aioredis), Node (ioredis / node-redis), Go (go-redis), and Ruby
	// (redis-rb). Non-pub/sub cache calls (GET/SET/etc.) are filtered out
	// by the fast-path pre-filter gate. Append-only — cannot regress
	// surrounding passes.
	applyPass(applyRedisPubSubEdges)
	// BullMQ / Bull task-queue topic attribution (#2865). Emits SCOPE.Queue
	// entities keyed by bullmq:<name> plus PUBLISHES_TO / SUBSCRIBES_TO edges
	// for `new Queue` / `queue.add` (producer) and `new Worker` /
	// `queue.process` (consumer). The queue name is the cross-repo rendezvous
	// point, so identical names join across services via the import-channel
	// linker with no new linker code. JS/TS only. Append-only — cannot regress
	// surrounding passes.
	applyPass(applyBullMQEdges)
	// Inngest producer → event-topic edges (#5482, epic #5479 Phase 2). For
	// each `inngest.send` / `sendEvent` call, emits a PUBLISHES_TO edge from
	// the enclosing scope to the SCOPE.MessageTopic event entity the custom
	// extractor created (#5481), keyed by the bare event name. Reuses the
	// existing PUBLISHES_TO kind so the cross-repo topic linker and dashboard
	// understand it with no new code. JS/TS only. Append-only — cannot regress
	// surrounding passes.
	applyPass(applyInngestEdges)
	// Managed event-bus edges (#927): AWS EventBridge, Azure EventGrid, and
	// CNCF CloudEvents. Emits SCOPE.EventBusEvent synthetic entities plus
	// PUBLISHES_TO / SUBSCRIBES_TO edges for producers/consumers, and
	// EVENTBRIDGE_TRIGGERS / EVENTGRID_TRIGGERS / CLOUDEVENT_FLOWS edges for
	// rule-to-target linkage. EventBridge rule targets reference Lambda
	// entity IDs from #925 (`aws-lambda:<name>`) without reinventing them.
	// Append-only — cannot regress surrounding passes.
	applyPass(applyEventBusEdges)

	// Extract final accumulated entities and relationships from passArgs.
	entities = passArgs.Entities
	relationships = passArgs.Relationships
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

	// Issue #3729 (epic #3628, area #24) — precision pass. Runs last, after
	// every rule-set and engine pass has contributed its entities and edges, so
	// it collapses cross-pass multi-kind double-emits and strips statement-level
	// `Operation` noise from the final graph. Edge endpoints anchored to a
	// collapsed kind are rewritten so no relationship is lost; opt-out via
	// PrecisionDedupEnabled.
	entities, relationships = applyPrecisionDedup(entities, relationships)

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

// extractGroupFromIndex extracts the text of the capture group at groupIdx from
// an index-format submatch (as returned by FindAllStringSubmatchIndex).
// groupIdx 0 returns the full match (idxMatch[0]:idxMatch[1]).
// Returns "" when the group is absent (negative offset) or out of range.
func extractGroupFromIndex(content string, idxMatch []int, groupIdx int) string {
	// idxMatch layout: [fullStart, fullEnd, g1Start, g1End, g2Start, g2End, …]
	pairIdx := groupIdx * 2
	if pairIdx+1 >= len(idxMatch) {
		return ""
	}
	start, end := idxMatch[pairIdx], idxMatch[pairIdx+1]
	if start < 0 || end < 0 || start > end || end > len(content) {
		return ""
	}
	return content[start:end]
}

// matchStartLine returns the 1-based line number of the start of a regex match
// within content, given its byte offset. Returns 1 for offsets at or below 0.
func matchStartLine(content string, byteOffset int) int {
	if byteOffset <= 0 {
		return 1
	}
	line := 1
	for i := 0; i < byteOffset && i < len(content); i++ {
		if content[i] == '\n' {
			line++
		}
	}
	return line
}

// detectorFilePathToModule converts a repo-relative Python file path to its
// dotted module path. Mirrors filePathToModule in internal/extractors/python
// but kept local to avoid an import cycle.
//
// Examples:
//
//	"app/orders/handlers.py"  → "orders.handlers"  (app/ stripped)
//	"src/app/models.py"       → "app.models"         (src/ stripped)
//	"users/__init__.py"       → "users"
func detectorFilePathToModule(filePath string) string {
	s := strings.TrimSuffix(filePath, ".py")
	if strings.HasSuffix(s, "/__init__") {
		s = strings.TrimSuffix(s, "/__init__")
	}
	for _, prefix := range []string{"src/", "lib/", "app/"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}
	return strings.ReplaceAll(s, "/", ".")
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

// deduplicateViewControllerForPython removes `Controller` entities whose
// (Name, SourceFile) pair is already covered by a `View` entity in the same
// slice.  Called for Python files only (see issue #3172).
//
// The Falcon YAML source_pattern `class\s+(\w+)...` is a catch-all that
// fires on every Python file, including Django/DRF files where the Django
// source_pattern already emits the same class as `View`.  Keeping both
// produces a dead phantom `Controller` node with zero edges alongside the
// live `View` node that carries CALLS/ROUTES_TO edges.
//
// This pass is order-independent: it scans the full entity slice in two
// passes (index → drop), so it is safe regardless of which YAML rule-set
// ran first.  The `View` entity is kept; the duplicate `Controller` is
// dropped.  Entities of other kinds (Model, Route, Config, …) are
// never affected.
func deduplicateViewControllerForPython(entities []types.EntityRecord) []types.EntityRecord {
	if len(entities) == 0 {
		return entities
	}

	// First pass: collect all (Name, SourceFile) pairs that have a View entity.
	viewPairs := make(map[[2]string]bool, len(entities))
	for i := range entities {
		e := &entities[i]
		if e.Kind == "View" {
			viewPairs[[2]string{e.Name, e.SourceFile}] = true
		}
	}
	if len(viewPairs) == 0 {
		return entities // no View entities → nothing to dedup
	}

	// Second pass: drop Controller entities shadowed by a View.
	out := make([]types.EntityRecord, 0, len(entities))
	for i := range entities {
		e := entities[i]
		if e.Kind == "Controller" && viewPairs[[2]string{e.Name, e.SourceFile}] {
			// Phantom: a View already exists for the same class symbol.
			continue
		}
		out = append(out, e)
	}
	return out
}

// fileConventionName derives an entity name from a file path according to the
// named NameFrom strategy.
//
//   - "filename":   base filename without extension
//     "core/migrations/0042_device.py" → "0042_device"
//   - "parent_dir": immediate parent directory name
//     "myapp/models/base.py" → "models"
func fileConventionName(filePath, nameFrom string) string {
	switch nameFrom {
	case "parent_dir":
		return path.Base(path.Dir(filepath.ToSlash(filePath)))
	default: // "filename" and fallback
		base := path.Base(filepath.ToSlash(filePath))
		// Strip the extension.
		if idx := strings.LastIndex(base, "."); idx > 0 {
			base = base[:idx]
		}
		return base
	}
}
