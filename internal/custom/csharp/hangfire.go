package csharp

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_csharp_hangfire", &hangfireExtractor{})
}

// hangfireExtractor detects Hangfire background-job producer and consumer patterns.
//
// Producers:
//   - BackgroundJob.Enqueue(() => X.Method())
//   - BackgroundJob.Enqueue<T>(x => x.Method())
//   - RecurringJob.AddOrUpdate("id", () => X.Method(), Cron.*)
//   - BackgroundJob.Schedule(() => X.Method(), delay)
//
// Consumers: classes with an Execute(IJobCancellationToken) method, or
// methods decorated with [AutomaticRetry].
type hangfireExtractor struct{}

func (e *hangfireExtractor) Language() string { return "custom_csharp_hangfire" }

var (
	// BackgroundJob.Enqueue(() => TypeName.MethodName(...)) — captures TypeName and MethodName
	hfEnqueueStaticRe = regexp.MustCompile(
		`BackgroundJob\.Enqueue\s*\(\s*\(\s*\)\s*=>\s*(\w+)\.(\w+)\s*\(`,
	)
	// BackgroundJob.Enqueue<TypeName>(x => x.MethodName(...)) — typed lambda
	hfEnqueueTypedRe = regexp.MustCompile(
		`BackgroundJob\.Enqueue\s*<\s*(\w+)\s*>\s*\(\s*\w+\s*=>\s*\w+\.(\w+)\s*\(`,
	)
	// RecurringJob.AddOrUpdate("job-id", () => TypeName.MethodName(...), Cron...)
	hfRecurringStaticRe = regexp.MustCompile(
		`RecurringJob\.AddOrUpdate\s*\(\s*["']([^"']+)["']\s*,\s*\(\s*\)\s*=>\s*(\w+)\.(\w+)\s*\(`,
	)
	// RecurringJob.AddOrUpdate<TypeName>("job-id", x => x.MethodName(...), Cron...)
	hfRecurringTypedRe = regexp.MustCompile(
		`RecurringJob\.AddOrUpdate\s*<\s*(\w+)\s*>\s*\(\s*["']([^"']+)["']\s*,\s*\w+\s*=>\s*\w+\.(\w+)\s*\(`,
	)
	// BackgroundJob.Schedule(() => TypeName.MethodName(...), ...)
	hfScheduleStaticRe = regexp.MustCompile(
		`BackgroundJob\.Schedule\s*\(\s*\(\s*\)\s*=>\s*(\w+)\.(\w+)\s*\(`,
	)
	// [AutomaticRetry] attribute — marks a consumer class or method
	hfAutoRetryRe = regexp.MustCompile(
		`\[AutomaticRetry(?:\([^)]*\))?\]`,
	)
	// class ClassName that implements IJob or has Execute(...) method signature
	hfJobClassRe = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|sealed)\s+)?class\s+(\w+)\s*(?::\s*[\w,\s<>]+)?\{[^}]*\bExecute\s*\(`,
	)
	// Explicit IBackgroundJob<T> or IJob interface implementation
	hfIJobImplRe = regexp.MustCompile(
		`(?m)class\s+(\w+)\s*:\s*[^{]*\bI(?:Background)?Job\b`,
	)
)

func (e *hangfireExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.hangfire_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "hangfire"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)
	var out []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name + ":" + ent.Subtype
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, ent)
	}

	// 1. BackgroundJob.Enqueue(() => TypeName.Method())
	for _, idx := range hfEnqueueStaticRe.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[idx[2]:idx[3]]
		methodName := src[idx[4]:idx[5]]
		line := lineOf(src, idx[0])
		taskID := "task:hangfire:" + typeName + "." + methodName
		ent := makeEntity(typeName+"."+methodName, "SCOPE.Operation", "task_enqueue", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "hangfire",
			"pattern_type", "enqueue",
			"job_type", typeName,
			"job_method", methodName,
			"task_id", taskID,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_HANGFIRE_ENQUEUE",
		)
		add(ent)
	}

	// 2. BackgroundJob.Enqueue<TypeName>(x => x.Method())
	for _, idx := range hfEnqueueTypedRe.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[idx[2]:idx[3]]
		methodName := src[idx[4]:idx[5]]
		line := lineOf(src, idx[0])
		taskID := "task:hangfire:" + typeName + "." + methodName
		ent := makeEntity(typeName+"."+methodName, "SCOPE.Operation", "task_enqueue", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "hangfire",
			"pattern_type", "enqueue_typed",
			"job_type", typeName,
			"job_method", methodName,
			"task_id", taskID,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_HANGFIRE_ENQUEUE_TYPED",
		)
		add(ent)
	}

	// 3. RecurringJob.AddOrUpdate("id", () => TypeName.Method(), Cron...)
	for _, idx := range hfRecurringStaticRe.FindAllStringSubmatchIndex(src, -1) {
		jobID := src[idx[2]:idx[3]]
		typeName := src[idx[4]:idx[5]]
		methodName := src[idx[6]:idx[7]]
		line := lineOf(src, idx[0])
		taskID := "task:hangfire:recurring:" + jobID
		ent := makeEntity(jobID, "SCOPE.Pattern", "recurring_job", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "hangfire",
			"pattern_type", "recurring",
			"job_type", typeName,
			"job_method", methodName,
			"task_id", taskID,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_HANGFIRE_RECURRING",
		)
		add(ent)
	}

	// 4. RecurringJob.AddOrUpdate<TypeName>("id", x => x.Method(), Cron...)
	for _, idx := range hfRecurringTypedRe.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[idx[2]:idx[3]]
		jobID := src[idx[4]:idx[5]]
		methodName := src[idx[6]:idx[7]]
		line := lineOf(src, idx[0])
		taskID := "task:hangfire:recurring:" + jobID
		ent := makeEntity(jobID, "SCOPE.Pattern", "recurring_job", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "hangfire",
			"pattern_type", "recurring_typed",
			"job_type", typeName,
			"job_method", methodName,
			"task_id", taskID,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_HANGFIRE_RECURRING_TYPED",
		)
		add(ent)
	}

	// 5. BackgroundJob.Schedule(() => TypeName.Method(), ...)
	for _, idx := range hfScheduleStaticRe.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[idx[2]:idx[3]]
		methodName := src[idx[4]:idx[5]]
		line := lineOf(src, idx[0])
		taskID := "task:hangfire:" + typeName + "." + methodName
		ent := makeEntity(typeName+"."+methodName, "SCOPE.Operation", "task_schedule", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "hangfire",
			"pattern_type", "schedule",
			"job_type", typeName,
			"job_method", methodName,
			"task_id", taskID,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_HANGFIRE_SCHEDULE",
		)
		add(ent)
	}

	// 6. Consumer: class implementing IJob / IBackgroundJob
	for _, idx := range hfIJobImplRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		taskID := "task:hangfire:" + className + ".Execute"
		ent := makeEntity(className, "SCOPE.Service", "job_class", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "hangfire",
			"pattern_type", "job_class",
			"task_id", taskID,
			"edge_kind", "CONSUMES",
			"provenance", "INFERRED_FROM_HANGFIRE_IJOB",
		)
		add(ent)
	}

	// 7. Consumer: [AutomaticRetry] decorated class/method
	for _, idx := range hfAutoRetryRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, idx[0])
		ent := makeEntity("AutomaticRetry@line"+intStr(line), "SCOPE.Pattern", "retry_policy", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "hangfire",
			"pattern_type", "automatic_retry",
			"edge_kind", "CONSUMES",
			"provenance", "INFERRED_FROM_HANGFIRE_AUTOMATIC_RETRY",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
