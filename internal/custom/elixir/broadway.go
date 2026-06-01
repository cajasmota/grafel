package elixir

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
	extractor.Register("custom_elixir_broadway", &broadwayExtractor{})
}

// broadwayExtractor recognises Broadway (https://hexdocs.pm/broadway) data
// pipelines. A Broadway pipeline is a module that does `use Broadway` and wires
// a producer in its `start_link/1` via `producer: [module: {ProducerMod, opts}]`.
// The producer module determines the message-broker ingress (Kafka topic, SQS
// queue, RabbitMQ queue, …); `handle_message/3` and `handle_batch/4` are the
// pipeline stages that process each ingested message (the flow roots).
//
// Emitted entities:
//   - SCOPE.Component/pipeline  : the `use Broadway` module
//   - SCOPE.MessageTopic        : the ingress topic/queue parsed from the
//     producer module + its `topics:`/`queue:`/`queue_url:` option
//   - SCOPE.Operation/handler   : handle_message/3 and handle_batch/4 stages
type broadwayExtractor struct{}

func (e *broadwayExtractor) Language() string { return "custom_elixir_broadway" }

var (
	reBroadwayUse = regexp.MustCompile(`(?m)^\s*use\s+Broadway\b`)
	// producer: [module: {BroadwayKafka.Producer, ...}] — capture the producer
	// module reference. Handles both `module: {Mod, opts}` and `module: Mod`.
	reBroadwayProducerModule = regexp.MustCompile(
		`module:\s*\{?\s*([A-Z][\w.]+)`,
	)
	// topics: ["orders", "payments"] — Kafka producer subscription list.
	reBroadwayKafkaTopics = regexp.MustCompile(
		`topics:\s*\[([^\]]*)\]`,
	)
	// topic: "orders" — single-topic producers (e.g. some OffBroadway adapters).
	reBroadwayKafkaTopic = regexp.MustCompile(
		`\btopic:\s*"([^"]+)"`,
	)
	// queue: "events" — SQS / RabbitMQ off_broadway producers.
	reBroadwaySQSQueue = regexp.MustCompile(
		`\bqueue:\s*"([^"]+)"`,
	)
	// queue_url: "https://sqs..../my-queue" — Broadway SQS producer.
	reBroadwayQueueURL = regexp.MustCompile(
		`\bqueue_url:\s*"([^"]+)"`,
	)
	// def handle_message(_processor, message, _context) / def handle_batch(...)
	reBroadwayHandler = regexp.MustCompile(
		`(?m)^\s*def\s+(handle_message|handle_batch)\s*\(`,
	)
	// A quoted string member of a list, e.g. `"orders"`.
	reQuotedMember = regexp.MustCompile(`"([^"]+)"`)
)

// broadwayProducerKind maps a Broadway producer module to a normalised broker
// name used as the MessageTopic `broker` property.
func broadwayProducerKind(mod string) string {
	lower := strings.ToLower(mod)
	switch {
	case strings.Contains(lower, "kafka"):
		return "kafka"
	case strings.Contains(lower, "sqs"):
		return "sqs"
	case strings.Contains(lower, "rabbitmq") || strings.Contains(lower, "amqp"):
		return "rabbitmq"
	case strings.Contains(lower, "pubsub") || strings.Contains(lower, "googlecloud"):
		return "gcp-pubsub"
	case strings.Contains(lower, "kinesis"):
		return "kinesis"
	case strings.Contains(lower, "dummy") || strings.Contains(lower, "test"):
		return "in-memory"
	default:
		return "unknown"
	}
}

func (e *broadwayExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/elixir")
	_, span := tracer.Start(ctx, "indexer.broadway_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "broadway"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "elixir" {
		return nil, nil
	}

	src := string(file.Content)

	// Gate: only files that actually `use Broadway` are pipelines.
	useLoc := reBroadwayUse.FindStringIndex(src)
	if useLoc == nil {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// 1. The pipeline module itself.
	moduleName := "BroadwayPipeline"
	if cm := rePhoenixModuleDecl.FindAllStringSubmatch(src[:useLoc[0]], -1); len(cm) > 0 {
		moduleName = cm[len(cm)-1][1]
	}
	pipeEnt := makeEntity(moduleName, "SCOPE.Component", "pipeline", file.Path, file.Language, lineOf(src, useLoc[0]))
	setProps(&pipeEnt, "framework", "broadway",
		"provenance", "INFERRED_FROM_BROADWAY_USE",
		"pipeline", "true")

	// 2. Producer module + ingress topic/queue.
	var producerMod, broker string
	if pm := reBroadwayProducerModule.FindStringSubmatch(src); pm != nil {
		producerMod = pm[1]
		broker = broadwayProducerKind(producerMod)
		setProps(&pipeEnt, "producer_module", producerMod, "broker", broker)
	}
	add(pipeEnt)

	// Collect ingress sources from the producer options.
	var ingress []struct{ name, kind string }
	for _, m := range reBroadwayKafkaTopics.FindAllStringSubmatch(src, -1) {
		for _, q := range reQuotedMember.FindAllStringSubmatch(m[1], -1) {
			ingress = append(ingress, struct{ name, kind string }{q[1], "topic"})
		}
	}
	for _, m := range reBroadwayKafkaTopic.FindAllStringSubmatch(src, -1) {
		ingress = append(ingress, struct{ name, kind string }{m[1], "topic"})
	}
	for _, m := range reBroadwaySQSQueue.FindAllStringSubmatch(src, -1) {
		ingress = append(ingress, struct{ name, kind string }{m[1], "queue"})
	}
	for _, m := range reBroadwayQueueURL.FindAllStringSubmatch(src, -1) {
		// Use the last path segment of a queue URL as the queue name.
		name := m[1]
		if idx := strings.LastIndex(name, "/"); idx >= 0 && idx < len(name)-1 {
			name = name[idx+1:]
		}
		ingress = append(ingress, struct{ name, kind string }{name, "queue"})
	}

	if broker == "" && len(ingress) > 0 {
		broker = "unknown"
	}
	for _, in := range ingress {
		topEnt := makeEntity(in.name, "SCOPE.MessageTopic", in.kind, file.Path, file.Language, lineOf(src, useLoc[0]))
		setProps(&topEnt, "framework", "broadway",
			"provenance", "INFERRED_FROM_BROADWAY_PRODUCER",
			"broker", broker,
			"topic", in.name,
			"ingress", "true",
			"pipeline", moduleName)
		if producerMod != "" {
			setProps(&topEnt, "producer_module", producerMod)
		}
		add(topEnt)
	}

	// 3. Pipeline stage handlers (flow roots).
	for _, m := range reBroadwayHandler.FindAllStringSubmatchIndex(src, -1) {
		handler := src[m[2]:m[3]]
		ent := makeEntity(moduleName+"."+handler, "SCOPE.Operation", "handler", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "broadway",
			"provenance", "INFERRED_FROM_BROADWAY_HANDLER",
			"handler_type", handler,
			"pipeline", moduleName,
			"flow_root", "true")
		if broker != "" {
			setProps(&ent, "broker", broker)
		}
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
