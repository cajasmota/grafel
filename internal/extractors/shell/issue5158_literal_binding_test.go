package shell_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #5158 — indirect command dispatch through a variable assigned a string
// literal earlier in the function body (`cmd=do_work; $cmd`) resolves the CALLS
// edge to the real function via the reusable literal-binding resolver.

func extractShell5158(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("shell")
	if !ok {
		t.Fatal("shell extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "dispatch.sh",
		Content:  []byte(src),
		Language: "shell",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return ents
}

// findCall returns the CALLS rel from caller→toID, or nil.
func findCall(ents []types.EntityRecord, caller, toID string) *types.RelationshipRecord {
	for i := range ents {
		if ents[i].Name != caller {
			continue
		}
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind == "CALLS" && r.ToID == toID {
				return r
			}
		}
	}
	return nil
}

func TestShell5158_HappyPath_BareWord(t *testing.T) {
	src := `dispatch() {
    cmd=do_work
    $cmd arg1
}
do_work() { echo hi; }
`
	ents := extractShell5158(t, src)
	rel := findCall(ents, "dispatch", "do_work")
	if rel == nil {
		t.Fatalf("expected resolved CALLS dispatch→do_work; got %#v", ents)
	}
	if rel.Properties["resolved_via"] != extractor.ResolvedViaLiteralBinding {
		t.Errorf("resolved_via = %q; want %q", rel.Properties["resolved_via"], extractor.ResolvedViaLiteralBinding)
	}
	if rel.Properties["dynamic_target"] != "cmd" {
		t.Errorf("dynamic_target = %q; want cmd", rel.Properties["dynamic_target"])
	}
}

func TestShell5158_HappyPath_QuotedLiteral(t *testing.T) {
	src := `dispatch() {
    handler="run_it"
    "$handler"
}
run_it() { echo run; }
`
	ents := extractShell5158(t, src)
	if rel := findCall(ents, "dispatch", "run_it"); rel == nil ||
		rel.Properties["dynamic_target"] != "handler" {
		t.Fatalf("expected resolved CALLS dispatch→run_it via handler; got %#v", ents)
	}
}

func TestShell5158_LastWriteWins(t *testing.T) {
	src := `dispatch() {
    cmd=do_work
    cmd=run_it
    $cmd
}
do_work() { echo a; }
run_it() { echo b; }
`
	ents := extractShell5158(t, src)
	if findCall(ents, "dispatch", "run_it") == nil {
		t.Fatal("last-write-wins: expected CALLS dispatch→run_it")
	}
	if findCall(ents, "dispatch", "do_work") != nil {
		t.Fatal("last-write-wins: stale CALLS dispatch→do_work must NOT be emitted")
	}
}

func TestShell5158_TaintNoResolve(t *testing.T) {
	// cmd is reassigned from a command substitution (non-literal) ⇒ tainted ⇒
	// no resolution, no stale edge.
	src := `dispatch() {
    cmd=do_work
    cmd=$(pick_handler)
    $cmd
}
do_work() { echo a; }
`
	ents := extractShell5158(t, src)
	if rel := findCall(ents, "dispatch", "do_work"); rel != nil {
		t.Fatalf("tainted binding must NOT resolve; got %#v", rel.Properties)
	}
}

func TestShell5158_NoMatch_NoOp(t *testing.T) {
	// $cmd is bound to a literal that is NOT a local function ⇒ no CALLS edge
	// (external program invocations are dropped, same as direct heads).
	src := `dispatch() {
    cmd=docker
    $cmd build .
}
do_work() { echo a; }
`
	ents := extractShell5158(t, src)
	if rel := findCall(ents, "dispatch", "docker"); rel != nil {
		t.Fatalf("non-local resolved literal must NOT produce CALLS; got %#v", rel)
	}
}

func TestShell5158_DirectCallUnaffected(t *testing.T) {
	// A normal direct call still carries NO resolved_via property.
	src := `dispatch() {
    do_work
}
do_work() { echo a; }
`
	ents := extractShell5158(t, src)
	rel := findCall(ents, "dispatch", "do_work")
	if rel == nil {
		t.Fatal("expected direct CALLS dispatch→do_work")
	}
	if _, ok := rel.Properties["resolved_via"]; ok {
		t.Errorf("direct call must NOT carry resolved_via; got %v", rel.Properties)
	}
}
