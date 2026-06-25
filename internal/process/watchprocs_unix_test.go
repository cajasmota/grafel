//go:build darwin || linux

package process

import "testing"

func TestParseWatchPsArgs(t *testing.T) {
	out := "" +
		"  100 /home/u/go/bin/grafel watch /work/repo-a --interval 30s\n" +
		"  101 /home/u/.grafel/bin/grafel watch /work/repo-b\n" +
		"  102 /usr/bin/some-other-tool watch /work/repo-c\n" + // not grafel → skipped
		"  103 /home/u/.grafel/bin/grafel index /work/repo-d\n" + // not watch → skipped
		"  104 /home/u/.grafel/bin/grafel daemon\n" // no watch → skipped
	got := parseWatchPsArgs(out)
	if len(got) != 2 {
		t.Fatalf("got %d watch procs, want 2: %+v", len(got), got)
	}
	if got[0].PID != 100 || got[0].Exe != "/home/u/go/bin/grafel" || got[0].Repo == "" {
		t.Fatalf("proc[0] = %+v, want pid 100 exe /home/u/go/bin/grafel non-empty repo", got[0])
	}
	if got[1].PID != 101 || got[1].Exe != "/home/u/.grafel/bin/grafel" {
		t.Fatalf("proc[1] = %+v, want pid 101 exe /home/u/.grafel/bin/grafel", got[1])
	}
}
