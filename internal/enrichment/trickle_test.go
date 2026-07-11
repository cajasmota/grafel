package enrichment

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// maxReadTracker wraps an io.Reader and records the largest single Read()
// request length it observed. os.ReadFile (and json.Unmarshal fed by it)
// effectively asks for the WHOLE file in one shot (a buffer sized to the
// full file length); a truly streaming decoder never does — it asks for
// small, roughly-constant-size chunks no matter how big the underlying file
// is. This is the concrete, testable signature of "no double
// materialization" from issue #5720.
type maxReadTracker struct {
	r      io.Reader
	maxLen int
}

func (m *maxReadTracker) Read(p []byte) (int, error) {
	if len(p) > m.maxLen {
		m.maxLen = len(p)
	}
	return m.r.Read(p)
}

// bigPriorCandidatesJSON builds a large prior candidates file (many records,
// each padded with a big Context blob) so a full in-memory re-materialization
// would be clearly distinguishable, size-wise, from a streaming decode.
func bigPriorCandidatesJSON(t *testing.T, n int) []byte {
	t.Helper()
	pad := make([]byte, 4096)
	for i := range pad {
		pad[i] = 'x'
	}
	cs := make([]Candidate, n)
	for i := range cs {
		cs[i] = Candidate{
			ID:           "cand-" + itoa(i),
			Kind:         "describe_entity",
			SubjectID:    "e" + itoa(i),
			DiscoveredAt: "2024-01-01T00:00:00Z",
			Context:      map[string]any{"padding": string(pad)},
		}
	}
	env := candidatesEnvelope{Version: CandidatesSchemaVersion, Candidates: cs}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return data
}

func itoa(i int) string {
	// tiny local itoa to avoid importing strconv twice for a one-off helper
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// TestLoadDiscoveredAtIndex_NeverSlurpsWholeFile is the RED-before-fix,
// GREEN-after-fix test for the "no double materialization" acceptance
// criterion (#5720 requirement 3): the streaming loader must never request a
// single Read() anywhere close to the full file size, no matter how large
// the prior candidates file is.
func TestLoadDiscoveredAtIndex_NeverSlurpsWholeFile(t *testing.T) {
	data := bigPriorCandidatesJSON(t, 2000) // several MB
	if len(data) < 2*1024*1024 {
		t.Fatalf("fixture too small to be a meaningful test: %d bytes", len(data))
	}

	tracker := &maxReadTracker{r: bytesReader(data)}
	idx := loadDiscoveredAtIndexFromReader(tracker)

	if len(idx) != 2000 {
		t.Fatalf("expected 2000 discovered_at entries, got %d", len(idx))
	}
	if got, ok := idx["cand-0"]; !ok || got != "2024-01-01T00:00:00Z" {
		t.Fatalf("expected cand-0 discovered_at preserved, got %q ok=%v", got, ok)
	}

	// The streaming decoder's internal buffer should stay tiny relative to
	// the multi-MB input — bound generously at 256KB to allow for internal
	// json.Decoder growth on any single (padded) record without allowing
	// anything resembling a whole-file slurp.
	const maxAllowedRead = 256 * 1024
	if tracker.maxLen > maxAllowedRead {
		t.Fatalf("loader requested a %d-byte read (file is %d bytes) — looks like a full-file materialization, not a stream",
			tracker.maxLen, len(data))
	}
	t.Logf("file=%d bytes, largest single Read()=%d bytes", len(data), tracker.maxLen)
}

// TestLoadDiscoveredAtIndex_BareArrayForm exercises the legacy bare-array
// on-disk shape.
func TestLoadDiscoveredAtIndex_BareArrayForm(t *testing.T) {
	arr := []Candidate{
		{ID: "a", DiscoveredAt: "t1"},
		{ID: "b", DiscoveredAt: "t2"},
	}
	data, err := json.Marshal(arr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	idx := loadDiscoveredAtIndexFromReader(bytesReader(data))
	if idx["a"] != "t1" || idx["b"] != "t2" {
		t.Fatalf("unexpected index: %#v", idx)
	}
}

// TestLoadDiscoveredAtIndex_MissingFile confirms the loader tolerates an
// absent prior file (first-ever index of a repo).
func TestLoadDiscoveredAtIndex_MissingFile(t *testing.T) {
	dir := t.TempDir()
	idx := loadDiscoveredAtIndex(filepath.Join(dir, "does-not-exist.json"))
	if len(idx) != 0 {
		t.Fatalf("expected empty index for missing file, got %#v", idx)
	}
}

// TestCandidateAppender_RoundTrip verifies chunked AppendChunk calls produce
// a valid, fully-readable enrichment-candidates.json, and that discovered_at
// values from a prior run are preserved (idempotence, issue #53).
func TestCandidateAppender_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Seed a prior candidates file with a discovered_at we expect to survive.
	prior := candidatesEnvelope{
		Version: CandidatesSchemaVersion,
		Candidates: []Candidate{
			{ID: "e1|describe_entity", Kind: "describe_entity", SubjectID: "e1", DiscoveredAt: "2020-01-01T00:00:00Z"},
		},
	}
	priorData, err := json.MarshalIndent(prior, "", "  ")
	if err != nil {
		t.Fatalf("marshal prior: %v", err)
	}
	if err := os.WriteFile(candidatesPath(dir), priorData, 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}

	appender, err := NewCandidateAppender(dir)
	if err != nil {
		t.Fatalf("NewCandidateAppender: %v", err)
	}

	chunk1 := []Candidate{
		{ID: "e1|describe_entity", Kind: "describe_entity", SubjectID: "e1", DiscoveredAt: "2099-12-31T00:00:00Z"},
	}
	chunk2 := []Candidate{
		{ID: "e2|describe_entity", Kind: "describe_entity", SubjectID: "e2", DiscoveredAt: "2099-12-31T00:00:00Z"},
	}
	if err := appender.AppendChunk(chunk1); err != nil {
		t.Fatalf("AppendChunk 1: %v", err)
	}
	if err := appender.AppendChunk(chunk2); err != nil {
		t.Fatalf("AppendChunk 2: %v", err)
	}
	if appender.Count() != 2 {
		t.Fatalf("expected count 2, got %d", appender.Count())
	}
	if err := appender.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	final, err := os.ReadFile(candidatesPath(dir))
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	var env candidatesEnvelope
	if err := json.Unmarshal(final, &env); err != nil {
		t.Fatalf("unmarshal final: %v (data=%s)", err, final)
	}
	if len(env.Candidates) != 2 {
		t.Fatalf("expected 2 candidates in final file, got %d: %+v", len(env.Candidates), env.Candidates)
	}
	byID := map[string]Candidate{}
	for _, c := range env.Candidates {
		byID[c.ID] = c
	}
	if got := byID["e1|describe_entity"].DiscoveredAt; got != "2020-01-01T00:00:00Z" {
		t.Fatalf("expected prior discovered_at preserved for e1, got %q", got)
	}
	if got := byID["e2|describe_entity"].DiscoveredAt; got != "2099-12-31T00:00:00Z" {
		t.Fatalf("expected fresh discovered_at for new candidate e2, got %q", got)
	}
}

// TestCandidateAppender_Abort verifies that Abort leaves the prior published
// file untouched and removes the temp file.
func TestCandidateAppender_Abort(t *testing.T) {
	dir := t.TempDir()
	priorData := []byte(`{"version":2,"candidates":[{"id":"keep","kind":"describe_entity","subject_id":"e1"}]}`)
	if err := os.WriteFile(candidatesPath(dir), priorData, 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}

	appender, err := NewCandidateAppender(dir)
	if err != nil {
		t.Fatalf("NewCandidateAppender: %v", err)
	}
	if err := appender.AppendChunk([]Candidate{{ID: "new", Kind: "describe_entity", SubjectID: "e2"}}); err != nil {
		t.Fatalf("AppendChunk: %v", err)
	}
	appender.Abort()

	if _, err := os.Stat(appender.tmpPath); !os.IsNotExist(err) {
		t.Fatalf("expected tmp file removed after Abort, stat err=%v", err)
	}
	final, err := os.ReadFile(candidatesPath(dir))
	if err != nil {
		t.Fatalf("read prior after abort: %v", err)
	}
	if string(final) != string(priorData) {
		t.Fatalf("expected published file untouched by Abort, got %s", final)
	}
}

// TestCandidateAppender_AbortAfterCloseIsNoop pins down the invariant the
// cmd/grafel background enrichment worker relies on for its "always defer
// appender.Abort() as a panic safety net" fix (#5739 item d): once Close()
// has already committed the rename, a subsequent Abort() (e.g. from a
// deferred call that runs on every return path, success included) must be a
// harmless no-op — it must NOT remove the just-published candidates file or
// otherwise disturb it.
func TestCandidateAppender_AbortAfterCloseIsNoop(t *testing.T) {
	dir := t.TempDir()

	appender, err := NewCandidateAppender(dir)
	if err != nil {
		t.Fatalf("NewCandidateAppender: %v", err)
	}
	if err := appender.AppendChunk([]Candidate{{ID: "one", Kind: "describe_entity", SubjectID: "e1"}}); err != nil {
		t.Fatalf("AppendChunk: %v", err)
	}
	if err := appender.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	published, err := os.ReadFile(candidatesPath(dir))
	if err != nil {
		t.Fatalf("read published file after Close: %v", err)
	}

	// Simulate the deferred safety-net call that now always runs, even on the
	// success path, after Close() already committed.
	appender.Abort()

	after, err := os.ReadFile(candidatesPath(dir))
	if err != nil {
		t.Fatalf("read published file after post-Close Abort: %v", err)
	}
	if string(after) != string(published) {
		t.Fatalf("expected Abort-after-Close to be a no-op, published file changed:\nbefore=%s\nafter=%s", published, after)
	}
}

// TestCandidateAppender_UniqueTmpPath is the RED-before-fix test for the
// #5736 review follow-up: two CandidateAppenders opened concurrently for the
// SAME repo (e.g. the daemon's background worker plus a manually-triggered
// `grafel index`/`rebuild` on that repo) must not collide on the same fixed
// tmp filename — each must get its own unique tmp path so one process's
// writes/aborts can never truncate or delete the other's in-progress file.
func TestCandidateAppender_UniqueTmpPath(t *testing.T) {
	dir := t.TempDir()

	a1, err := NewCandidateAppender(dir)
	if err != nil {
		t.Fatalf("NewCandidateAppender (a1): %v", err)
	}
	defer a1.Abort()

	a2, err := NewCandidateAppender(dir)
	if err != nil {
		t.Fatalf("NewCandidateAppender (a2): %v", err)
	}
	defer a2.Abort()

	if a1.tmpPath == a2.tmpPath {
		t.Fatalf("expected unique tmp paths per appender, both got %q — concurrent appenders for the same repo would clobber each other", a1.tmpPath)
	}
	if _, err := os.Stat(a1.tmpPath); err != nil {
		t.Fatalf("a1 tmp file missing: %v", err)
	}
	if _, err := os.Stat(a2.tmpPath); err != nil {
		t.Fatalf("a2 tmp file missing: %v", err)
	}

	if err := a1.AppendChunk([]Candidate{{ID: "one", Kind: "describe_entity", SubjectID: "e1"}}); err != nil {
		t.Fatalf("a1 AppendChunk: %v", err)
	}
	if err := a1.Close(); err != nil {
		t.Fatalf("a1 Close: %v", err)
	}

	// a2 must be unaffected by a1's Close (independent files).
	if err := a2.AppendChunk([]Candidate{{ID: "two", Kind: "describe_entity", SubjectID: "e2"}}); err != nil {
		t.Fatalf("a2 AppendChunk after a1 Close: %v", err)
	}
}

// bytesReader avoids importing bytes just for a one-liner in test files that
// otherwise don't need it.
func bytesReader(b []byte) io.Reader {
	return &sliceReader{b: b}
}

type sliceReader struct {
	b   []byte
	pos int
}

func (s *sliceReader) Read(p []byte) (int, error) {
	if s.pos >= len(s.b) {
		return 0, io.EOF
	}
	n := copy(p, s.b[s.pos:])
	s.pos += n
	return n, nil
}
