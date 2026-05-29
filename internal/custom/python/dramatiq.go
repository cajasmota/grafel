package python

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("python_dramatiq", &dramatiqExtractor{})
}

// dramatiqExtractor detects dramatiq actors and producer call sites.
//
// Consumer pattern: @dramatiq.actor or @dramatiq.actor(...) above a def.
// Producer patterns: actor_var.send(...) and actor_var.send_with_options(...)
type dramatiqExtractor struct{}

func (e *dramatiqExtractor) Language() string { return "python_dramatiq" }

var (
	// @dramatiq.actor optionally followed by decorator arguments, then def funcName(
	dmActorDecoratorRe = regexp.MustCompile(
		`(?m)@dramatiq\.actor\s*(?:\([^)]*\))?\s*\n(?:\s*#[^\n]*\n)*\s*(?:async\s+)?def\s+(\w+)\s*\(`,
	)
	// actor.send(...) — the variable name before .send is captured as actor_ref
	dmSendRe = regexp.MustCompile(
		`(?m)(\w+)\.send\s*\(`,
	)
	// actor.send_with_options(...)
	dmSendWithOptionsRe = regexp.MustCompile(
		`(?m)(\w+)\.send_with_options\s*\(`,
	)
	// False-positive guard: skip generic non-dramatiq @actor decorators
	// (i.e. bare @actor without the dramatiq. prefix)
	dmBareActorRe = regexp.MustCompile(
		`(?m)^@actor\s*\n`,
	)

	// Broker binding: dramatiq.set_broker(SomeBroker(...)) or dramatiq.set_broker(var)
	// Captures the first identifier passed (broker class or variable).
	// Issue #3074.
	dmSetBrokerRe = regexp.MustCompile(
		`(?m)dramatiq\.set_broker\s*\(\s*([A-Za-z_][\w.]*)`)

	// Retry policy: max_retries=N inside @dramatiq.actor(...) decorator args.
	dmActorMaxRetriesRe = regexp.MustCompile(
		`@dramatiq\.actor\s*\([^)]*max_retries\s*=\s*(\d+)`)

	// Routing — queue_name on the @dramatiq.actor(...) decorator, paired with
	// the actor function it decorates. Captures (1) queue name, (2) func name.
	// Issue #3193 (task_routing).
	dmActorQueueNameRe = regexp.MustCompile(
		`(?m)@dramatiq\.actor\s*\([^)]*queue_name\s*=\s*["']([^"']+)["'][^)]*\)\s*\n(?:\s*#[^\n]*\n)*\s*(?:async\s+)?def\s+(\w+)\s*\(`)

	// Routing — explicit queue override at dispatch:
	// actor.send_with_options(queue_name="..."). Captures (1) actor ref,
	// (2) queue name. Issue #3193 (task_routing).
	dmSendQueueNameRe = regexp.MustCompile(
		`(?m)(\w+)\.send_with_options\s*\((?:[^()]|\([^()]*\))*?queue_name\s*=\s*["']([^"']+)["']`)
)

func (e *dramatiqExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_dramatiq")
	_, span := tracer.Start(ctx, "custom.python_dramatiq")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)
	var out []types.EntityRecord

	// 1. Consumer: @dramatiq.actor decorated function
	for _, idx := range allMatchesIndex(dmActorDecoratorRe, source) {
		funcName := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		taskID := "task:dramatiq:" + funcName
		out = append(out, entity(funcName, "SCOPE.Service", "task", file.Path, line,
			map[string]string{
				"framework":    "dramatiq",
				"pattern_type": "actor",
				"task_id":      taskID,
				"edge_kind":    "CONSUMES",
				"provenance":   "INFERRED_FROM_DRAMATIQ_ACTOR",
			}))
	}

	// 2. Producer: actor.send(...)
	for _, idx := range allMatchesIndex(dmSendRe, source) {
		actorRef := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		name := actorRef + ".send"
		out = append(out, entity(name, "SCOPE.Operation", "task_send", file.Path, line,
			map[string]string{
				"framework":    "dramatiq",
				"pattern_type": "send",
				"actor_ref":    actorRef,
				"task_id":      "task:dramatiq:" + actorRef,
				"edge_kind":    "PRODUCES",
				"provenance":   "INFERRED_FROM_DRAMATIQ_SEND",
			}))
	}

	// 3. Producer: actor.send_with_options(...)
	for _, idx := range allMatchesIndex(dmSendWithOptionsRe, source) {
		actorRef := source[idx[2]:idx[3]]
		line := lineOf(source, idx[0])
		name := actorRef + ".send_with_options"
		out = append(out, entity(name, "SCOPE.Operation", "task_send", file.Path, line,
			map[string]string{
				"framework":    "dramatiq",
				"pattern_type": "send_with_options",
				"actor_ref":    actorRef,
				"task_id":      "task:dramatiq:" + actorRef,
				"edge_kind":    "PRODUCES",
				"provenance":   "INFERRED_FROM_DRAMATIQ_SEND_WITH_OPTIONS",
			}))
	}

	// 4. Broker binding: dramatiq.set_broker(SomeBroker(...)) — Issue #3074.
	if m := dmSetBrokerRe.FindStringSubmatchIndex(source); m != nil {
		brokerClass := source[m[2]:m[3]]
		line := lineOf(source, m[0])
		out = append(out, entity("dramatiq.broker", "SCOPE.Config", "broker_binding", file.Path, line,
			map[string]string{
				"framework":    "dramatiq",
				"pattern_type": "broker_binding",
				"broker_class": brokerClass,
				"provenance":   "INFERRED_FROM_DRAMATIQ_SET_BROKER",
			}))
	}

	// 5. Retry policy: @dramatiq.actor(max_retries=N) — Issue #3074.
	for _, m := range dmActorMaxRetriesRe.FindAllStringSubmatchIndex(source, -1) {
		maxRetries := source[m[2]:m[3]]
		line := lineOf(source, m[0])
		out = append(out, entity("dramatiq.retry_policy", "SCOPE.Config", "retry_policy", file.Path, line,
			map[string]string{
				"framework":    "dramatiq",
				"pattern_type": "retry_policy",
				"max_retries":  maxRetries,
				"provenance":   "INFERRED_FROM_DRAMATIQ_ACTOR_MAX_RETRIES",
			}))
	}

	// 6. Routing: queue_name on @dramatiq.actor(...) decorator — maps a queue
	//    to the actor that consumes it. Issue #3193 (task_routing).
	for _, m := range dmActorQueueNameRe.FindAllStringSubmatchIndex(source, -1) {
		queueName := source[m[2]:m[3]]
		funcName := source[m[4]:m[5]]
		line := lineOf(source, m[0])
		out = append(out, entity(funcName, "SCOPE.Pattern", "task_routing", file.Path, line,
			map[string]string{
				"framework":    "dramatiq",
				"pattern_type": "actor_queue",
				"queue_name":   queueName,
				"actor":        funcName,
				"task_id":      "task:dramatiq:" + funcName,
				"edge_kind":    "ROUTES_TO",
				"provenance":   "INFERRED_FROM_DRAMATIQ_ACTOR_QUEUE_NAME",
			}))
	}

	// 7. Routing: explicit queue override at dispatch via
	//    actor.send_with_options(queue_name="..."). Issue #3193 (task_routing).
	for _, m := range dmSendQueueNameRe.FindAllStringSubmatchIndex(source, -1) {
		actorRef := source[m[2]:m[3]]
		queueName := source[m[4]:m[5]]
		line := lineOf(source, m[0])
		out = append(out, entity(actorRef+".send_with_options", "SCOPE.Pattern", "task_routing", file.Path, line,
			map[string]string{
				"framework":    "dramatiq",
				"pattern_type": "send_queue_override",
				"queue_name":   queueName,
				"actor_ref":    actorRef,
				"task_id":      "task:dramatiq:" + actorRef,
				"edge_kind":    "ROUTES_TO",
				"provenance":   "INFERRED_FROM_DRAMATIQ_SEND_WITH_OPTIONS_QUEUE_NAME",
			}))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
