package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// entity_kind_report_stderr_6776_test.go — #6776 arm A, review D3.
//
// The arm-A twin of kind_report_stderr_6773_test.go, written for the same
// reason and after the same finding: arm A added a second stderr line for the
// ENTITY-kind tally and NOTHING observed it. Deleting the whole `if sum :=
// nonEnumKinds.EntitySummary()` block from index.go left
// `go test ./cmd/grafel/` green, so the one user-facing surface of the entity
// measurement was free to say anything at all — or nothing.
//
// It also pins the arm's separability claim AT THE SURFACE THAT MAKES IT:
// fbwriter's TestEntitySummaryIsSeparableFromTheRelationshipSummary compares
// two strings, which says nothing about whether index.go prints them as two
// lines under two prefixes. An over-broad whole-transcript grep would survive
// the two lines being merged; these assertions are scoped to the single line
// carrying the entity prefix.
//
// This runs the REAL Index() over an Ansible playbook, which the rule engine
// turns into `Task` and `Config` entities — two of #6744's 25 ledger kinds,
// and (per arm A's measurement) the largest and second-largest of them by
// runtime population.
func TestIndexStderrReportsTheEntityKindTally(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	// Premise: these must really be outside the entity enum, or the line under
	// test is never printed and every assertion below is vacuous. If the #6776
	// migration has landed, this fixture has done its job and stops applying.
	for _, k := range []string{"Task", "Config"} {
		if types.IsValidEntityKind(k) {
			t.Skipf("%q is now a valid entity kind; the #6776 migration has landed for it", k)
		}
	}

	// Isolate any state this run writes; nothing here may touch a real
	// ~/.grafel.
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "playbook.yml"),
		[]byte("- hosts: all\n"+
			"  tasks:\n"+
			"    - name: install nginx\n"+
			"      apt:\n"+
			"        name: nginx\n"+
			"        state: present\n"+
			"    - name: start nginx\n"+
			"      service:\n"+
			"        name: nginx\n"+
			"        state: started\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "graph.fb")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	captured := make(chan string, 1)
	go func() {
		buf := make([]byte, 0, 8192)
		tmp := make([]byte, 4096)
		for {
			n, rerr := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if rerr != nil {
				break
			}
		}
		captured <- string(buf)
	}()

	idxErr := Index(repo, out, "entkindreport6776", []string{"graph-algo"}, false, false)

	os.Stderr = orig
	_ = w.Close()
	got := <-captured

	if idxErr != nil {
		t.Fatalf("Index: %v", idxErr)
	}

	// Find the entity tally line and assert on THAT line, not on the whole
	// index transcript: a whole-output grep for "Task" would pass off any
	// other diagnostic entirely.
	var line string
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "entity-kind report:") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no entity-kind report line on stderr. This is the only surface a user sees for the "+
			"entity tally, and arm A added it with nothing observing it. stderr:\n%s", got)
	}
	for _, want := range []string{"Task", "Config"} {
		if !strings.Contains(line, want) {
			t.Errorf("report line = %q, does not name the non-enum entity kind %q the fixture emits", line, want)
		}
	}
	// The line must report ENTITIES, not edges: the two populations have
	// separate totals and a line that borrowed the relationship phrasing would
	// mislabel every number on it.
	if !strings.Contains(line, "entity(s)") {
		t.Errorf("report line = %q does not say what it is counting", line)
	}
	if strings.Contains(line, "relationship edge(s)") {
		t.Errorf("report line = %q reports a relationship count under the entity prefix", line)
	}
	// Two lines under two prefixes, not one merged line. A check scoped to the
	// relationship report must not start matching entity text (and this
	// assertion is what stops the two being merged).
	if strings.Contains(line, "relationship-kind report:") {
		t.Errorf("report line = %q carries BOTH prefixes — the entity and relationship tallies were "+
			"merged onto one line, so a check scoped to either now matches the other", line)
	}
}
