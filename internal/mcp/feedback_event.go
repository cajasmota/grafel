package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// feedbackOutcomes is the allowed set of outcome values for
// archigraph_feedback_event. These map to the rollup's quality buckets.
var feedbackOutcomes = map[string]bool{
	"helped":             true, // the graph answered well / saved work
	"partial":            true, // answered but incomplete
	"wrong":              true, // fabricated or contradicted the source
	"missing_capability": true, // the thing asked for isn't extracted at all
	// milestone is a neutral narrative beat (no quality judgment). Agents use it
	// to mark planning decisions and parity milestones (e.g. "inspections module
	// reaches parity"). Excluded from the rollup fix-queue, and it lets the
	// planning phase be captured before any code exists. See #3206.
	"milestone": true,
}

// feedbackEventsFile returns the path for today's feedback-events JSONL file.
// It shares the persona events directory (~/.archigraph/events) and rotates by
// calendar date. LOCAL ONLY — same privacy promise as persona telemetry.
func feedbackEventsFile() (string, error) {
	dir, err := personaEventsDir()
	if err != nil {
		return "", err
	}
	date := time.Now().UTC().Format("2006-01-02")
	return filepath.Join(dir, fmt.Sprintf("feedback-events-%s.jsonl", date)), nil
}

// FeedbackEvent is one agent-experience datum captured during a test run
// (e.g. an internal backend rewrite). It records how a real archigraph
// interaction went, tagged so a later rollup can compare groups (old vs new)
// and map shortfalls to fixable extractor lanes.
//
// LOCAL ONLY — never transmitted remotely.
type FeedbackEvent struct {
	// Timestamp is the UTC ISO-8601 instant the event was recorded.
	Timestamp string `json:"ts"`
	// Group is the archigraph group the interaction was about (enables
	// old-vs-new comparison). Optional but strongly recommended.
	Group string `json:"group,omitempty"`
	// Phase is a free-form label for the work in progress, e.g.
	// "port:inspections". Optional.
	Phase string `json:"phase,omitempty"`
	// Library is the framework or library in play, e.g. "pymongo",
	// "typeorm", "class-validator". Optional.
	Library string `json:"library,omitempty"`
	// Capability uses the coverage-matrix taxonomy (data_access, routing,
	// auth, dto, testing, …) so wrong/missing outcomes map to an extractor
	// lane. Optional, free-form.
	Capability string `json:"capability,omitempty"`
	// Outcome is one of helped|partial|wrong|missing_capability|milestone (required).
	Outcome string `json:"outcome"`
	// Note is an optional one-line free-form detail.
	Note string `json:"note,omitempty"`
}

// appendFeedbackEvent writes one JSON line to the daily feedback JSONL file.
// Append-only on a local filesystem; safe to call concurrently.
func appendFeedbackEvent(evt FeedbackEvent) (string, error) {
	path, err := feedbackEventsFile()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("feedback_event: mkdir %s: %w", dir, err)
	}
	line, err := json.Marshal(evt)
	if err != nil {
		return "", fmt.Errorf("feedback_event: marshal event: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("feedback_event: open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return "", fmt.Errorf("feedback_event: write event: %w", err)
	}
	return path, nil
}

// handleFeedbackEvent is the handler for archigraph_feedback_event (#3204).
//
// Agents call this opportunistically during a test run — when an archigraph
// answer was wrong/incomplete or a library wasn't recognized — and at phase
// checkpoints. Events append to:
//
//	~/.archigraph/events/feedback-events-YYYY-MM-DD.jsonl
//
// Privacy guarantee: LOCAL ONLY. No remote emission. Recording failures never
// block the agent — the tool returns a non-error result with a warning.
func (s *Server) handleFeedbackEvent(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	outcome, err := req.RequireString("outcome")
	if err != nil {
		return mcpapi.NewToolResultError("archigraph_feedback_event: outcome (string, required): " + err.Error()), nil
	}
	if !feedbackOutcomes[outcome] {
		return mcpapi.NewToolResultError(
			fmt.Sprintf("archigraph_feedback_event: outcome must be one of helped|partial|wrong|missing_capability|milestone; got %q", outcome),
		), nil
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	evt := FeedbackEvent{
		Timestamp:  ts,
		Group:      argString(req, "group", ""),
		Phase:      argString(req, "phase", ""),
		Library:    argString(req, "library", ""),
		Capability: argString(req, "capability", ""),
		Outcome:    outcome,
		Note:       argString(req, "note", ""),
	}

	if _, writeErr := appendFeedbackEvent(evt); writeErr != nil {
		return jsonResult(map[string]any{
			"recorded": false,
			"ts":       ts,
			"warning":  writeErr.Error(),
		}), nil
	}
	return jsonResult(map[string]any{
		"recorded": true,
		"ts":       ts,
	}), nil
}
