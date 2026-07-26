package main

// links_internal.go — hidden `grafel links-internal <group>` subcommand
// (#5954).
//
// One cross-repo link pass for one group, in a short-lived child process. The
// daemon's scheduler fork-execs this instead of running the pass inside the
// long-lived engine (see sched.RunSubprocessLinks); it is the third and last
// heavy stage to be moved out, after the index child and group-algo.
//
// WHY. The pass materialises the whole group union TWICE in sequence — every
// repo's Document held live across ~19 passes in links.RunAllPasses, then
// reloaded into the phantom-edge pass's docs map — and the engine kept that
// arena for the rest of its life. Per-phase measurement on the real corpus put
// the pass window at ~830MB LIVE heap (~1.2GB heap_inuse; the gap is exactly
// why live heap must be read off next_gc/(1+GOGC/100) and not off heap_inuse),
// peaking in link_pass_load → link_pass_import → link_pass_constant_propagation
// — i.e. the union load and the passes that immediately follow it, not the
// phantom reload. In a child the OS takes all of it back on exit.
//
// Not part of the public command surface; intercepted before cobra dispatch in
// main.go, exactly like index-internal and group-algo. The user-facing
// `grafel links pass <group>` (internal/cli) is unchanged and still runs in
// whatever process invoked it.
//
//	grafel links-internal <group>

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cajasmota/grafel/internal/cli"
	"github.com/cajasmota/grafel/internal/daemon/sched"
	"github.com/cajasmota/grafel/internal/links"
	"github.com/cajasmota/grafel/internal/memtrace"
)

func runLinksInternal(args []string) int {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "usage: grafel links-internal <group>")
		return 2
	}
	group := args[0]

	// Lower this child's OS scheduling priority so even its capped cores yield
	// to foreground work. Best-effort self-renice, so it holds however the child
	// is spawned (the GOMAXPROCS cap itself comes from the parent's env).
	sched.NiceSelf()

	// #5954 / #5956 memtrace: phase-tagged memstats sampler + per-phase heap
	// profiles, gated entirely behind GRAFEL_MEMTRACE_DIR (inherited, since the
	// child's env derives from the daemon's os.Environ()). Start returns nil —
	// no goroutine, no file — when the var is unset, so this is zero overhead by
	// default. The phase comes from links.CurrentPhase, stamped by the pass
	// itself, so the trace cannot drift from what the process is doing.
	memSampler := memtrace.Start("links", links.CurrentPhase, memtraceLogf)
	defer memSampler.Stop()

	// context.Background(), deliberately: cancellation of this child is a
	// SIGKILL from the parent (exec.CommandContext's default Cancel is
	// cmd.Process.Kill), not an in-process ctx. A signal handler is therefore
	// not an option — SIGKILL cannot be caught — and none is wanted: the kill
	// is more prompt than the ctx checks it replaces, which only took effect at
	// a pass boundary. The pass's writes are temp+rename so a kill cannot tear
	// them; it does leak the staging temp dir. See RunSubprocessLinks's doc.
	t0 := time.Now()
	if err := cli.RunLinksForGroupCtx(context.Background(), group); err != nil {
		fmt.Fprintf(os.Stderr, "grafel links-internal: %v\n", err)
		return 1
	}
	fmt.Printf("links-internal: group=%s elapsed=%s\n", group, time.Since(t0).Truncate(time.Millisecond))
	return 0
}
