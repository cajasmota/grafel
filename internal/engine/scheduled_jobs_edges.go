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
func applyScheduledJobEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
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
		// #3628 area: RQ (Redis Queue) enqueue→handler cross-link. A
		// `queue.enqueue(my_func)` / `enqueue_call(func="mod.my_func")` dispatch
		// site ENQUEUES the named callable; the callable's own def IS the
		// consumer, so we emit an ENQUEUES edge from the enclosing function to
		// Function:<callable> directly (mirroring Celery's TRIGGERS target
		// convention). No synthetic queue node: the function is the rendezvous.
		relationships = synthesizeRQEnqueueEdges(src, relationships)
	case "javascript", "typescript":
		synthesizeNodeCron(src, path, emitJob)
		synthesizeNodeBull(src, path, emitJob)
	case "java", "kotlin":
		synthesizeJavaSpringScheduled(src, path, lang, emitJob)
		synthesizeJavaQuartz(src, path, lang, emitJob)
		synthesizeQuarkusScheduled(src, path, lang, emitJob)
	case "go":
		synthesizeGoCron(src, path, emitJob)
	case "ruby":
		// #3700: Sidekiq worker jobs + sidekiq-cron scheduled jobs, plus
		// ENQUEUES edges from `Worker.perform_async/in/at` dispatch sites to
		// the worker job entity. synthesizeRubySidekiq registers job IDs into
		// seenJob; synthesizeSidekiqEnqueueEdges resolves dispatch call sites
		// to those IDs and appends the caller→job ENQUEUES edges.
		synthesizeRubySidekiq(src, path, emitJob)
		relationships = synthesizeSidekiqEnqueueEdges(src, seenJob, relationships)
		// #3628 area: Resque jobs + ENQUEUES edges. synthesizeRubyResque emits
		// a SCOPE.ScheduledJob (resque:<Job>) with a TRIGGERS edge to the job's
		// `self.perform` handler; synthesizeResqueEnqueueEdges resolves
		// `Resque.enqueue(Job, …)` dispatch sites to that job ID and appends the
		// caller→job ENQUEUES edges. Same join shape as Sidekiq.
		synthesizeRubyResque(src, path, emitJob)
		relationships = synthesizeResqueEnqueueEdges(src, seenJob, relationships)
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

	return DetectorPassResult{Entities: entities, Relationships: relationships}
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
// Ruby — Sidekiq workers + sidekiq-cron (#3700)
// ---------------------------------------------------------------------------
//
// A Sidekiq worker is a class that `include Sidekiq::Worker` (or the newer
// `Sidekiq::Job`) and defines `def perform`. The async execution of that
// `perform` method IS the background job, so we model the worker as a
// SCOPE.ScheduledJob entity and emit a TRIGGERS edge to the `perform`
// handler (the runtime invokes perform when the job is popped off the queue).
//
// Dispatch happens at `Worker.perform_async/perform_in/perform_at(...)` call
// sites; each such call ENQUEUES work onto the queue. We emit an ENQUEUES
// edge from the enclosing Ruby method to the worker's job entity.
//
// sidekiq-cron declares recurring jobs via `Sidekiq::Cron::Job.create(
// name: '...', cron: 'EXPR', class: 'SomeWorker')` (or a YAML schedule loaded
// through `Sidekiq::Cron::Job.load_from_hash`). The `cron:` expression is the
// schedule; the `class:` value is the worker that runs.

// rubySidekiqWorkerJobID is the canonical job-entity ID for a Sidekiq worker
// class. Stable across files (no path) so a `perform_async` dispatch in one
// file resolves to the worker job defined in another. Cron jobs reuse the
// same ID when their `class:` names the worker, so the scheduled job and the
// dispatch target collapse onto one node.
func rubySidekiqWorkerJobID(workerClass string) string {
	return "sidekiq:" + workerClass
}

// reRubySidekiqClass captures a `class Foo` (or `class Foo::Bar`) declaration.
// Group 1 = class name.
var reRubySidekiqClass = regexp.MustCompile(`(?m)^\s*class\s+([A-Z][A-Za-z0-9_:]*)`)

// reRubySidekiqInclude matches `include Sidekiq::Worker` / `include Sidekiq::Job`.
var reRubySidekiqInclude = regexp.MustCompile(`(?m)^\s*include\s+Sidekiq::(?:Worker|Job)\b`)

// reRubySidekiqPerform matches the `def perform` method of a worker.
var reRubySidekiqPerform = regexp.MustCompile(`(?m)^\s*def\s+perform\b`)

// reRubySidekiqDispatch captures `Worker.perform_async(...)` /
// `Worker.perform_in(...)` / `Worker.perform_at(...)` dispatch sites.
// Group 1 = worker class, group 2 = dispatch method.
var reRubySidekiqDispatch = regexp.MustCompile(`([A-Z][A-Za-z0-9_:]*)\.(perform_async|perform_in|perform_at|perform_bulk)\b`)

// reRubySidekiqCron captures sidekiq-cron job declarations. We match the
// `cron:` and `class:` kwargs of a `Sidekiq::Cron::Job.create`/`load_from_hash`
// block within a single literal hash. Two narrowly-scoped regexes pull the
// cron expression and the worker class respectively from the same statement.
var reRubySidekiqCronCreate = regexp.MustCompile(`Sidekiq::Cron::Job\.(?:create|load_from_hash!?)`)
var reRubySidekiqCronExpr = regexp.MustCompile(`(?m)['"]?cron['"]?\s*(?:=>|:)\s*['"]([^'"]+)['"]`)
var reRubySidekiqCronClass = regexp.MustCompile(`(?m)['"]?class['"]?\s*(?:=>|:)\s*['"]?([A-Z][A-Za-z0-9_:]*)`)

// reRubyEnclosingMethod captures Ruby `def name` declarations (with or without
// parentheses). Used to attribute a dispatch call site to its enclosing method.
var reRubyEnclosingMethod = regexp.MustCompile(`(?m)^\s*def\s+([A-Za-z_][\w?!]*)`)

// rubyEnclosingMethod returns the name of the Ruby method enclosing pos, or
// "" when the call site is at class/module body level (no enclosing def).
func rubyEnclosingMethod(src string, pos int) string {
	if pos > len(src) {
		pos = len(src)
	}
	matches := reRubyEnclosingMethod.FindAllStringSubmatch(src[:pos], -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

// synthesizeRubySidekiq emits SCOPE.ScheduledJob entities for Sidekiq worker
// classes (with a TRIGGERS edge to `perform`) and for sidekiq-cron recurring
// jobs (carrying the cron expression). emitJob registers every job ID into the
// caller's seenJob map so the ENQUEUES pass can resolve dispatch targets.
func synthesizeRubySidekiq(
	src, path string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "Sidekiq") && !strings.Contains(src, "perform_async") {
		return
	}

	// 1. sidekiq-cron recurring jobs FIRST, so the schedule-carrying variant
	//    wins the seenJob dedup over the plain worker-class entity below (both
	//    share the `sidekiq:<Worker>` ID). emitJob already emits the
	//    TRIGGERS → perform edge for the scheduled variant.
	if reRubySidekiqCronCreate.MatchString(src) {
		for _, eloc := range reRubySidekiqCronExpr.FindAllStringSubmatchIndex(src, -1) {
			expr := src[eloc[2]:eloc[3]]
			// Find the worker class within the same `create(...)` literal hash.
			workerClass := ""
			win := src[eloc[0]:min(eloc[0]+400, len(src))]
			if cm := reRubySidekiqCronClass.FindStringSubmatch(win); len(cm) >= 2 {
				workerClass = cm[1]
			} else {
				back := src[max(eloc[0]-400, 0):eloc[0]]
				if cm := reRubySidekiqCronClass.FindStringSubmatch(back); len(cm) >= 2 {
					workerClass = cm[1]
				}
			}
			var jobID string
			if workerClass != "" {
				jobID = rubySidekiqWorkerJobID(workerClass)
			} else {
				jobID = "sidekiq_cron:" + path + ":" + expr
			}
			emitJob(jobID, "perform", expr, "sidekiq_cron", map[string]string{
				"worker_class": workerClass,
				"job_type":     "scheduled",
			})
		}
	}

	// 2. Worker classes: a class that includes Sidekiq::Worker/Job AND defines
	//    `def perform`. We require both to avoid flagging arbitrary classes.
	//    Deduped against any cron entity emitted above (same job ID).
	if reRubySidekiqInclude.MatchString(src) && reRubySidekiqPerform.MatchString(src) {
		// Attribute each `include Sidekiq::Worker` to the class declared above
		// it, mirroring internal/custom/ruby/sidekiq.go's class resolution.
		for _, inc := range reRubySidekiqInclude.FindAllStringIndex(src, -1) {
			classMatches := reRubySidekiqClass.FindAllStringSubmatch(src[:inc[0]], -1)
			if len(classMatches) == 0 {
				continue
			}
			workerClass := classMatches[len(classMatches)-1][1]
			jobID := rubySidekiqWorkerJobID(workerClass)
			// handler == "perform": the job triggers the worker's perform method.
			emitJob(jobID, "perform", "", "sidekiq", map[string]string{
				"worker_class": workerClass,
				"job_type":     "queue",
			})
		}
	}
}

// synthesizeSidekiqEnqueueEdges emits ENQUEUES edges from the enclosing Ruby
// method of each `Worker.perform_async/in/at` dispatch site to the worker's
// job entity. Only emits when the worker job ID is present in knownJobs (the
// seenJob map) — this prevents phantom-node creation when the worker class is
// defined in another, un-indexed file. Dedupes on (caller, jobID).
func synthesizeSidekiqEnqueueEdges(
	src string,
	knownJobs map[string]bool,
	relationships []types.RelationshipRecord,
) []types.RelationshipRecord {
	if !strings.Contains(src, "perform_async") && !strings.Contains(src, "perform_in") &&
		!strings.Contains(src, "perform_at") && !strings.Contains(src, "perform_bulk") {
		return relationships
	}

	seenEdge := map[string]bool{}
	for _, idx := range reRubySidekiqDispatch.FindAllStringSubmatchIndex(src, -1) {
		workerClass := src[idx[2]:idx[3]]
		jobID := rubySidekiqWorkerJobID(workerClass)
		if !knownJobs[jobID] {
			continue
		}
		caller := rubyEnclosingMethod(src, idx[0])
		if caller == "" {
			caller = "module"
		}
		key := caller + "|" + jobID
		if seenEdge[key] {
			continue
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: "SCOPE.Operation:" + caller,
			ToID:   scheduledJobKind + ":" + jobID,
			Kind:   string(types.RelationshipKindEnqueues),
			Properties: map[string]string{
				"framework":       "sidekiq",
				"pattern_type":    "sidekiq_enqueue_synthesis",
				"worker_class":    workerClass,
				"dispatch_method": src[idx[4]:idx[5]],
			},
		})
	}
	return relationships
}

// ---------------------------------------------------------------------------
// Ruby — Resque jobs + ENQUEUES (#3628 area)
// ---------------------------------------------------------------------------
//
// A Resque job is a Ruby class that defines a class method `self.perform`
// (Resque pops a job off the queue and calls `Job.perform(*args)`). The class
// usually declares `@queue = :name`, but the perform method is the consumer.
// We model the job as a SCOPE.ScheduledJob entity keyed `resque:<Job>` (stable
// across files, no path) with a TRIGGERS edge to `perform`, exactly mirroring
// the Sidekiq shape so a `Resque.enqueue(Job)` dispatch in another file joins.
//
// Dispatch happens at `Resque.enqueue(Job, …)` / `Resque.enqueue_to(queue,
// Job, …)` / `Resque.enqueue_in(sec, Job, …)` call sites; each ENQUEUES work
// onto the queue. We emit an ENQUEUES edge from the enclosing Ruby method to
// the job entity.

// rubyResqueJobID is the canonical job-entity ID for a Resque job class.
// Stable across files (no path) so an enqueue dispatch in one file resolves to
// the job class defined in another — same convention as rubySidekiqWorkerJobID.
func rubyResqueJobID(jobClass string) string {
	return "resque:" + jobClass
}

// reRubyResqueQueueDecl matches `@queue = :name` / `@queue = "name"`, the
// idiomatic Resque queue declaration that distinguishes a Resque job class
// from an arbitrary class with a self.perform method. Group 1 = queue name.
var reRubyResqueQueueDecl = regexp.MustCompile(`(?m)^\s*@queue\s*=\s*[:'"]([A-Za-z0-9_]+)`)

// reRubyResqueSelfPerform matches the `def self.perform` class method that
// Resque invokes when running the job.
var reRubyResqueSelfPerform = regexp.MustCompile(`(?m)^\s*def\s+self\.perform\b`)

// reRubyResqueDispatch captures `Resque.enqueue(Job, …)`,
// `Resque.enqueue_to(:q, Job, …)` and `Resque.enqueue_in(sec, Job, …)`
// dispatch sites. Group 1 = dispatch method, group 2 = job class. For the
// _to / _in forms the job class is the second positional arg, so we tolerate a
// leading non-class argument before the class name.
var reRubyResqueDispatch = regexp.MustCompile(`Resque\.(enqueue|enqueue_to|enqueue_in)\s*\(\s*(?:[^,()]+,\s*)?([A-Z][A-Za-z0-9_:]*)`)

// synthesizeRubyResque emits a SCOPE.ScheduledJob entity (resque:<Job>) with a
// TRIGGERS edge to `perform` for each Resque job class — a class that both
// declares `@queue` AND defines `def self.perform`. Both markers are required
// to avoid flagging arbitrary classes. Registers job IDs into seenJob (via
// emitJob) so the ENQUEUES pass can resolve dispatch targets.
func synthesizeRubyResque(
	src, path string,
	emitJob func(jobID, handler, schedule, framework string, extra map[string]string),
) {
	if !strings.Contains(src, "@queue") || !strings.Contains(src, "self.perform") {
		return
	}
	if !reRubyResqueSelfPerform.MatchString(src) {
		return
	}
	// Attribute each `@queue =` declaration to the class declared above it,
	// reusing the Sidekiq class-resolution regex.
	for _, qloc := range reRubyResqueQueueDecl.FindAllStringSubmatchIndex(src, -1) {
		queueName := src[qloc[2]:qloc[3]]
		classMatches := reRubySidekiqClass.FindAllStringSubmatch(src[:qloc[0]], -1)
		if len(classMatches) == 0 {
			continue
		}
		jobClass := classMatches[len(classMatches)-1][1]
		jobID := rubyResqueJobID(jobClass)
		emitJob(jobID, "perform", "", "resque", map[string]string{
			"job_class":  jobClass,
			"queue_name": queueName,
			"job_type":   "queue",
		})
	}
}

// synthesizeResqueEnqueueEdges emits ENQUEUES edges from the enclosing Ruby
// method of each `Resque.enqueue*` dispatch site to the job entity. Only emits
// when the job ID is present in knownJobs (the seenJob map) — preventing
// phantom-node creation when the job class is defined in another, un-indexed
// file. Dedupes on (caller, jobID).
func synthesizeResqueEnqueueEdges(
	src string,
	knownJobs map[string]bool,
	relationships []types.RelationshipRecord,
) []types.RelationshipRecord {
	if !strings.Contains(src, "Resque.enqueue") {
		return relationships
	}
	seenEdge := map[string]bool{}
	for _, idx := range reRubyResqueDispatch.FindAllStringSubmatchIndex(src, -1) {
		dispatchMethod := src[idx[2]:idx[3]]
		jobClass := src[idx[4]:idx[5]]
		jobID := rubyResqueJobID(jobClass)
		if !knownJobs[jobID] {
			continue
		}
		caller := rubyEnclosingMethod(src, idx[0])
		if caller == "" {
			caller = "module"
		}
		key := caller + "|" + jobID
		if seenEdge[key] {
			continue
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: "SCOPE.Operation:" + caller,
			ToID:   scheduledJobKind + ":" + jobID,
			Kind:   string(types.RelationshipKindEnqueues),
			Properties: map[string]string{
				"framework":       "resque",
				"pattern_type":    "resque_enqueue_synthesis",
				"job_class":       jobClass,
				"dispatch_method": dispatchMethod,
			},
		})
	}
	return relationships
}

// ---------------------------------------------------------------------------
// Python — RQ (Redis Queue) enqueue→handler ENQUEUES (#3628 area)
// ---------------------------------------------------------------------------
//
// RQ dispatches a job by handing the consumer callable directly to enqueue:
//
//	queue.enqueue(send_email, to, subject)          # callable reference
//	queue.enqueue_call(func="workers.email.gen")    # dotted string
//	queue.enqueue_call(func=generate_report)        # callable reference
//
// The callable IS the consumer (RQ's Worker just pops the job and calls it),
// so unlike Sidekiq/Resque there is no separate worker class to model — we
// emit an ENQUEUES edge straight from the enclosing function to
// Function:<callable>, the same target convention Celery's TRIGGERS uses.
// Honest-partial: dynamic callables (e.g. `enqueue(getattr(mod, name))`) yield
// no static name and are skipped.

// reRQEnqueueRef matches `queue.enqueue(callable` with a bare callable arg.
// Group 1 = queue var, group 2 = callable (possibly dotted). We exclude the
// _call form and string literals: a `'` / `"` immediately after `(` means a
// job-id string, not a callable, so the leading char class rejects quotes.
var reRQEnqueueRef = regexp.MustCompile(`(?m)(\w+)\.enqueue\s*\(\s*([A-Za-z_][\w.]*)`)

// reRQEnqueueCallStr matches `queue.enqueue_call(func="module.fn")`.
// Group 1 = dotted function path.
var reRQEnqueueCallStr = regexp.MustCompile(`(?m)\w+\.enqueue_call\s*\([^)]*func\s*=\s*["']([^"']+)["']`)

// reRQEnqueueCallRef matches `queue.enqueue_call(func=callable_ref)`.
// Group 1 = callable (possibly dotted).
var reRQEnqueueCallRef = regexp.MustCompile(`(?m)\w+\.enqueue_call\s*\([^)]*func\s*=\s*([A-Za-z_][\w.]*)`)

// rqCallableShortName returns the last dotted segment of a callable reference
// (`workers.email.send` → `send`), the bare function name the consumer def is
// keyed on. Returns "" for an empty or trailing-dot input.
func rqCallableShortName(callable string) string {
	parts := strings.Split(callable, ".")
	return parts[len(parts)-1]
}

// synthesizeRQEnqueueEdges emits ENQUEUES edges from the enclosing Python
// function of each RQ enqueue dispatch site to Function:<callable>. Dedupes on
// (caller, target). The producer-side `.enqueue_call` matches are stripped from
// the bare `.enqueue` scan via the distinct method name, so a single call site
// never double-emits.
func synthesizeRQEnqueueEdges(
	src string,
	relationships []types.RelationshipRecord,
) []types.RelationshipRecord {
	if !strings.Contains(src, ".enqueue") {
		return relationships
	}
	// RQ-only guard: require an rq import so a generic `q.enqueue(x)` on an
	// unrelated object in non-RQ code does not fabricate an edge.
	if !strings.Contains(src, "from rq") && !strings.Contains(src, "import rq") &&
		!strings.Contains(src, "rq.") {
		return relationships
	}

	seenEdge := map[string]bool{}
	// emit attributes an ENQUEUES edge. callEnd is the byte offset just past the
	// captured callable; when the next non-space char is '(' the "callable" is
	// actually a nested call (e.g. `getattr(mod, name)`), i.e. the real target
	// is computed dynamically — honest-partial, so we skip it.
	emit := func(callerOffset, callEnd int, callable string) {
		j := callEnd
		for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
			j++
		}
		if j < len(src) && src[j] == '(' {
			return
		}
		short := rqCallableShortName(callable)
		if short == "" {
			return
		}
		caller := enclosingFunction(src, callerOffset)
		if caller == "" {
			return
		}
		if caller == short {
			return // self-enqueue inside the callable's own def
		}
		key := caller + "|" + short
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		relationships = append(relationships, types.RelationshipRecord{
			FromID: "SCOPE.Operation:" + caller,
			ToID:   "Function:" + short,
			Kind:   string(types.RelationshipKindEnqueues),
			Properties: map[string]string{
				"framework":    "rq",
				"pattern_type": "rq_enqueue_synthesis",
				"callable":     callable,
			},
		})
	}

	// enqueue_call(func="…") / func=ref — handle first so the bare enqueue
	// scan below can skip these spans is unnecessary (distinct method name).
	for _, m := range reRQEnqueueCallStr.FindAllStringSubmatchIndex(src, -1) {
		// String func= is always a literal name, never a nested call: pass the
		// match end so the '(' guard is a no-op (next char is '"').
		emit(m[0], m[3], src[m[2]:m[3]])
	}
	for _, m := range reRQEnqueueCallRef.FindAllStringSubmatchIndex(src, -1) {
		emit(m[0], m[3], src[m[2]:m[3]])
	}
	// bare enqueue(callable). reRQEnqueueRef matches `.enqueue(` but NOT
	// `.enqueue_call(` because `_call` is not consumed by `\s*\(`.
	for _, m := range reRQEnqueueRef.FindAllStringSubmatchIndex(src, -1) {
		emit(m[0], m[5], src[m[4]:m[5]])
	}
	return relationships
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
