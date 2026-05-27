// synth_calls_line_property_test.go — regression tests for #2638.
//
// Verifies that synthetic CALLS edges emitted by engine passes
// (ApplyCeleryDispatchEdges, applyServerlessEdges) carry a non-zero
// Properties["line"] value.
//
// No tree-sitter node is available at engine-pass time; line is derived
// from strings.Count(src[:matchOffset], "\n") + 1.
package engine

import (
	"strconv"
	"testing"
)

// ---------------------------------------------------------------------------
// ApplyCeleryDispatchEdges — CALLS line
// ---------------------------------------------------------------------------

// TestCeleryDispatchEdge_HasLineProperty asserts that cross-file Celery
// .delay() dispatch edges carry a non-zero Properties["line"].
func TestCeleryDispatchEdge_HasLineProperty(t *testing.T) {
	// tasks.py — defines the task
	tasksSrc := `from celery import shared_task

@shared_task
def send_email(user_id):
    pass
`
	// views.py — dispatches the task; .delay() is on line 4
	viewsSrc := `from tasks import send_email

def handle_signup(request):
    send_email.delay(request.user.id)
`

	paths := []string{"tasks.py", "views.py"}
	files := map[string][]byte{
		"tasks.py": []byte(tasksSrc),
		"views.py": []byte(viewsSrc),
	}
	reader := func(p string) []byte { return files[p] }

	rels := ApplyCeleryDispatchEdges(paths, reader)
	if len(rels) == 0 {
		t.Fatal("no CALLS edges emitted by ApplyCeleryDispatchEdges")
	}

	for _, r := range rels {
		if r.Kind != "CALLS" {
			continue
		}
		lineStr, ok := r.Properties["line"]
		if !ok {
			t.Errorf("CALLS edge %q→%q missing Properties[\"line\"]", r.FromID, r.ToID)
			continue
		}
		n, err := strconv.Atoi(lineStr)
		if err != nil {
			t.Errorf("Properties[\"line\"] = %q is not a valid integer: %v", lineStr, err)
			continue
		}
		if n <= 0 {
			t.Errorf("Properties[\"line\"] = %d, want > 0", n)
		}
	}
}

// ---------------------------------------------------------------------------
// applyServerlessEdges — CALLS line (Python boto3 invoke)
// ---------------------------------------------------------------------------

// TestServerlessCallsEdge_HasLineProperty asserts that the serverless pass
// emits CALLS edges with a non-zero Properties["line"].
func TestServerlessCallsEdge_HasLineProperty(t *testing.T) {
	// boto3 invoke on line 4
	src := `import boto3

lambda_client = boto3.client('lambda')
lambda_client.invoke(FunctionName='my-function', Payload='{}')
`
	res := applyServerlessEdges(DetectorPassArgs{
		Lang:    "python",
		Path:    "invoker.py",
		Content: []byte(src),
	})

	var foundCalls bool
	for _, r := range res.Relationships {
		if r.Kind != "CALLS" {
			continue
		}
		foundCalls = true
		lineStr, ok := r.Properties["line"]
		if !ok {
			t.Errorf("CALLS edge %q→%q missing Properties[\"line\"]", r.FromID, r.ToID)
			continue
		}
		n, err := strconv.Atoi(lineStr)
		if err != nil {
			t.Errorf("Properties[\"line\"] = %q is not a valid integer: %v", lineStr, err)
			continue
		}
		if n <= 0 {
			t.Errorf("Properties[\"line\"] = %d, want > 0", n)
		}
	}
	if !foundCalls {
		t.Fatal("no CALLS edges emitted by applyServerlessEdges for Python boto3 invoke")
	}
}
