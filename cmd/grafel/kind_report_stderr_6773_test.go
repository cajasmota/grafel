package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// kind_report_stderr_6773_test.go — #6773 review D5.
//
// #6773 changed the stderr line the index emits for the relationship-kind
// tally: its prefix used to claim everything it printed was a kind absent from
// the enum, which stopped being true once Summary() started carrying the
// declared-derived population as a second clause. That change was asserted by
// nothing — neither the old string nor the new one appeared outside index.go —
// so the one user-facing surface of this whole measurement was free to say
// anything at all.
//
// This runs the REAL Index() over a repo whose SQL file makes the extractor
// emit INDEXES (a kind in neither vocabulary) and reads the bytes a user sees.
func TestIndexStderrReportsTheRelationshipKindTally(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a real index; skipped under -short")
	}
	// Premise: INDEXES must really be undeclared, or the line under test would
	// never be printed and every assertion below would be vacuous.
	if types.IsDeclaredRelationshipKind("INDEXES") {
		t.Skip("INDEXES is now declared; this fixture no longer produces an unknown kind")
	}

	// Isolate any state this run writes; nothing here may touch a real
	// ~/.grafel.
	t.Setenv("GRAFEL_DAEMON_ROOT", t.TempDir())

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "schema.sql"),
		[]byte("CREATE TABLE users (id INT PRIMARY KEY, email TEXT);\n"+
			"CREATE INDEX users_email_idx ON users (email);\n"), 0o644); err != nil {
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

	idxErr := Index(repo, out, "kindreport6773", []string{"graph-algo"}, false, false)

	os.Stderr = orig
	_ = w.Close()
	got := <-captured

	if idxErr != nil {
		t.Fatalf("Index: %v", idxErr)
	}

	// Find the tally line itself and assert on THAT line, not on the whole
	// index transcript: a whole-output grep for "INDEXES" would pass off some
	// other diagnostic entirely.
	var line string
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "relationship-kind report:") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no relationship-kind report line on stderr. This is the only surface a user sees "+
			"for the tally, and #6773 rewrote its prefix with nothing observing it. stderr:\n%s", got)
	}
	if !strings.Contains(line, "INDEXES") {
		t.Errorf("report line = %q, does not name the unknown kind the fixture emits", line)
	}
	// The prefix must not re-acquire the claim #6773 removed: Summary() can
	// carry declared-derived kinds too, so "kinds not in the enum" would be a
	// false label on part of what the line prints.
	if strings.Contains(line, "not in the enum") {
		t.Errorf("report line = %q claims everything it prints is absent from the enum; Summary() "+
			"also carries the declared-derived population", line)
	}
}
