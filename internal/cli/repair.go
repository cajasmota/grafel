package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/client"
	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/notifications"
	"github.com/cajasmota/grafel/internal/quality"
)

// rebuild and reset both forward to the daemon's Rebuild RPC; reset
// additionally requests the daemon wipe each repo's .grafel/ before
// indexing. The deprecated remerge alias was removed in ADR-0017 —
// callers must use `grafel rebuild [group]` now.

func newRebuildCmd() *cobra.Command {
	var quiet bool
	var jsonProgress bool
	var plain bool
	var incremental bool
	var full bool
	var refFlag string
	var timeoutFlag string

	cmd := &cobra.Command{
		Use:   "rebuild [group] [slug]",
		Short: "Force rebuild via the daemon",
		Long: `Force rebuild triggers an AST extraction + graph rebuild for every repo in
a group (or one slug). Progress is streamed live from the indexer's event
broker — the same events the web dashboard shows.

Flags:
  --quiet           suppress progress output; print only the final summary
  --plain           no ANSI color or carriage-return overwriting (CI-safe)
  --json-progress   NDJSON output: one broker event per line (for scripting)
  --incremental     only re-process files changed since the last index (faster)
  --full            force a full rebuild, ignoring any cached file-hash manifest
  --ref <ref>       operate on a specific git ref; @all is refused (destructive)
  --timeout <dur>   override the per-repo rebuild watchdog for THIS invocation
                    (default 30m via GRAFEL_REBUILD_REPO_TIMEOUT; "0" disables
                    it). Without this a repo taking longer than the watchdog
                    is SIGKILLed and surfaced as a failed rebuild (#5822) —
                    raise this for a genuinely large monorepo instead of
                    editing the daemon's env.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// @all is refused for destructive commands.
			resolvedRef, _, err := resolveRef(refFlag, false /* @all NOT ok */)
			if err != nil {
				return err
			}
			// --full overrides --incremental.
			inc := incremental && !full
			// `rebuild` does not request a completion guarantee, so it has no
			// wait to bound (#5991 is scoped to `reset`).
			return runRebuildClient(cmd, args, false, quiet, jsonProgress, plain, resolvedRef, inc, timeoutFlag, 0)
		},
	}
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress progress output; print only the final summary")
	cmd.Flags().BoolVar(&jsonProgress, "json-progress", false, "emit one NDJSON broker event per line (for scripting)")
	cmd.Flags().BoolVar(&plain, "plain", false, "disable ANSI color and carriage-return overwrites (CI-safe)")
	cmd.Flags().BoolVar(&incremental, "incremental", false, "only re-process files changed since the last index")
	cmd.Flags().BoolVar(&full, "full", false, "force full rebuild, ignoring cached file-hash manifest")
	cmd.Flags().StringVar(&refFlag, "ref", "", refFlagUsage)
	cmd.Flags().StringVar(&timeoutFlag, "timeout", "",
		`override the per-repo rebuild watchdog for this invocation (Go duration, e.g. "45m"; "0" disables it; default: GRAFEL_REBUILD_REPO_TIMEOUT or 30m)`)
	return cmd
}

func newResetCmd() *cobra.Command {
	var quiet bool
	var jsonProgress bool
	var plain bool
	var refFlag string
	var timeoutFlag string
	var waitTimeoutFlag string

	cmd := &cobra.Command{
		Use:   "reset [group] [slug]",
		Short: "Wipe .grafel/ and rebuild via the daemon",
		Long: `Reset wipes each repo's .grafel/ and rebuilds the group from scratch.

Unlike ` + "`grafel rebuild`" + `, reset BLOCKS until the daemon confirms the wipe and
rebuild actually completed, and exits non-zero if it cannot confirm them
(#5991) — a reset that reports success must mean the graph was really rebuilt.
A large group therefore holds the command for the whole rebuild; the daemon's
own bound is 2h. Use --wait-timeout to bound the wait on scripted paths.

Flags:
  --quiet           suppress progress output; print only the final summary
  --plain           no ANSI color or carriage-return overwriting (CI-safe)
  --json-progress   NDJSON output: one broker event per line (for scripting)
  --ref <ref>       operate on a specific git ref; @all is refused (destructive)
  --timeout <dur>   per-repo rebuild watchdog (daemon-side; see below)
  --wait-timeout    how long THIS command waits for the daemon to confirm`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedRef, _, err := resolveRef(refFlag, false /* @all NOT ok — destructive */)
			if err != nil {
				return err
			}
			waitTimeout, err := parseWaitTimeout(waitTimeoutFlag)
			if err != nil {
				return err
			}
			return runRebuildClient(cmd, args, true, quiet, jsonProgress, plain, resolvedRef, false, timeoutFlag, waitTimeout)
		},
	}
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress progress output; print only the final summary")
	cmd.Flags().BoolVar(&jsonProgress, "json-progress", false, "emit one NDJSON broker event per line (for scripting)")
	cmd.Flags().BoolVar(&plain, "plain", false, "disable ANSI color and carriage-return overwrites (CI-safe)")
	cmd.Flags().StringVar(&refFlag, "ref", "", refFlagUsage)
	cmd.Flags().StringVar(&timeoutFlag, "timeout", "",
		`override the per-repo rebuild watchdog for this invocation (Go duration, e.g. "45m"; "0" disables it; default: GRAFEL_REBUILD_REPO_TIMEOUT or 30m)`)
	cmd.Flags().StringVar(&waitTimeoutFlag, "wait-timeout", "",
		`bound how long THIS command waits for the daemon to confirm the wipe+rebuild (Go duration, e.g. "30m"; default/"0" = unbounded, i.e. until the daemon's own 2h bound). On expiry reset exits non-zero with an UNCONFIRMED outcome — the rebuild may still be running. Distinct from --timeout, which is the daemon-side per-repo watchdog.`)
	return cmd
}

// parseWaitTimeout parses the --wait-timeout flag. "" and "0" mean unbounded.
// A malformed value is REJECTED rather than ignored: silently falling back to
// unbounded would leave the caller believing they were bounded.
func parseWaitTimeout(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("--wait-timeout %q is not a valid Go duration (e.g. \"30m\", \"90s\"; \"0\" or unset = unbounded): %w", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("--wait-timeout %q must not be negative", raw)
	}
	return d, nil
}

// errRebuildUnconfirmed marks the CLI's OWN give-up: the --wait-timeout bound
// expired before the daemon answered. It is deliberately distinct from a
// daemon-reported failure — we stopped waiting, we did not learn the outcome,
// so the wipe+rebuild may or may not have completed.
var errRebuildUnconfirmed = errors.New("the daemon did not answer in time")

// boundOutcome bounds a rebuild-outcome wait at timeout, yielding an
// errRebuildUnconfirmed outcome if the daemon has not answered by then. A
// non-positive timeout means unbounded and returns src unchanged, so the common
// path adds no goroutine at all.
//
// On expiry the source goroutine is left blocked on the RPC — net/rpc offers no
// cancellation and the process is on its way out; the bound exists so a scripted
// caller regains control, not to tear the call down.
func boundOutcome(src <-chan rebuildOutcome, timeout time.Duration) <-chan rebuildOutcome {
	if timeout <= 0 {
		return src
	}
	out := make(chan rebuildOutcome, 1)
	go func() {
		t := time.NewTimer(timeout)
		defer t.Stop()
		select {
		case o := <-src:
			out <- o
		case <-t.C:
			out <- rebuildOutcome{err: fmt.Errorf("no completion confirmed within %s: %w", timeout, errRebuildUnconfirmed)}
		}
	}()
	return out
}

// progressToken generates a short unique token for a rebuild session.
//
// Uses the full 64 bits of rand.Uint64 (was previously only 16 bits, which
// caused collisions on Windows where time.Now().UnixNano() has lower
// resolution — 100 tokens in a tight loop within the same clock tick
// exhausted the 65k suffix space and triggered TestProgressToken_Unique
// failures on windows-latest CI). 64-bit suffix is collision-resistant
// for realistic session counts.
func progressToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) +
		strconv.FormatUint(rand.Uint64(), 36) //nolint:gosec
}

// isTTY reports whether w is connected to a terminal.
func isTTY(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		fi, err := f.Stat()
		if err != nil {
			return false
		}
		return (fi.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

// fmtDuration formats a duration as a human string: never "3611s".
func fmtDuration(d time.Duration) string {
	d = d.Truncate(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) - m*60
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	s := int(d.Seconds()) - h*3600 - m*60
	return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
}

// runRebuildClient runs the rebuild or reset command with live progress output.
//
// Progress strategy (tries in order):
//  1. Broker via SSE: subscribe to /api/index-progress/{group} on the daemon's
//     dashboard HTTP port. Gives the CLI the exact same event stream as the web
//     dashboard — single source of truth from the in-memory broker.
//  2. Poll fallback: if SSE is unavailable (dashboard not running, old daemon),
//     fall back to the existing 2-second RPC poll of IndexProgress.
//
// The primary Rebuild RPC runs in a goroutine (it blocks until done); progress
// is rendered concurrently from whichever source is available.
//
// ref is the resolved --ref value ("" means current HEAD, a named string means
// operate on that specific ref). @all is pre-rejected by the caller since
// rebuild/reset are destructive. Wiring ref into the daemon RPC is tracked
// separately (#2220); for now it is validated and stored but not forwarded.
//
// waitTimeout bounds how long THIS command waits for the daemon's answer
// (`reset --wait-timeout`); 0 means unbounded. See boundOutcome.
func runRebuildClient(cmd *cobra.Command, args []string, wipe bool, quiet bool, jsonProgress bool, plain bool, ref string, incremental bool, repoTimeout string, waitTimeout time.Duration) error {
	if len(args) == 0 {
		return errors.New("supply [group] (and optional [slug])")
	}

	inc := incremental

	// #5991 — `reset` must never report success for work that did not happen.
	//
	// Split mode is the DEFAULT (daemon.SplitModeEnabled is true unless
	// GRAFEL_SPLIT_MODE is explicitly falsy). There, Service.Rebuild is
	// fire-and-forget: it writes a KindRebuild request file for the group and
	// returns nil immediately, with the ZERO RebuildReply. The engine drains
	// that file on its own schedule — and if it never does (engine down,
	// crash-resume backoff, dead-letter), nothing ever wipes `.grafel/` or
	// rebuilds, because BOTH the wipe and the rebuild live inside the
	// engine-side RebuildFunc. The CLI saw err==nil + an empty reply and
	// printed nothing after "Rebuilding group '<g>'...", so `reset` exited 0
	// having done precisely nothing.
	//
	// `reset` is the destructive escape hatch users reach for when they
	// suspect the index is bad, so a silent no-op there is the worst possible
	// outcome. Opt INTO the #5790 completion contract (the same one
	// `group add --index` uses): WaitForCompletion makes the daemon block on
	// the engine's terminal ack and return nil ONLY on a real StatusOK
	// completion, and a clear error on failure / dead-letter / engine-death /
	// timeout. In monolith mode the flag is inert (that path is already
	// synchronous), so this is a no-op there.
	//
	// Deliberately scoped to wipe (i.e. `reset`) — plain `rebuild` keeps its
	// existing fire-and-forget semantics; see #5991 for that discussion.
	waitForCompletion := wipe

	c, err := client.Dial()
	if err != nil {
		if errors.Is(err, client.ErrDaemonNotRunning) {
			return errDaemonNotRunning
		}
		return err
	}
	defer c.Close()

	group := args[0]
	slug := ""
	if len(args) > 1 {
		slug = args[1]
	}

	w := cmd.OutOrStdout()

	// Note when a specific ref is targeted (wiring into daemon RPC is #2220).
	if ref != "" && !quiet && !jsonProgress {
		fmt.Fprintf(w, "Note: --ref %q recorded; daemon-side ref routing is tracked in #2220.\n", ref)
	}

	// --quiet: skip progress, no token. The RPC still runs on a goroutine so
	// --wait-timeout can bound it — with WaitForCompletion this call blocks for
	// the whole rebuild, and --quiet prints nothing while it does, so an
	// unbounded wait here is a silent multi-hour hang on scripted paths.
	if quiet {
		quietCh := make(chan rebuildOutcome, 1)
		go func() {
			// #5328: an explicit `grafel rebuild`/repair is human-awaited → foreground.
			reply, rpcErr := c.Rebuild(proto.RebuildArgs{Group: group, Slug: slug, Wipe: wipe, Incremental: inc, Interactive: true, RepoTimeout: repoTimeout, WaitForCompletion: waitForCompletion})
			quietCh <- rebuildOutcome{repos: reply.Repos, warning: reply.Warning, err: rpcErr}
		}()
		outcome := <-boundOutcome(quietCh, waitTimeout)
		if outcome.err != nil {
			return wrapResetFailure(wipe, group, outcome.err)
		}
		for _, r := range outcome.repos {
			// repos contains absolute paths since #1076 fix; show basename.
			fmt.Fprintf(w, "rebuilt %s\n", filepath.Base(r))
		}
		if outcome.warning != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", outcome.warning)
		}
		return nil
	}

	// Attempt to resolve the dashboard port for SSE subscription.
	dashPort := 0
	if st, stErr := c.Status(); stErr == nil && st.DashboardPort > 0 {
		dashPort = st.DashboardPort
	}

	// Kick off the async rebuild RPC on the primary connection.
	rpcCh := make(chan rebuildOutcome, 1)
	var outcomeCh <-chan rebuildOutcome
	token := progressToken()
	go func() {
		reply, rpcErr := c.Rebuild(proto.RebuildArgs{
			Group:         group,
			Slug:          slug,
			Wipe:          wipe,
			ProgressToken: token,
			Incremental:   inc,
			// #5328: explicit user-triggered repair → foreground (priority + cap).
			Interactive: true,
			RepoTimeout: repoTimeout,
			// #5991: reset blocks for a real completion ack; see above.
			WaitForCompletion: waitForCompletion,
		})
		rpcCh <- rebuildOutcome{
			repos:    reply.Repos,
			warning:  reply.Warning,
			elapsed:  reply.ElapsedSec,
			entities: reply.TotalEntities,
			rels:     reply.TotalRels,
			err:      rpcErr,
		}
	}()

	// Apply the --wait-timeout bound ONCE, on the channel every progress path
	// below consumes, so the bound cannot be reachable on one renderer and not
	// the other. No-op (same channel) when unbounded.
	outcomeCh = boundOutcome(rpcCh, waitTimeout)

	if !jsonProgress {
		fmt.Fprintf(w, "Rebuilding group '%s'...\n", group)
	}

	// --- Path 1: broker via SSE ---
	if dashPort > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sseCh, sseErr := subscribeSSE(ctx, dashPort, group, token)
		if sseErr == nil {
			outcome := runBrokerProgress(ctx, w, group, sseCh, outcomeCh, plain, jsonProgress, waitForCompletion)
			cancel()
			if outcome.err != nil {
				return wrapResetFailure(wipe, group, outcome.err)
			}
			return finishRebuild(cmd, w, group, token, outcome.repos, outcome.warning,
				outcome.elapsed, outcome.entities, outcome.rels, jsonProgress)
		}
		// SSE connect failed — fall through to poll path.
	}

	// --- Path 2: poll fallback ---
	return runPollProgress(cmd, w, group, slug, token, wipe, outcomeCh, jsonProgress, c)
}

// runPollProgress is the legacy 2-second RPC polling fallback. It is used when
// the SSE endpoint is unavailable (dashboard not running or old daemon version).
func runPollProgress(
	cmd *cobra.Command,
	w io.Writer,
	group, _ string,
	token string,
	// wipe identifies a `reset` (as opposed to a plain `rebuild`) so a
	// non-completing run is reported as "not wiped or rebuilt" (#5991). This
	// parameter was previously accepted and ignored (`_ bool`).
	wipe bool,
	resultCh <-chan rebuildOutcome,
	jsonProgress bool,
	c *client.Client,
) error {
	// Open a second connection for polling (avoids blocking on the primary).
	pollClient, pollDialErr := client.DialProgress(c.SocketPath())
	if pollDialErr != nil {
		// Polling unavailable — wait for RPC result silently.
		outcome := <-resultCh
		if outcome.err != nil {
			return wrapResetFailure(wipe, group, outcome.err)
		}
		for _, r := range outcome.repos {
			fmt.Fprintf(w, "rebuilt %s\n", filepath.Base(r))
		}
		if outcome.warning != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", outcome.warning)
		}
		return nil
	}
	defer pollClient.Close()

	// Poll loop — 2-second interval, heartbeat after 10s of silence.
	// Track the last printed phase per repo path to avoid duplicating unchanged lines.
	seenPhases := map[string]string{}
	lastEventAt := time.Now()
	const pollInterval = 2 * time.Second
	const heartbeatThreshold = 10 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var finalOutcome rebuildOutcome
	done := false

	for !done {
		select {
		case finalOutcome = <-resultCh:
			done = true
			// Fall through to do one final poll.
		case <-ticker.C:
		}

		prog, pollErr := pollClient.IndexProgress(token)
		if pollErr != nil {
			// Poll RPC failed — emit heartbeat if silent too long.
			if time.Since(lastEventAt) >= heartbeatThreshold {
				sinceStr := fmtDuration(time.Since(lastEventAt))
				if jsonProgress {
					emitJSONEvent(w, "heartbeat", group, "")
				} else {
					fmt.Fprintf(w, "  ... still working (%s elapsed)\n", sinceStr)
				}
				lastEventAt = time.Now()
			}
			continue
		}

		now := time.Now()
		for _, r := range prog.Repos {
			prevPhase := seenPhases[r.Path]
			if prevPhase != r.Phase {
				seenPhases[r.Path] = r.Phase
				lastEventAt = now
				if jsonProgress {
					emitJSONProgressState(w, token, r)
				} else {
					printProgressLine(w, r)
				}
			}
		}

		// Heartbeat if nothing has printed in heartbeatThreshold.
		if time.Since(lastEventAt) >= heartbeatThreshold {
			if jsonProgress {
				emitJSONEvent(w, "heartbeat", group, "")
			} else {
				fmt.Fprintf(w, "  ... still working (%s elapsed)\n",
					fmtDuration(time.Since(lastEventAt)))
			}
			lastEventAt = time.Now()
		}
	}

	if finalOutcome.err != nil {
		return wrapResetFailure(wipe, group, finalOutcome.err)
	}

	return finishRebuild(cmd, w, group, token, finalOutcome.repos, finalOutcome.warning,
		finalOutcome.elapsed, finalOutcome.entities, finalOutcome.rels, jsonProgress)
}

// wrapResetFailure names what did NOT happen when a `reset` fails to complete
// (#5991). `reset`'s whole contract is "wipe `.grafel/`, then rebuild", and
// both halves run engine-side inside RebuildFunc — so an unconfirmed rebuild
// means neither happened, and the previous (possibly corrupt) graph is still
// on disk. The raw daemon error alone ("timed out after 2h0m0s") does not say
// that; this prefix does, and names the group.
//
// Plain `rebuild` (wipe=false) is returned unwrapped so its message is
// unchanged.
func wrapResetFailure(wipe bool, group string, err error) error {
	if err == nil || !wipe {
		return err
	}
	// The CLI's own --wait-timeout give-up is NOT a confirmed failure: we
	// stopped waiting, we did not learn the outcome. Saying "was NOT wiped or
	// rebuilt" there would be exactly the kind of confident-but-wrong report
	// this issue is about, pointed the other way.
	if errors.Is(err, errRebuildUnconfirmed) {
		return fmt.Errorf("reset: group %q — %w; the rebuild may or may not have completed (check `grafel status`), and --wait-timeout only bounds this command, it does not cancel the daemon", group, err)
	}
	return fmt.Errorf("reset: group %q was NOT wiped or rebuilt (the previous graph is still on disk): %w", group, err)
}

// finishRebuild renders the final summary after a rebuild completes.
func finishRebuild(
	cmd *cobra.Command,
	w io.Writer,
	group, token string,
	repos []string,
	warning string,
	elapsedSec float64,
	totalEntities, totalRels int64,
	jsonProgress bool,
) error {
	var elapsedStr string
	elapsed := time.Duration(elapsedSec * float64(time.Second))
	if elapsedSec > 0 {
		elapsedStr = fmtDuration(elapsed)
	}

	if jsonProgress {
		type summaryEvent struct {
			Event    string   `json:"event"`
			Token    string   `json:"token"`
			Group    string   `json:"group"`
			Repos    []string `json:"repos"`
			Entities int64    `json:"total_entities,omitempty"`
			Rels     int64    `json:"total_rels,omitempty"`
			Elapsed  string   `json:"elapsed,omitempty"`
			Warning  string   `json:"warning,omitempty"`
		}
		// Convert absolute paths back to slug/basename for the JSON event so
		// the wire format stays stable (slugs, not paths).
		slugs := make([]string, len(repos))
		for i, r := range repos {
			slugs[i] = filepath.Base(r)
		}
		enc := json.NewEncoder(w)
		_ = enc.Encode(summaryEvent{
			Event:    "done",
			Token:    token,
			Group:    group,
			Repos:    slugs,
			Entities: totalEntities,
			Rels:     totalRels,
			Elapsed:  elapsedStr,
			Warning:  warning,
		})
	} else {
		// Rich summary — read graph artefacts client-side and render the full table.
		if len(repos) > 0 {
			sum := ComputeRebuildSummary(group, repos, elapsed)
			PrintRebuildSummary(w, sum)
			recordHealthHistory(group, sum)
		} else {
			// No repos reported (e.g. single-slug rebuild with no stats). Fall
			// back to the legacy one-liner so the output is never empty.
			summaryParts := []string{}
			if elapsedStr != "" {
				summaryParts = append(summaryParts, elapsedStr)
			}
			if totalEntities > 0 {
				summaryParts = append(summaryParts,
					fmt.Sprintf("%d entities", totalEntities),
					fmt.Sprintf("%d relationships", totalRels))
			}
			if len(summaryParts) > 0 {
				fmt.Fprintf(w, "Group '%s' rebuilt (%s)\n", group, strings.Join(summaryParts, ", "))
			}
		}
		if warning != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
		}
	}
	return nil
}

// indexGroupWithProgress triggers a full index of a group via the daemon and
// renders live phase progress to w (the same broker SSE / poll-fallback flow as
// `grafel rebuild`), then prints a summary. It is the shared entry point used by
// the wizard and `group add` so a freshly-registered group ends with the same
// "Indexing… → Done" experience as the dashboard (#5338).
//
// It returns errDaemonNotRunning when the daemon is down so callers can degrade
// gracefully (the group is still registered; the user can index later).
func indexGroupWithProgress(w, errW io.Writer, group string) error {
	c, err := client.Dial()
	if err != nil {
		if errors.Is(err, client.ErrDaemonNotRunning) {
			return errDaemonNotRunning
		}
		return err
	}
	defer c.Close()

	dashPort := 0
	if st, stErr := c.Status(); stErr == nil && st.DashboardPort > 0 {
		dashPort = st.DashboardPort
	}

	outcomeCh := make(chan rebuildOutcome, 1)
	token := progressToken()
	go func() {
		reply, rpcErr := c.Rebuild(proto.RebuildArgs{
			Group:         group,
			ProgressToken: token,
			// #5328: explicit user-triggered rebuild → foreground (priority + cap).
			Interactive: true,
		})
		outcomeCh <- rebuildOutcome{
			repos:    reply.Repos,
			warning:  reply.Warning,
			elapsed:  reply.ElapsedSec,
			entities: reply.TotalEntities,
			rels:     reply.TotalRels,
			err:      rpcErr,
		}
	}()

	fmt.Fprintf(w, "Indexing group '%s'...\n", group)

	// Path 1: live broker progress via SSE (matches the dashboard exactly).
	if dashPort > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if sseCh, sseErr := subscribeSSE(ctx, dashPort, group, token); sseErr == nil {
			// indexGroupWithProgress does not request WaitForCompletion, so it
			// keeps the historical 5s post-SSE-close give-up (#5991).
			outcome := runBrokerProgress(ctx, w, group, sseCh, outcomeCh, false, false, false)
			cancel()
			if outcome.err != nil {
				return outcome.err
			}
			return finishIndexSummary(w, errW, group, token, outcome)
		}
	}

	// Path 2: no dashboard broker — wait for the RPC and print the summary.
	outcome := <-outcomeCh
	if outcome.err != nil {
		return outcome.err
	}
	return finishIndexSummary(w, errW, group, token, outcome)
}

// finishIndexSummary renders the post-index summary for indexGroupWithProgress.
// It mirrors finishRebuild's non-JSON branch but is writer-based (no cobra).
func finishIndexSummary(w, errW io.Writer, group, token string, o rebuildOutcome) error {
	elapsed := time.Duration(o.elapsed * float64(time.Second))
	if len(o.repos) > 0 {
		sum := ComputeRebuildSummary(group, o.repos, elapsed)
		PrintRebuildSummary(w, sum)
		recordHealthHistory(group, sum)
	} else {
		fmt.Fprintf(w, "Group '%s' indexed\n", group)
	}
	if o.warning != "" {
		fmt.Fprintf(errW, "warning: %s\n", o.warning)
	}
	_ = token
	return nil
}

// printProgressLine emits one human-readable progress line for a repo.
//
// Format follows the spec from issue #989:
//
//	acme-mobile: scanning 1134 files…
//	acme-mobile: extracted 4521 entities (482 functions, 312 classes, …)
//	acme-mobile: 12,318 relationships emitted
//	acme-mobile: P4 algorithms running (PageRank, Communities)…
//	acme-mobile: DONE 5.2s
//
// In-progress phases (walking, extracting, finalizing) use a carriage-return
// suffix when the writer is a TTY so the line updates in place. Terminal
// phases (completed, failed) always use a newline so the final state is
// preserved in the scroll-back buffer.
func printProgressLine(w io.Writer, r proto.RepoProgressState) {
	slug := r.Slug
	if slug == "" {
		slug = r.Path
	}
	tty := isTTY(w)

	switch r.Phase {
	case proto.PhaseQueued:
		// Queued is transient and low-value; skip on TTY (overwritten next tick),
		// print a single line on non-TTY so logs are complete.
		if !tty {
			fmt.Fprintf(w, "%s: queued\n", slug)
		}

	case proto.PhaseStarted:
		if tty {
			fmt.Fprintf(w, "%s: starting…\r", slug)
		} else {
			fmt.Fprintf(w, "%s: starting\n", slug)
		}

	case proto.PhaseWalking:
		if r.FilesWalked > 0 {
			if tty {
				fmt.Fprintf(w, "%s: scanning %s files…\r", slug, fmtInt(r.FilesWalked))
			} else {
				fmt.Fprintf(w, "%s: scanning %s files…\n", slug, fmtInt(r.FilesWalked))
			}
		} else {
			if tty {
				fmt.Fprintf(w, "%s: scanning files…\r", slug)
			} else {
				fmt.Fprintf(w, "%s: scanning files…\n", slug)
			}
		}

	case proto.PhaseExtracting:
		if r.FilesWalked > 0 {
			pct := 0
			if r.FilesWalked > 0 {
				pct = 100 * r.FilesExtracted / r.FilesWalked
			}
			if tty {
				fmt.Fprintf(w, "%s: extracting… %d%% (%s/%s files)\r",
					slug, pct, fmtInt(r.FilesExtracted), fmtInt(r.FilesWalked))
			} else {
				fmt.Fprintf(w, "%s: extracting… %d%% (%s/%s files)\n",
					slug, pct, fmtInt(r.FilesExtracted), fmtInt(r.FilesWalked))
			}
		} else {
			if tty {
				fmt.Fprintf(w, "%s: extracting…\r", slug)
			} else {
				fmt.Fprintf(w, "%s: extracting…\n", slug)
			}
		}

	case proto.PhaseFinalizing:
		// Finalizing covers Pass 4 graph algorithms (PageRank, communities).
		if tty {
			fmt.Fprintf(w, "%s: P4 algorithms running (PageRank, Communities)…\r", slug)
		} else {
			fmt.Fprintf(w, "%s: P4 algorithms running (PageRank, Communities)…\n", slug)
		}

	case proto.PhaseCompleted:
		// Clear the in-progress line (if TTY) and print the final DONE line.
		dur := time.Duration(r.ElapsedSec * float64(time.Second))
		durStr := ""
		if r.ElapsedSec > 0 {
			durStr = fmtDuration(dur)
		}
		if tty {
			// Pad to overwrite any previous carriage-return line.
			fmt.Fprintf(w, "\r%-80s\r", "")
		}
		if r.Entities > 0 || r.Rels > 0 {
			fmt.Fprintf(w, "%s: DONE %s  (%s entities, %s relationships)\n",
				slug, durStr, fmtInt(int(r.Entities)), fmtInt(int(r.Rels)))
		} else if durStr != "" {
			fmt.Fprintf(w, "%s: DONE %s\n", slug, durStr)
		} else {
			fmt.Fprintf(w, "%s: DONE\n", slug)
		}

	case proto.PhaseFailed:
		if tty {
			fmt.Fprintf(w, "\r%-80s\r", "")
		}
		if r.ErrMsg != "" {
			fmt.Fprintf(w, "%s: FAILED — %s\n", slug, r.ErrMsg)
		} else {
			fmt.Fprintf(w, "%s: FAILED\n", slug)
		}

	default:
		fmt.Fprintf(w, "%s: %s\n", slug, r.Phase)
	}
}

// emitJSONProgressState emits a single JSON line for a repo progress state.
func emitJSONProgressState(w io.Writer, token string, r proto.RepoProgressState) {
	type progressEvent struct {
		Event    string `json:"event"`
		Token    string `json:"token"`
		Index    int    `json:"index"`
		Total    int    `json:"total"`
		Slug     string `json:"slug"`
		Path     string `json:"path"`
		Phase    string `json:"phase"`
		Elapsed  string `json:"elapsed,omitempty"`
		Entities int64  `json:"entities,omitempty"`
		Rels     int64  `json:"rels,omitempty"`
		ErrMsg   string `json:"err_msg,omitempty"`
	}
	elapsed := ""
	if r.ElapsedSec > 0 {
		elapsed = fmtDuration(time.Duration(r.ElapsedSec * float64(time.Second)))
	}
	enc := json.NewEncoder(w)
	_ = enc.Encode(progressEvent{
		Event:    "progress",
		Token:    token,
		Index:    r.Index,
		Total:    r.Total,
		Slug:     r.Slug,
		Path:     r.Path,
		Phase:    r.Phase,
		Elapsed:  elapsed,
		Entities: r.Entities,
		Rels:     r.Rels,
		ErrMsg:   r.ErrMsg,
	})
}

// recordHealthHistory appends a HealthEntry to ~/.grafel/health-history.jsonl
// after a successful rebuild and fires configured webhook notifications.
// Errors are silently ignored so a storage failure never disrupts the CLI output.
func recordHealthHistory(group string, sum *RebuildSummary) {
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return
	}
	healthScore := quality.ComputeHealthScore(sum.OrphanRate, 0)
	entry := quality.HealthEntry{
		Timestamp:     time.Now().UTC(),
		Group:         group,
		TotalEntities: sum.TotalEntities,
		OrphanRate:    sum.OrphanRate,
		HealthScore:   healthScore,
	}
	_ = quality.AppendEntry(layout.Root, entry)

	// Fire webhook notifications asynchronously — never block the CLI.
	go dispatchRebuildWebhooks(group, sum, healthScore, layout.Root)
}

// dispatchRebuildWebhooks loads webhook configuration from settings and fires
// appropriate events based on the rebuild outcome. Called in a goroutine after
// a successful rebuild so webhook latency never affects the user-facing output.
func dispatchRebuildWebhooks(group string, sum *RebuildSummary, healthScore float64, root string) {
	// Load settings — silently bail on any error so this path is truly best-effort.
	settings, err := loadWebhookSettings()
	if err != nil || len(settings.Webhooks) == 0 {
		return
	}

	snap := notifications.QualitySnapshot{
		Group:         group,
		OrphanRate:    sum.OrphanRate,
		BugRate:       0, // BugRate not yet computed in rebuild path
		HealthScore:   healthScore,
		TotalEntities: sum.TotalEntities,
	}

	dispatcher := notifications.NewDispatcher()
	now := time.Now().UTC()

	// Always fire rebuild_complete.
	dispatcher.DispatchAll(settings.Webhooks, notifications.WebhookPayload{
		Event:     notifications.EventRebuildComplete,
		Timestamp: now,
		Quality:   snap,
	})

	// Check budgets and fire budget_exceeded when any threshold is breached.
	violations := notifications.CheckBudgets(snap, settings.QualityBudgets)
	if len(violations) > 0 {
		details := make(map[string]any, len(violations))
		for _, v := range violations {
			details[v.Metric] = map[string]any{
				"threshold": v.Threshold,
				"actual":    v.Actual,
			}
		}
		dispatcher.DispatchAll(settings.Webhooks, notifications.WebhookPayload{
			Event:     notifications.EventBudgetExceeded,
			Timestamp: now,
			Quality:   snap,
			Details:   details,
		})
	}

	// Compare against previous entry to detect regression.
	prev, readErr := quality.ReadHistory(root, group, 2)
	if readErr == nil && len(prev) >= 2 {
		prevEntry := prev[len(prev)-2] // second-to-last = prior rebuild
		prevSnap := notifications.QualitySnapshot{
			Group:       group,
			OrphanRate:  prevEntry.OrphanRate,
			BugRate:     prevEntry.BugRate,
			HealthScore: prevEntry.HealthScore,
		}
		if notifications.RegressionDetected(prevSnap, snap) {
			dispatcher.DispatchAll(settings.Webhooks, notifications.WebhookPayload{
				Event:     notifications.EventQualityRegressed,
				Timestamp: now,
				Quality:   snap,
				Details: map[string]any{
					"previous_health": prevEntry.HealthScore,
					"previous_orphan": prevEntry.OrphanRate,
				},
			})
		}
	}
}

// webhookSettingsShape is a minimal subset of AppSettings used to avoid a
// circular import between cli and dashboard packages. Settings are read
// directly from the JSON file.
type webhookSettingsShape struct {
	Webhooks       []notifications.WebhookConfig `json:"webhooks"`
	QualityBudgets notifications.QualityBudgets  `json:"quality_budgets"`
}

// loadWebhookSettings reads only the webhook-relevant fields from settings.json.
func loadWebhookSettings() (webhookSettingsShape, error) {
	layout, err := daemon.DefaultLayout()
	if err != nil {
		return webhookSettingsShape{}, err
	}
	p := layout.Root + "/settings.json"
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return webhookSettingsShape{}, nil
		}
		return webhookSettingsShape{}, err
	}
	var s webhookSettingsShape
	if err := json.Unmarshal(b, &s); err != nil {
		return webhookSettingsShape{}, err
	}
	return s, nil
}

// emitJSONEvent emits a simple JSON heartbeat/generic event line.
func emitJSONEvent(w io.Writer, event, group, slug string) {
	type genericEvent struct {
		Event string `json:"event"`
		Group string `json:"group,omitempty"`
		Slug  string `json:"slug,omitempty"`
	}
	enc := json.NewEncoder(w)
	_ = enc.Encode(genericEvent{
		Event: event,
		Group: group,
		Slug:  slug,
	})
}
