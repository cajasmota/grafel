package javascript

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extreg.Register("custom_js_bull", &bullExtractor{})
}

type bullExtractor struct{}

func (e *bullExtractor) Language() string { return "custom_js_bull" }

var (
	reBullQueueNew = regexp.MustCompile(
		"(?:const|let|var)\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*=\\s*new\\s+(?:Queue|QueueScheduler)\\s*\\(\\s*" +
			"['\"`]([A-Za-z0-9_\\-/: ]+)['\"`]",
	)
	reBullQueueExport = regexp.MustCompile(
		"export\\s+(?:const|let)\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*=\\s*new\\s+(?:Queue|QueueScheduler)\\s*\\(\\s*" +
			"['\"`]([A-Za-z0-9_\\-/: ]+)['\"`]",
	)
	reBullWorkerNew = regexp.MustCompile(
		"(?:(?:const|let|var)\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*=\\s*)?new\\s+Worker\\s*\\(\\s*" +
			"['\"`]([A-Za-z0-9_\\-/: ]+)['\"`]",
	)
	reBullQueueAdd = regexp.MustCompile(
		"([A-Za-z_][A-Za-z0-9_.]*)\\.add\\s*\\(\\s*['\"`]([A-Za-z0-9_\\-/: ]+)['\"`]",
	)
	reBullQueueProcess = regexp.MustCompile(
		`([A-Za-z_][A-Za-z0-9_.]*)\.process\s*\(`,
	)
	reBullQueueOn = regexp.MustCompile(
		"([A-Za-z_][A-Za-z0-9_.]*)\\s*\\.\\s*on\\s*\\(\\s*['\"`](completed|failed|progress|stalled|error|drained|paused|resumed|cleaned|removed|waiting|active|delayed)['\"`]",
	)
	reBullRepeatKey    = regexp.MustCompile(`\brepeat\s*:`)
	reBullFlowProducer = regexp.MustCompile(
		`new\s+FlowProducer\s*\(`,
	)
)

func (e *bullExtractor) Extract(ctx context.Context, file extreg.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/javascript")
	_, span := tracer.Start(ctx, "indexer.bull_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "bull"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	src := string(file.Content)
	lang := strings.ToLower(file.Language)
	if lang != "typescript" && lang != "javascript" {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)
	addEntity := func(ent types.EntityRecord) {
		key := fmt.Sprintf("%s:%s:%s", ent.Kind, ent.Name, ent.SourceFile)
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	// Track queue variable names for DEPENDS_ON relationships
	queueVars := make(map[string]string) // varName -> queueName

	// Queue instantiation (export form first to avoid double-matching)
	exportedQueueVars := make(map[string]bool)
	for _, m := range reBullQueueExport.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		queueName := src[m[4]:m[5]]
		exportedQueueVars[varName] = true
		queueVars[varName] = queueName
		name := fmt.Sprintf("queue:%s", queueName)
		ent := makeEntity(name, "SCOPE.Service", "queue", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "bull", "queue_name", queueName, "var_name", varName,
			"provenance", "INFERRED_FROM_BULL_QUEUE")
		addEntity(ent)
	}
	for _, m := range reBullQueueNew.FindAllStringSubmatchIndex(src, -1) {
		varName := src[m[2]:m[3]]
		if exportedQueueVars[varName] {
			continue
		}
		queueName := src[m[4]:m[5]]
		queueVars[varName] = queueName
		name := fmt.Sprintf("queue:%s", queueName)
		ent := makeEntity(name, "SCOPE.Service", "queue", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "bull", "queue_name", queueName, "var_name", varName,
			"provenance", "INFERRED_FROM_BULL_QUEUE")
		addEntity(ent)
	}

	// Worker instantiation
	for _, m := range reBullWorkerNew.FindAllStringSubmatchIndex(src, -1) {
		queueName := ""
		if m[4] >= 0 {
			queueName = src[m[4]:m[5]]
		}
		varName := ""
		if m[2] >= 0 {
			varName = src[m[2]:m[3]]
		}
		name := fmt.Sprintf("worker:%s", queueName)
		ent := makeEntity(name, "SCOPE.Service", "worker", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "bull", "queue_name", queueName, "var_name", varName,
			"provenance", "INFERRED_FROM_BULL_WORKER")
		addEntity(ent)
	}

	// queue.add("jobName", data[, opts])
	for _, m := range reBullQueueAdd.FindAllStringSubmatchIndex(src, -1) {
		varExpr := src[m[2]:m[3]]
		jobName := src[m[4]:m[5]]
		// Check if this add() call has a repeat key by scanning ahead for the closing paren
		hasRepeat := false
		openParen := strings.Index(src[m[0]:], "(")
		if openParen >= 0 {
			start := m[0] + openParen
			depth := 0
			end := start
			for i := start; i < len(src) && i < start+2000; i++ {
				switch src[i] {
				case '(':
					depth++
				case ')':
					depth--
					if depth == 0 {
						end = i
						i = start + 2001 // break
					}
				}
			}
			segment := src[start : end+1]
			hasRepeat = reBullRepeatKey.MatchString(segment)
		}

		if hasRepeat {
			name := fmt.Sprintf("repeatable:%s", jobName)
			ent := makeEntity(name, "SCOPE.Operation", "job", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "bull", "job_name", jobName, "queue_var", varExpr,
				"is_repeatable", "true", "provenance", "INFERRED_FROM_BULL_REPEATABLE")
			addEntity(ent)
		} else {
			name := fmt.Sprintf("job:%s", jobName)
			ent := makeEntity(name, "SCOPE.Operation", "job", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "bull", "job_name", jobName, "queue_var", varExpr,
				"provenance", "INFERRED_FROM_BULL_JOB_ADD")
			addEntity(ent)
		}
	}

	// queue.process() legacy
	for _, m := range reBullQueueProcess.FindAllStringSubmatchIndex(src, -1) {
		varExpr := src[m[2]:m[3]]
		name := fmt.Sprintf("process:%s", varExpr)
		ent := makeEntity(name, "SCOPE.Operation", "job", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "bull", "queue_var", varExpr,
			"provenance", "INFERRED_FROM_BULL_PROCESS")
		addEntity(ent)
	}

	// queue.on("event")
	for _, m := range reBullQueueOn.FindAllStringSubmatchIndex(src, -1) {
		varExpr := src[m[2]:m[3]]
		event := src[m[4]:m[5]]
		name := fmt.Sprintf("event:%s:%s", varExpr, event)
		ent := makeEntity(name, "SCOPE.Pattern", "queue_event", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "bull", "queue_var", varExpr, "event", event,
			"provenance", "INFERRED_FROM_BULL_EVENT")
		addEntity(ent)
	}

	// FlowProducer
	for _, m := range reBullFlowProducer.FindAllStringIndex(src, -1) {
		ent := makeEntity("FlowProducer", "SCOPE.Service", "flow_producer", file.Path, file.Language, lineOf(src, m[0]))
		setProps(&ent, "framework", "bull", "provenance", "INFERRED_FROM_BULL_FLOW_PRODUCER")
		addEntity(ent)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
