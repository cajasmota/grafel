// Scheduled-job entry-point detection — #728.
//
// This pass scans file content for every major scheduled-job framework and
// emits synthetic SCOPE.ScheduledJob entities plus TRIGGERS edges from each
// job to its handler function. The entity kind matches the issue spec; the
// edge kind reuses RelationshipKindTriggers (added to kinds.go in this
// changeset — see comment there).
//
// Frameworks covered:
//
//	Python Celery         — @celery.task / @app.task and celery_beat_schedule dict
//	Python APScheduler    — scheduler.add_job(func, trigger='cron', ...)
//	Python schedule lib   — schedule.every(N).<unit>.do(func)
//	Node node-cron        — cron.schedule('EXPR', callback)
//	Node bull / bullmq    — queue.add('name', data, { repeat: { cron: 'EXPR' } })
//	Quarkus / MicroProfile — @Scheduled(cron="EXPR") on a method
//	Spring                 — @Scheduled(cron=...) / fixedRate=... / fixedDelay=...
//	Java Quartz            — JobBuilder.newJob(Foo.class) + TriggerBuilder w/ cronSchedule
//	Go robfig/cron         — c.AddFunc("EXPR", func() { ... })
//	AWS EventBridge        — rate(1 hour) / cron(0 0 * * ? *) in a Lambda event source
//	Kubernetes CronJob     — YAML manifests with spec.schedule
//	GitHub Actions         — schedule: / cron: in .github/workflows/*.yml
//
// Celery pub/sub topology edges — #1404:
//
//	Publisher edge: call sites (<task>.delay() / <task>.apply_async() /
//	  <task>.s().delay() / send_task("name") / signature("name")) emit
//	  PUBLISHES_TO from the enclosing function → SCOPE.ScheduledJob entity.
//
// All emissions are append-only — existing entities and edges are never
// modified or removed, so this pass cannot regress surrounding passes.
//
// Refs #728, #1404.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// scheduledJobKind is the entity kind for scheduled-job entry points.
const scheduledJobKind = "SCOPE.ScheduledJob"

// triggersEdgeKind is the edge from a ScheduledJob to its handler.
// Uses the RelationshipKindTriggers constant declared in kinds.go (#728).
const triggersEdgeKind = "TRIGGERS"

// applyScheduledJobEdges is the per-file entry point. Appends
// SCOPE.ScheduledJob entities + TRIGGERS edges; never modifies or removes
// existing entities or edges. Language dispatches to per-framework helpers.
func applyScheduledJobEdges(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if len(content) == 0 {
		return entities, relationships
	}
	src := string(content)

	seenJob := map[string]bool{}

	emitJob := func(jobID, handler, schedule, framework string, extraProps map[string]string) {
		if seenJob[jobID] {
			return
		}
		seenJob[jobID] = true
		props := map[string]string{
			"schedule":     schedule,
			"handler":      handler,
			"framework":    framework,
			"pattern_type": "scheduled_job_synthesis",
		}
		for k, v := range extraProps {
			if v != "" {
				props[k] = v
			}
		}
		entities = append(entities, types.EntityRecord{
			Name:               jobID,
			Kind:               scheduledJobKind,
			SourceFile:         path,
			Language:           lang,
			Properties:         props,
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
		if handler == "" {
			return
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID: scheduledJobKind + ":" + jobID,
			ToID:   "Function:" + handler,
			Kind:   triggersEdgeKind,
			Properties: map[string]string{
				"framework":    framework,
				"pattern_type": "scheduled_job_synthesis",
			},
		})
	}

	switch lang {
	case "python":
		synthesizePyCelery(src, path, emitJob)
		// #1404: Publisher edges — emit PUBLISHES_TO from call sites to the
		// ScheduledJob entity just produced by synthesizePyCelery. We pass the
		// already-emitted job IDs (seenJob map) so the call-site detector can
		// resolve `task.delay()` references to canonical entity IDs without
		// creating phantom nodes.
		relationships = synthesizeCeleryCallSiteEdges(src, path, seenJob, relationships)
		synthesizePyAPScheduler(src, path, emitJob)
		synthesizePyScheduleLib(src, path, emitJob)
	case "javascript", "typescript":
		synthesizeNodeCron(src, path, emitJob)
		synthesizeNodeBull(src, path, emitJob)
	case "java", "kotlin":
		synthesizeJavaSpringScheduled(src, path, lang, emitJob)
		synthesizeJavaQuartz(src, path, lang, emitJob)
		synthesizeQuarkusScheduled(src, path, lang, emitJob)
	case "go":
		synthesizeGoCron(src, path, emitJob)
	}

	// YAML-based detectors run regardless of `lang` because the language
	// is resolved from the file extension by the extractor — CronJob YAML
	// and GitHub Actions YAML are not "go" or "python" files.
	if isKubernetesCronJob(path, src) {
		synthesizeK8sCronJob(src, path, emitJob)
	}
	if isGitHubActionsWorkflow(path, src) {
		synthesizeGitHubActionsSchedule(src, path, emitJob)
	}

	return entities, relationships
}

// ---------------------------------------------------------------------------
// Python — Celery
// ---------------------------------------------------------------------------

// pyCeleryTaskDecoratorRe matches @celery.task / @app.task / @shared_task
// above a `def <name>(` declaration. Group 1 = function name.
var pyCeleryTaskDecoratorRe = regexp.MustCompile(`(?m)@(?:celery\.task|app\.task|shared_task)[^\n]*\n\s*(?:async\s+)?def\s+(\w+)\s*\(`)

// pyCeleryBeatScheduleRe captures task entries in celery_beat_schedule dict:
//
//	'task': 'app.tasks.send_report'
//
// Group 1 = task path.
var pyCeleryBeatScheduleRe = regexp.MustCompile(`['"]task['"]\s*:\s*['"]([^'"]+)['"]`)

// pyCeleryScheduleExprRe captures the schedule value next to a task entry.
// It matches `'schedule': crontab(hour=0)` or `'schedule': timedelta(...)`.
var pyCeleryScheduleExprRe = regexp.MustCompile(`['"]schedule['"]\s*:\s*([^\n,}]+)`)

func synthesizePyCelery(
	src, path string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	// Fast pre-filter.
	if !strings.Contains(src, "celery") && !strings.Contains(src, "Celery") {
		return
	}

	// @celery.task / @app.task / @shared_task decorated functions.
	for _, m := range pyCeleryTaskDecoratorRe.FindAllStringSubmatch(src, -1) {
		fn := m[1]
		jobID := "celery:" + path + ":" + fn
		emitJob(jobID, fn, "", "celery", nil)
	}

	// celery_beat_schedule dictionary entries.
	if strings.Contains(src, "beat_schedule") {
		taskMatches := pyCeleryBeatScheduleRe.FindAllStringIndex(src, -1)
		for _, tm := range taskMatches {
			task := pyCeleryBeatScheduleRe.FindStringSubmatch(src[tm[0]:tm[1]])[1]
			// Try to find the schedule value immediately after the task entry.
			rest := src[tm[1]:]
			schedule := ""
			if sm := pyCeleryScheduleExprRe.FindStringSubmatch(rest[:min(len(rest), 200)]); len(sm) >= 2 {
				schedule = strings.TrimSpace(sm[1])
			}
			// Handler is the last dotted segment of the task path.
			parts := strings.Split(task, ".")
			handler := parts[len(parts)-1]
			jobID := "celery_beat:" + task
			emitJob(jobID, handler, schedule, "celery_beat", map[string]string{
				"task_path": task,
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Python — Celery call-site publisher edges (#1404)
// ---------------------------------------------------------------------------

// pyCeleryDelayRe matches `<taskvar>.delay(` or `<taskvar>.apply_async(`.
// Group 1 = task variable name.
var pyCeleryDelayRe = regexp.MustCompile(`(?m)\b(\w+)\.(?:delay|apply_async)\s*\(`)

// pyCelerySigRe matches `<taskvar>.s(` or `<taskvar>.si(` (canvas signatures).
// Group 1 = task variable name.
var pyCelerySigRe = regexp.MustCompile(`(?m)\b(\w+)\.si?\s*\(`)

// pyCelerySendTaskRe matches `app.send_task("task.name"` or `celery.send_task("...".
// Group 1 = task dotted name.
var pyCelerySendTaskRe = regexp.MustCompile(`(?m)\w+\.send_task\s*\(\s*["']([^"']+)["']`)

// pyCelerySignatureRe matches `signature("task.name"` or `subtask("task.name"`.
// Group 1 = task dotted name.
var pyCelerySignatureRe = regexp.MustCompile(`(?m)(?:signature|subtask)\s*\(\s*["']([^"']+)["']`)

// pyCeleryEnclosingFuncRe finds the nearest enclosing `def <name>` before a position.
// We use a simple scan: find the last `def <name>(` before the match offset.
var pyCeleryEnclosingFuncRe = regexp.MustCompile(`(?m)^(?:async\s+)?def\s+(\w+)\s*\(`)

// enclosingFunction returns the name of the Python function that contains the
// byte offset `pos` in `src`. Returns "" if no enclosing function is found.
func enclosingFunction(src string, pos int) string {
	// Scan all def statements before pos; the last one is the enclosing function.
	sub := src[:pos]
	matches := pyCeleryEnclosingFuncRe.FindAllStringSubmatch(sub, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

// synthesizeCeleryCallSiteEdges scans `src` for Celery call sites and emits
// PUBLISHES_TO relationship records from the enclosing function to the
// canonical ScheduledJob entity. Only emits edges for task names that resolve
// to a known job ID in `knownJobs` (the seenJob map from synthesizePyCelery) —
// this prevents phantom node creation (#1377 lesson).
//
// `path` is the source file path; `knownJobs` maps celery:<path>:<name> →
// true for every task defined in this file by synthesizePyCelery.
func synthesizeCeleryCallSiteEdges(
	src, path string,
	knownJobs map[string]bool,
	relationships []types.RelationshipRecord,
) []types.RelationshipRecord {
	if !strings.Contains(src, "celery") && !strings.Contains(src, "Celery") &&
		!strings.Contains(src, ".delay(") && !strings.Contains(src, ".apply_async(") &&
		!strings.Contains(src, "send_task") && !strings.Contains(src, "signature") {
		return relationships
	}

	// Build a quick lookup: task variable name → jobID.
	// For @app.task/@shared_task decorated defs the variable name equals the function name.
	taskVarToJobID := map[string]string{}
	for jobID := range knownJobs {
		// jobID format: "celery:<path>:<funcname>" or "celery_beat:<dotted.path>"
		if !strings.HasPrefix(jobID, "celery:") {
			continue
		}
		// Strip "celery:<path>:" prefix to get the function name.
		rest := strings.TrimPrefix(jobID, "celery:")
		colonIdx := strings.LastIndex(rest, ":")
		if colonIdx < 0 {
			continue
		}
		funcName := rest[colonIdx+1:]
		if funcName != "" {
			taskVarToJobID[funcName] = jobID
		}
	}

	seenEdge := map[string]bool{}

	emitPublishesTo := func(callerFunc, jobID string) {
		if callerFunc == "" || jobID == "" {
			return
		}
		key := callerFunc + "|" + jobID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: "SCOPE.Operation:" + callerFunc,
			ToID:   scheduledJobKind + ":" + jobID,
			Kind:   "PUBLISHES_TO",
			Properties: map[string]string{
				"framework":    "celery",
				"pattern_type": "celery_pubsub_synthesis",
			},
		})
	}

	// 1. task.delay(...) / task.apply_async(...)
	for _, idx := range pyCeleryDelayRe.FindAllStringSubmatchIndex(src, -1) {
		taskVar := src[idx[2]:idx[3]]
		if jobID, ok := taskVarToJobID[taskVar]; ok {
			caller := enclosingFunction(src, idx[0])
			// Skip if call site is inside the task's own definition (self-call edge).
			if caller == taskVar {
				continue
			}
			emitPublishesTo(caller, jobID)
		}
	}

	// 2. task.s(...) / task.si(...) — canvas signatures used in chains/chords.
	for _, idx := range pyCelerySigRe.FindAllStringSubmatchIndex(src, -1) {
		taskVar := src[idx[2]:idx[3]]
		if jobID, ok := taskVarToJobID[taskVar]; ok {
			caller := enclosingFunction(src, idx[0])
			if caller == taskVar {
				continue
			}
			emitPublishesTo(caller, jobID)
		}
	}

	// 3. app.send_task("module.task_name") — string-based dispatch.
	for _, m := range pyCelerySendTaskRe.FindAllStringSubmatchIndex(src, -1) {
		taskPath := src[m[2]:m[3]]
		// Last segment of the dotted path is the function name.
		parts := strings.Split(taskPath, ".")
		funcName := parts[len(parts)-1]
		if jobID, ok := taskVarToJobID[funcName]; ok {
			caller := enclosingFunction(src, m[0])
			emitPublishesTo(caller, jobID)
		}
	}

	// 4. signature("module.task_name") / subtask("module.task_name").
	for _, m := range pyCelerySignatureRe.FindAllStringSubmatchIndex(src, -1) {
		taskPath := src[m[2]:m[3]]
		parts := strings.Split(taskPath, ".")
		funcName := parts[len(parts)-1]
		if jobID, ok := taskVarToJobID[funcName]; ok {
			caller := enclosingFunction(src, m[0])
			emitPublishesTo(caller, jobID)
		}
	}

	return relationships
}

// ---------------------------------------------------------------------------
// Python — APScheduler
// ---------------------------------------------------------------------------

// pyAPSchedulerAddJobRe captures `scheduler.add_job(func, 'cron', ...)` and
// `scheduler.add_job(func, trigger='cron', ...)`.
// Group 1 = function/callable name.
var pyAPSchedulerAddJobRe = regexp.MustCompile(`scheduler\.add_job\s*\(\s*([\w.]+)\s*,`)

// pyAPSchedulerTriggerRe captures the trigger argument. Group 1 = trigger type.
var pyAPSchedulerTriggerRe = regexp.MustCompile(`trigger\s*=\s*['"](\w+)['"]`)

// pyAPSchedulerCronArgsRe captures `hour=0, minute=30` style cron kwargs
// after trigger='cron'.
var pyAPSchedulerCronArgsRe = regexp.MustCompile(`(?:hour|minute|second|day|week|day_of_week|month)\s*=\s*[^,)]+`)

func synthesizePyAPScheduler(
	src, path string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "apscheduler") && !strings.Contains(src, "APScheduler") &&
		!strings.Contains(src, "add_job") {
		return
	}
	for _, m := range pyAPSchedulerAddJobRe.FindAllStringSubmatchIndex(src, -1) {
		handler := src[m[2]:m[3]]
		ctx := src[m[0]:min(m[0]+400, len(src))]
		trigger := ""
		if tm := pyAPSchedulerTriggerRe.FindStringSubmatch(ctx); len(tm) >= 2 {
			trigger = tm[1]
		}
		schedule := trigger
		// Collect cron-like kwargs to form a human-readable schedule string.
		if trigger == "cron" {
			var parts []string
			for _, km := range pyAPSchedulerCronArgsRe.FindAllString(ctx, -1) {
				parts = append(parts, strings.TrimSpace(km))
			}
			if len(parts) > 0 {
				schedule = "cron(" + strings.Join(parts, ", ") + ")"
			}
		}
		jobID := "apscheduler:" + path + ":" + handler
		emitJob(jobID, handler, schedule, "apscheduler", nil)
	}
}

// ---------------------------------------------------------------------------
// Python — schedule library
// ---------------------------------------------------------------------------

// pyScheduleEveryRe captures `schedule.every(N).<unit>.do(func)`.
// Group 1 = interval (N), group 2 = unit (minutes/hours/…), group 3 = func.
var pyScheduleEveryRe = regexp.MustCompile(`schedule\.every\s*\(\s*(\d+)\s*\)\s*\.(\w+)\s*\.do\s*\(\s*([\w.]+)\s*[,)]`)

func synthesizePyScheduleLib(
	src, path string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "schedule") {
		return
	}
	for _, m := range pyScheduleEveryRe.FindAllStringSubmatch(src, -1) {
		interval, unit, handler := m[1], m[2], m[3]
		schedule := "every(" + interval + ")." + unit
		jobID := "schedule_lib:" + path + ":" + handler
		emitJob(jobID, handler, schedule, "schedule_lib", nil)
	}
}

// ---------------------------------------------------------------------------
// Node — node-cron
// ---------------------------------------------------------------------------

// nodeCronScheduleRe captures `cron.schedule('EXPR', callback)`.
// Group 1 = cron expression, group 2 = callback identifier (or empty for
// anonymous).
var nodeCronScheduleRe = regexp.MustCompile(`cron\.schedule\s*\(\s*['"\x60]([^'"\x60\n\r]+)['"\x60]\s*,\s*([\w]+)?`)

func synthesizeNodeCron(
	src, path string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "cron") {
		return
	}
	for _, m := range nodeCronScheduleRe.FindAllStringSubmatch(src, -1) {
		expr := m[1]
		handler := m[2]
		jobID := "node_cron:" + path + ":" + expr
		emitJob(jobID, handler, expr, "node_cron", nil)
	}
}

// ---------------------------------------------------------------------------
// Node — bull / bullmq repeat jobs
// ---------------------------------------------------------------------------

// nodeBullRepeatCronRe captures bull/bullmq `queue.add('name', data, { repeat: { cron: 'EXPR' } })`.
// Group 1 = job name, group 2 = cron expression.
var nodeBullRepeatCronRe = regexp.MustCompile(`\.add\s*\(\s*['"\x60]([^'"\x60\n\r]+)['"\x60][^)]*?repeat\s*:\s*\{[^}]*?cron\s*:\s*['"\x60]([^'"\x60\n\r]+)['"\x60]`)

func synthesizeNodeBull(
	src, path string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "bull") && !strings.Contains(src, "Bull") {
		return
	}
	for _, m := range nodeBullRepeatCronRe.FindAllStringSubmatch(src, -1) {
		name, expr := m[1], m[2]
		jobID := "bull_repeat:" + path + ":" + name
		emitJob(jobID, name, expr, "bullmq", nil)
	}
}

// ---------------------------------------------------------------------------
// Java / Kotlin — Spring @Scheduled
// ---------------------------------------------------------------------------

// springScheduledRe captures `@Scheduled(cron=...)`, `@Scheduled(fixedRate=...)`
// and `@Scheduled(fixedDelay=...)`. Group 1 = attribute name, group 2 = value.
var springScheduledRe = regexp.MustCompile(`@Scheduled\s*\(\s*(\w+)\s*=\s*["']?([^"'\n\r,)]+)["']?`)

// springScheduledMethodRe finds the method name following a @Scheduled annotation.
// Reuses the same heuristic as findFollowingMethod but compiled here for independence.
var springScheduledMethodRe = regexp.MustCompile(`(?m)^\s*(?:public|protected|private|static|void|\s)*\s+(?:void|[\w<>\[\],\s.]+)\s+(\w+)\s*\(`)

func synthesizeJavaSpringScheduled(
	src, path, lang string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "@Scheduled") {
		return
	}
	for _, m := range springScheduledRe.FindAllStringSubmatchIndex(src, -1) {
		attrName := src[m[2]:m[3]]
		attrVal := strings.TrimSpace(src[m[4]:m[5]])
		schedule := attrName + "=" + attrVal
		methodName := findFollowingMethod(src, m[0])
		jobID := "spring_scheduled:" + path + ":" + methodName
		emitJob(jobID, methodName, schedule, "spring_scheduled", map[string]string{
			"trigger_type": attrName,
		})
	}
}

// ---------------------------------------------------------------------------
// Java / Kotlin — Quarkus @Scheduled (MicroProfile)
// ---------------------------------------------------------------------------

// quarkusScheduledRe captures `@Scheduled(cron="EXPR")` or
// `@Scheduled(every="1s")`. Group 1 = attribute, group 2 = value.
var quarkusScheduledRe = regexp.MustCompile(`@Scheduled\s*\(\s*(\w+)\s*=\s*["']([^"'\n\r]+)["']`)

func synthesizeQuarkusScheduled(
	src, path, lang string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	// Distinguish Quarkus from Spring by import hints; if neither is present
	// we run both detectors (the IDs are namespaced so there's no collision).
	if !strings.Contains(src, "@Scheduled") {
		return
	}
	isQuarkus := strings.Contains(src, "io.quarkus") || strings.Contains(src, "quarkus") ||
		strings.Contains(src, "every=") || strings.Contains(src, "every =")
	isSpring := strings.Contains(src, "springframework") || strings.Contains(src, "fixedRate") ||
		strings.Contains(src, "fixedDelay")
	// If Spring markers found but not Quarkus, skip (handled by Spring pass).
	if isSpring && !isQuarkus {
		return
	}
	for _, m := range quarkusScheduledRe.FindAllStringSubmatchIndex(src, -1) {
		attr := src[m[2]:m[3]]
		val := src[m[4]:m[5]]
		schedule := attr + "=" + val
		methodName := findFollowingMethod(src, m[0])
		jobID := "quarkus_scheduled:" + path + ":" + methodName
		emitJob(jobID, methodName, schedule, "quarkus_scheduled", map[string]string{
			"trigger_type": attr,
		})
	}
}

// ---------------------------------------------------------------------------
// Java — Quartz scheduler
// ---------------------------------------------------------------------------

// quartzJobBuilderRe captures `JobBuilder.newJob(Foo.class)`. Group 1 = class name.
var quartzJobBuilderRe = regexp.MustCompile(`JobBuilder\.newJob\s*\(\s*(\w+)\.class\s*\)`)

// quartzCronScheduleRe captures `cronSchedule("EXPR")` or
// `CronScheduleBuilder.cronSchedule("EXPR")`. Group 1 = cron expression.
var quartzCronScheduleRe = regexp.MustCompile(`cronSchedule\s*\(\s*"([^"\n\r]+)"\s*\)`)

// quartzSimpleScheduleRe captures `simpleSchedule().withIntervalInSeconds(N)`.
// Group 1 = interval value.
var quartzSimpleScheduleRe = regexp.MustCompile(`withIntervalIn(?:Seconds|Minutes|Hours|Milliseconds)\s*\(\s*(\d+)\s*\)`)

func synthesizeJavaQuartz(
	src, path, lang string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "JobBuilder") && !strings.Contains(src, "TriggerBuilder") {
		return
	}
	// Find each job class reference.
	jobMatches := quartzJobBuilderRe.FindAllStringSubmatch(src, -1)
	for i, m := range jobMatches {
		className := m[1]
		// Try to find the closest cron expression.
		schedule := ""
		if cm := quartzCronScheduleRe.FindStringSubmatch(src); len(cm) >= 2 {
			schedule = cm[1]
		} else if sm := quartzSimpleScheduleRe.FindStringSubmatch(src); len(sm) >= 2 {
			schedule = "interval=" + sm[1]
		}
		jobID := "quartz:" + path + ":" + className + ":" + itoa(i)
		emitJob(jobID, className, schedule, "quartz", nil)
	}
}

// ---------------------------------------------------------------------------
// Go — robfig/cron
// ---------------------------------------------------------------------------

// goCronAddFuncRe captures `c.AddFunc("EXPR", func() { ... })` or
// `cr.AddFunc("EXPR", handlerFunc)`.
// Group 1 = cron expression, group 2 = handler identifier (may be empty for inline).
var goCronAddFuncRe = regexp.MustCompile(`\.AddFunc\s*\(\s*"([^"\n\r]+)"\s*,\s*([\w.]+)?`)

func synthesizeGoCron(
	src, path string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "cron") {
		return
	}
	for _, m := range goCronAddFuncRe.FindAllStringSubmatch(src, -1) {
		expr := m[1]
		handler := m[2]
		if handler == "func" {
			handler = "" // anonymous func literal
		}
		jobID := "go_cron:" + path + ":" + expr
		emitJob(jobID, handler, expr, "go_cron", nil)
	}
}

// ---------------------------------------------------------------------------
// AWS Lambda — EventBridge / CloudWatch Events
// ---------------------------------------------------------------------------

// awsEventBridgeRateRe captures `rate(1 hour)` or `rate(5 minutes)`.
// Group 1 = full expression.
var awsEventBridgeRateRe = regexp.MustCompile(`(?i)rate\s*\(\s*\d+\s+(?:minute|minutes|hour|hours|day|days)\s*\)`)

// awsEventBridgeCronRe captures `cron(0 0 * * ? *)` style expressions.
// Group 1 = cron body.
var awsEventBridgeCronRe = regexp.MustCompile(`(?i)cron\s*\(\s*([^)\n\r]+)\s*\)`)

// awsLambdaHandlerRe attempts to find the Lambda handler in the same file.
// Group 1 = handler function name.
var awsLambdaHandlerRe = regexp.MustCompile(`(?m)(?:def|func|function)\s+(\w*[Hh]andler\w*)\s*\(`)

func synthesizeAWSEventBridge(
	src, path string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	hasRate := awsEventBridgeRateRe.MatchString(src)
	hasCron := strings.Contains(src, "cron(")
	if !hasRate && !hasCron {
		return
	}
	handler := ""
	if hm := awsLambdaHandlerRe.FindStringSubmatch(src); len(hm) >= 2 {
		handler = hm[1]
	}
	if hasRate {
		for _, m := range awsEventBridgeRateRe.FindAllString(src, -1) {
			jobID := "aws_eventbridge:" + path + ":" + m
			emitJob(jobID, handler, m, "aws_eventbridge", nil)
		}
	}
	if hasCron {
		for _, m := range awsEventBridgeCronRe.FindAllStringSubmatch(src, -1) {
			expr := "cron(" + m[1] + ")"
			jobID := "aws_eventbridge:" + path + ":" + expr
			emitJob(jobID, handler, expr, "aws_eventbridge", nil)
		}
	}
}

// ---------------------------------------------------------------------------
// Kubernetes CronJob YAML
// ---------------------------------------------------------------------------

// k8sCronJobScheduleRe captures `schedule: "0 0 * * *"` inside a YAML doc.
// Group 1 = schedule expression.
var k8sCronJobScheduleRe = regexp.MustCompile(`(?m)^\s*schedule\s*:\s*["']?([^"'\n\r]+)["']?`)

// k8sCronJobImageRe captures the container image as a proxy for what runs.
var k8sCronJobImageRe = regexp.MustCompile(`(?m)^\s*image\s*:\s*(\S+)`)

// k8sCronJobCommandRe captures the first command entry.
var k8sCronJobCommandRe = regexp.MustCompile(`(?m)^\s*-\s*([\w/.-]+(?:\s[\w/.-]+)*)`)

func isKubernetesCronJob(path, src string) bool {
	return strings.Contains(src, "CronJob") && strings.Contains(src, "kind:") &&
		(strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml"))
}

func synthesizeK8sCronJob(
	src, path string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "spec:") {
		return
	}
	for _, sm := range k8sCronJobScheduleRe.FindAllStringSubmatch(src, -1) {
		schedule := strings.TrimSpace(sm[1])
		image := ""
		if im := k8sCronJobImageRe.FindStringSubmatch(src); len(im) >= 2 {
			image = im[1]
		}
		handler := image
		jobID := "k8s_cronjob:" + path + ":" + schedule
		emitJob(jobID, handler, schedule, "kubernetes_cronjob", map[string]string{
			"image": image,
		})
	}
}

// ---------------------------------------------------------------------------
// GitHub Actions — schedule: cron triggers
// ---------------------------------------------------------------------------

// ghActionsScheduleCronRe captures `- cron: '0 0 * * *'` inside a workflow.
// Group 1 = cron expression.
var ghActionsScheduleCronRe = regexp.MustCompile(`(?m)^\s*-\s*cron\s*:\s*['"]?([^'"\n\r]+)['"]?`)

// ghActionsJobNameRe captures the first job name (`jobs:<name>:`).
var ghActionsJobNameRe = regexp.MustCompile(`(?m)^(\s{0,2}\w[\w-]*):\s*$`)

func isGitHubActionsWorkflow(path, src string) bool {
	return strings.Contains(path, ".github/workflows") &&
		(strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml"))
}

func synthesizeGitHubActionsSchedule(
	src, path string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "schedule") {
		return
	}
	// Find job name as handler proxy.
	jobName := ""
	if jm := ghActionsJobNameRe.FindStringSubmatch(src); len(jm) >= 2 {
		candidate := strings.TrimSpace(jm[1])
		if candidate != "on" && candidate != "jobs" && candidate != "steps" {
			jobName = candidate
		}
	}
	for _, m := range ghActionsScheduleCronRe.FindAllStringSubmatch(src, -1) {
		expr := strings.TrimSpace(m[1])
		jobID := "gh_actions_schedule:" + path + ":" + expr
		emitJob(jobID, jobName, expr, "github_actions_schedule", nil)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// min returns the smaller of a and b. Used to bound slice operations.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// itoa converts an int to a decimal string without importing strconv.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
