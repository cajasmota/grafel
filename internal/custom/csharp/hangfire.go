package csharp

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

	// Dynamic / non-literal RecurringJob.AddOrUpdate — the job-id and/or lambda
	// body cannot be statically resolved (captured variable id, method-group, or
	// a lambda body that is not the simple `Type.Method(` / `x => x.Method(` shape).
	// Matched only after the literal recurring patterns have had their chance, so
	// these stay an honest unresolved producer rather than silently dropping.
	hfRecurringAnyRe = regexp.MustCompile(
		`(?s)RecurringJob\.AddOrUpdate\s*(?:<\s*\w+\s*>\s*)?\(`,
	)
	// Dynamic / non-literal BackgroundJob.Enqueue / Schedule — same idea: the call
	// exists but the target method is not a resolvable literal lambda.
	hfEnqueueAnyRe = regexp.MustCompile(
		`(?s)BackgroundJob\.(Enqueue|Schedule|ContinueJobWith|ContinueWith)\s*(?:<\s*\w+\s*>\s*)?\(`,
	)

	// Hangfire Cron.* fluent helpers, e.g. Cron.Daily, Cron.Hourly,
	// Cron.Minutely, Cron.MinuteInterval(5), Cron.Weekly(DayOfWeek.Monday, 3).
	hfCronHelperRe = regexp.MustCompile(
		`Cron\.(\w+)\s*(?:\(\s*([^)]*)\s*\))?`,
	)
	// A raw 5- or 6-field cron string literal, e.g. "0 12 * * *".
	hfCronRawRe = regexp.MustCompile(
		`["']((?:[\d*/,\-?A-Za-z]+\s+){4,5}[\d*/,\-?A-Za-z]+)["']`,
	)
)

// hfCronHelperExpr maps a Hangfire `Cron.*` helper (and optional first arg) to
// its canonical NCrontab expression (5-field: minute hour day-of-month month
// day-of-week), mirroring the Hangfire.Cron static helpers. Unknown helpers and
// interval helpers (which depend on runtime args) return an empty string and a
// best-effort schedule label so the node stays honest.
func hfCronHelperExpr(helper, arg string) (expr, label string) {
	switch helper {
	case "Never":
		return "", "never"
	case "Yearly", "Monthly", "Weekly", "Daily", "Hourly", "Minutely":
		// Default (no-arg) forms have fixed canonical expressions. The
		// argument-bearing overloads shift the fixed fields, which we can't
		// resolve from non-literal args, so we only emit the canonical default.
		if strings.TrimSpace(arg) != "" {
			return "", strings.ToLower(helper)
		}
		switch helper {
		case "Yearly":
			return "0 0 1 1 *", "yearly"
		case "Monthly":
			return "0 0 1 * *", "monthly"
		case "Weekly":
			return "0 0 * * 0", "weekly"
		case "Daily":
			return "0 0 * * *", "daily"
		case "Hourly":
			return "0 * * * *", "hourly"
		case "Minutely":
			return "* * * * *", "minutely"
		}
	case "MinuteInterval", "HourInterval", "DayInterval", "MonthInterval":
		// Interval helpers expand from a runtime count; record the label only.
		return "", "interval"
	}
	return "", ""
}

// hfParseSchedule extracts a Hangfire cron expression and schedule label from
// the trailing schedule argument of a RecurringJob.AddOrUpdate call (the slice
// of source after the lambda body, bounded to the statement's closing paren).
func hfParseSchedule(tail string) (cronExpr, scheduleType string) {
	if m := hfCronHelperRe.FindStringSubmatch(tail); m != nil {
		expr, label := hfCronHelperExpr(m[1], m[2])
		if expr != "" {
			return expr, "cron"
		}
		if label != "" {
			return "", label
		}
	}
	if m := hfCronRawRe.FindStringSubmatch(tail); m != nil {
		return m[1], "cron"
	}
	return "", ""
}

// hfApplySchedule parses the schedule argument from tail and, when found, stamps
// cron_expression / schedule_type onto the entity.
func hfApplySchedule(e *types.EntityRecord, tail string) {
	cronExpr, scheduleType := hfParseSchedule(tail)
	if scheduleType != "" {
		setProps(e, "schedule_type", scheduleType)
	}
	if cronExpr != "" {
		setProps(e, "cron_expression", cronExpr)
	}
}

// hfStatementTail returns the source from offset up to the next ';' (or end of
// source), so cron/id parsing for one call doesn't bleed into a later statement.
func hfStatementTail(src string, offset int) string {
	if offset >= len(src) {
		return ""
	}
	rest := src[offset:]
	if semi := strings.IndexByte(rest, ';'); semi >= 0 {
		return rest[:semi]
	}
	return rest
}

// hfFirstStringArg returns the first single/double-quoted string literal in tail,
// used as the job-id for dynamic recurring calls when the lambda is non-literal
// but the id itself is a literal.
func hfFirstStringArg(tail string) string {
	// The job-id precedes the lambda; bound the search to before the first
	// "=>" so a trailing raw-cron string literal isn't mistaken for the id.
	scope := tail
	if arrow := strings.Index(scope, "=>"); arrow >= 0 {
		scope = scope[:arrow]
	}
	if m := hfFirstStringRe.FindStringSubmatch(scope); m != nil {
		return m[1]
	}
	return ""
}

var hfFirstStringRe = regexp.MustCompile(`["']([^"']+)["']`)

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
	// resolvedCalls records the source offset of every RecurringJob/BackgroundJob
	// call that a literal pattern resolved, so the dynamic fallback (sections 8/9)
	// only fires for genuinely non-literal call-sites.
	resolvedCalls := make(map[int]bool)

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
		resolvedCalls[idx[0]] = true
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
		resolvedCalls[idx[0]] = true
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
		hfApplySchedule(&ent, hfStatementTail(src, idx[1]))
		resolvedCalls[idx[0]] = true
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
		hfApplySchedule(&ent, hfStatementTail(src, idx[1]))
		resolvedCalls[idx[0]] = true
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
		resolvedCalls[idx[0]] = true
		add(ent)
	}

	// 8. Dynamic / non-literal RecurringJob.AddOrUpdate — job-id or lambda body
	//    not statically resolvable (captured-var id, method-group, dynamic args).
	//    Emitted as an honest unresolved producer so the call is still in-graph.
	for _, idx := range hfRecurringAnyRe.FindAllStringIndex(src, -1) {
		if resolvedCalls[idx[0]] {
			continue
		}
		line := lineOf(src, idx[0])
		tail := hfStatementTail(src, idx[1])
		jobID := hfFirstStringArg(tail)
		name := "RecurringJob@line" + intStr(line)
		if jobID != "" {
			name = "recurring:" + jobID
		}
		ent := makeEntity(name, "SCOPE.Pattern", "recurring_job", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "hangfire",
			"pattern_type", "recurring_dynamic",
			"resolution", "unresolved",
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_HANGFIRE_RECURRING_DYNAMIC",
		)
		if jobID != "" {
			setProps(&ent, "task_id", "task:hangfire:recurring:"+jobID)
		}
		hfApplySchedule(&ent, tail)
		add(ent)
	}

	// 9. Dynamic / non-literal BackgroundJob.Enqueue / Schedule — target method
	//    not a resolvable literal lambda (captured delegate, method-group, etc.).
	for _, idx := range hfEnqueueAnyRe.FindAllStringSubmatchIndex(src, -1) {
		if resolvedCalls[idx[0]] {
			continue
		}
		op := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		ent := makeEntity("BackgroundJob."+op+"@line"+intStr(line), "SCOPE.Operation", "task_enqueue", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "hangfire",
			"pattern_type", "enqueue_dynamic",
			"resolution", "unresolved",
			"job_op", op,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_HANGFIRE_ENQUEUE_DYNAMIC",
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
