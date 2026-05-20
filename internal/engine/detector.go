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

	// Go HTTP route binding pass: rewrites YAML-emitted
	// `Route:<path> -ROUTES_TO-> Controller:<receiverVar>` edges to point at
	// the qualified handler method (`Controller:<Type>.<Method>`). Covers
	// chi, gin, echo, fiber, gorilla_mux — every framework whose YAML rule
	// captures only the bare receiver identifier with `(\w+)`. Edits ToID
	// only; never adds/removes entities. No-op for non-Go files. Refs #613.
	entities, relationships = applyGoRouteComposition(
		file.Language, file.Path, file.Content, entities, relationships,
	)

	// Synthetic http_endpoint emission for typed-HTTP cross-repo matching.
	// Runs AFTER the Spring + Django composition passes so it can re-use
	// the composed Route entities they emit. Appends new entities/edges
	// only — never modifies or removes existing ones, so this pass cannot
	// regress the surrounding pipeline's bug-rate. Refs #534.
	entities, relationships = applyHTTPEndpointSynthesis(
		file.Language, file.Path, file.Content, entities, relationships,
	)

	// ORM QUERIES edge synthesis (#723): for every detectable ORM call
	// site, emit a directed QUERIES edge from the enclosing function to
	// the targeted model class. Runs AFTER http_endpoint_synthesis so
	// the per-file entity index already includes any synthetic class
	// entities emitted earlier. Append-only — never modifies existing
	// entities or edges, so this pass cannot regress bug-rate on files
	// without ORM calls.
	entities, relationships = applyORMQueries(
		file.Language, file.Path, file.Content, entities, relationships,
	)

	// Kafka producer/consumer cross-repo edges (wave 1 of #726). Emits
	// synthetic MessageTopic entities + PUBLISHES_TO / SUBSCRIBES_TO edges
	// using the same cross-repo matching strategy as #534: identical
	// topic IDs on both sides naturally link via the existing import-
	// channel linker. Append-only — cannot regress the surrounding
	// pipeline's bug-rate.
	entities, relationships = applyKafkaEdges(
		file.Language, file.Path, file.RepoRoot, file.Content, entities, relationships,
	)

	// RabbitMQ producer/consumer cross-repo edges (wave 2 of #726). Emits
	// SCOPE.Queue entities + PUBLISHES_TO / SUBSCRIBES_TO edges for pika
	// (Python), amqplib (Node), Spring AMQP / direct RabbitMQ client (Java),
	// amqp091-go (Go), Quarkus RabbitMQ connector, and Celery with AMQP
	// broker. Append-only — cannot regress the surrounding pipeline's bug-rate.
	entities, relationships = applyRabbitMQEdges(
		file.Language, file.Path, file.Content, entities, relationships,
	)

	// AWS SQS producer/consumer cross-repo edges (wave 2 of #726). Emits
	// SCOPE.Queue entities + PUBLISHES_TO / SUBSCRIBES_TO edges for boto3
	// (Python), aws-sdk v2/v3 (Node), aws-sdk-go-v2 (Go), AWS SDK v2
	// (Java), and Lambda SQS triggers. Also detects SNS→SQS fanout.
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	entities, relationships = applySQSEdges(
		file.Language, file.Path, file.Content, entities, relationships,
	)

	// Google Cloud Pub/Sub producer/consumer cross-repo edges (wave 3 of #726).
	// Emits SCOPE.Queue entities + PUBLISHES_TO / SUBSCRIBES_TO edges for
	// google-cloud-pubsub (Python/Node/Go/Java), Pub/Sub Lite, and
	// Eventarc / Cloud Run trigger consumers.
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	entities, relationships = applyPubSubEdges(
		file.Language, file.Path, file.Content, entities, relationships,
	)

	// NATS producer/consumer cross-repo edges (wave 3 of #726). Emits
	// SCOPE.Queue entities + PUBLISHES_TO / SUBSCRIBES_TO edges for
	// nats.go / nats.js / nats.py / nats.java, JetStream, and NATS
	// Streaming (STAN). Wildcard subjects and request/reply pattern tracked.
	// Append-only — cannot regress the surrounding pipeline's bug-rate.
	entities, relationships = applyNATSEdges(
		file.Language, file.Path, file.Content, entities, relationships,
	)

	// #727: Real-time event channel synthesis. Three append-only passes
	// for WebSocket, Server-Sent Events, and GraphQL subscriptions. Each
	// scans the file directly and emits ChannelEvent / Stream /
	// Subscription entities plus the WS_SUBSCRIBES_TO / WS_EMITS /
	// WS_CONNECTS / STREAMS_{TO,FROM} / GRAPHQL_{PUBLISHES,SUBSCRIBES}
	// edges. Same architectural shape as applyHTTPEndpointSynthesis: no
	// existing entity or edge is touched, so these passes cannot regress
	// the surrounding pipeline.
	entities, relationships = applyWebSocketSynthesis(
		file.Language, file.Path, file.Content, entities, relationships,
	)
	entities, relationships = applySSESynthesis(
		file.Language, file.Path, file.Content, entities, relationships,
	)
	entities, relationships = applyGraphQLSubscriptionSynthesis(
		file.Language, file.Path, file.Content, entities, relationships,
	)
	// #728: Scheduled-job entry-point detection. Emits SCOPE.ScheduledJob
	// entities + TRIGGERS edges for every major scheduler framework across
	// Python, Node, Java/Kotlin, Go; plus Kubernetes CronJob YAML and
	// GitHub Actions schedule triggers (path-driven, not language-gated).
	// Append-only — cannot regress surrounding passes.
	entities, relationships = applyScheduledJobEdges(
		file.Language, file.Path, file.Content, entities, relationships,
	)
	// #728: Webhook endpoint detection. Tags HTTP endpoints that verify
	// inbound callbacks from external providers (Stripe, GitHub, Twilio,
	// Slack, Mailgun, Svix, generic) with is_webhook=true +
	// webhook_provider and emits SUBSCRIBES_TO edges to SCOPE.External
	// entities. Append-only — cannot regress surrounding passes.
	entities, relationships = applyWebhookEdges(
		file.Language, file.Path, file.Content, entities, relationships,
	)
	// gRPC service definitions + client/server cross-repo edges (#725).
	// Emits SCOPE.GrpcService + SCOPE.GrpcMethod entities and
	// GRPC_IMPLEMENTS / GRPC_HANDLES edges for Java/Kotlin, Go, Python,
	// and Node/TypeScript. Cross-repo matching: both sides emit GrpcMethod
	// entities keyed by `grpc:ServiceName/MethodName`; the import-channel
	// linker joins them without any new linker code. Append-only — cannot
	// regress surrounding passes.
	entities, relationships = applyGRPCEdges(
		file.Language, file.Path, file.Content, entities, relationships,
	)
	// Serverless function invocation edges (#925). Emits
	// SCOPE.ServerlessFunction entities + CALLS / HANDLES edges for AWS Lambda
	// (boto3, AWS SDK v2/v3, Go SDK v2, Java RequestHandler), Google Cloud
	// Functions (functions-framework Python/Node), and Azure Functions (durable
	// Python/Node/C#, [FunctionName] C# attribute). Cross-repo: both sides emit
	// the same provider-prefixed entity ID so the import-channel linker joins
	// them without new linker code. Append-only — cannot regress surrounding passes.
	// Lays groundwork for #927 EventBridge (Lambda synthetics as anchor targets).
	entities, relationships = applyServerlessEdges(
		file.Language, file.Path, file.Content, entities, relationships,
	)
	// Redis pub/sub + Streams channel discovery (#930). Emits SCOPE.Queue
	// entities keyed by channel:redis-pubsub:<name> or stream:redis:<name>,
	// plus PUBLISHES_TO / SUBSCRIBES_TO edges. Covers Python (redis-py /
	// aioredis), Node (ioredis / node-redis), Go (go-redis), and Ruby
	// (redis-rb). Non-pub/sub cache calls (GET/SET/etc.) are filtered out
	// by the fast-path pre-filter gate. Append-only — cannot regress
	// surrounding passes.
	entities, relationships = applyRedisPubSubEdges(
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
