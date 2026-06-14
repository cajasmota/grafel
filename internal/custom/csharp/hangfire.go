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
		// argument-bearing overloads shift the fixed fields; #5085 resolves
		// them when every argument is a literal constant (parseable below),
		// and otherwise falls back to the schedule label only.
		if strings.TrimSpace(arg) != "" {
			if e := hfCronOverloadExpr(helper, arg); e != "" {
				return e, "cron"
			}
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
		// Interval helpers expand from a runtime count. #5085: resolve to a
		// step expression when the count is a literal int; else label only.
		if e := hfCronIntervalExpr(helper, arg); e != "" {
			return e, "cron"
		}
		return "", "interval"
	}
	return "", ""
}

// hfDayOfWeekRe maps DayOfWeek.Monday → 1, etc. (cron Sun=0..Sat=6).
var hfDayOfWeekNum = map[string]string{
	"Sunday": "0", "Monday": "1", "Tuesday": "2", "Wednesday": "3",
	"Thursday": "4", "Friday": "5", "Saturday": "6",
}

// hfIntLitRe matches a bare integer literal argument token.
var hfIntLitRe = regexp.MustCompile(`^\d+$`)

// hfDowLitRe matches a DayOfWeek.X enum literal token.
var hfDowLitRe = regexp.MustCompile(`^DayOfWeek\.(\w+)$`)

// hfCronOverloadArgs splits a Cron helper's argument list into trimmed tokens.
func hfCronOverloadArgs(arg string) []string {
	parts := strings.Split(arg, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// hfCronOverloadExpr resolves an argument-bearing Cron.* overload to a 5-field
// NCrontab expression when ALL arguments are literal constants (#5085). The
// argument shapes mirror the Hangfire.Cron static helpers:
//
//	Cron.Minutely()                        // no arg → handled by caller
//	Cron.Hourly(int minute)
//	Cron.Daily(int hour) / Daily(hour, minute)
//	Cron.Weekly(DayOfWeek) / Weekly(dow, hour) / Weekly(dow, hour, minute)
//	Cron.Monthly(int day) / Monthly(day, hour) / Monthly(day, hour, minute)
//	Cron.Yearly(int month) / ... / Yearly(month, day, hour, minute)
//
// Returns "" when any argument is non-literal (captured var, expression), so
// the node stays honest rather than fabricating a shifted schedule.
func hfCronOverloadExpr(helper, arg string) string {
	args := hfCronOverloadArgs(arg)
	if len(args) == 0 {
		return ""
	}
	// Default field values per the canonical (no-arg) expression.
	minute, hour, dom, month, dow := "0", "0", "1", "1", "0"
	intArg := func(s string) (string, bool) {
		if hfIntLitRe.MatchString(s) {
			return s, true
		}
		return "", false
	}
	switch helper {
	case "Hourly": // (minute)
		v, ok := intArg(args[0])
		if !ok {
			return ""
		}
		return v + " * * * *"
	case "Daily": // (hour) | (hour, minute)
		h, ok := intArg(args[0])
		if !ok {
			return ""
		}
		hour = h
		if len(args) >= 2 {
			m, ok := intArg(args[1])
			if !ok {
				return ""
			}
			minute = m
		}
		return minute + " " + hour + " * * *"
	case "Weekly": // (dow) | (dow, hour) | (dow, hour, minute)
		m := hfDowLitRe.FindStringSubmatch(args[0])
		if m == nil {
			return ""
		}
		d, ok := hfDayOfWeekNum[m[1]]
		if !ok {
			return ""
		}
		dow = d
		if len(args) >= 2 {
			h, ok := intArg(args[1])
			if !ok {
				return ""
			}
			hour = h
		}
		if len(args) >= 3 {
			mm, ok := intArg(args[2])
			if !ok {
				return ""
			}
			minute = mm
		}
		return minute + " " + hour + " * * " + dow
	case "Monthly": // (day) | (day, hour) | (day, hour, minute)
		d, ok := intArg(args[0])
		if !ok {
			return ""
		}
		dom = d
		if len(args) >= 2 {
			h, ok := intArg(args[1])
			if !ok {
				return ""
			}
			hour = h
		}
		if len(args) >= 3 {
			mm, ok := intArg(args[2])
			if !ok {
				return ""
			}
			minute = mm
		}
		return minute + " " + hour + " " + dom + " * *"
	case "Yearly": // (month) | (month, day) | (month, day, hour) | (month, day, hour, minute)
		mo, ok := intArg(args[0])
		if !ok {
			return ""
		}
		month = mo
		dom = "1"
		if len(args) >= 2 {
			d, ok := intArg(args[1])
			if !ok {
				return ""
			}
			dom = d
		}
		if len(args) >= 3 {
			h, ok := intArg(args[2])
			if !ok {
				return ""
			}
			hour = h
		}
		if len(args) >= 4 {
			mm, ok := intArg(args[3])
			if !ok {
				return ""
			}
			minute = mm
		}
		return minute + " " + hour + " " + dom + " " + month + " *"
	}
	return ""
}

// hfCronIntervalExpr resolves Cron.MinuteInterval(n)/HourInterval(n)/etc. to a
// step expression when n is a literal int (#5085). Mirrors Hangfire's interval
// helpers which produce a `*/n` field at the relevant position.
func hfCronIntervalExpr(helper, arg string) string {
	n := strings.TrimSpace(arg)
	if !hfIntLitRe.MatchString(n) {
		return ""
	}
	switch helper {
	case "MinuteInterval":
		return "*/" + n + " * * * *"
	case "HourInterval":
		return "0 */" + n + " * * *"
	case "DayInterval":
		return "0 0 */" + n + " * *"
	case "MonthInterval":
		return "0 0 1 */" + n + " *"
	}
	return ""
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

// hfStringAssignRe captures local string-constant assignments used as captured
// job-ids, e.g. `var jobId = "daily-report";` or `string id = "x";` or
// `const string JobId = "x";`. Group 1 = identifier, group 2 = literal value.
// Used by #5085 local-dataflow resolution to recover a captured-variable job-id
// back to its literal at the same-file/class scope.
var hfStringAssignRe = regexp.MustCompile(
	`(?m)\b(?:var|string|const\s+string)\s+(\w+)\s*=\s*["']([^"']+)["']`,
)

// hfIdentArgRe matches a bare identifier as the first argument token (a captured
// variable reference), e.g. the `jobId` in `AddOrUpdate(jobId, ...)`.
var hfIdentArgRe = regexp.MustCompile(`^\s*(\w+)\s*[,)]`)

// hfMethodGroupRe captures a method-group enqueue target without a call, e.g.
// `BackgroundJob.Enqueue<IEmailSender>(x => x.Send)` (no trailing `(`) — the
// lambda body is `x.Method` rather than `x.Method(...)`. Group 1 = type, group
// 2 = method. #5085 first-class typed resolution for the method-group form.
var hfMethodGroupRe = regexp.MustCompile(
	`BackgroundJob\.(?:Enqueue|Schedule)\s*<\s*(\w+)\s*>\s*\(\s*\w+\s*=>\s*\w+\.(\w+)\s*\)`,
)

// hfResolveCapturedJobID attempts to resolve a captured-variable job-id to its
// literal assignment within the same file (#5085 local dataflow). It returns the
// resolved literal and true when the first argument of the call tail is a bare
// identifier that has a literal string assignment in `assigns`.
func hfResolveCapturedJobID(tail string, assigns map[string]string) (string, bool) {
	// Bound the lookup to before the first lambda arrow so a later string
	// literal isn't mistaken for the (variable) job-id argument.
	scope := tail
	if arrow := strings.Index(scope, "=>"); arrow >= 0 {
		scope = scope[:arrow]
	}
	// `tail` begins immediately after the call's opening paren (the matcher
	// anchors on `AddOrUpdate(`), so the first argument is the leading token.
	// Trim a leading paren defensively in case the match boundary shifts.
	scope = strings.TrimLeft(scope, " \t")
	scope = strings.TrimPrefix(scope, "(")
	m := hfIdentArgRe.FindStringSubmatch(scope)
	if m == nil {
		return "", false
	}
	if v, ok := assigns[m[1]]; ok {
		return v, true
	}
	return "", false
}

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

	// #5085 local-dataflow: build a same-file string-constant assignment table so
	// a captured-variable job-id (`var jobId = "x"; AddOrUpdate(jobId, ...)`) can
	// be resolved back to its literal in the dynamic fallback below.
	assigns := make(map[string]string)
	for _, m := range hfStringAssignRe.FindAllStringSubmatch(src, -1) {
		assigns[m[1]] = m[2]
	}

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

	// 5b. Method-group enqueue: BackgroundJob.Enqueue<T>(x => x.Method) without a
	//     call (#5085). First-class typed resolution so it isn't swept into the
	//     dynamic fallback as unresolved.
	for _, idx := range hfMethodGroupRe.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[idx[2]:idx[3]]
		methodName := src[idx[4]:idx[5]]
		line := lineOf(src, idx[0])
		taskID := "task:hangfire:" + typeName + "." + methodName
		ent := makeEntity(typeName+"."+methodName, "SCOPE.Operation", "task_enqueue", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "hangfire",
			"pattern_type", "enqueue_method_group",
			"job_type", typeName,
			"job_method", methodName,
			"task_id", taskID,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_HANGFIRE_ENQUEUE_METHOD_GROUP",
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
		// #5085 local-dataflow: a captured-variable job-id resolves to its
		// same-file literal assignment, upgrading the node from unresolved.
		resolution := "unresolved"
		if jobID == "" {
			if resolved, ok := hfResolveCapturedJobID(tail, assigns); ok {
				jobID = resolved
				resolution = "resolved_dataflow"
			}
		}
		name := "RecurringJob@line" + intStr(line)
		if jobID != "" {
			name = "recurring:" + jobID
		}
		ent := makeEntity(name, "SCOPE.Pattern", "recurring_job", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "hangfire",
			"pattern_type", "recurring_dynamic",
			"resolution", resolution,
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
