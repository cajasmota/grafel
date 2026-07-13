package progress

// SidecarReader is the serve-side tail primitive for a group's progress NDJSON
// file. It reads all COMPLETE lines from a byte offset, reconstructs Events,
// and reports the new offset so it can be called repeatedly (append-aware) to
// pick up newly-appended lines. It tolerates a torn/partial trailing line — the
// classic crash-safety hazard of an append-only file being read concurrently.

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
)

// SidecarReader reads a single group's progress sidecar file.
type SidecarReader struct {
	path string
}

// NewSidecarReader constructs a reader for groupSlug, deriving the identical
// on-disk path a SidecarWriter for the same slug writes to.
func NewSidecarReader(groupSlug string) (*SidecarReader, error) {
	path, err := SidecarPath(groupSlug)
	if err != nil {
		return nil, err
	}
	return &SidecarReader{path: path}, nil
}

// Path returns the on-disk NDJSON path this reader tails.
func (r *SidecarReader) Path() string { return r.path }

// ReadFrom reads every complete line starting at byte offset, reconstructs the
// Events, and returns the offset to resume from next time. Only whole lines
// (terminated by '\n') are consumed: a trailing partial line — a writer's
// in-flight append or a torn crash tail — is left unconsumed so the returned
// offset points just past the last complete line, and a later call picks the
// rest up once it is completed. A complete line that fails json.Unmarshal is
// discarded (and its bytes skipped) rather than failing the whole read.
//
// A non-existent file is not an error: it yields (nil, offset, nil), the normal
// "writer hasn't started yet" state for a poll-safe tailer.
func (r *SidecarReader) ReadFrom(offset int64) ([]Event, int64, error) {
	lines, newOffset, err := readSidecarLines(r.path, offset)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, offset, nil
		}
		return nil, offset, err
	}
	if len(lines) == 0 {
		return nil, newOffset, nil
	}
	events := make([]Event, 0, len(lines))
	for _, l := range lines {
		events = append(events, l.toEvent())
	}
	return events, newOffset, nil
}

// ReadAll reads the whole file from the start.
func (r *SidecarReader) ReadAll() ([]Event, error) {
	evs, _, err := r.ReadFrom(0)
	return evs, err
}

// readSidecarLines is the shared low-level parse: open path, read from offset,
// split into complete newline-terminated lines, json.Unmarshal each into a
// SidecarLine (skipping any that fail), and return the parsed lines plus the
// offset just past the last complete line consumed. The trailing partial line
// (no newline) is NOT consumed and NOT included in newOffset.
func readSidecarLines(path string, offset int64) ([]SidecarLine, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, 0); err != nil {
			return nil, offset, err
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, offset, err
	}

	// Only bytes up to and including the final newline form complete lines.
	lastNL := bytes.LastIndexByte(data, '\n')
	if lastNL < 0 {
		// No complete line available from this offset (only a partial tail).
		return nil, offset, nil
	}
	complete := data[:lastNL+1]
	newOffset := offset + int64(len(complete))

	var out []SidecarLine
	for _, raw := range bytes.Split(complete, []byte{'\n'}) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var l SidecarLine
		if err := json.Unmarshal(raw, &l); err != nil {
			// Discard a corrupt/torn complete line, continue (crash-safety).
			continue
		}
		out = append(out, l)
	}
	return out, newOffset, nil
}
