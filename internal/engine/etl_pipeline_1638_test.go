// End-to-end verification for the polyglot-platform multi-broker ETL pipeline
// fixture (#1638). Drives every broker pass over the actual fixture source and
// asserts the pipeline is connected stage->stage: each hop's producer emits a
// synthetic channel/topic/queue whose ID is matched by the next stage's
// consumer (cross-repo linking is pure shared-ID matching, so equal IDs ⇒ a
// connected edge in Topology + a traceable Flow).
//
// Brokers exercised: SQS, SNS, Redis Streams, Google Pub/Sub, RabbitMQ
// (aio-pika — the NEW extractor added by this change).
package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

var etlFixtureDir = func() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata/etl_pipeline_1638")
}()

// brokerPass is the shared signature of every per-file broker synthesis pass.
type brokerPass func(args DetectorPassArgs) DetectorPassResult

// allBrokerPasses runs every broker pass over a file and returns the union of
// emitted entities + relationships, mirroring detector.go's pass ordering.
func allBrokerPasses(t *testing.T, path string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture not present (%v) — skipping e2e", err)
	}
	var ents []types.EntityRecord
	var rels []types.RelationshipRecord
	for _, pass := range []brokerPass{
		applyKafkaWrapperEdges, // SNS code-level publish
		applyRabbitMQEdges,     // pika + aio-pika
		applySQSEdges,
		applyPubSubEdges,
		applyRedisPubSubEdges,
	} {
		res := pass(DetectorPassArgs{Lang: "python", Path: path, Content: content, Entities: ents, Relationships: rels})
		ents, rels = res.Entities, res.Relationships
	}
	return ents, rels
}

// hasEntity reports whether an emitted synthetic channel entity carries sub in
// its ID or Name. Broker passes key synthetic queues/topics on the Name field
// (SourceFile left empty for cross-repo collapse), so check both.
func hasEntityID(ents []types.EntityRecord, sub string) bool {
	for _, e := range ents {
		if containsStr(e.ID, sub) || containsStr(e.Name, sub) {
			return true
		}
	}
	return false
}

// hasEdgeTo reports whether a relationship of kind targets an ID containing sub.
func hasEdgeTo(rels []types.RelationshipRecord, kind, sub string) bool {
	for _, r := range rels {
		if r.Kind == kind && containsStr(r.ToID, sub) {
			return true
		}
	}
	return false
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestETLPipeline1638_EndToEnd(t *testing.T) {
	stage := func(name string) ([]types.EntityRecord, []types.RelationshipRecord) {
		return allBrokerPasses(t, filepath.Join(etlFixtureDir, name))
	}

	// Hop 1: ingest -> SQS etl-ingest-queue (producer).
	e1, r1 := stage("stage1_ingest.py")
	if !hasEntityID(e1, "etl-ingest-queue") || !hasEdgeTo(r1, publishesToEdgeKind, "etl-ingest-queue") {
		t.Errorf("stage1: missing SQS producer for etl-ingest-queue")
	}

	// Hop 2: SQS consumer + SNS etl-transformed-topic producer.
	e2, r2 := stage("stage2_transform.py")
	if !hasEdgeTo(r2, subscribesToEdgeKind, "etl-ingest-queue") {
		t.Errorf("stage2: missing SQS consumer for etl-ingest-queue")
	}
	if !hasEntityID(e2, "etl-transformed-topic") || !hasEdgeTo(r2, publishesToEdgeKind, "etl-transformed-topic") {
		t.Errorf("stage2: missing SNS producer for etl-transformed-topic")
	}

	// Hop 3: SNS consumer (subscribe) + Redis stream producer.
	_, r3 := stage("stage3_enrich.py")
	if !hasEdgeTo(r3, subscribesToEdgeKind, "etl-transformed-topic") {
		t.Errorf("stage3: missing SNS consumer for etl-transformed-topic")
	}
	if !hasEdgeTo(r3, publishesToEdgeKind, "etl-enriched-stream") {
		t.Errorf("stage3: missing Redis-stream producer for etl-enriched-stream")
	}

	// Hop 4: Redis stream consumer + Google Pub/Sub producer.
	_, r4 := stage("stage4_dedup.py")
	if !hasEdgeTo(r4, subscribesToEdgeKind, "etl-enriched-stream") {
		t.Errorf("stage4: missing Redis-stream consumer for etl-enriched-stream")
	}
	if !hasEdgeTo(r4, publishesToEdgeKind, "etl-deduped-topic") {
		t.Errorf("stage4: missing Pub/Sub producer for etl-deduped-topic")
	}

	// Hop 5: Pub/Sub consumer + RabbitMQ (aio-pika) producer.
	_, r5 := stage("stage5_aggregate.py")
	if !hasEdgeTo(r5, subscribesToEdgeKind, "etl-deduped-topic") {
		t.Errorf("stage5: missing Pub/Sub consumer for etl-deduped-topic")
	}
	if !hasEdgeTo(r5, publishesToEdgeKind, "etl-load-queue") {
		t.Errorf("stage5: missing aio-pika producer for etl-load-queue")
	}

	// Hop 6: RabbitMQ (aio-pika) consumer = sink.
	_, r6 := stage("stage6_load.py")
	if !hasEdgeTo(r6, subscribesToEdgeKind, "etl-load-queue") {
		t.Errorf("stage6: missing aio-pika consumer for etl-load-queue")
	}
}
