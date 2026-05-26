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

// personaEventTypes is the set of allowed event_type values for archigraph_persona_event.
var personaEventTypes = map[string]bool{
	"invoke":       true,
	"consult_out":  true,
	"save_finding": true,
}

// personaEventsDir returns the daily JSONL file path for persona telemetry.
// Files rotate by calendar date: ~/.archigraph/events/persona-events-YYYY-MM-DD.jsonl
// The directory is created on first write; callers must handle the mkdirall.
func personaEventsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("persona_telemetry: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".archigraph", "events"), nil
}

// personaEventsFile returns the path for today's persona-events JSONL file.
func personaEventsFile() (string, error) {
	dir, err := personaEventsDir()
	if err != nil {
		return "", err
	}
	date := time.Now().UTC().Format("2006-01-02")
	return filepath.Join(dir, fmt.Sprintf("persona-events-%s.jsonl", date)), nil
}

// PersonaEvent is the structure written to the JSONL event log.
// All fields map directly to archigraph_persona_event inputs.
// LOCAL ONLY — never transmitted remotely (privacy promise; see Section 10 in personas.md).
type PersonaEvent struct {
	// Timestamp is the UTC ISO-8601 instant the event was recorded.
	Timestamp string `json:"ts"`
	// Persona is the name of the active persona (e.g. "architect").
	Persona string `json:"persona"`
	// EventType is one of: "invoke", "consult_out", "save_finding".
	EventType string `json:"event_type"`
	// TargetPersona is the peer persona name on consult_out events. Empty otherwise.
	TargetPersona string `json:"target_persona,omitempty"`
	// Depth is the hop count for consult_out events (PR #2531). Empty otherwise.
	Depth int `json:"depth,omitempty"`
	// Chain is the ordered sequence of persona names visited in a consult chain (PR #2531). Empty otherwise.
	Chain []string `json:"chain,omitempty"`
	// Metadata is an optional free-form map for future extensibility.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// appendPersonaEvent writes one JSON line to the daily JSONL file.
// It is safe to call concurrently — the OS write + sync model provides
// sufficient ordering guarantees for append-only JSONL on a local filesystem.
func appendPersonaEvent(evt PersonaEvent) (string, error) {
	path, err := personaEventsFile()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("persona_telemetry: mkdir %s: %w", dir, err)
	}
	line, err := json.Marshal(evt)
	if err != nil {
		return "", fmt.Errorf("persona_telemetry: marshal event: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("persona_telemetry: open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return "", fmt.Errorf("persona_telemetry: write event: %w", err)
	}
	return path, nil
}

// handlePersonaEvent is the handler for archigraph_persona_event (#2474).
//
// Personas call this tool:
//   - On session start:   event_type="invoke"
//   - On each Consult-Out: event_type="consult_out", target_persona=<name>
//   - When a finding is persisted: event_type="save_finding"
//
// The event is appended to the daily JSONL file at:
//
//	~/.archigraph/events/persona-events-YYYY-MM-DD.jsonl
//
// Privacy guarantee: LOCAL ONLY. No remote emission occurs. Callers need not
// pass group/cwd — this tool is group-agnostic by design.
func (s *Server) handlePersonaEvent(ctx context.Context, req mcpapi.CallToolRequest) (*mcpapi.CallToolResult, error) {
	persona, err := req.RequireString("persona")
	if err != nil {
		return mcpapi.NewToolResultError("archigraph_persona_event: persona (string, required): " + err.Error()), nil
	}
	if persona == "" {
		return mcpapi.NewToolResultError("archigraph_persona_event: persona must not be empty"), nil
	}

	eventType, err := req.RequireString("event_type")
	if err != nil {
		return mcpapi.NewToolResultError("archigraph_persona_event: event_type (string, required): " + err.Error()), nil
	}
	if !personaEventTypes[eventType] {
		return mcpapi.NewToolResultError(
			fmt.Sprintf("archigraph_persona_event: event_type must be one of invoke|consult_out|save_finding; got %q", eventType),
		), nil
	}

	targetPersona := argString(req, "target_persona", "")

	// Extract depth if present (consult_out events only, but allow all event types for flexibility).
	depth := argInt(req, "depth", 0)

	// Extract chain if present (consult_out events only, but allow all event types for flexibility).
	chain := argStringSlice(req, "chain")

	// metadata is accepted as a raw map if the client provides it.
	var metadata map[string]any
	if raw, ok := req.GetArguments()["metadata"]; ok {
		if m, ok := raw.(map[string]any); ok {
			metadata = m
		}
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	evt := PersonaEvent{
		Timestamp:     ts,
		Persona:       persona,
		EventType:     eventType,
		TargetPersona: targetPersona,
		Depth:         depth,
		Chain:         chain,
		Metadata:      metadata,
	}

	_, writeErr := appendPersonaEvent(evt)
	if writeErr != nil {
		// Telemetry failures should NOT block the persona workflow. Return a
		// non-error result with a warning so the calling persona can continue.
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
