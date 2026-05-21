package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEnqueue_basic checks that a job goes queued → running → done.
func TestEnqueue_basic(t *testing.T) {
	q := NewQueue("", 1)
	q.Start()
	defer q.Stop()

	id, err := q.Enqueue("g1", "flow::checkout", "describe_entity")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Poll until done (stub takes 50ms).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, ok := q.Get(id)
		if !ok {
			t.Fatalf("job %s not found", id)
		}
		if j.Status == StatusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	j, _ := q.Get(id)
	if j.Status != StatusDone {
		t.Errorf("want status=done, got %s (error=%s)", j.Status, j.Error)
	}
	if j.StartedAt == nil || j.FinishedAt == nil {
		t.Errorf("expected StartedAt and FinishedAt to be set")
	}
}

// TestEnqueue_multiple checks that multiple jobs all complete.
func TestEnqueue_multiple(t *testing.T) {
	q := NewQueue("", 2)
	q.Start()
	defer q.Stop()

	ids := make([]string, 4)
	for i := range ids {
		id, err := q.Enqueue("g1", "flow::x", "describe_entity")
		if err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
		ids[i] = id
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		done := 0
		for _, id := range ids {
			j, _ := q.Get(id)
			if j.Status == StatusDone || j.Status == StatusFailed {
				done++
			}
		}
		if done == len(ids) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	for _, id := range ids {
		j, _ := q.Get(id)
		if j.Status != StatusDone {
			t.Errorf("job %s: want done, got %s", id, j.Status)
		}
	}
}

// TestCancel_queued cancels a job before a worker picks it up.
func TestCancel_queued(t *testing.T) {
	// 0 workers: nothing executes, jobs stay queued.
	q := NewQueue("", 0)
	q.Start()
	defer q.Stop()

	id, _ := q.Enqueue("g1", "flow::x", "describe_entity")
	q.Cancel(id)

	j, ok := q.Get(id)
	if !ok {
		t.Fatal("job not found")
	}
	if j.Status != StatusFailed {
		t.Errorf("want failed, got %s", j.Status)
	}
	if j.Error != "cancelled" {
		t.Errorf("want error=cancelled, got %q", j.Error)
	}
}

// TestListForGroup filters correctly.
func TestListForGroup(t *testing.T) {
	q := NewQueue("", 2)
	q.Start()
	defer q.Stop()

	q.Enqueue("groupA", "flow::a", "describe_entity")
	q.Enqueue("groupB", "flow::b", "describe_entity")
	q.Enqueue("groupA", "flow::c", "describe_entity")

	// Wait for all to complete.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		all := q.List()
		done := 0
		for _, j := range all {
			if j.Status == StatusDone || j.Status == StatusFailed {
				done++
			}
		}
		if done == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	a := q.ListForGroup("groupA")
	if len(a) != 2 {
		t.Errorf("groupA: want 2 jobs, got %d", len(a))
	}
	b := q.ListForGroup("groupB")
	if len(b) != 1 {
		t.Errorf("groupB: want 1 job, got %d", len(b))
	}
}

// TestHistory_persistence verifies that job events are appended to JSONL.
func TestHistory_persistence(t *testing.T) {
	tmp := t.TempDir()
	hist := filepath.Join(tmp, "jobs.jsonl")

	q := NewQueue(hist, 1)
	q.Start()
	id, _ := q.Enqueue("g1", "flow::x", "describe_entity")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := q.Get(id)
		if j.Status == StatusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	q.Stop()

	data, err := os.ReadFile(hist)
	if err != nil {
		t.Fatalf("reading history: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// Expect at least 2 lines: queued + done (running may also appear).
	if len(lines) < 2 {
		t.Errorf("want ≥2 history lines, got %d: %s", len(lines), string(data))
	}
	for _, line := range lines {
		if !strings.Contains(line, `"id"`) {
			t.Errorf("history line missing id field: %s", line)
		}
	}
}

// TestBuildEnrichmentPrompt checks that the prompt contains required fields.
func TestBuildEnrichmentPrompt(t *testing.T) {
	job := &Job{
		SubjectID: "flow::checkout",
		Kind:      "describe_entity",
		Group:     "mygroup",
	}
	p := buildEnrichmentPrompt(job)
	for _, want := range []string{"flow::checkout", "describe_entity", "mygroup"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q: %s", want, p)
		}
	}
}
