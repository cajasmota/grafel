package java

import "regexp"

// Quartz Java custom extractor: IJob consumers and JobBuilder/Trigger producers.
// Supports both Quartz 1.x (Job.execute) and Quartz 2.x (IJob/JobDetail/Trigger).

var quartzJavaFrameworks = map[string]bool{
	"quartz": true, "quartz-scheduler": true, "quartz_scheduler": true,
}

var (
	// class ClassName implements Job (or org.quartz.Job)
	qzIJobImplRE = regexp.MustCompile(
		`(?m)(?:public\s+)?(?:(?:abstract|final)\s+)?class\s+(\w+)\s+(?:extends\s+\w+\s+)?implements\s+[^{]*\bJob\b`,
	)
	// @DisallowConcurrentExecution class annotation
	qzDisallowRE = regexp.MustCompile(
		`@DisallowConcurrentExecution\b`,
	)
	// public void execute(JobExecutionContext ...) { — consumer method
	qzExecuteMethodRE = regexp.MustCompile(
		`(?m)(?:public\s+)?void\s+execute\s*\(\s*(?:JobExecutionContext|org\.quartz\.JobExecutionContext)`,
	)
	// JobBuilder.newJob(ClassName.class) or JobBuilder.newJob(ClassName.class)
	qzJobBuilderNewJobRE = regexp.MustCompile(
		`JobBuilder\.newJob\s*\(\s*(\w+)\.class\s*\)`,
	)
	// JobDetail job = newJob(ClassName.class) (static import variant)
	qzNewJobStaticRE = regexp.MustCompile(
		`\bnewJob\s*\(\s*(\w+)\.class\s*\)`,
	)
	// .withIdentity("job-name") — job or trigger identity
	qzWithIdentityRE = regexp.MustCompile(
		`\.withIdentity\s*\(\s*["']([^"']+)["']`,
	)
	// scheduler.scheduleJob(jobDetail, trigger) or scheduler.scheduleJob(trigger)
	qzScheduleJobRE = regexp.MustCompile(
		`(?m)(\w+)\.scheduleJob\s*\(`,
	)
	// TriggerBuilder.newTrigger() — producer side
	qzTriggerBuilderRE = regexp.MustCompile(
		`TriggerBuilder\.newTrigger\s*\(\s*\)`,
	)
	// scheduler.start() — marks the scheduler as active
	qzSchedulerStartRE = regexp.MustCompile(
		`(?m)(\w+)\.start\s*\(\s*\)`,
	)
)

// ExtractQuartzJava runs the Quartz Java extractor.
func ExtractQuartzJava(ctx PatternContext) PatternResult {
	var result PatternResult
	if ctx.Language != "java" {
		return result
	}

	source := ctx.Source
	fp := ctx.FilePath
	seenRefs := make(map[string]bool)
	seenRels := make(map[relKey]bool)

	// 1. Consumer: classes that implement Job
	type jobClassInfo struct {
		name   string
		offset int
	}
	var jobClasses []jobClassInfo

	for _, m := range qzIJobImplRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		taskID := "task:quartz:" + className
		line := lineOf(source, m[0])
		ref := "scope:service:quartz_job:" + fp + ":" + className
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: className, Kind: "SCOPE.Service",
			Subtype: "job_class", SourceFile: fp,
			LineStart: line, LineEnd: line,
			Provenance: "INFERRED_FROM_QUARTZ_IJOB",
			Ref:        ref,
			Properties: map[string]any{
				"framework":    "quartz",
				"pattern_type": "ijob_impl",
				"task_id":      taskID,
				"edge_kind":    "CONSUMES",
			},
		})
		jobClasses = append(jobClasses, jobClassInfo{className, m[0]})
	}

	// 2. Consumer: execute(JobExecutionContext) method — link to enclosing class
	for _, m := range qzExecuteMethodRE.FindAllStringIndex(source, -1) {
		enclosing := findEnclosingClass(source, m[0])
		if enclosing == "" {
			continue
		}
		taskID := "task:quartz:" + enclosing
		line := lineOf(source, m[0])
		ref := "scope:operation:quartz_execute:" + fp + ":" + enclosing
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: enclosing + ".execute", Kind: "SCOPE.Operation",
			Subtype: "job_execute", SourceFile: fp,
			LineStart: line, LineEnd: line,
			Provenance: "INFERRED_FROM_QUARTZ_EXECUTE",
			Ref:        ref,
			Properties: map[string]any{
				"framework":    "quartz",
				"pattern_type": "execute_method",
				"class":        enclosing,
				"task_id":      taskID,
				"edge_kind":    "CONSUMES",
			},
		})
	}

	// 3. Consumer: @DisallowConcurrentExecution — mark each annotated class
	for _, m := range qzDisallowRE.FindAllStringIndex(source, -1) {
		line := lineOf(source, m[0])
		// Find the class declared after this annotation
		rest := source[m[1]:]
		cls := ""
		if cm := classDeclRE.FindStringSubmatch(rest); cm != nil {
			cls = cm[1]
		}
		name := "@DisallowConcurrentExecution"
		if cls != "" {
			name = cls + ":" + name
		}
		ref := "scope:pattern:quartz_disallow:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Pattern",
			Subtype: "concurrency_policy", SourceFile: fp,
			LineStart: line, LineEnd: line,
			Provenance: "INFERRED_FROM_QUARTZ_DISALLOW_CONCURRENT",
			Ref:        ref,
			Properties: map[string]any{
				"framework":    "quartz",
				"pattern_type": "disallow_concurrent",
				"job_class":    cls,
				"edge_kind":    "CONSUMES",
			},
		})
	}

	// 4. Producer: JobBuilder.newJob(ClassName.class)
	for _, m := range qzJobBuilderNewJobRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		taskID := "task:quartz:" + className
		line := lineOf(source, m[0])
		// Attempt to find .withIdentity("name") after this match
		rest := source[m[1]:]
		jobName := ""
		if im := qzWithIdentityRE.FindStringSubmatch(rest); im != nil {
			jobName = im[1]
		}
		name := "JobBuilder.newJob<" + className + ">"
		if jobName != "" {
			name = "job:" + jobName
		}
		ref := "scope:operation:quartz_job_builder:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Operation",
			Subtype: "job_builder", SourceFile: fp,
			LineStart: line, LineEnd: line,
			Provenance: "INFERRED_FROM_QUARTZ_JOB_BUILDER",
			Ref:        ref,
			Properties: map[string]any{
				"framework":    "quartz",
				"pattern_type": "job_builder",
				"job_class":    className,
				"task_id":      taskID,
				"job_name":     jobName,
				"edge_kind":    "PRODUCES",
			},
		})
	}

	// 5. Producer: newJob(ClassName.class) — static import variant
	for _, m := range qzNewJobStaticRE.FindAllStringSubmatchIndex(source, -1) {
		className := source[m[2]:m[3]]
		// Skip if already matched by full JobBuilder form (avoid double-count)
		alreadyMatched := false
		for _, em := range result.Entities {
			if p, ok := em.Properties["job_class"]; ok && p == className && em.Subtype == "job_builder" {
				alreadyMatched = true
				break
			}
		}
		if alreadyMatched {
			continue
		}
		taskID := "task:quartz:" + className
		line := lineOf(source, m[0])
		name := "newJob<" + className + ">"
		ref := "scope:operation:quartz_new_job:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Operation",
			Subtype: "job_builder", SourceFile: fp,
			LineStart: line, LineEnd: line,
			Provenance: "INFERRED_FROM_QUARTZ_NEW_JOB_STATIC",
			Ref:        ref,
			Properties: map[string]any{
				"framework":    "quartz",
				"pattern_type": "new_job_static",
				"job_class":    className,
				"task_id":      taskID,
				"edge_kind":    "PRODUCES",
			},
		})
	}

	// 6. Producer: TriggerBuilder.newTrigger()
	for _, m := range qzTriggerBuilderRE.FindAllStringIndex(source, -1) {
		line := lineOf(source, m[0])
		rest := source[m[1]:]
		triggerName := ""
		if im := qzWithIdentityRE.FindStringSubmatch(rest); im != nil {
			triggerName = im[1]
		}
		name := "TriggerBuilder.newTrigger"
		if triggerName != "" {
			name = "trigger:" + triggerName
		}
		ref := "scope:operation:quartz_trigger:" + fp + ":" + name
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: name, Kind: "SCOPE.Operation",
			Subtype: "trigger", SourceFile: fp,
			LineStart: line, LineEnd: line,
			Provenance: "INFERRED_FROM_QUARTZ_TRIGGER_BUILDER",
			Ref:        ref,
			Properties: map[string]any{
				"framework":    "quartz",
				"pattern_type": "trigger_builder",
				"trigger_name": triggerName,
				"edge_kind":    "PRODUCES",
			},
		})
	}

	// 7. Producer: scheduler.scheduleJob(...)
	for _, m := range qzScheduleJobRE.FindAllStringSubmatchIndex(source, -1) {
		schedulerVar := source[m[2]:m[3]]
		line := lineOf(source, m[0])
		ref := "scope:operation:quartz_schedule:" + fp + ":" + schedulerVar
		addEntity(&result, seenRefs, SecondaryEntity{
			Name: schedulerVar + ".scheduleJob", Kind: "SCOPE.Operation",
			Subtype: "schedule_job", SourceFile: fp,
			LineStart: line, LineEnd: line,
			Provenance: "INFERRED_FROM_QUARTZ_SCHEDULE_JOB",
			Ref:        ref,
			Properties: map[string]any{
				"framework":     "quartz",
				"pattern_type":  "schedule_job",
				"scheduler_var": schedulerVar,
				"edge_kind":     "PRODUCES",
			},
		})
	}

	// PRODUCES→CONSUMES cross-edges: link job builders to job class consumers
	for _, producer := range result.Entities {
		if producer.Subtype != "job_builder" {
			continue
		}
		jobClass, _ := producer.Properties["job_class"].(string)
		if jobClass == "" {
			continue
		}
		for _, consumer := range result.Entities {
			if consumer.Subtype == "job_class" && consumer.Name == jobClass {
				addRel(&result, seenRels, Relationship{
					SourceRef:        producer.Ref,
					TargetRef:        consumer.Ref,
					RelationshipType: "PRODUCES",
					Properties:       map[string]string{"framework": "quartz", "task_id": "task:quartz:" + jobClass},
				})
			}
		}
	}

	_ = quartzJavaFrameworks // prevent unused var compile error; gate used at call sites if needed
	return result
}
