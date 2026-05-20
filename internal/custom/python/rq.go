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
	extractor.Register("python_rq", &rqExtractor{})
}

// rqExtractor detects Redis Queue (RQ) producer and consumer patterns.
//
// Producer: queue.enqueue(func, ...) and queue.enqueue_call(func="module.fn", ...)
// Consumer: Worker([queue, ...]) instantiation; the callable passed to enqueue is the consumer.
type rqExtractor struct{}

func (e *rqExtractor) Language() string { return "python_rq" }

var (
	// queue.enqueue(callable, ...) — captures the queue variable and the callable arg
	rqEnqueueRe = regexp.MustCompile(
		`(?m)(\w+)\.enqueue\s*\(\s*([A-Za-z_][\w.]*)`,
	)
	// queue.enqueue_call(func="module.fn") or func=some_callable
	rqEnqueueCallStrRe = regexp.MustCompile(
		`(?m)(\w+)\.enqueue_call\s*\([^)]*func\s*=\s*["']([^"']+)["']`,
	)
	rqEnqueueCallRefRe = regexp.MustCompile(
		`(?m)(\w+)\.enqueue_call\s*\([^)]*func\s*=\s*([A-Za-z_][\w.]*)`,
	)
	// Worker([queues]) — RQ worker declaration
	rqWorkerRe = regexp.MustCompile(
		`(?m)\bWorker\s*\(\s*\[([^\]]*)\]`,
	)
	// False-positive guard: Queue class that is NOT rq.Queue — we require the
	// pattern "queue.enqueue" which is generic enough but the worker guard is
	// tied to RQ's Worker class. We emit workers only when Worker is imported
	// from rq or when rq is imported at the top of the file.
	rqImportRe = regexp.MustCompile(
		`(?m)(?:from\s+rq\b|import\s+rq\b)`,
	)
)

func (e *rqExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("custom.python_rq")
	_, span := tracer.Start(ctx, "custom.python_rq")
	defer span.End()
	span.SetAttributes(attribute.String("file", file.Path))

	if len(file.Content) == 0 {
		return nil, nil
	}

	source := string(file.Content)

	// Only emit worker entities when rq is imported (reduces false positives
	// from generic Queue/Worker class names in non-RQ code).
	hasRQImport := rqImportRe.MatchString(source)

	var out []types.EntityRecord

	// 1. Producer: queue.enqueue(callable, ...)
	for _, idx := range allMatchesIndex(rqEnqueueRe, source) {
		queueVar := source[idx[2]:idx[3]]
		callable := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		taskID := "task:rq:" + callable
		out = append(out, entity(callable+".enqueue", "SCOPE.Operation", "task_enqueue", file.Path, line,
			map[string]string{
				"framework":    "rq",
				"pattern_type": "enqueue",
				"queue_var":    queueVar,
				"callable":     callable,
				"task_id":      taskID,
				"edge_kind":    "PRODUCES",
				"provenance":   "INFERRED_FROM_RQ_ENQUEUE",
			}))
	}

	// 2. Producer: queue.enqueue_call(func="module.fn")
	for _, idx := range allMatchesIndex(rqEnqueueCallStrRe, source) {
		queueVar := source[idx[2]:idx[3]]
		fnName := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		taskID := "task:rq:" + fnName
		out = append(out, entity(fnName+".enqueue_call", "SCOPE.Operation", "task_enqueue", file.Path, line,
			map[string]string{
				"framework":    "rq",
				"pattern_type": "enqueue_call",
				"queue_var":    queueVar,
				"callable":     fnName,
				"task_id":      taskID,
				"edge_kind":    "PRODUCES",
				"provenance":   "INFERRED_FROM_RQ_ENQUEUE_CALL",
			}))
	}

	// 3. Producer: queue.enqueue_call(func=callable_ref)
	for _, idx := range allMatchesIndex(rqEnqueueCallRefRe, source) {
		queueVar := source[idx[2]:idx[3]]
		callable := source[idx[4]:idx[5]]
		line := lineOf(source, idx[0])
		taskID := "task:rq:" + callable
		out = append(out, entity(callable+".enqueue_call", "SCOPE.Operation", "task_enqueue", file.Path, line,
			map[string]string{
				"framework":    "rq",
				"pattern_type": "enqueue_call",
				"queue_var":    queueVar,
				"callable":     callable,
				"task_id":      taskID,
				"edge_kind":    "PRODUCES",
				"provenance":   "INFERRED_FROM_RQ_ENQUEUE_CALL_REF",
			}))
	}

	// 4. Consumer: Worker([queues]) — only when rq is in scope
	if hasRQImport {
		for _, idx := range allMatchesIndex(rqWorkerRe, source) {
			queues := source[idx[2]:idx[3]]
			line := lineOf(source, idx[0])
			out = append(out, entity("Worker("+queues+")", "SCOPE.Service", "worker", file.Path, line,
				map[string]string{
					"framework":    "rq",
					"pattern_type": "worker",
					"queues":       queues,
					"edge_kind":    "CONSUMES",
					"provenance":   "INFERRED_FROM_RQ_WORKER",
				}))
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
