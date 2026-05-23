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

// TestTopicPass_RealSkewedShape_BoundedEmission is the #1456 regression guard.
//
// The #1453/#1454 scale test used a *synthetic-uniform* shape: every repo
// publishes AND subscribes to one hot topic with the same op fan-out. The
// real shipfast §3 message topology is *skewed* — one publisher repo, many
// subscriber repos, and a heavily lopsided per-repo op fan-out (e.g.
// payments.settled: 1 publisher, 5 subscribers; orders.placed: 1 publisher,
// 3 subscribers) — plus many distinct topic Names. This test reproduces
// that asymmetric shape with a deliberately huge one-sided op fan-out and
// asserts not just that the pass *completes*, but that the emitted edge
// count stays O(publisher-repos × subscriber-repos) — i.e. independent of
// the per-repo op fan-out. A regression to the pre-#1454 O(R²·ops²) emission
// (or any failure of the #1456 per-Name cap) would blow this bound.
func TestTopicPass_RealSkewedShape_BoundedEmission(t *testing.T) {
	root := fixtureRoot(t)

	// Real-shaped: one publisher repo, many subscriber repos, with a large
	// asymmetric per-repo op fan-out on BOTH the publisher and each
	// subscriber. Pre-#1454 this produced pubOps × subOps edges *per repo
	// pair*; #1454 collapses that to exactly one edge per (pub-repo,
	// sub-repo). With 1 publisher repo and N subscriber repos that is N
	// edges total, regardless of opsPerSide.
	const subscriberRepos = 8
	const opsPerSide = 40 // huge fan-out per repo on the hot topic

	topicName := "kafka:payments.settled"

	mkTopicRepo := func(repo string, publish, subscribe bool) {
		var ents []map[string]any
		var edges []map[string]string
		topicID := "topic_" + repo
		ents = append(ents, map[string]any{
			"id": topicID, "name": topicName,
			"kind": "SCOPE.MessageTopic", "source_file": "",
		})
		for o := 0; o < opsPerSide; o++ {
			if publish {
				pub := repo + "_pub_" + itoa(o)
				ents = append(ents, map[string]any{"id": pub, "name": pub, "kind": "SCOPE.Operation", "source_file": repo + "/p.go"})
				edges = append(edges, map[string]string{"from_id": pub, "to_id": topicID, "kind": "PUBLISHES_TO"})
			}
			if subscribe {
				sub := repo + "_sub_" + itoa(o)
				ents = append(ents, map[string]any{"id": sub, "name": sub, "kind": "SCOPE.Operation", "source_file": repo + "/s.go"})
				edges = append(edges, map[string]string{"from_id": sub, "to_id": topicID, "kind": "SUBSCRIBES_TO"})
			}
		}
		writeFixture(t, root, fixtureGraph{Repo: repo, Entities: ents, Edges: edges})
	}

	// One publisher repo (payments), many subscriber repos.
	mkTopicRepo("payments", true, false)
	for i := 0; i < subscriberRepos; i++ {
		mkTopicRepo("sub-"+itoa(i), false, true)
	}

	home := filepath.Join(root, "ag-home-skew")

	done := make(chan struct{})
	var runErr error
	var topicCount int
	go func() {
		defer close(done)
		if _, err := RunAllPasses("skew", root, home); err != nil {
			runErr = err
			return
		}
		doc, err := readDoc(filepath.Join(home, "groups", "skew-links.json"))
		if err != nil {
			runErr = err
			return
		}
		for _, l := range doc.Links {
			if l.Method == MethodTopic {
				topicCount++
			}
		}
	}()

	select {
	case <-done:
		if runErr != nil {
			t.Fatalf("RunAllPasses failed: %v", runErr)
		}
		// Bound: one edge per (publisher-repo, subscriber-repo) pair. With
		// 1 publisher repo and subscriberRepos subscribers, exactly that
		// many edges — NOT multiplied by opsPerSide.
		const wantMax = subscriberRepos
		if topicCount == 0 {
			t.Errorf("expected topic links on the hot topic, got 0 (behavior regression)")
		}
		if topicCount > wantMax {
			t.Errorf("topic emission not bounded: got %d links, want <= %d "+
				"(one per pub/sub repo pair, independent of the %d ops/side fan-out) — "+
				"O(ops²) blow-up regression (#1456)", topicCount, wantMax, opsPerSide)
		}
		t.Logf("skewed real-shape run: %d topic links (bound %d), %d ops/side", topicCount, wantMax, opsPerSide)
	case <-time.After(30 * time.Second):
		t.Fatalf("RunAllPasses did not complete within 30s on the skewed real-shape graph — " +
			"combinatorial blow-up regression (#1456)")
	}
}
