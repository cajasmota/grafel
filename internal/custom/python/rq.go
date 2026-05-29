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

	// Broker binding: Redis connection used as RQ's broker — Issue #3074.
	// Captures forms:
	//   conn = Redis(host="localhost", port=6379)
	//   Redis(host="redis", port=6379, db=0)
	//   StrictRedis(host=..., port=...)
	//   from_url("redis://...")
	rqRedisConnRe = regexp.MustCompile(
		`(?m)(?:StrictRedis|Redis)\s*\(\s*(?:host\s*=\s*["']([^"']+)["']|url\s*=\s*["']([^"']+)["'])`)
	rqRedisFromURLRe = regexp.MustCompile(
		`(?m)(?:StrictRedis|Redis)\.from_url\s*\(\s*["']([^"']+)["']`)

	// Retry policy: Retry(max=N) or job.retry(max=N) — Issue #3074.
	rqRetryRe = regexp.MustCompile(
		`(?m)(?:Retry|retry)\s*\(\s*max\s*=\s*(\d+)`)

	// Schedule extraction: scheduler.enqueue_at / enqueue_in / cron — Issue #3074.
	// rq-scheduler patterns.
	rqScheduleEnqueueAtRe = regexp.MustCompile(
		`(?m)(?:\w+)\.enqueue_at\s*\(`)
	rqScheduleEnqueueInRe = regexp.MustCompile(
		`(?m)(?:\w+)\.enqueue_in\s*\(`)
	rqScheduleCronRe = regexp.MustCompile(
		`(?m)(?:\w+)\.cron\s*\(\s*["']([^"']+)["']`)
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

	// 5. Broker binding: Redis(...) / StrictRedis(...) connection — Issue #3074.
	// RQ uses Redis directly as both broker and result store; we detect the
	// connection instantiation and emit a broker_binding Config entity.
	if hasRQImport {
		brokerEmitted := false
		if m := rqRedisFromURLRe.FindStringSubmatchIndex(source); m != nil {
			redisURL := source[m[2]:m[3]]
			line := lineOf(source, m[0])
			out = append(out, entity("rq.redis_conn", "SCOPE.Config", "broker_binding", file.Path, line,
				map[string]string{
					"framework":    "rq",
					"pattern_type": "broker_binding",
					"redis_url":    redisURL,
					"provenance":   "INFERRED_FROM_RQ_REDIS_FROM_URL",
				}))
			brokerEmitted = true
		}
		if !brokerEmitted {
			if m := rqRedisConnRe.FindStringSubmatchIndex(source); m != nil {
				host := ""
				if m[2] != -1 {
					host = source[m[2]:m[3]]
				} else if m[4] != -1 {
					host = source[m[4]:m[5]]
				}
				line := lineOf(source, m[0])
				out = append(out, entity("rq.redis_conn", "SCOPE.Config", "broker_binding", file.Path, line,
					map[string]string{
						"framework":    "rq",
						"pattern_type": "broker_binding",
						"redis_host":   host,
						"provenance":   "INFERRED_FROM_RQ_REDIS_CONN",
					}))
			}
		}
	}

	// 6. Retry policy: Retry(max=N) — Issue #3074.
	for _, m := range rqRetryRe.FindAllStringSubmatchIndex(source, -1) {
		maxRetries := source[m[2]:m[3]]
		line := lineOf(source, m[0])
		out = append(out, entity("rq.retry_policy", "SCOPE.Config", "retry_policy", file.Path, line,
			map[string]string{
				"framework":    "rq",
				"pattern_type": "retry_policy",
				"max_retries":  maxRetries,
				"provenance":   "INFERRED_FROM_RQ_RETRY",
			}))
	}

	// 7. Schedule extraction: rq-scheduler enqueue_at / enqueue_in / cron — Issue #3074.
	for _, m := range rqScheduleEnqueueAtRe.FindAllStringSubmatchIndex(source, -1) {
		line := lineOf(source, m[0])
		out = append(out, entity("rq.schedule_enqueue_at", "SCOPE.Pattern", "scheduled_job", file.Path, line,
			map[string]string{
				"framework":     "rq",
				"pattern_type":  "schedule_extraction",
				"schedule_type": "enqueue_at",
				"provenance":    "INFERRED_FROM_RQ_ENQUEUE_AT",
			}))
	}
	for _, m := range rqScheduleEnqueueInRe.FindAllStringSubmatchIndex(source, -1) {
		line := lineOf(source, m[0])
		out = append(out, entity("rq.schedule_enqueue_in", "SCOPE.Pattern", "scheduled_job", file.Path, line,
			map[string]string{
				"framework":     "rq",
				"pattern_type":  "schedule_extraction",
				"schedule_type": "enqueue_in",
				"provenance":    "INFERRED_FROM_RQ_ENQUEUE_IN",
			}))
	}
	for _, m := range rqScheduleCronRe.FindAllStringSubmatchIndex(source, -1) {
		cronExpr := source[m[2]:m[3]]
		line := lineOf(source, m[0])
		out = append(out, entity("rq.schedule_cron("+cronExpr+")", "SCOPE.Pattern", "scheduled_job", file.Path, line,
			map[string]string{
				"framework":     "rq",
				"pattern_type":  "schedule_extraction",
				"schedule_type": "cron",
				"cron_expr":     cronExpr,
				"provenance":    "INFERRED_FROM_RQ_CRON",
			}))
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
