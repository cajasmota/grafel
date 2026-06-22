package cli

// repair_broker.go — broker-backed progress rendering for `grafel rebuild`.
//
// When the daemon's embedded dashboard is running, the CLI opens an HTTP SSE
// connection to /api/index-progress/{group} and renders progress.Event values
// in real time. This gives CLI and web users identical event streams — both
// are subscribers of the same in-memory broker.
//
// Rendering rules:
//   - TTY (default): ANSI color + carriage-return overwrites for in-progress phases.
//   - --plain: no ANSI, one line per phase transition (for CI/scripted use).
//   - --quiet: no progress at all (handled before this layer).
//   - --json-progress / --json: one NDJSON line per broker event.
//
// ANSI color scheme:
//   - green  (\033[32m): done / completed events
//   - yellow (\033[33m): in-progress phases
//   - red    (\033[31m): error
//   - gray   (\033[90m): queued / pending
//   - reset  (\033[0m)

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/progress"
)

const (
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiGray   = "\033[90m"
	ansiReset  = "\033[0m"
)

// sseEvent holds a parsed SSE event.
type sseEvent struct {
	name string
	data string
}

// subscribeSSE opens an SSE connection to the daemon's progress endpoint for
// the given group and feeds parsed events onto the returned channel. It
// returns an error if the initial HTTP connection fails (e.g. dashboard not
// running). The caller must cancel ctx when done — that closes the channel.
func subscribeSSE(ctx context.Context, dashPort int, group string) (<-chan sseEvent, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/index-progress/%s", dashPort, group)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	// Use a dedicated client with no read timeout (SSE is long-lived).
	httpClient := &http.Client{Transport: &http.Transport{}}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SSE connect: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("SSE endpoint returned %d", resp.StatusCode)
	}

	ch := make(chan sseEvent, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		readSSEEvents(ctx, resp.Body, ch)
	}()
	return ch, nil
}

// readSSEEvents parses the SSE text/event-stream body and emits events onto ch.
// Terminates when ctx is cancelled or the body closes.
func readSSEEvents(ctx context.Context, r io.Reader, ch chan<- sseEvent) {
	scanner := bufio.NewScanner(r)
	var curEvent sseEvent
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := scanner.Text()
		if line == "" {
			// Empty line = event boundary.
			if curEvent.name != "" || curEvent.data != "" {
				select {
				case ch <- curEvent:
				default:
					// Slow consumer — drop event (best-effort).
				}
			}
			curEvent = sseEvent{}
			continue
		}
		if after, ok := strings.CutPrefix(line, "event: "); ok {
			curEvent.name = after
		} else if after, ok := strings.CutPrefix(line, "data: "); ok {
			if curEvent.data != "" {
				curEvent.data += "\n"
			}
			curEvent.data += after
		}
		// Ignore id: and retry: lines.
	}
}

// runBrokerProgress consumes SSE events for the given group and renders them.
// It returns when ctx is cancelled or the channel is closed. resultCh carries
// the final Rebuild RPC outcome; done is closed when that result arrives so
// the renderer can flush a final heartbeat and exit cleanly.
//
// Flags:
//
//	plain      — no ANSI escapes, one line per event
//	jsonEvents — NDJSON one line per broker event
func runBrokerProgress(
	ctx context.Context,
	w io.Writer,
	group string,
	sseCh <-chan sseEvent,
	resultCh <-chan rebuildOutcome,
	plain bool,
	jsonEvents bool,
) rebuildOutcome {
	tty := isTTY(w) && !plain

	// lastLine tracks the last in-progress line per repo slug for TTY overwrite.
	lastLineLen := map[string]int{}

	// heartbeat fires if we hear nothing for 10 seconds.
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	lastActivity := time.Now()

	var outcome rebuildOutcome
	rpcDone := false

	for {
		select {
		case o, ok := <-resultCh:
			if ok {
				outcome = o
				rpcDone = true
				// Drain one last batch from SSE before returning.
				drainSSE(ctx, w, group, sseCh, lastLineLen, tty, plain, jsonEvents, 300*time.Millisecond)
				return outcome
			}

		case ev, ok := <-sseCh:
			if !ok {
				// SSE stream closed (daemon shutdown or group done).
				if rpcDone {
					return outcome
				}
				// Wait up to 5 seconds for the RPC to land.
				select {
				case o := <-resultCh:
					return o
				case <-time.After(5 * time.Second):
					return outcome
				}
			}
			lastActivity = time.Now()
			_ = lastActivity
			heartbeat.Reset(10 * time.Second)

			if ev.name == "connected" || ev.name == "heartbeat" || ev.name == "close" {
				continue
			}
			if ev.name != "progress" || ev.data == "" {
				continue
			}

			var e progress.Event
			if err := json.Unmarshal([]byte(ev.data), &e); err != nil {
				continue
			}
			renderBrokerEvent(w, e, lastLineLen, tty, plain, jsonEvents)

		case <-heartbeat.C:
			if !jsonEvents && !rpcDone {
				elapsed := fmtDuration(time.Since(lastActivity))
				if plain || !tty {
					fmt.Fprintf(w, "  ... still working (%s elapsed)\n", elapsed)
				} else {
					fmt.Fprintf(w, "  ... still working (%s elapsed)\n", elapsed)
				}
			}
		}
	}
}

// drainSSE reads from sseCh for up to maxWait, rendering any final events.
func drainSSE(
	ctx context.Context,
	w io.Writer,
	_ string,
	sseCh <-chan sseEvent,
	lastLineLen map[string]int,
	tty, plain, jsonEvents bool,
	maxWait time.Duration,
) {
	deadline := time.After(maxWait)
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case ev, ok := <-sseCh:
			if !ok {
				return
			}
			if ev.name != "progress" || ev.data == "" {
				continue
			}
			var e progress.Event
			if err := json.Unmarshal([]byte(ev.data), &e); err != nil {
				continue
			}
			renderBrokerEvent(w, e, lastLineLen, tty, plain, jsonEvents)
		}
	}
}

// renderBrokerEvent formats and writes one progress.Event to w.
func renderBrokerEvent(
	w io.Writer,
	e progress.Event,
	lastLineLen map[string]int,
	tty, plain, jsonEvents bool,
) {
	if jsonEvents {
		enc := json.NewEncoder(w)
		_ = enc.Encode(e)
		return
	}

	slug := e.RepoSlug
	if slug == "" {
		slug = e.GroupSlug
	}
	if slug == "" {
		slug = "?"
	}

	var line string
	terminal := false // terminal phases end with \n, in-progress with \r

	switch e.Phase {
	case progress.PhaseScan:
		if e.FilesTotal > 0 {
			line = fmt.Sprintf("%s: scanning %s files…", slug, fmtInt(e.FilesTotal))
		} else {
			line = fmt.Sprintf("%s: scanning…", slug)
		}
		line = colorize(line, ansiYellow, tty, plain)

	case progress.PhaseExtractAST:
		pct := 0
		if e.FilesTotal > 0 {
			pct = 100 * e.FilesDone / e.FilesTotal
		}
		if e.CurrentFile != "" {
			line = fmt.Sprintf("%s: extracting_ast %d/%d files (%s) %d%%",
				slug, e.FilesDone, e.FilesTotal, e.CurrentFile, pct)
		} else if e.FilesTotal > 0 {
			line = fmt.Sprintf("%s: extracting_ast %d/%d files %d%%",
				slug, e.FilesDone, e.FilesTotal, pct)
		} else {
			line = fmt.Sprintf("%s: extracting_ast…", slug)
		}
		line = colorize(line, ansiYellow, tty, plain)

	case progress.PhaseResolveRefs:
		line = colorize(fmt.Sprintf("%s: resolving_refs…", slug), ansiYellow, tty, plain)

	case progress.PhaseAlgorithms:
		if e.AlgorithmName != "" {
			line = colorize(fmt.Sprintf("%s: running_algorithms (%s)…", slug, e.AlgorithmName), ansiYellow, tty, plain)
		} else {
			line = colorize(fmt.Sprintf("%s: running_algorithms…", slug), ansiYellow, tty, plain)
		}

	// #5334 — granular graph-assembly phases. Labels are kept in lock-step with
	// the dashboard wizard (webui-v2 PHASE_LABEL) so CLI and web users see the
	// same human phrase for the same phase.
	case progress.PhaseBuildCommunities:
		line = colorize(fmt.Sprintf("%s: Building communities…", slug), ansiYellow, tty, plain)

	case progress.PhaseComputeCentrality:
		line = colorize(fmt.Sprintf("%s: Computing centrality…", slug), ansiYellow, tty, plain)

	case progress.PhaseDetectLinks:
		line = colorize(fmt.Sprintf("%s: Detecting cross-repo links…", slug), ansiYellow, tty, plain)

	case progress.PhaseComputeFlows:
		line = colorize(fmt.Sprintf("%s: Computing flows…", slug), ansiYellow, tty, plain)

	case progress.PhaseWriteGraph:
		line = colorize(fmt.Sprintf("%s: Writing graph…", slug), ansiYellow, tty, plain)

	case progress.PhaseMaterialize:
		line = colorize(fmt.Sprintf("%s: materializing…", slug), ansiYellow, tty, plain)

	case progress.PhaseDone:
		terminal = true
		if e.EntitiesSoFar > 0 {
			line = colorize(
				fmt.Sprintf("%s: done  (%s entities)", slug, fmtInt(e.EntitiesSoFar)),
				ansiGreen, tty, plain)
		} else {
			line = colorize(fmt.Sprintf("%s: done", slug), ansiGreen, tty, plain)
		}

	case progress.PhaseError:
		terminal = true
		if e.Error != "" {
			line = colorize(fmt.Sprintf("%s: error — %s", slug, e.Error), ansiRed, tty, plain)
		} else {
			line = colorize(fmt.Sprintf("%s: error", slug), ansiRed, tty, plain)
		}

	default:
		line = colorize(fmt.Sprintf("%s: %s", slug, e.Phase), ansiGray, tty, plain)
	}

	if tty {
		// Clear any previous in-progress overwrite line for this slug.
		if prev := lastLineLen[slug]; prev > 0 && !terminal {
			// Pad to overwrite old line then \r to return.
			padding := ""
			if len(line) < prev {
				padding = strings.Repeat(" ", prev-len(line))
			}
			fmt.Fprintf(w, "%s%s\r", line, padding)
			lastLineLen[slug] = len(line)
		} else if terminal {
			// Clear the in-progress line before printing the terminal line.
			if prev := lastLineLen[slug]; prev > 0 {
				fmt.Fprintf(w, "\r%-*s\r", prev, "")
				delete(lastLineLen, slug)
			}
			fmt.Fprintf(w, "%s\n", line)
		} else {
			fmt.Fprintf(w, "%s\r", line)
			lastLineLen[slug] = len(line)
		}
	} else {
		// Non-TTY: always newline-terminated.
		fmt.Fprintf(w, "%s\n", line)
	}
}

// colorize wraps text with ANSI color codes when tty is true and plain is false.
func colorize(text, color string, tty, plain bool) string {
	if !tty || plain {
		return text
	}
	return color + text + ansiReset
}

// rebuildOutcome captures the result of the async Rebuild RPC call.
type rebuildOutcome struct {
	repos    []string
	warning  string
	elapsed  float64
	entities int64
	rels     int64
	err      error
}
