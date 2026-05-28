// Tests for the BullMQ / Bull topic-attribution pass added by #2865.
//
// Covers, per the rabbitmq_edges_test.go convention:
//   - producer side: new Queue('name') + queue.add('job', …) → SCOPE.Queue + PUBLISHES_TO
//   - consumer side: new Worker('name', handler) → SCOPE.Queue + SUBSCRIBES_TO
//   - Bull v3 queue.process() consumer registration
//   - var→name binding so .add()/.process() attribute to the right queue
//   - canonical cross-repo ID (bullmq:<name>) identical on producer + consumer
//   - non-BullMQ files emit nothing (pre-filter gate)
package engine

import (
	"testing"
)

// runBullMQDetect is a lightweight in-process driver mirroring runRabbitMQDetect.
func runBullMQDetect(t *testing.T, lang, path, src string) ([]entityResult, []relResult) {
	t.Helper()
	res := applyBullMQEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	out := make([]entityResult, 0, len(res.Entities))
	for _, e := range res.Entities {
		out = append(out, entityResult{kind: e.Kind, name: e.Name, props: e.Properties})
	}
	relOut := make([]relResult, 0, len(res.Relationships))
	for _, r := range res.Relationships {
		relOut = append(relOut, relResult{from: r.FromID, to: r.ToID, kind: r.Kind, props: r.Properties})
	}
	return out, relOut
}

func TestBullMQ_Producer_NewQueueAndAdd(t *testing.T) {
	src := `import { Queue } from 'bullmq';

const emailQueue = new Queue('emails', { connection });

export async function enqueueWelcome(userId) {
  await emailQueue.add('welcome', { userId });
}
`
	ents, rels := runBullMQDetect(t, "typescript", "producer.ts", src)

	q := queueByName(ents, "bullmq:emails")
	if q == nil {
		t.Fatalf("expected SCOPE.Queue bullmq:emails, got %+v", ents)
	}
	if q.props["broker"] != "bullmq" || q.props["queue_name"] != "emails" {
		t.Errorf("queue props = %+v, want broker=bullmq queue_name=emails", q.props)
	}

	pub := relsByKind(rels, "PUBLISHES_TO")
	if len(pub) == 0 {
		t.Fatalf("expected PUBLISHES_TO edges, got %+v", rels)
	}
	var sawJob bool
	for _, r := range pub {
		if r.to != "SCOPE.Queue:bullmq:emails" {
			t.Errorf("PUBLISHES_TO ToID = %q, want SCOPE.Queue:bullmq:emails", r.to)
		}
		if r.props["job_name"] == "welcome" {
			sawJob = true
		}
	}
	if !sawJob {
		t.Errorf("expected a PUBLISHES_TO edge carrying job_name=welcome, got %+v", pub)
	}
}

func TestBullMQ_Consumer_NewWorker(t *testing.T) {
	src := `import { Worker } from 'bullmq';

const worker = new Worker('emails', async (job) => {
  await sendEmail(job.data);
});
`
	ents, rels := runBullMQDetect(t, "typescript", "worker.ts", src)

	if queueByName(ents, "bullmq:emails") == nil {
		t.Fatalf("expected SCOPE.Queue bullmq:emails, got %+v", ents)
	}
	sub := relsByKind(rels, "SUBSCRIBES_TO")
	if len(sub) == 0 {
		t.Fatalf("expected SUBSCRIBES_TO edge, got %+v", rels)
	}
	if sub[0].to != "SCOPE.Queue:bullmq:emails" {
		t.Errorf("SUBSCRIBES_TO ToID = %q, want SCOPE.Queue:bullmq:emails", sub[0].to)
	}
}

func TestBullMQ_BullV3_Process(t *testing.T) {
	src := `const Queue = require('bull');
const audioQueue = new Queue('audio transcoding');

audioQueue.process(async (job) => {
  return transcode(job.data);
});
`
	ents, rels := runBullMQDetect(t, "javascript", "consumer.js", src)
	if queueByName(ents, "bullmq:audio transcoding") == nil {
		t.Fatalf("expected SCOPE.Queue bullmq:audio transcoding, got %+v", ents)
	}
	if len(relsByKind(rels, "SUBSCRIBES_TO")) == 0 {
		t.Errorf("expected SUBSCRIBES_TO from queue.process(), got %+v", rels)
	}
}

// TestBullMQ_CrossRepoID asserts the producer-side `new Queue('jobs')` and the
// consumer-side `new Worker('jobs')` produce IDENTICAL synthetic IDs so the
// import-channel linker joins them across repos with no new matching code.
func TestBullMQ_CrossRepoID(t *testing.T) {
	prod := `import { Queue } from 'bullmq';
const q = new Queue('jobs');
function enqueue() { q.add('do', {}); }
`
	cons := `import { Worker } from 'bullmq';
new Worker('jobs', async (job) => run(job));
`
	pEnts, _ := runBullMQDetect(t, "typescript", "a.ts", prod)
	cEnts, _ := runBullMQDetect(t, "typescript", "b.ts", cons)

	if queueByName(pEnts, "bullmq:jobs") == nil {
		t.Fatalf("producer side missing bullmq:jobs, got %+v", pEnts)
	}
	if queueByName(cEnts, "bullmq:jobs") == nil {
		t.Fatalf("consumer side missing bullmq:jobs, got %+v", cEnts)
	}
}

func TestBullMQ_NonBullFileEmitsNothing(t *testing.T) {
	src := `const arr = [];
arr.add(1);
class Foo { process() {} }
`
	ents, rels := runBullMQDetect(t, "typescript", "unrelated.ts", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-BullMQ file should emit nothing, got %d entities %d rels", len(ents), len(rels))
	}
}

func TestBullMQ_NonJSLanguageSkipped(t *testing.T) {
	src := `q = Queue('emails'); new Worker('emails')`
	ents, rels := runBullMQDetect(t, "python", "x.py", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-JS/TS language should be skipped, got %d entities %d rels", len(ents), len(rels))
	}
}
