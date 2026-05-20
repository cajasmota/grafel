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

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
