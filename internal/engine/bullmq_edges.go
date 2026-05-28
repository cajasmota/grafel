// BullMQ / Bull producer/consumer cross-repo topic attribution — #2865.
//
// Bull (v3/v4) and BullMQ (v1+) are Redis-backed Node task queues. A queue
// is named at construction (`new Queue('emails')` / `new Worker('emails',
// …)`), and that NAME is the cross-process rendezvous point: a producer in
// one service `new Queue('emails')` + `q.add('welcome', data)` and a consumer
// in another `new Worker('emails', handler)` are talking over the same
// logical topic. Before this pass BullMQ queues were only recognised as
// scheduled-job repeat sources (scheduled_jobs_edges.go) — they carried no
// MessageTopic/SCOPE.Queue node, so the cross-repo topic linker (P7 in
// internal/links/topic_pass.go) had nothing to join on and BullMQ
// topic_attribution stayed partial.
//
// This pass emits one synthetic SCOPE.Queue entity per queue name, keyed by
// the canonical ID `bullmq:<name>` (identical across repos, so the existing
// import-channel linker matches producer and consumer sides with no new
// linker code — same technique as kafka_edges.go / rabbitmq_edges.go), plus:
//
//	PUBLISHES_TO  enclosing fn → bullmq:<name>   (new Queue, queue.add, FlowProducer.add)
//	SUBSCRIBES_TO enclosing fn → bullmq:<name>   (new Worker, queue.process)
//
// The queue-variable → queue-name binding is tracked file-locally so an
// `.add()` / `.process()` call on a known queue variable attributes to the
// right topic even when the literal name is only at the construction site.
//
// Append-only — never modifies or removes existing entities or edges, so this
// pass cannot regress the surrounding pipeline's bug-rate. Refs #2865.
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// bullmqQueueEntityKind reuses SCOPE.Queue, matching the broker synthetics in
// rabbitmq_edges.go / redis_pubsub_edges.go so the topic pass treats BullMQ
// queues identically.
const bullmqQueueEntityKind = "SCOPE.Queue"

// bullmqSynthesisSupportsLanguage reports whether applyBullMQEdges can emit
// synthetics for `lang`. BullMQ is a Node library, so only JS/TS qualify.
func bullmqSynthesisSupportsLanguage(lang string) bool {
	switch lang {
	case "javascript", "typescript":
		return true
	default:
		return false
	}
}

// bullmqQueueID returns the canonical synthetic ID for a BullMQ queue name.
// Identical across repos so the cross-repo linker joins producer and consumer
// sides on shared entity ID without any new matching code.
func bullmqQueueID(name string) string {
	return "bullmq:" + name
}

// bullmqNewQueueRe captures `new Queue('name'` and `new QueueEvents('name'`
// (the producer-side construction). Group 1 = the variable assigned to (when
// present), group 2 = the constructor (Queue / QueueEvents), group 3 = the
// queue name literal.
//
//	const emailQueue = new Queue('emails', { connection })
//	new Queue("emails")
var bullmqNewQueueRe = regexp.MustCompile(
	`(?:(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*)?new\s+(Queue|QueueEvents)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`,
)

// bullmqNewWorkerRe captures the consumer-side `new Worker('name', handler)`.
// Group 1 = optional assigned variable, group 2 = queue name literal.
var bullmqNewWorkerRe = regexp.MustCompile(
	`(?:(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*)?new\s+Worker\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`,
)

// bullmqQueueAddRe captures `<var>.add('jobName', data, …)` — a producer call.
// Group 1 = the queue variable, group 2 = the job name. The job name is NOT
// the topic; the topic is the queue the variable was bound to. We record the
// job name as an edge property.
var bullmqQueueAddRe = regexp.MustCompile(
	`([A-Za-z_$][\w$]*)\s*\.\s*add\s*\(\s*['"` + "`" + `]([^'"` + "`" + `\n\r]+)['"` + "`" + `]`,
)

// bullmqQueueProcessRe captures Bull v3 `<var>.process(…)` consumer
// registration. Group 1 = the queue variable.
var bullmqQueueProcessRe = regexp.MustCompile(
	`([A-Za-z_$][\w$]*)\s*\.\s*process\s*\(`,
)

// applyBullMQEdges APPENDS SCOPE.Queue entities + PUBLISHES_TO / SUBSCRIBES_TO
// edges for Bull / BullMQ queues. Append-only.
func applyBullMQEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships
	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}
	if !bullmqSynthesisSupportsLanguage(lang) {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	// Fast pre-filter: a file with no bull/bullmq import and no Queue/Worker
	// construction cannot define a queue. Guards against matching the generic
	// `.add(` / `.process(` on unrelated objects in non-BullMQ files.
	if !strings.Contains(src, "bull") && !strings.Contains(src, "bullmq") &&
		!strings.Contains(src, "BullMQ") && !strings.Contains(src, "new Queue") &&
		!strings.Contains(src, "new Worker") {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	seenQueue := map[string]bool{}
	seenEdge := map[string]bool{}
	// varToQueue maps a file-local queue variable name to its queue name, so
	// `.add()` / `.process()` calls on the variable attribute to the right
	// topic even when the literal only appears at the construction site.
	varToQueue := map[string]string{}

	emitQueue := func(name, role string) {
		qID := bullmqQueueID(name)
		if seenQueue[qID] {
			return
		}
		seenQueue[qID] = true
		entities = append(entities, types.EntityRecord{
			Name:       qID,
			Kind:       bullmqQueueEntityKind,
			SourceFile: "", // empty so identical names collapse + match cross-repo
			Language:   lang,
			Properties: map[string]string{
				"broker":       "bullmq",
				"queue_name":   name,
				"pattern_type": "bullmq_synthesis",
				"role":         role,
			},
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	emitEdge := func(caller, name, edgeKind string, props map[string]string) {
		if caller == "" || name == "" {
			return
		}
		qID := bullmqQueueID(name)
		key := edgeKind + "|" + caller + "|" + qID
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		base := map[string]string{
			"broker":       "bullmq",
			"pattern_type": "bullmq_synthesis",
		}
		for k, v := range props {
			if v != "" {
				base[k] = v
			}
		}
		relationships = append(relationships, types.RelationshipRecord{
			FromID:     fmt.Sprintf("Function:%s", caller),
			ToID:       fmt.Sprintf("%s:%s", bullmqQueueEntityKind, qID),
			Kind:       edgeKind,
			Properties: base,
		})
	}

	enclosing := func(offset int) string { return findEnclosingNodeName(src, offset) }

	// Producer side: new Queue('name') — declares a queue this module
	// publishes to. Record the var→name binding and emit a PUBLISHES_TO from
	// the enclosing function (the module wires the producer here).
	for _, m := range bullmqNewQueueRe.FindAllStringSubmatchIndex(src, -1) {
		varName := extractGroupFromIndex(src, m, 1)
		name := extractGroupFromIndex(src, m, 3)
		if name == "" {
			continue
		}
		emitQueue(name, "producer")
		if varName != "" {
			varToQueue[varName] = name
		}
		emitEdge(enclosing(m[0]), name, publishesToEdgeKind, map[string]string{
			"messaging_layer": "bullmq",
		})
	}

	// Consumer side: new Worker('name', handler) — registers a consumer.
	for _, m := range bullmqNewWorkerRe.FindAllStringSubmatchIndex(src, -1) {
		varName := extractGroupFromIndex(src, m, 1)
		name := extractGroupFromIndex(src, m, 2)
		if name == "" {
			continue
		}
		emitQueue(name, "consumer")
		if varName != "" {
			varToQueue[varName] = name
		}
		emitEdge(enclosing(m[0]), name, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "bullmq",
		})
	}

	// Producer call: queueVar.add('jobName', data). Attribute to the queue the
	// variable was bound to; record the job name as an edge property.
	for _, m := range bullmqQueueAddRe.FindAllStringSubmatchIndex(src, -1) {
		qVar := extractGroupFromIndex(src, m, 1)
		jobName := extractGroupFromIndex(src, m, 2)
		name, ok := varToQueue[qVar]
		if !ok {
			continue
		}
		emitEdge(enclosing(m[0]), name, publishesToEdgeKind, map[string]string{
			"messaging_layer": "bullmq",
			"job_name":        jobName,
		})
	}

	// Consumer call (Bull v3): queueVar.process(handler).
	for _, m := range bullmqQueueProcessRe.FindAllStringSubmatchIndex(src, -1) {
		qVar := extractGroupFromIndex(src, m, 1)
		name, ok := varToQueue[qVar]
		if !ok {
			continue
		}
		emitQueue(name, "consumer")
		emitEdge(enclosing(m[0]), name, subscribesToEdgeKind, map[string]string{
			"messaging_layer": "bullmq",
		})
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}
