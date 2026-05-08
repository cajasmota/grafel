package java

import "regexp"

// Spring Ecosystem advanced extractor: Security, Batch, AMQP, AI, Vault,
// Data, Cloud, Integration, WebFlux, Kafka.
// Ported from: spring_ecosystem_extractor.py

var springEcoFrameworks = map[string]bool{
	"spring_boot": true, "spring-boot": true, "springboot": true,
	"spring_mvc": true, "spring-mvc": true, "springmvc": true,
	"spring_webflux": true, "spring-webflux": true, "springwebflux": true,
}

var (
	// Security
	seSecurityFilterChainRE = regexp.MustCompile(
		`(?s)@Bean\b[^;{]*?\s+SecurityFilterChain\s+(\w+)\s*\(`)
	seUserDetailsServiceRE = regexp.MustCompile(
		`(?s)(?:public\s+)?class\s+(\w+)\s+implements\s+[^{]*\bUserDetailsService\b`)
	sePreAuthorizeRE = regexp.MustCompile(
		`(?s)@PreAuthorize\s*\(\s*"([^"]+)"\s*\)\s*(?:public|protected|private|)\s*` +
			`(?:static\s+)?(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

	// Batch
	seBatchJobRE   = regexp.MustCompile(`(?s)@Bean\b[^;{]*?\s+Job\s+(\w+)\s*\(`)
	seBatchStepRE  = regexp.MustCompile(`(?s)@Bean\b[^;{]*?\s+Step\s+(\w+)\s*\(`)
	seItemReaderRE = regexp.MustCompile(
		`(?s)(?:public\s+)?class\s+(\w+)\s+(?:extends|implements)\s+[^{]*\bItemReader\b`)
	seItemProcessorRE = regexp.MustCompile(
		`(?s)(?:public\s+)?class\s+(\w+)\s+(?:extends|implements)\s+[^{]*\bItemProcessor\b`)
	seItemWriterRE = regexp.MustCompile(
		`(?s)(?:public\s+)?class\s+(\w+)\s+(?:extends|implements)\s+[^{]*\bItemWriter\b`)

	// AMQP
	seRabbitListenerRE = regexp.MustCompile(
		`(?s)@RabbitListener\s*\([^)]*queues\s*=\s*(?:\{[^}]*\}|"([^"]+)")[^)]*\)\s*` +
			`(?:public|protected|private|)\s*(?:static\s+)?(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

	// Kafka
	seKafkaListenerRE = regexp.MustCompile(
		`(?s)@KafkaListener\s*\([^)]*topics\s*=\s*(?:\{[^}]*\}|"([^"]+)")[^)]*\)\s*` +
			`(?:public|protected|private|)\s*(?:static\s+)?(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

	// Cloud
	seFeignClientRE = regexp.MustCompile(
		`(?s)@FeignClient\s*\([^)]*(?:name|value)\s*=\s*"([^"]+)"[^)]*\)\s*` +
			`(?:public\s+)?interface\s+(\w+)`)

	// Data
	seMongoRepoRE = regexp.MustCompile(
		`(?s)(?:public\s+)?interface\s+(\w+)\s+extends\s+[^{]*\bMongoRepository\b`)
	seDocumentClassRE = regexp.MustCompile(
		`(?s)@Document\b[^{]*?(?:public\s+)?class\s+(\w+)`)
	seRedisHashClassRE = regexp.MustCompile(
		`(?s)@RedisHash\b[^{]*?(?:public\s+)?class\s+(\w+)`)
	seQueryMethodRE = regexp.MustCompile(
		`(?s)@Query\s*\(\s*(?:value\s*=\s*)?"([^"]+)"\s*\)\s*(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

	// Integration
	seServiceActivatorRE = regexp.MustCompile(
		`(?s)@ServiceActivator\s*\([^)]*inputChannel\s*=\s*"([^"]+)"[^)]*\)\s*` +
			`(?:public|protected|private|)\s*(?:static\s+)?(?:<[^>]*>\s*)?(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)
)

// ExtractSpringEcosystem runs the Spring ecosystem extractor.
func ExtractSpringEcosystem(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" || !springEcoFrameworks[ctx.Framework] {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// Security: SecurityFilterChain
	for _, m := range seSecurityFilterChainRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:pattern:spring_security_filter:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_SECURITY", Ref: ref,
			Properties: map[string]any{"framework": "spring_security"},
		})
	}

	// Security: UserDetailsService
	for _, m := range seUserDetailsServiceRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:component:spring_user_details_service:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_SECURITY", Ref: ref,
			Properties: map[string]any{"framework": "spring_security"},
		})
	}

	// Security: @PreAuthorize
	for _, m := range sePreAuthorizeRE.FindAllStringSubmatchIndex(source, -1) {
		expr := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]
		ref := "scope:pattern:spring_preauthorize:" + fp + ":" + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_SECURITY", Ref: ref,
			Properties: map[string]any{"expression": expr, "framework": "spring_security"},
		})
	}

	// Batch: Job beans
	for _, m := range seBatchJobRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:service:spring_batch_job:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Service", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_BATCH", Ref: ref,
			Properties: map[string]any{"batch_type": "job", "framework": "spring_batch"},
		})
	}

	// Batch: Step beans
	for _, m := range seBatchStepRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:operation:spring_batch_step:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_BATCH", Ref: ref,
			Properties: map[string]any{"batch_type": "step", "framework": "spring_batch"},
		})
	}

	// Batch: ItemReader/Processor/Writer
	for _, pair := range []struct {
		re   *regexp.Regexp
		kind string
	}{
		{seItemReaderRE, "reader"}, {seItemProcessorRE, "processor"}, {seItemWriterRE, "writer"},
	} {
		for _, m := range pair.re.FindAllStringSubmatchIndex(source, -1) {
			name := source[m[2]:m[3]]
			ref := "scope:component:spring_batch_" + pair.kind + ":" + fp + ":" + name
			addEntity(&result, seenRefs, SecondaryEntity{
				Name: name, Kind: "SCOPE.Component", SourceFile: fp,
				LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
				Provenance: "INFERRED_FROM_SPRING_BATCH", Ref: ref,
				Properties: map[string]any{"batch_type": pair.kind, "framework": "spring_batch"},
			})
		}
	}

	// AMQP: @RabbitListener
	for _, m := range seRabbitListenerRE.FindAllStringSubmatchIndex(source, -1) {
		queue := ""
		if m[2] >= 0 {
			queue = source[m[2]:m[3]]
		}
		methodName := source[m[4]:m[5]]
		ref := "scope:operation:spring_amqp_listener:" + fp + ":" + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_AMQP", Ref: ref,
			Properties: map[string]any{"queue": queue, "framework": "spring_amqp"},
		})
	}

	// Kafka: @KafkaListener
	for _, m := range seKafkaListenerRE.FindAllStringSubmatchIndex(source, -1) {
		topic := ""
		if m[2] >= 0 {
			topic = source[m[2]:m[3]]
		}
		methodName := source[m[4]:m[5]]
		ref := "scope:operation:spring_kafka_listener:" + fp + ":" + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_KAFKA", Ref: ref,
			Properties: map[string]any{"topic": topic, "framework": "spring_kafka"},
		})
	}

	// Cloud: @FeignClient
	for _, m := range seFeignClientRE.FindAllStringSubmatchIndex(source, -1) {
		serviceName := source[m[2]:m[3]]
		ifaceName := source[m[4]:m[5]]
		ref := "scope:component:spring_feign_client:" + fp + ":" + ifaceName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: ifaceName, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_CLOUD", Ref: ref,
			Properties: map[string]any{"service_name": serviceName, "framework": "spring_cloud"},
		})
		depRef := "scope:dependency:spring_cloud:" + fp + ":" + serviceName
		addRel(&result, seenRels, Relationship{
			SourceRef: ref, TargetRef: depRef, RelationshipType: "DEPENDS_ON",
			Properties: map[string]string{"kind": "feign_client", "service_name": serviceName},
		})
	}

	// Data: MongoRepository
	for _, m := range seMongoRepoRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:component:spring_data_mongo_repo:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_DATA", Ref: ref,
			Properties: map[string]any{"data_type": "mongo_repository", "framework": "spring_data"},
		})
	}

	// Data: @Document
	for _, m := range seDocumentClassRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:schema:spring_data_document:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Schema", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_DATA", Ref: ref,
			Properties: map[string]any{"data_type": "document", "framework": "spring_data"},
		})
	}

	// Data: @RedisHash
	for _, m := range seRedisHashClassRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:schema:spring_data_redis_hash:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Schema", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_DATA", Ref: ref,
			Properties: map[string]any{"data_type": "redis_hash", "framework": "spring_data"},
		})
	}

	// Data: @Query on repository methods
	for _, m := range seQueryMethodRE.FindAllStringSubmatchIndex(source, -1) {
		query := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]
		ref := "scope:operation:spring_data_query:" + fp + ":" + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_DATA", Ref: ref,
			Properties: map[string]any{"query": query, "framework": "spring_data"},
		})
	}

	// Integration: @ServiceActivator
	for _, m := range seServiceActivatorRE.FindAllStringSubmatchIndex(source, -1) {
		channel := source[m[2]:m[3]]
		methodName := source[m[4]:m[5]]
		ref := "scope:operation:spring_integration_activator:" + fp + ":" + methodName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: methodName, Kind: "SCOPE.Operation", Subtype: "function",
			SourceFile: fp,
			LineStart:  lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_INTEGRATION", Ref: ref,
			Properties: map[string]any{"channel": channel, "framework": "spring_integration"},
		})
	}

	_ = seenRels // used by FeignClient above
	return result
}
