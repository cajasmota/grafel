package dashboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	diskPayloadMagic             = "GFPAY01\n"
	diskPayloadMaxBody           = 1 << 30
	diskPayloadHeader            = len(diskPayloadMagic) + 4 + 4 + 8 + sha256.Size
	diskPayloadVersionsPerGroup  = 8
	diskPayloadVariantsPerSource = 64
)

// diskPayloadCache stores immutable pre-serialised HTTP responses. The source
// version is part of the path and the file header, so memory-pressure eviction
// never destroys a reusable snapshot and a changed graph can never hit an old
// response. Corrupt or unknown files are treated as ordinary cache misses.
type diskPayloadCache struct {
	root    string
	writes  sync.Map // artifact path -> struct{}; coalesces concurrent persistence
	pruneMu sync.Mutex
}

func (c *diskPayloadCache) SetAsync(key, sourceVersion string, entry *payloadEntry) {
	path, ok := c.path(key, sourceVersion)
	if !ok {
		return
	}
	if _, loaded := c.writes.LoadOrStore(path, struct{}{}); loaded {
		return
	}
	go func() {
		defer c.writes.Delete(path)
		_ = c.Set(key, sourceVersion, entry)
	}()
}

func newDiskPayloadCache(root string) *diskPayloadCache {
	if root == "" {
		return nil
	}
	return &diskPayloadCache{root: root}
}

func (c *diskPayloadCache) Get(key, sourceVersion string) (*payloadEntry, bool) {
	path, ok := c.path(key, sourceVersion)
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) < diskPayloadHeader {
		return nil, false
	}
	if string(data[:len(diskPayloadMagic)]) != diskPayloadMagic {
		return nil, false
	}
	off := len(diskPayloadMagic)
	keyLen := int(binary.LittleEndian.Uint32(data[off : off+4]))
	off += 4
	etagLen := int(binary.LittleEndian.Uint32(data[off : off+4]))
	off += 4
	bodyLen := binary.LittleEndian.Uint64(data[off : off+8])
	off += 8
	checksum := data[off : off+sha256.Size]
	off += sha256.Size
	if keyLen < 1 || keyLen > 1<<20 || etagLen < 1 || etagLen > 1<<20 || bodyLen > diskPayloadMaxBody {
		return nil, false
	}
	wantLen := uint64(off) + uint64(keyLen) + uint64(etagLen) + bodyLen
	if uint64(len(data)) != wantLen {
		return nil, false
	}
	storedKey := string(data[off : off+keyLen])
	off += keyLen
	etag := string(data[off : off+etagLen])
	off += etagLen
	body := data[off:]
	if storedKey != key {
		return nil, false
	}
	sum := sha256.Sum256(body)
	if !bytes.Equal(checksum, sum[:]) {
		return nil, false
	}
	return &payloadEntry{body: body, etag: etag, sourceVersion: sourceVersion}, true
}

func (c *diskPayloadCache) Set(key, sourceVersion string, entry *payloadEntry) error {
	if entry == nil || len(entry.body) > diskPayloadMaxBody || len(key) > 1<<20 || len(entry.etag) > 1<<20 {
		return nil
	}
	path, ok := c.path(key, sourceVersion)
	if !ok {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil // immutable artifact already exists
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("dashboard payload cache mkdir: %w", err)
	}

	sum := sha256.Sum256(entry.body)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".payload-*.tmp")
	if err != nil {
		return fmt.Errorf("dashboard payload cache temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	header := make([]byte, 0, diskPayloadHeader)
	header = append(header, diskPayloadMagic...)
	header = binary.LittleEndian.AppendUint32(header, uint32(len(key)))
	header = binary.LittleEndian.AppendUint32(header, uint32(len(entry.etag)))
	header = binary.LittleEndian.AppendUint64(header, uint64(len(entry.body)))
	header = append(header, sum[:]...)
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
}

type cachePathInfo struct {
	path    string
	modTime int64
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
