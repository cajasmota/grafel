package cli

import (
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

	"github.com/cajasmota/archigraph/internal/daemon/client"
	"github.com/cajasmota/archigraph/internal/daemon/proto"
)

// rebuild and reset both forward to the daemon's Rebuild RPC; reset
// additionally requests the daemon wipe each repo's .archigraph/ before
// indexing. The deprecated remerge alias was removed in ADR-0017 —
// callers must use `archigraph rebuild [group]` now.

func newRebuildCmd() *cobra.Command {
	var quiet bool
	var jsonProgress bool

	cmd := &cobra.Command{
		Use:   "rebuild [group] [slug]",
		Short: "Force rebuild via the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRebuildClient(cmd, args, false, quiet, jsonProgress)
		},
	}
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress progress output; print only the final summary")
	cmd.Flags().BoolVar(&jsonProgress, "json-progress", false, "emit one JSON event per line (for scripting)")
	return cmd
}

func newResetCmd() *cobra.Command {
	var quiet bool
	var jsonProgress bool

	cmd := &cobra.Command{
		Use:   "reset [group] [slug]",
		Short: "Wipe .archigraph/ and rebuild via the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRebuildClient(cmd, args, true, quiet, jsonProgress)
		},
	}
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress progress output; print only the final summary")
	cmd.Flags().BoolVar(&jsonProgress, "json-progress", false, "emit one JSON event per line (for scripting)")
	return cmd
}

// progressToken generates a short unique token for a rebuild session.
func progressToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) +
		strconv.FormatUint(rand.Uint64()&0xffff, 36) //nolint:gosec
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
// The design uses two daemon connections:
//  1. Primary (long-lived): sends the blocking Rebuild RPC.
//  2. Poll (short-lived): opened on the same socket to poll IndexProgress
//     every 2 seconds while the primary connection is blocked.
//
// This is necessary because net/rpc serialises calls on a single connection;
// a blocked Rebuild call would starve all IndexProgress polls on the same Client.
func runRebuildClient(cmd *cobra.Command, args []string, wipe bool, quiet bool, jsonProgress bool) error {
	if len(args) == 0 {
		return errors.New("supply [group] (and optional [slug])")
	}

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

	// --quiet: skip progress, run synchronously with no token.
	if quiet {
		reply, err := c.Rebuild(proto.RebuildArgs{Group: group, Slug: slug, Wipe: wipe})
		if err != nil {
			return err
		}
		for _, r := range reply.Repos {
			// reply.Repos contains absolute paths since #1076 fix; show basename.
			fmt.Fprintf(w, "rebuilt %s\n", filepath.Base(r))
		}
		if reply.Warning != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", reply.Warning)
		}
		return nil
	}

	token := progressToken()

	// Open a second connection for polling (avoids blocking on the primary).
	pollClient, pollDialErr := client.DialProgress(c.SocketPath())
	if pollDialErr != nil {
		// Polling unavailable — fall back to quiet mode.
		reply, err2 := c.Rebuild(proto.RebuildArgs{Group: group, Slug: slug, Wipe: wipe})
		if err2 != nil {
			return err2
		}
		for _, r := range reply.Repos {
			// reply.Repos contains absolute paths since #1076 fix; show basename.
			fmt.Fprintf(w, "rebuilt %s\n", filepath.Base(r))
		}
		if reply.Warning != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", reply.Warning)
		}
		return nil
	}
	defer pollClient.Close()

	// Start the rebuild asynchronously on the primary connection.
	type rebuildResult struct {
		reply proto.RebuildReply
		err   error
	}
	resultCh := make(chan rebuildResult, 1)
	go func() {
		reply, err := c.Rebuild(proto.RebuildArgs{
			Group:         group,
			Slug:          slug,
			Wipe:          wipe,
			ProgressToken: token,
		})
		resultCh <- rebuildResult{reply: reply, err: err}
	}()

	if !jsonProgress {
		fmt.Fprintf(w, "Rebuilding group '%s'...\n", group)
	}

	// Poll loop — 2-second interval, heartbeat after 10s of silence.
	// Track the last printed phase per repo path to avoid duplicating unchanged lines.
	seenPhases := map[string]string{}
	lastEventAt := time.Now()
	const pollInterval = 2 * time.Second
	const heartbeatThreshold = 10 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var finalResult rebuildResult
	done := false

	for !done {
		select {
		case finalResult = <-resultCh:
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

	if finalResult.err != nil {
		return finalResult.err
	}

	reply := finalResult.reply

	var elapsedStr string
	elapsed := time.Duration(reply.ElapsedSec * float64(time.Second))
	if reply.ElapsedSec > 0 {
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
		slugs := make([]string, len(reply.Repos))
		for i, r := range reply.Repos {
			slugs[i] = filepath.Base(r)
		}
		enc := json.NewEncoder(w)
		_ = enc.Encode(summaryEvent{
			Event:    "done",
			Token:    token,
			Group:    group,
			Repos:    slugs,
			Entities: reply.TotalEntities,
			Rels:     reply.TotalRels,
			Elapsed:  elapsedStr,
			Warning:  reply.Warning,
		})
	} else {
		// Rich summary — read graph artefacts client-side and render the full table.
		if len(reply.Repos) > 0 {
			sum := ComputeRebuildSummary(group, reply.Repos, elapsed)
			PrintRebuildSummary(w, sum)
		} else {
			// No repos reported (e.g. single-slug rebuild with no stats). Fall
			// back to the legacy one-liner so the output is never empty.
			summaryParts := []string{}
			if elapsedStr != "" {
				summaryParts = append(summaryParts, elapsedStr)
			}
			if reply.TotalEntities > 0 {
				summaryParts = append(summaryParts,
					fmt.Sprintf("%d entities", reply.TotalEntities),
					fmt.Sprintf("%d relationships", reply.TotalRels))
			}
			if len(summaryParts) > 0 {
				fmt.Fprintf(w, "Group '%s' rebuilt (%s)\n", group, strings.Join(summaryParts, ", "))
			}
		}
		if reply.Warning != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", reply.Warning)
		}
	}
	return nil
}

// printProgressLine emits one human-readable progress line for a repo.
//
// Format follows the spec from issue #989:
//
//	core-mobile: scanning 1134 files…
//	core-mobile: extracted 4521 entities (482 functions, 312 classes, …)
//	core-mobile: 12,318 relationships emitted
//	core-mobile: P4 algorithms running (PageRank, Communities)…
//	core-mobile: DONE 5.2s
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
