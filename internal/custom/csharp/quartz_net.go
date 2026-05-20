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
	extractor.Register("custom_csharp_quartz_net", &quartzNetExtractor{})
}

// quartzNetExtractor detects Quartz.NET scheduler patterns.
//
// Consumers: classes implementing IJob (Execute(IJobExecutionContext)).
// Also: [DisallowConcurrentExecution] decorated classes.
//
// Producers:
//   - JobBuilder.Create<TJobType>() — job detail construction
//   - TriggerBuilder / TriggerKey.Create<T>() — trigger that fires the job
//   - scheduler.ScheduleJob(job, trigger)
type quartzNetExtractor struct{}

func (e *quartzNetExtractor) Language() string { return "custom_csharp_quartz_net" }

var (
	// class ClassName : IJob
	qnIJobImplRe = regexp.MustCompile(
		`(?m)class\s+(\w+)\s*(?::\s*[^{]*\bIJob\b)`,
	)
	// [DisallowConcurrentExecution] — marks a consumer job class
	qnDisallowRe = regexp.MustCompile(
		`\[DisallowConcurrentExecution(?:\([^)]*\))?\]`,
	)
	// JobBuilder.Create<TypeName>()
	qnJobBuilderRe = regexp.MustCompile(
		`JobBuilder\.Create\s*<\s*(\w+)\s*>\s*\(`,
	)
	// TriggerBuilder.Create().WithIdentity("name") or similar identity patterns
	qnTriggerBuilderRe = regexp.MustCompile(
		`TriggerBuilder\.Create\s*\(\s*\)`,
	)
	// scheduler.ScheduleJob(jobDetail, trigger)
	qnScheduleJobRe = regexp.MustCompile(
		`(?m)(\w+)\.ScheduleJob\s*\(`,
	)
	// IJobDetail / IJobDetail named variable: var job = JobBuilder.Create<T>().WithIdentity("name")
	qnJobIdentityRe = regexp.MustCompile(
		`\.WithIdentity\s*\(\s*["']([^"']+)["']`,
	)
)

func (e *quartzNetExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/csharp")
	_, span := tracer.Start(ctx, "indexer.quartz_net_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "quartz.net"),
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

	// 1. Consumer: class implementing IJob
	for _, idx := range qnIJobImplRe.FindAllStringSubmatchIndex(src, -1) {
		className := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		taskID := "task:quartz.net:" + className
		ent := makeEntity(className, "SCOPE.Service", "job_class", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "quartz.net",
			"pattern_type", "ijob_impl",
			"task_id", taskID,
			"edge_kind", "CONSUMES",
			"provenance", "INFERRED_FROM_QUARTZ_NET_IJOB",
		)
		add(ent)
	}

	// 2. Consumer: [DisallowConcurrentExecution]
	for _, idx := range qnDisallowRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, idx[0])
		ent := makeEntity("DisallowConcurrentExecution@line"+intStr(line), "SCOPE.Pattern", "concurrency_policy", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "quartz.net",
			"pattern_type", "disallow_concurrent",
			"edge_kind", "CONSUMES",
			"provenance", "INFERRED_FROM_QUARTZ_NET_DISALLOW_CONCURRENT",
		)
		add(ent)
	}

	// 3. Producer: JobBuilder.Create<TypeName>()
	for _, idx := range qnJobBuilderRe.FindAllStringSubmatchIndex(src, -1) {
		typeName := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		taskID := "task:quartz.net:" + typeName
		ent := makeEntity("JobBuilder.Create<"+typeName+">", "SCOPE.Operation", "job_builder", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "quartz.net",
			"pattern_type", "job_builder",
			"job_type", typeName,
			"task_id", taskID,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_QUARTZ_NET_JOB_BUILDER",
		)
		add(ent)
	}

	// 4. Producer: TriggerBuilder.Create()
	for _, idx := range qnTriggerBuilderRe.FindAllStringIndex(src, -1) {
		line := lineOf(src, idx[0])
		// Try to extract trigger identity from .WithIdentity("name") after this position
		rest := src[idx[1]:]
		triggerName := ""
		if im := qnJobIdentityRe.FindStringSubmatch(rest); im != nil {
			triggerName = im[1]
		}
		name := "TriggerBuilder.Create"
		if triggerName != "" {
			name = "trigger:" + triggerName
		}
		ent := makeEntity(name, "SCOPE.Operation", "trigger", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "quartz.net",
			"pattern_type", "trigger_builder",
			"trigger_name", triggerName,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_QUARTZ_NET_TRIGGER_BUILDER",
		)
		add(ent)
	}

	// 5. Producer: scheduler.ScheduleJob(job, trigger)
	for _, idx := range qnScheduleJobRe.FindAllStringSubmatchIndex(src, -1) {
		schedulerVar := src[idx[2]:idx[3]]
		line := lineOf(src, idx[0])
		ent := makeEntity(schedulerVar+".ScheduleJob", "SCOPE.Operation", "schedule_job", file.Path, file.Language, line)
		setProps(&ent,
			"framework", "quartz.net",
			"pattern_type", "schedule_job",
			"scheduler_var", schedulerVar,
			"edge_kind", "PRODUCES",
			"provenance", "INFERRED_FROM_QUARTZ_NET_SCHEDULE_JOB",
		)
		add(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(out)))
	return out, nil
}
