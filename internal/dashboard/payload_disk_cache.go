package dashboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	// diskPayloadMagic also carries the record-format version. Bumping it
	// retires every record written by an older format: the magic check fails
	// first, so a stale record is an ordinary miss rather than a record
	// verified under the wrong rules. 02 widened the checksum from the body
	// alone to the whole frame.
	diskPayloadMagic   = "GFPAY02\n"
	diskPayloadMaxBody = 1 << 30
	// diskPayloadMaxField caps the key and the etag independently of the body.
	diskPayloadMaxField = 1 << 20
	diskPayloadLengths  = 4 + 4 + 8
	diskPayloadHeader   = len(diskPayloadMagic) + diskPayloadLengths + sha256.Size
	// diskPayloadMaxRecord is the largest record the framing can legally
	// describe, and Get rejects anything above it on its size alone. It is
	// belt-and-braces rather than the real bound: because it is defined as
	// exactly the largest framable record, any file big enough to trip it must
	// also declare lengths that the per-field caps or the size cross-check
	// reject, and neither of those allocates either. What actually keeps a
	// stray oversized file out of the daemon's heap is that the tail is sized
	// from the *validated* declared lengths, never from the file size.
	diskPayloadMaxRecord         = int64(diskPayloadHeader) + 2*diskPayloadMaxField + diskPayloadMaxBody
	diskPayloadVersionsPerGroup  = 8
	diskPayloadVariantsPerSource = 64
	// diskPayloadAsyncWrites bounds how many persistence goroutines may run at
	// once across the process. Each one retains an entry whose body can be
	// large, so the concurrent peak — not the total count — is what matters.
	diskPayloadAsyncWrites = 4
	// diskPayloadBytesPerGroup bounds the total on-disk footprint of one group.
	// The count caps alone cannot: the cache key mixes in client-supplied query
	// params, and the dashboard is loopback-reachable, so a page in the user's
	// browser can drive many distinct variants whose reads CORS blocks but
	// whose writes still land. Bytes, not file counts, are the honest bound on
	// what ends up on the user's disk.
	diskPayloadBytesPerGroup = 512 << 20
)

// diskPayloadWriteSlots bounds the SetAsync goroutine fan-out process-wide. A
// full semaphore drops the persist instead of waiting: the disk cache is a pure
// optimisation, a skipped write costs one later rebuild, and the alternative —
// blocking — would push that wait onto an HTTP handler.
var diskPayloadWriteSlots = make(chan struct{}, diskPayloadAsyncWrites)

// diskPayloadCache stores immutable pre-serialised HTTP responses. The source
// version is part of the path (not of the file itself), so memory-pressure
// eviction never destroys a reusable snapshot and a changed graph can never hit
// an old response. Corrupt or unknown files are treated as ordinary cache
// misses.
type diskPayloadCache struct {
	root string
	// bytesBudget is the per-group footprint ceiling; tests lower it.
	bytesBudget int64
	// asyncHook runs inside a persistence goroutine while it holds a write
	// slot. Nil outside tests.
	asyncHook func()
	writes    sync.Map // artifact path -> struct{}; coalesces concurrent persistence
	pruneMu   sync.Mutex
}

func (c *diskPayloadCache) SetAsync(key, sourceVersion string, entry *payloadEntry) {
	path, ok := c.path(key, sourceVersion)
	if !ok {
		return
	}
	if _, loaded := c.writes.LoadOrStore(path, struct{}{}); loaded {
		return
	}
	select {
	case diskPayloadWriteSlots <- struct{}{}:
	default:
		// Never block the request path for an optional write.
		c.writes.Delete(path)
		return
	}
	go func() {
		defer c.writes.Delete(path)
		defer func() { <-diskPayloadWriteSlots }()
		if c.asyncHook != nil {
			c.asyncHook()
		}
		_ = c.Set(key, sourceVersion, entry)
	}()
}

func newDiskPayloadCache(root string) *diskPayloadCache {
	if root == "" {
		return nil
	}
	return &diskPayloadCache{root: root, bytesBudget: diskPayloadBytesPerGroup}
}

func (c *diskPayloadCache) Get(key, sourceVersion string) (*payloadEntry, bool) {
	path, ok := c.path(key, sourceVersion)
	if !ok {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	// Stat the open handle, not the path: a stat-then-open pair leaves a window
	// in which the file grows past the ceiling between the two calls.
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	size := info.Size()
	if size < int64(diskPayloadHeader) || size > diskPayloadMaxRecord {
		return nil, false
	}

	header := make([]byte, diskPayloadHeader)
	if _, err = io.ReadFull(f, header); err != nil {
		return nil, false
	}
	if string(header[:len(diskPayloadMagic)]) != diskPayloadMagic {
		return nil, false
	}
	lengths := header[len(diskPayloadMagic) : len(diskPayloadMagic)+diskPayloadLengths]
	checksum := header[len(diskPayloadMagic)+diskPayloadLengths:]
	keyLen := int(binary.LittleEndian.Uint32(lengths[0:4]))
	etagLen := int(binary.LittleEndian.Uint32(lengths[4:8]))
	bodyLen := binary.LittleEndian.Uint64(lengths[8:16])
	if keyLen < 1 || keyLen > diskPayloadMaxField || etagLen < 1 || etagLen > diskPayloadMaxField || bodyLen > diskPayloadMaxBody {
		return nil, false
	}
	// Every declared length is validated, and cross-checked against the file
	// size, before the record tail is allocated.
	wantLen := uint64(diskPayloadHeader) + uint64(keyLen) + uint64(etagLen) + bodyLen
	if uint64(size) != wantLen {
		return nil, false
	}
	tail := make([]byte, keyLen+etagLen+int(bodyLen))
	if _, err = io.ReadFull(f, tail); err != nil {
		return nil, false
	}
	storedKey := tail[:keyLen]
	etag := tail[keyLen : keyLen+etagLen]
	body := tail[keyLen+etagLen:]
	if string(storedKey) != key {
		return nil, false
	}
	if !bytes.Equal(checksum, diskPayloadChecksum(lengths, storedKey, etag, body)) {
		return nil, false
	}
	return &payloadEntry{body: body, etag: string(etag), sourceVersion: sourceVersion}, true
}

// diskPayloadChecksum digests the whole frame — magic, declared lengths, key,
// etag and body — so a bit flip in the metadata is caught too. A body-only
// digest left the etag unverified, and a wrong ETag is served straight to a
// browser.
func diskPayloadChecksum(lengths, key, etag, body []byte) []byte {
	h := sha256.New()
	h.Write([]byte(diskPayloadMagic))
	h.Write(lengths)
	h.Write(key)
	h.Write(etag)
	h.Write(body)
	return h.Sum(nil)
}

func (c *diskPayloadCache) Set(key, sourceVersion string, entry *payloadEntry) error {
	if entry == nil || len(entry.body) > diskPayloadMaxBody || len(key) > diskPayloadMaxField || len(entry.etag) > diskPayloadMaxField {
		return nil
	}
	path, ok := c.path(key, sourceVersion)
	if !ok {
		return nil
	}
	// The immutability shortcut must test "a reader can use what is already
	// there", not merely "a file exists". The artifact path is a hash of the
	// key alone, so it does not change when the record format does: a bare
	// existence check would let a record this build can no longer read — one
	// written by an older format, or a corrupt one — block its own replacement
	// forever, and prune only runs after a successful rename, so nothing would
	// ever reclaim it. Verifying instead makes an unreadable record overwritable
	// and the cache self-healing. Reaching this at all means the in-memory
	// lookup missed, so the extra read is off the common path.
	if _, usable := c.Get(key, sourceVersion); usable {
		return nil // immutable artifact already exists and is readable
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("dashboard payload cache mkdir: %w", err)
	}

	lengths := make([]byte, 0, diskPayloadLengths)
	lengths = binary.LittleEndian.AppendUint32(lengths, uint32(len(key)))
	lengths = binary.LittleEndian.AppendUint32(lengths, uint32(len(entry.etag)))
	lengths = binary.LittleEndian.AppendUint64(lengths, uint64(len(entry.body)))
	sum := diskPayloadChecksum(lengths, []byte(key), []byte(entry.etag), entry.body)

	tmp, err := os.CreateTemp(filepath.Dir(path), ".payload-*.tmp")
	if err != nil {
		// MkdirAll above and this CreateTemp are deliberately outside pruneMu,
		// so a concurrent prune can drop the target directory in between. The
		// byte-budget sweep no longer removes directories, but the version
		// count cap still does — pruneOldCacheEntries RemoveAll's whole
		// source-version directories, which does not even require them to be
		// empty. Recreate and retry once rather than lose the write; SetAsync
		// discards the error, so a lost write here would be silent.
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			return fmt.Errorf("dashboard payload cache mkdir: %w", mkErr)
		}
		tmp, err = os.CreateTemp(filepath.Dir(path), ".payload-*.tmp")
	}
	if err != nil {
		return fmt.Errorf("dashboard payload cache temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	header := make([]byte, 0, diskPayloadHeader)
	header = append(header, diskPayloadMagic...)
	header = append(header, lengths...)
	header = append(header, sum...)
	for _, chunk := range [][]byte{header, []byte(key), []byte(entry.etag), entry.body} {
		if _, err = tmp.Write(chunk); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("dashboard payload cache write: %w", err)
		}
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("dashboard payload cache sync: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("dashboard payload cache close: %w", err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return nil // another request won the immutable write race
		}
		return fmt.Errorf("dashboard payload cache rename: %w", err)
	}
	c.prune(path)
	return nil
}

func (c *diskPayloadCache) prune(currentPath string) {
	c.pruneMu.Lock()
	defer c.pruneMu.Unlock()

	sourceDir := filepath.Dir(currentPath)
	pruneOldCacheEntries(sourceDir, diskPayloadVariantsPerSource, currentPath, false)
	groupDir := filepath.Dir(sourceDir)
	pruneOldCacheEntries(groupDir, diskPayloadVersionsPerGroup, sourceDir, true)
	// The count caps are the cheap first pass; the byte budget is the bound
	// that actually holds once variants get large.
	pruneCacheBytes(groupDir, c.bytesBudget, currentPath)
}

type cachePathInfo struct {
	path    string
	modTime int64
	size    int64
}

func pruneOldCacheEntries(dir string, keep int, preserve string, directories bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	paths := make([]cachePathInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() != directories {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if path == preserve {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		paths = append(paths, cachePathInfo{path: path, modTime: info.ModTime().UnixNano()})
	}
	// preserve is an additional retained entry when it exists.
	removeCount := len(paths) + 1 - keep
	if removeCount <= 0 {
		return
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i].modTime < paths[j].modTime })
	for i := 0; i < removeCount && i < len(paths); i++ {
		_ = os.RemoveAll(paths[i].path)
	}
}

// pruneCacheBytes evicts records, oldest mod time first, until the group's
// total footprint fits budget. It uses the same ordering as
// pruneOldCacheEntries and honours the same preserve contract: the record the
// current request just wrote is never a candidate, so a hit is available
// immediately after a Set even when that one record exceeds the budget alone.
func pruneCacheBytes(groupDir string, budget int64, preserve string) {
	if budget <= 0 {
		return
	}
	sourceDirs, err := os.ReadDir(groupDir)
	if err != nil {
		return
	}
	var total int64
	candidates := make([]cachePathInfo, 0, len(sourceDirs)*8)
	for _, dir := range sourceDirs {
		if !dir.IsDir() {
			continue
		}
		sourceDir := filepath.Join(groupDir, dir.Name())
		entries, dirErr := os.ReadDir(sourceDir)
		if dirErr != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				continue
			}
			// A .tmp file belongs to a Set still in flight. It is deliberately
			// excluded from the total as well as from the candidates: counting
			// bytes that cannot be evicted would let a burst of concurrent
			// writes push the total over budget with nothing attributable to
			// reclaim, and the loop would then evict every record in the group
			// and still exit over budget. The overshoot this concedes is
			// bounded by diskPayloadAsyncWrites in-flight writes and is
			// reclaimed by the next sweep, once those temps become records.
			if !strings.HasSuffix(entry.Name(), ".gpc") {
				continue
			}
			total += info.Size()
			path := filepath.Join(sourceDir, entry.Name())
			if path == preserve {
				continue
			}
			candidates = append(candidates, cachePathInfo{path: path, modTime: info.ModTime().UnixNano(), size: info.Size()})
		}
	}
	if total <= budget {
		return
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modTime < candidates[j].modTime })
	for _, candidate := range candidates {
		if total <= budget {
			break
		}
		if os.Remove(candidate.path) == nil {
			total -= candidate.size
		}
	}
	// Directories this sweep empties are deliberately left in place. Removing
	// them would race a concurrent Set that has completed its MkdirAll and not
	// yet created its temp file — os.Remove failing on a non-empty directory is
	// not the protection it looks like, because in that window the directory
	// *is* empty. The only thing an empty directory costs is a slot in the
	// diskPayloadVersionsPerGroup count cap, which already evicts oldest-first
	// and will reclaim it. An inode is a cheaper price than a lost write.
}

func (c *diskPayloadCache) path(key, sourceVersion string) (string, bool) {
	group, _, ok := strings.Cut(key, "::")
	if !ok || group == "" || sourceVersion == "" {
		return "", false
	}
	return filepath.Join(c.root, shortPayloadHash(group), shortPayloadHash(sourceVersion), shortPayloadHash(key)+".gpc"), true
}

func shortPayloadHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:16])
}
