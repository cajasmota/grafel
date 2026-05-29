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
	// Spring Data Cassandra: @Table marks a Cassandra entity (distinct from JPA @Table;
	// disambiguated by presence of CassandraRepository imports or @PrimaryKey annotation).
	seCassandraRepoRE = regexp.MustCompile(
		`(?s)(?:public\s+)?interface\s+(\w+)\s+extends\s+[^{]*\bCassandraRepository\b`)
	seCassandraTableClassRE = regexp.MustCompile(
		`(?s)@Table\b[^{]*?(?:public\s+)?class\s+(\w+)[^{]*\{[^}]*@PrimaryKey\b`)
	seElasticRepoRE = regexp.MustCompile(
		`(?s)(?:public\s+)?interface\s+(\w+)\s+extends\s+[^{]*\bElasticsearchRepository\b`)
	seQueryMethodRE = regexp.MustCompile(
		`(?s)@Query\s*\(\s*(?:value\s*=\s*)?"([^"]+)"\s*\)\s*(?:\w+(?:\s*<[^>]*>)?\s+)(\w+)\s*\(`)

	// AutoConfiguration
	// Matches @EnableAutoConfiguration, @SpringBootApplication, @AutoConfiguration on a class.
	seAutoConfigClassRE = regexp.MustCompile(
		`(?s)(@(?:EnableAutoConfiguration|SpringBootApplication|AutoConfiguration))\b[^{]*?` +
			`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)
	// Matches @ConditionalOn* followed by a class declaration.
	seConditionalOnClassRE = regexp.MustCompile(
		`(?s)(@ConditionalOn\w+)(?:\([^)]*\))?\s*(?:@\w+(?:\([^)]*\))?\s*)*` +
			`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

	// Profile detection: @Profile("expr") on a class.
	seProfileAnnotationRE = regexp.MustCompile(
		`(?s)@Profile\s*\(\s*(?:\{[^}]*\}|"([^"]+)")\s*\)\s*` +
			`(?:@\w+(?:\([^)]*\))?\s*)*` +
			`(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)`)

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

	// Data: CassandraRepository
	for _, m := range seCassandraRepoRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:component:spring_data_cassandra_repo:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_DATA_CASSANDRA", Ref: ref,
			Properties: map[string]any{"data_type": "cassandra_repository", "framework": "spring_data_cassandra"},
		})
	}

	// Data: @Table (Spring Data Cassandra — disambiguated by @PrimaryKey body presence)
	for _, m := range seCassandraTableClassRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:schema:spring_data_cassandra_table:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Schema", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_DATA_CASSANDRA", Ref: ref,
			Properties: map[string]any{"data_type": "cassandra_table", "framework": "spring_data_cassandra"},
		})
	}

	// Data: ElasticsearchRepository
	for _, m := range seElasticRepoRE.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		ref := "scope:component:spring_data_elastic_repo:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Component", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_DATA_ELASTIC", Ref: ref,
			Properties: map[string]any{"data_type": "elasticsearch_repository", "framework": "spring_data_elastic"},
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

	// AutoConfiguration: @EnableAutoConfiguration / @SpringBootApplication / @AutoConfiguration / @ConditionalOn*
	for _, m := range seAutoConfigClassRE.FindAllStringSubmatchIndex(source, -1) {
		annName := source[m[2]:m[3]]
		className := source[m[4]:m[5]]
		ref := "scope:pattern:spring_autoconfig_class:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_AUTOCONFIG", Ref: ref,
			Properties: map[string]any{"autoconfig_annotation": annName, "framework": "spring_boot"},
		})
	}
	for _, m := range seConditionalOnClassRE.FindAllStringSubmatchIndex(source, -1) {
		annText := source[m[2]:m[3]]
		className := source[m[4]:m[5]]
		ref := "scope:pattern:spring_conditional:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_AUTOCONFIG", Ref: ref,
			Properties: map[string]any{"conditional_annotation": annText, "framework": "spring_boot"},
		})
	}

	// Profile detection: @Profile("name") on classes
	for _, m := range seProfileAnnotationRE.FindAllStringSubmatchIndex(source, -1) {
		profileExpr := ""
		if m[2] >= 0 {
			profileExpr = source[m[2]:m[3]]
		}
		if m[4] < 0 {
			continue // no class name captured
		}
		targetName := source[m[4]:m[5]]
		ref := "scope:pattern:spring_profile:" + fp + ":" + targetName
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: targetName, Kind: "SCOPE.Pattern", SourceFile: fp,
			LineStart: lineOf(source, m[0]), LineEnd: lineOf(source, m[0]),
			Provenance: "INFERRED_FROM_SPRING_PROFILE", Ref: ref,
			Properties: map[string]any{"profile": profileExpr, "framework": "spring_boot"},
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

	// JPA FK + lazy-loading for Spring Data JPA / Spring Boot entities.
	// ExtractSpringEcosystem fires for spring_boot / spring_mvc / spring_webflux
	// frameworks, which also own Spring Data JPA @Entity classes that carry
	// @JoinColumn / FetchType annotations. We resolve the owning class via the
	// general class-declaration scanner.
	seOwnerFn := func(offset int) string {
		return findEnclosingClass(source, offset)
	}
	seFKResult := ExtractJPAFKAndLazy(source, seOwnerFn)
	emitJPAFKLazy(seFKResult, fp, "java", "spring_data_jpa", &result.Entities, seenRefs)

	_ = seenRels // used by FeignClient above
	return result
}
