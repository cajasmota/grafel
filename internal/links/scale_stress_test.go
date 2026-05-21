package links

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// TestTopicGRPCPass_ScaleCompletes is the #1453 regression guard.
//
// It loads a shipfast-sized (27-repo) cross-repo graph through the full
// link pipeline where a single hot topic is touched by every repo with a
// realistic fan-out of publisher AND subscriber operations per repo, and
// asserts the run COMPLETES within a hard timeout. Before the #1453 fix the
// topic pass's per-(pub,sub)-repo-pair × (pubID × subID) emission produced a
// combinatorial blow-up that did not terminate in a reasonable time on the
// grown graph.
func TestTopicGRPCPass_ScaleCompletes(t *testing.T) {
	root := fixtureRoot(t)

	const repos = 27
	// Per-repo fan-out of publisher/subscriber operations on the hot topic.
	// Each repo both publishes and subscribes to the shared topic.
	const opsPerRepo = 12

	for r := 0; r < repos; r++ {
		repo := fmt.Sprintf("svc-%02d", r)
		var ents []map[string]any
		var edges []map[string]string

		topicID := fmt.Sprintf("topic_hot_%s", repo)
		ents = append(ents, map[string]any{
			"id": topicID, "name": "kafka:orders.placed",
			"kind": "SCOPE.MessageTopic", "source_file": "",
		})
		for o := 0; o < opsPerRepo; o++ {
			pub := fmt.Sprintf("%s_pub_%d", repo, o)
			sub := fmt.Sprintf("%s_sub_%d", repo, o)
			ents = append(ents,
				map[string]any{"id": pub, "name": pub, "kind": "SCOPE.Operation", "source_file": repo + "/p.go"},
				map[string]any{"id": sub, "name": sub, "kind": "SCOPE.Operation", "source_file": repo + "/s.go"},
			)
			edges = append(edges,
				map[string]string{"from_id": pub, "to_id": topicID, "kind": "PUBLISHES_TO"},
				map[string]string{"from_id": sub, "to_id": topicID, "kind": "SUBSCRIBES_TO"},
			)
		}

		// A gRPC method touched on both client and server side per repo too,
		// to exercise P6 at scale.
		gm := fmt.Sprintf("gm_%s", repo)
		caller := fmt.Sprintf("%s_caller", repo)
		handler := fmt.Sprintf("%s_handler", repo)
		ents = append(ents,
			map[string]any{"id": gm, "name": "grpc:Inventory/Reserve", "kind": "SCOPE.GrpcMethod", "source_file": ""},
			map[string]any{"id": caller, "name": caller, "kind": "SCOPE.Operation", "source_file": repo + "/c.go"},
			map[string]any{"id": handler, "name": handler, "kind": "SCOPE.Operation", "source_file": repo + "/h.go"},
		)
		edges = append(edges,
			map[string]string{"from_id": caller, "to_id": gm, "kind": "GRPC_HANDLES"},
			map[string]string{"from_id": handler, "to_id": gm, "kind": "GRPC_IMPLEMENTS"},
		)

		writeFixture(t, root, fixtureGraph{Repo: repo, Entities: ents, Edges: edges})
	}

	home := filepath.Join(root, "ag-home-scale")

	done := make(chan struct{})
	var runErr error
	var topicCount, grpcCount int
	go func() {
		defer close(done)
		res, err := RunAllPasses("scale", root, home)
		if err != nil {
			runErr = err
			return
		}
		_ = res
		doc, err := readDoc(filepath.Join(home, "groups", "scale-links.json"))
		if err != nil {
			runErr = err
			return
		}
		for _, l := range doc.Links {
			switch l.Method {
			case MethodTopic:
				topicCount++
			case MethodGRPC:
				grpcCount++
			}
		}
	}()

	select {
	case <-done:
		if runErr != nil {
			t.Fatalf("RunAllPasses failed: %v", runErr)
		}
		t.Logf("scale run completed: %d topic links, %d grpc links", topicCount, grpcCount)
		if topicCount == 0 {
			t.Errorf("expected topic links on the hot topic, got 0 (behavior regression)")
		}
		if grpcCount == 0 {
			t.Errorf("expected grpc links, got 0 (behavior regression)")
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("RunAllPasses did not complete within 30s on a %d-repo graph — "+
			"combinatorial blow-up regression (#1453)", repos)
	}
}
