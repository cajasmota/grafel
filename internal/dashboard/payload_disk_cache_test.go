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

// TestDiskPayloadCacheRejectsOversizedRecordWithoutReadingIt covers the read
// path bound: a file larger than any legal record must be rejected on its size
// alone, before the body is allocated.
func TestDiskPayloadCacheRejectsOversizedRecordWithoutReadingIt(t *testing.T) {
	cache := newDiskPayloadCache(t.TempDir())
	const (
		key     = "assessment::default"
		version = "graph-v1"
	)
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
	// Sparse: costs no disk, but any implementation that reads before it
	// validates the length pays for every byte in RAM.
	if err := f.Truncate(int64(diskPayloadMaxBody) + 4<<20); err != nil {
		t.Skipf("sparse file unsupported: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, hit := cache.Get(key, version); hit {
		t.Fatal("oversized cache record must be a miss")
	}
	runtime.ReadMemStats(&after)
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Fatalf("Get allocated %d bytes for an oversized record; it must reject on size before reading the body", grew)
	}
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
