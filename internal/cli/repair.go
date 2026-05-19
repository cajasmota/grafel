package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
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
			fmt.Fprintf(w, "rebuilt %s\n", r)
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
			fmt.Fprintf(w, "rebuilt %s\n", r)
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

	// Final summary line.
	var elapsedStr string
	if reply.ElapsedSec > 0 {
		elapsedStr = fmtDuration(time.Duration(reply.ElapsedSec * float64(time.Second)))
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
		enc := json.NewEncoder(w)
		_ = enc.Encode(summaryEvent{
			Event:    "done",
			Token:    token,
			Group:    group,
			Repos:    reply.Repos,
			Entities: reply.TotalEntities,
			Rels:     reply.TotalRels,
			Elapsed:  elapsedStr,
			Warning:  reply.Warning,
		})
	} else {
		// Pretty summary.
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
			fmt.Fprintf(w, "Total: %s\n", strings.Join(summaryParts, ", "))
		}
		for _, r := range reply.Repos {
			fmt.Fprintf(w, "rebuilt %s\n", r)
		}
		if reply.Warning != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", reply.Warning)
		}
	}
	return nil
}

// printProgressLine emits one human-readable progress line for a repo.
func printProgressLine(w io.Writer, r proto.RepoProgressState) {
	prefix := ""
	if r.Total > 0 {
		prefix = fmt.Sprintf("  [%d/%d] %s: ", r.Index, r.Total, r.Slug)
	} else {
		prefix = fmt.Sprintf("  %s: ", r.Slug)
	}

	switch r.Phase {
	case proto.PhaseQueued:
		fmt.Fprintf(w, "%squeued\n", prefix)
	case proto.PhaseStarted:
		fmt.Fprintf(w, "%sstarted\n", prefix)
	case proto.PhaseWalking:
		if r.FilesWalked > 0 {
			fmt.Fprintf(w, "%swalking files... %d candidates\n", prefix, r.FilesWalked)
		} else {
			fmt.Fprintf(w, "%swalking files...\n", prefix)
		}
	case proto.PhaseExtracting:
		if r.FilesExtracted > 0 && r.FilesWalked > 0 {
			fmt.Fprintf(w, "%sextracting (%d/%d files, %s)\n",
				prefix, r.FilesExtracted, r.FilesWalked,
				fmtDuration(time.Duration(r.ElapsedSec*float64(time.Second))))
		} else {
			fmt.Fprintf(w, "%sextracting...\n", prefix)
		}
	case proto.PhaseFinalizing:
		fmt.Fprintf(w, "%sfinalizing...\n", prefix)
	case proto.PhaseCompleted:
		parts := []string{}
		if r.ElapsedSec > 0 {
			parts = append(parts, fmtDuration(time.Duration(r.ElapsedSec*float64(time.Second))))
		}
		if r.Entities > 0 {
			parts = append(parts, fmt.Sprintf("%d entities", r.Entities))
		}
		if r.Rels > 0 {
			parts = append(parts, fmt.Sprintf("%d rels", r.Rels))
		}
		if len(parts) > 0 {
			fmt.Fprintf(w, "%scompleted (%s)\n", prefix, strings.Join(parts, ", "))
		} else {
			fmt.Fprintf(w, "%scompleted\n", prefix)
		}
	case proto.PhaseFailed:
		if r.ErrMsg != "" {
			fmt.Fprintf(w, "%sfailed: %s\n", prefix, r.ErrMsg)
		} else {
			fmt.Fprintf(w, "%sfailed\n", prefix)
		}
	default:
		fmt.Fprintf(w, "%s%s\n", prefix, r.Phase)
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
