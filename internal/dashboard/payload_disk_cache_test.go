package dashboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph/descriptions"
	"github.com/cajasmota/grafel/internal/graph/flows"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
)

func TestDashboardRepoArtifactsTrackSegmentGenerationAndSidecars(t *testing.T) {
	stateDir := t.TempDir()
	digest := func() string {
		h := sha256.New()
		hashDashboardRepoArtifacts(h, stateDir)
		return string(h.Sum(nil))
	}

	writeDashboardSegmentSetFixture(t, stateDir, 5, time.Now().Add(-time.Hour))
	gen5 := digest()
	writeDashboardSegmentSetFixture(t, stateDir, 6, time.Now().Add(-time.Hour))
	gen6 := digest()
	if gen6 == gen5 {
		t.Fatal("segment generation change did not invalidate the payload source version")
	}

	if err := os.WriteFile(descriptions.Path(stateDir), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	withDescriptions := digest()
	if withDescriptions == gen6 {
		t.Fatal("description sidecar change did not invalidate the payload source version")
	}

	if err := os.WriteFile(flows.Path(stateDir), []byte(`{"version":1,"flows":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if withFlows := digest(); withFlows == withDescriptions {
		t.Fatal("flow sidecar change did not invalidate the payload source version")
	}
}

func BenchmarkDiskPayloadCacheGet8MiB(b *testing.B) {
	cache := newDiskPayloadCache(b.TempDir())
	const (
		key     = "v2:assessment::default"
		version = "graph-v1"
	)
	body := bytes.Repeat([]byte("grafel-cache"), (8<<20)/len("grafel-cache"))
	entry := &payloadEntry{body: body, etag: `"etag-1"`, sourceVersion: version}
	if err := cache.Set(key, version, entry); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, ok := cache.Get(key, version)
		if !ok || len(got.body) != len(body) {
			b.Fatal("disk payload cache miss")
		}
	}
}

func TestGraphPayloadCacheRestoresDiskSnapshot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dashboard-cache")
	first := newGraphPayloadCacheAt(root)
	key := "v2:assessment::default"
	version := "graph-v1"
	entry := &payloadEntry{body: []byte(`{"ok":true}`), etag: `"etag-1"`, sourceVersion: version}
	if err := first.disk.Set(key, version, entry); err != nil {
		t.Fatal(err)
	}

	second := newGraphPayloadCacheAt(root)
	entry, ok := second.Get(key, version)
	if !ok {
		t.Fatal("expected disk-backed payload hit after in-memory cache restart")
	}
	if got := string(entry.body); got != `{"ok":true}` {
		t.Fatalf("body = %q", got)
	}
	if entry.etag != `"etag-1"` || entry.sourceVersion != version {
		t.Fatalf("metadata not restored: %+v", entry)
	}
}

func TestGraphPayloadCacheRejectsStaleSourceVersion(t *testing.T) {
	cache := newGraphPayloadCacheAt(t.TempDir())
	t.Cleanup(func() { waitForDiskPayloadWrites(t, cache.disk) })
	key := "assessment::default"
	cache.Set(key, []byte(`{"version":1}`), `"etag-1"`, "graph-v1")

	if _, ok := cache.Get(key, "graph-v2"); ok {
		t.Fatal("stale in-memory or disk payload was served for a changed graph")
	}
}

func waitForDiskPayloadWrites(t *testing.T, cache *diskPayloadCache) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		pending := false
		cache.writes.Range(func(_, _ any) bool {
			pending = true
			return false
		})
		if !pending {
			return
		}
		if time.Now().After(deadline) {
			t.Error("timed out waiting for asynchronous payload-cache write")
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestGraphPayloadCacheInvalidatesV1AndV2MemoryEntries(t *testing.T) {
	cache := newGraphPayloadCacheAt(t.TempDir())
	cache.Set("assessment::default", []byte("v1"), `"v1"`)
	cache.Set("v2:assessment::default", []byte("v2"), `"v2"`)
	cache.Set("v2:other::default", []byte("other"), `"other"`)

	cache.InvalidateGroup("assessment")
	if _, ok := cache.Get("assessment::default"); ok {
		t.Fatal("v1 entry survived group invalidation")
	}
	if _, ok := cache.Get("v2:assessment::default"); ok {
		t.Fatal("v2 entry survived group invalidation")
	}
	if _, ok := cache.Get("v2:other::default"); !ok {
		t.Fatal("unrelated group was invalidated")
	}
}

func TestGraphPayloadCacheTreatsCorruptionAsMiss(t *testing.T) {
	root := t.TempDir()
	cache := newGraphPayloadCacheAt(root)
	key := "assessment::default"
	version := "graph-v1"
	entry := &payloadEntry{body: []byte(`{"ok":true}`), etag: `"etag-1"`, sourceVersion: version}
	if err := cache.disk.Set(key, version, entry); err != nil {
		t.Fatal(err)
	}

	path, ok := cache.disk.path(key, version)
	if !ok {
		t.Fatal("expected cache path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	restarted := newGraphPayloadCacheAt(root)
	if _, ok := restarted.Get(key, version); ok {
		t.Fatal("corrupt disk payload must degrade to a cache miss")
	}
}

func TestDiskPayloadCachePrunesOldSourceVersions(t *testing.T) {
	cache := newDiskPayloadCache(t.TempDir())
	key := "assessment::default"
	entry := &payloadEntry{body: []byte(`{"ok":true}`), etag: `"etag"`}
	for i := 0; i < diskPayloadVersionsPerGroup+2; i++ {
		version := "graph-v" + string(rune('a'+i))
		if err := cache.Set(key, version, entry); err != nil {
			t.Fatal(err)
		}
	}
	groupDir := filepath.Join(cache.root, shortPayloadHash("assessment"))
	dirs, err := os.ReadDir(groupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != diskPayloadVersionsPerGroup {
		t.Fatalf("retained source versions = %d, want %d", len(dirs), diskPayloadVersionsPerGroup)
	}
}

func TestV2GraphRestoresDiskPayloadBeforeLoadingGraph(t *testing.T) {
	// On Windows the process temp directory can live under the real user home,
	// which the registry write guard correctly rejects even after HOME is
	// redirected. Point t.TempDir at this worktree before isolating the test.
	workDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", workDir)
	t.Setenv("TEMP", workDir)
	t.Setenv("TMP", workDir)
	root := testsupport.IsolateHome(t)
	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := daemon.StateDirForRepoRef(repoPath, "")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Deliberately invalid graph bytes: the request can only succeed if the
	// persisted HTTP payload is served before graph materialisation.
	if err := os.WriteFile(filepath.Join(stateDir, "graph.fb"), []byte("not-a-graph"), 0o644); err != nil {
		t.Fatal(err)
	}

	const group = "cold-disk-payload"
	cfgPath, err := registry.ConfigPathFor(group)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SaveGroupConfig(cfgPath, &registry.GroupConfig{
		Name:  group,
		Repos: []registry.Repo{{Slug: "repo", Path: repoPath}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.AddGroup(group, cfgPath); err != nil {
		t.Fatal(err)
	}
	version, err := dashboardSourceVersion(group, "")
	if err != nil {
		t.Fatal(err)
	}
	key := "v2:" + payloadCacheKey(group, "", "", "", false, false, "") + ":lod="
	body := []byte(`{"ok":true,"data":{"nodes":[]}}` + "\n")
	first := NewGraphCache(0)
	entry := &payloadEntry{body: body, etag: `"cold-etag"`, sourceVersion: version}
	if err := first.Payloads.disk.Set(key, version, entry); err != nil {
		t.Fatal(err)
	}

	restarted := NewGraphCache(0)
	server := &Server{graphs: restarted}
	req := httptest.NewRequest(http.MethodGet, "/api/v2/graph/"+group, nil)
	req.SetPathValue("group", group)
	rec := httptest.NewRecorder()
	server.handleV2Graph(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != string(body) {
		t.Fatalf("cold disk response = status %d body %q", rec.Code, rec.Body.String())
	}
	if len(restarted.entries) != 0 {
		t.Fatal("graph was materialised even though a valid disk payload existed")
	}
}

// --- hardening follow-ups (#5941) --------------------------------------------

// writeDiskPayloadRecord frames a record exactly as Set does, but with a
// caller-chosen magic, so tests can build well-formed records that differ from
// the current format in one specific way.
func writeDiskPayloadRecord(t *testing.T, path, magic, key, etag string, body []byte, checksum []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	record := make([]byte, 0, diskPayloadHeader+len(key)+len(etag)+len(body))
	record = append(record, magic...)
	record = binary.LittleEndian.AppendUint32(record, uint32(len(key)))
	record = binary.LittleEndian.AppendUint32(record, uint32(len(etag)))
	record = binary.LittleEndian.AppendUint64(record, uint64(len(body)))
	record = append(record, checksum...)
	record = append(record, key...)
	record = append(record, etag...)
	record = append(record, body...)
	if err := os.WriteFile(path, record, 0o644); err != nil {
		t.Fatal(err)
	}
}

// frameChecksum digests a record the way Get will, so a fixture can be made
// checksum-valid and differ from a real record only in its magic.
func frameChecksum(key, etag string, body []byte) []byte {
	lengths := make([]byte, 0, diskPayloadLengths)
	lengths = binary.LittleEndian.AppendUint32(lengths, uint32(len(key)))
	lengths = binary.LittleEndian.AppendUint32(lengths, uint32(len(etag)))
	lengths = binary.LittleEndian.AppendUint64(lengths, uint64(len(body)))
	return diskPayloadChecksum(lengths, []byte(key), []byte(etag), body)
}

// TestDiskPayloadCacheRejectsRecordWhoseSizeContradictsItsHeader covers the
// bound that actually protects the heap: the record tail is sized from the
// validated declared lengths and cross-checked against the file size, so a file
// can never be read merely because it is on disk.
//
// The diskPayloadMaxRecord ceiling is deliberately NOT what this asserts. That
// ceiling is defined as the largest framable record, so it is unreachable as a
// distinct rejection — every file large enough to trip it also declares lengths
// that the checks below reject, without allocating either.
func TestDiskPayloadCacheRejectsRecordWhoseSizeContradictsItsHeader(t *testing.T) {
	const (
		key     = "assessment::default"
		version = "graph-v1"
		etag    = `"etag-1"`
	)
	body := []byte(`{"ok":true}`)

	t.Run("trailing bytes beyond the declared frame", func(t *testing.T) {
		cache := newDiskPayloadCache(t.TempDir())
		path, ok := cache.path(key, version)
		if !ok {
			t.Fatal("expected cache path")
		}
		writeDiskPayloadRecord(t, path, diskPayloadMagic, key, etag, body, frameChecksum(key, etag, body))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		// Every declared length is individually legal and the digest over the
		// declared frame still verifies; only the file size disagrees.
		if _, err := f.Write(bytes.Repeat([]byte("\x00"), 4096)); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if _, hit := cache.Get(key, version); hit {
			t.Fatal("a record whose file size contradicts its declared lengths must be a miss")
		}
	})

	t.Run("declared body length above the cap", func(t *testing.T) {
		cache := newDiskPayloadCache(t.TempDir())
		path, ok := cache.path(key, version)
		if !ok {
			t.Fatal("expected cache path")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		bodyLen := uint64(diskPayloadMaxBody) + 1
		header := make([]byte, 0, diskPayloadHeader)
		header = append(header, diskPayloadMagic...)
		header = binary.LittleEndian.AppendUint32(header, uint32(len(key)))
		header = binary.LittleEndian.AppendUint32(header, uint32(len(etag)))
		header = binary.LittleEndian.AppendUint64(header, bodyLen)
		header = append(header, make([]byte, sha256.Size)...)
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(header); err != nil {
			t.Fatal(err)
		}
		// Sized sparsely so the file size agrees exactly with the declared
		// lengths, and so it stays under diskPayloadMaxRecord. The per-field
		// body cap is therefore the only check that can reject this record —
		// without it, Get would allocate the full declared gigabyte.
		wantLen := int64(diskPayloadHeader) + int64(len(key)) + int64(len(etag)) + int64(bodyLen)
		if wantLen > diskPayloadMaxRecord {
			t.Fatal("fixture must stay under the record ceiling to isolate the body cap")
		}
		if err := f.Truncate(wantLen); err != nil {
			t.Skipf("sparse file unsupported: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		// The miss alone cannot see this guard: without it the record is still
		// rejected, but only after the declared gigabyte has been allocated and
		// read. The allocation is the observable property, so assert on it.
		// TotalAlloc is process-cumulative, so the threshold is deliberately
		// wide — background goroutines contribute kilobytes here, against a
		// signal of 1 GiB.
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		if _, hit := cache.Get(key, version); hit {
			t.Fatal("a declared body length above the cap must be a miss")
		}
		runtime.ReadMemStats(&after)
		if grew := after.TotalAlloc - before.TotalAlloc; grew > 64<<20 {
			t.Fatalf("Get allocated %d bytes; the tail must be rejected on its declared length, not materialised", grew)
		}
	})

	// Documents the diskPayloadMaxRecord early-out. Unlike the subtests above
	// this one is knowingly not a mutation-killer: deleting the ceiling still
	// leaves the size-vs-wantLen cross-check to reject the file, and no valid
	// set of declared lengths can exceed the ceiling in the first place. It is
	// here to pin the behaviour, not to claim the guard is load-bearing.
	t.Run("file larger than any framable record", func(t *testing.T) {
		cache := newDiskPayloadCache(t.TempDir())
		path, ok := cache.path(key, version)
		if !ok {
			t.Fatal("expected cache path")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(diskPayloadMagic)); err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(diskPayloadMaxRecord + 1); err != nil {
			t.Skipf("sparse file unsupported: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if _, hit := cache.Get(key, version); hit {
			t.Fatal("a file larger than any framable record must be a miss")
		}
	})
}

// TestDiskPayloadCacheEnforcesGroupByteBudget covers the total-footprint bound
// that the per-directory count caps cannot express.
func TestDiskPayloadCacheEnforcesGroupByteBudget(t *testing.T) {
	cache := newDiskPayloadCache(t.TempDir())
	cache.bytesBudget = 3000
	const version = "graph-v1"
	body := bytes.Repeat([]byte("x"), 1000)

	paths := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		key := "assessment::variant-" + string(rune('a'+i))
		entry := &payloadEntry{body: body, etag: `"etag"`, sourceVersion: version}
		if err := cache.Set(key, version, entry); err != nil {
			t.Fatal(err)
		}
		path, ok := cache.path(key, version)
		if !ok {
			t.Fatal("expected cache path")
		}
		// Deterministic oldest-first ordering independent of filesystem
		// timestamp granularity.
		stamp := time.Now().Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}

	groupDir := filepath.Join(cache.root, shortPayloadHash("assessment"))
	var total int64
	survivors := map[string]bool{}
	sourceDirs, err := os.ReadDir(groupDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range sourceDirs {
		entries, dirErr := os.ReadDir(filepath.Join(groupDir, dir.Name()))
		if dirErr != nil {
			t.Fatal(dirErr)
		}
		for _, entry := range entries {
			info, infoErr := entry.Info()
			if infoErr != nil {
				t.Fatal(infoErr)
			}
			total += info.Size()
			survivors[filepath.Join(groupDir, dir.Name(), entry.Name())] = true
		}
	}
	if total > cache.bytesBudget {
		t.Fatalf("group footprint = %d bytes, budget %d", total, cache.bytesBudget)
	}
	if !survivors[paths[len(paths)-1]] {
		t.Fatal("the record written by the current request was evicted")
	}
	if survivors[paths[0]] {
		t.Fatal("byte-budget eviction must drop the oldest record first")
	}
}

// TestDiskPayloadCacheBoundsAsyncWriteFanOut covers the concurrency bound on
// SetAsync: bursts must neither exceed the slot count nor block the caller.
func TestDiskPayloadCacheBoundsAsyncWriteFanOut(t *testing.T) {
	cache := newDiskPayloadCache(t.TempDir())
	var inFlight, peak atomic.Int32
	cache.asyncHook = func() {
		now := inFlight.Add(1)
		for {
			high := peak.Load()
			if now <= high || peak.CompareAndSwap(high, now) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		inFlight.Add(-1)
	}

	const version = "graph-v1"
	entry := &payloadEntry{body: []byte(`{"ok":true}`), etag: `"etag"`, sourceVersion: version}
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cache.SetAsync(fmt.Sprintf("assessment::variant-%d", i), version, entry)
		}(i)
	}
	wg.Wait()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("SetAsync blocked its caller for %v; persistence must never stall a request", elapsed)
	}
	waitForDiskPayloadWrites(t, cache)
	if got := peak.Load(); got > diskPayloadAsyncWrites {
		t.Fatalf("peak concurrent disk writes = %d, bound %d", got, diskPayloadAsyncWrites)
	}
	if peak.Load() == 0 {
		t.Fatal("no asynchronous write ran")
	}
}

// TestDiskPayloadCacheRejectsLegacyRecordFormat covers the record-format bump:
// the pre-bump framing must read as an ordinary miss, never as a hit and never
// as a panic.
func TestDiskPayloadCacheRejectsLegacyRecordFormat(t *testing.T) {
	cache := newDiskPayloadCache(t.TempDir())
	const (
		key     = "assessment::default"
		version = "graph-v1"
		etag    = `"etag-1"`
	)
	body := []byte(`{"ok":true}`)
	path, ok := cache.path(key, version)
	if !ok {
		t.Fatal("expected cache path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Byte-for-byte the record layout shipped in #5941: magic "GFPAY01\n" and
	// a checksum over the body alone.
	sum := sha256.Sum256(body)
	record := make([]byte, 0, 64)
	record = append(record, "GFPAY01\n"...)
	record = binary.LittleEndian.AppendUint32(record, uint32(len(key)))
	record = binary.LittleEndian.AppendUint32(record, uint32(len(etag)))
	record = binary.LittleEndian.AppendUint64(record, uint64(len(body)))
	record = append(record, sum[:]...)
	record = append(record, key...)
	record = append(record, etag...)
	record = append(record, body...)
	if err := os.WriteFile(path, record, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, hit := cache.Get(key, version); hit {
		t.Fatal("a record in the superseded on-disk format must be a clean miss")
	}
}

// TestDiskPayloadCacheDetectsEtagCorruption covers the widened checksum: the
// body-only digest could not see a flipped etag byte.
func TestDiskPayloadCacheDetectsEtagCorruption(t *testing.T) {
	cache := newDiskPayloadCache(t.TempDir())
	const (
		key     = "assessment::default"
		version = "graph-v1"
		etag    = `"etag-1"`
	)
	entry := &payloadEntry{body: []byte(`{"ok":true}`), etag: etag, sourceVersion: version}
	if err := cache.Set(key, version, entry); err != nil {
		t.Fatal(err)
	}
	path, ok := cache.path(key, version)
	if !ok {
		t.Fatal("expected cache path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	etagOff := diskPayloadHeader + len(key)
	if !bytes.Equal(data[etagOff:etagOff+len(etag)], []byte(etag)) {
		t.Fatalf("etag not at the expected offset: %q", data[etagOff:etagOff+len(etag)])
	}
	data[etagOff+2] ^= 0x20
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if got, hit := cache.Get(key, version); hit {
		t.Fatalf("a corrupted etag must fail verification, got etag %q", got.etag)
	}
}

// TestDiskPayloadCacheReplacesUnreadableRecord covers reclaimability, which is
// a different property from the clean miss asserted above: the artifact path is
// a hash of the key alone and does not change with the record format, so a
// record this build cannot read must not be able to block its own replacement.
// Without this, every record written before the format bump would wedge its key
// permanently — Get misses on the magic, Set used to bail on mere existence,
// and prune only runs after a successful rename.
func TestDiskPayloadCacheReplacesUnreadableRecord(t *testing.T) {
	const (
		key     = "assessment::default"
		version = "graph-v1"
		etag    = `"etag-1"`
	)
	body := []byte(`{"ok":true}`)

	cases := map[string]func(t *testing.T, path string){
		"superseded format": func(t *testing.T, path string) {
			sum := sha256.Sum256(body) // the body-only digest of GFPAY01
			writeDiskPayloadRecord(t, path, "GFPAY01\n", key, etag, body, sum[:])
		},
		"corrupt checksum": func(t *testing.T, path string) {
			bad := frameChecksum(key, etag, body)
			bad[0] ^= 0xff
			writeDiskPayloadRecord(t, path, diskPayloadMagic, key, etag, body, bad)
		},
	}
	for name, seed := range cases {
		t.Run(name, func(t *testing.T) {
			cache := newDiskPayloadCache(t.TempDir())
			path, ok := cache.path(key, version)
			if !ok {
				t.Fatal("expected cache path")
			}
			seed(t, path)
			if _, hit := cache.Get(key, version); hit {
				t.Fatal("unreadable record must be a miss")
			}

			fresh := []byte(`{"ok":true,"generation":2}`)
			if err := cache.Set(key, version, &payloadEntry{body: fresh, etag: `"etag-2"`, sourceVersion: version}); err != nil {
				t.Fatal(err)
			}
			entry, hit := cache.Get(key, version)
			if !hit {
				t.Fatal("an unreadable record blocked its own replacement; the key is wedged forever")
			}
			if string(entry.body) != string(fresh) || entry.etag != `"etag-2"` {
				t.Fatalf("stale record survived: body %q etag %q", entry.body, entry.etag)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data[:len(diskPayloadMagic)]) != diskPayloadMagic {
				t.Fatalf("on-disk magic = %q, want the current format", data[:len(diskPayloadMagic)])
			}
		})
	}
}

// TestDiskPayloadCacheRejectsForeignMagic isolates the magic comparison. The
// fixture is well-formed in every other respect — its digest is the current
// frame checksum, its lengths and size agree, its key matches — so the magic
// check is the only thing that can reject it.
func TestDiskPayloadCacheRejectsForeignMagic(t *testing.T) {
	cache := newDiskPayloadCache(t.TempDir())
	const (
		key     = "assessment::default"
		version = "graph-v1"
		etag    = `"etag-1"`
	)
	body := []byte(`{"ok":true}`)
	path, ok := cache.path(key, version)
	if !ok {
		t.Fatal("expected cache path")
	}
	if len("GFPAY01\n") != len(diskPayloadMagic) {
		t.Fatal("fixture assumes a fixed-width magic")
	}
	writeDiskPayloadRecord(t, path, "GFPAY01\n", key, etag, body, frameChecksum(key, etag, body))

	if _, hit := cache.Get(key, version); hit {
		t.Fatal("a record carrying a foreign magic must be rejected by the magic check alone")
	}
}

// TestDiskPayloadCacheByteSweepLeavesDirectories pins the decision that the
// byte-budget sweep evicts files but never removes the directories it empties.
// Removing them would race a concurrent Set sitting between its MkdirAll and
// its CreateTemp; an empty directory only costs a slot in the version count
// cap, which reclaims it oldest-first.
func TestDiskPayloadCacheByteSweepLeavesDirectories(t *testing.T) {
	cache := newDiskPayloadCache(t.TempDir())
	cache.bytesBudget = 1
	body := bytes.Repeat([]byte("x"), 512)
	for i := 0; i < 3; i++ {
		version := fmt.Sprintf("graph-v%d", i)
		key := fmt.Sprintf("assessment::k%d", i)
		if err := cache.Set(key, version, &payloadEntry{body: body, etag: `"etag"`, sourceVersion: version}); err != nil {
			t.Fatal(err)
		}
	}

	groupDir := filepath.Join(cache.root, shortPayloadHash("assessment"))
	dirs, err := os.ReadDir(groupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 3 {
		t.Fatalf("source-version directories = %d, want 3 left in place after the sweep", len(dirs))
	}
	// The budget still held: only the newest record survives.
	var files int
	for _, dir := range dirs {
		entries, dirErr := os.ReadDir(filepath.Join(groupDir, dir.Name()))
		if dirErr != nil {
			t.Fatal(dirErr)
		}
		files += len(entries)
	}
	if files != 1 {
		t.Fatalf("records surviving the byte budget = %d, want 1", files)
	}
}
