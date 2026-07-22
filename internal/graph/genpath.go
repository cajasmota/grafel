// Package graph — genpath.go implements the generation-file + pointer layout
// for the on-disk FlatBuffers graph (issue #5891, "Approach A").
//
// # Why this exists
//
// Historically every reindex overwrote a single fixed file, <dir>/graph.fb,
// via a tmp+rename. On Windows renaming OVER a file that another process still
// has memory-mapped fails with ERROR_USER_MAPPED_FILE — precisely the case for
// grafel's serve/MCP zero-copy reader, which keeps graph.fb mmap'd while agents
// query it. That hazard forced the mmap serve path OFF by default on Windows.
//
// The generation layout removes the rename-over-a-mapped-file entirely:
//
//   - Each write emits a BRAND-NEW file graph.<gen>.fb (gen = monotonically
//     increasing integer) and then atomically writes a tiny `current` pointer
//     file naming the active generation. No existing (possibly mapped) file is
//     ever renamed over.
//   - Readers resolve the active file through CurrentGraphPath: read `current`
//     → return <dir>/graph.<gen>.fb; if the pointer is absent (a legacy repo
//     that has not been reindexed since the migration) fall back to the flat
//     <dir>/graph.fb. This makes the migration LAZY: existing flat-file repos
//     keep working untouched and pick up the gen layout only on their next
//     naturally-triggered reindex. There is NO forced reindex and NO
//     fbversion bump.
//
// The `.fb` suffix is preserved on gen files (graph.<gen>.fb) so existing
// strings.HasSuffix(path, ".fb") checks keep working unchanged.
package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// currentPointerName is the tiny pointer file naming the active generation.
const currentPointerName = "current"

// CurrentPointerName is the exported basename of the `current` generation
// pointer file. Callers that scan a directory tree for state dirs (e.g. the
// cross-repo link-pass discovery walk in internal/links) use it as the cheapest
// marker of a state dir whose active graph is a segment-set (which has no flat
// graph.fb to match on), then confirm via CurrentGraphDescriptor.
const CurrentPointerName = currentPointerName

// flatGraphName is the legacy fixed graph filename used before the gen layout
// and still used as the resolver's fallback for un-migrated repos.
const flatGraphName = "graph.fb"

// genFileRe matches a generation graph file: graph.<digits>.fb.
var genFileRe = regexp.MustCompile(`^graph\.(\d+)\.fb$`)

// genDirRe matches a MULTI-SEGMENT generation directory: graph.<digits> (no
// .fb suffix). Under the #5890/#5901 segmented layout a graph too large for one
// segment is written as graph.<gen>/seg-NNNN.fb + manifest.json, and the
// `current` pointer may name that DIR (or its manifest) — so `current` is no
// longer guaranteed to name a *.fb file. This regex is the segment-set
// counterpart to genFileRe, with the same path-traversal hardening (a pointer
// value that is not exactly graph.<digits> can never resolve to a gen dir).
var genDirRe = regexp.MustCompile(`^graph\.(\d+)$`)

// GenFileName renders the on-disk filename for a given generation.
func GenFileName(gen uint64) string {
	return fmt.Sprintf("graph.%d.fb", gen)
}

// GenDirName renders the on-disk MULTI-SEGMENT generation directory name for a
// given generation: graph.<gen> (no .fb suffix). The future streaming writer
// (#5902) creates this dir and fills it with seg-NNNN.fb + manifest.json; the
// dark read substrate only needs the name to recognise + resolve it.
func GenDirName(gen uint64) string {
	return fmt.Sprintf("graph.%d", gen)
}

// IsGraphFileName reports whether base (a bare filename, not a path) is a
// FlatBuffers graph file recognised by the gen layout: the legacy flat
// "graph.fb" or a generation file "graph.<gen>.fb". Enumeration/GC callers use
// this so a directory walk that previously matched only "graph.fb" also sees
// the generation files.
func IsGraphFileName(base string) bool {
	if base == flatGraphName {
		return true
	}
	_, ok := parseGen(base)
	return ok
}

// parseGen returns the generation integer encoded in a bare filename
// (graph.<gen>.fb) and whether it matched.
func parseGen(name string) (uint64, bool) {
	m := genFileRe.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// readPointer returns the gen filename named by <dir>/current, validated
// against the graph.<gen>.fb shape so a corrupt/hostile pointer can never
// escape dir via path traversal. ok is false when the pointer is absent,
// unreadable, or malformed.
func readPointer(dir string) (name string, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, currentPointerName))
	if err != nil {
		return "", false
	}
	name = strings.TrimSpace(string(data))
	if _, valid := parseGen(name); !valid {
		return "", false
	}
	return name, true
}

// CurrentGraphPath resolves the active graph file for a state dir. It is the
// single central resolver every reader routes through:
//
//  1. If <dir>/current names an existing graph.<gen>.fb, return that path.
//  2. Otherwise fall back to the legacy flat <dir>/graph.fb path.
//
// The flat path is returned even when it does not exist on disk — callers stat
// or open it and handle absence, exactly as they did before the gen layout.
// This preserves the pre-existing "graph absent" semantics for a brand-new,
// never-indexed repo (no pointer, no flat file).
func CurrentGraphPath(dir string) string {
	if dir == "" {
		return ""
	}
	if name, ok := readPointer(dir); ok {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
		// Pointer names a missing gen file (torn write / manual deletion) —
		// fall through to the flat fallback rather than returning a
		// guaranteed-ENOENT gen path.
	}
	return filepath.Join(dir, flatGraphName)
}

// GraphKind classifies what the `current` pointer (or the legacy flat file)
// resolves to for a state dir.
type GraphKind int

const (
	// GraphAbsent — no graph on disk (never-indexed repo): neither a resolvable
	// gen file/dir nor the legacy flat graph.fb exists.
	GraphAbsent GraphKind = iota
	// GraphSingleFile — the single-segment fast path: graph.<gen>.fb (or the
	// legacy flat graph.fb). Byte-identical to the pre-#5901 world; the
	// overwhelming common case.
	GraphSingleFile
	// GraphSegmentSet — the multi-segment layout: a graph.<gen>/ dir holding
	// seg-NNNN.fb files + manifest.json.
	GraphSegmentSet
)

// GraphDescriptor is the resolved shape of a state dir's active graph. It is
// the segment-aware superset of CurrentGraphPath: where CurrentGraphPath only
// ever hands back a single .fb path (and is preserved unchanged for the ~11
// #5891 single-file/legacy callers), CurrentGraphDescriptor additionally
// recognises the segment-set layout so a reader can route to it.
type GraphDescriptor struct {
	Kind GraphKind
	// Dir is the state dir this descriptor was resolved for.
	Dir string
	// Path is the single .fb file for GraphSingleFile (and, for GraphAbsent,
	// the flat graph.fb path that does not exist — mirroring CurrentGraphPath's
	// "return the flat path even when absent" contract). Empty for a segment-set.
	Path string
	// GenDir is the absolute graph.<gen>/ directory for GraphSegmentSet; empty
	// otherwise.
	GenDir string
	// Manifest is the parsed, validated manifest for GraphSegmentSet; nil
	// otherwise.
	Manifest *Manifest
	// Segments are the absolute seg-NNNN.fb paths (in manifest order) for
	// GraphSegmentSet; nil otherwise. Handed straight to OpenSegmentsWithRanges.
	Segments []string
}

// CurrentGraphDescriptor resolves dir's active graph into a GraphDescriptor,
// classifying it as single-file, segment-set, or absent. Resolution order
// mirrors CurrentGraphPath, extended for the segment-set layout:
//
//  1. `current` names graph.<gen>.fb (an existing file) → GraphSingleFile.
//  2. `current` names graph.<gen> (a dir) or graph.<gen>/manifest.json → read
//     + validate that dir's manifest → GraphSegmentSet. A malformed/hostile
//     manifest returns a non-nil error (the reader MUST NOT proceed).
//  3. Otherwise fall back to the legacy flat graph.fb: GraphSingleFile when it
//     exists on disk, else GraphAbsent (Path still set to the flat path, for
//     parity with CurrentGraphPath's absent semantics).
//
// A missing pointer, a pointer to a missing gen file, or a hostile pointer all
// fall through to step 3 (never an error) — only a pointer that DOES resolve to
// a gen dir whose manifest is corrupt surfaces an error, so a genuinely broken
// segment-set is loud rather than silently mis-read as "absent".
func CurrentGraphDescriptor(dir string) (GraphDescriptor, error) {
	if dir == "" {
		return GraphDescriptor{Kind: GraphAbsent}, nil
	}
	flat := filepath.Join(dir, flatGraphName)
	raw, ok := readPointerRaw(dir)
	if ok {
		// (1) Single-file gen pointer.
		if _, isGen := parseGen(raw); isGen {
			p := filepath.Join(dir, raw)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return GraphDescriptor{Kind: GraphSingleFile, Dir: dir, Path: p}, nil
			}
			// Pointer names a missing gen file — fall through to flat fallback.
		} else if genDirName, isSeg := parseSegPointer(raw); isSeg {
			// (2) Segment-set pointer (gen dir or its manifest).
			genDir := filepath.Join(dir, genDirName)
			if fi, err := os.Stat(genDir); err == nil && fi.IsDir() {
				m, mErr := ReadManifest(genDir)
				if mErr != nil {
					return GraphDescriptor{}, fmt.Errorf("graph: resolve segment-set %s: %w", genDir, mErr)
				}
				segs := make([]string, 0, len(m.Segments))
				for _, s := range m.Segments {
					segs = append(segs, filepath.Join(genDir, s.File))
				}
				return GraphDescriptor{
					Kind:     GraphSegmentSet,
					Dir:      dir,
					GenDir:   genDir,
					Manifest: m,
					Segments: segs,
				}, nil
			}
			// Pointer names a missing gen dir — fall through to flat fallback.
		}
	}
	// (3) Legacy flat fallback.
	if fi, err := os.Stat(flat); err == nil && !fi.IsDir() {
		return GraphDescriptor{Kind: GraphSingleFile, Dir: dir, Path: flat}, nil
	}
	return GraphDescriptor{Kind: GraphAbsent, Dir: dir, Path: flat}, nil
}

// readPointerRaw returns the trimmed content of <dir>/current with NO shape
// validation beyond non-emptiness and a coarse path-safety guard (no separator,
// except the single "/manifest.json" suffix a segment-set pointer may carry;
// no ".."). CurrentGraphDescriptor then classifies the value via parseGen /
// parseSegPointer. This is the segment-aware sibling of readPointer (which only
// accepts the graph.<gen>.fb single-file shape); the two share the same trust
// boundary — a hostile pointer resolves to nothing and falls back to flat.
func readPointerRaw(dir string) (raw string, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, currentPointerName))
	if err != nil {
		return "", false
	}
	raw = strings.TrimSpace(string(data))
	if raw == "" || strings.Contains(raw, "..") {
		return "", false
	}
	return raw, true
}

// parseSegPointer recognises a segment-set `current` pointer value and returns
// the bare gen-dir name (graph.<gen>). It accepts either the dir itself
// ("graph.<gen>") or the dir-qualified manifest ("graph.<gen>/manifest.json")
// per decision 2. Anything else (a single-file gen name, junk, a deeper path)
// yields ok=false. The returned name is always a bare "graph.<digits>" with no
// separator, so joining it under dir can never escape dir.
func parseSegPointer(raw string) (genDirName string, ok bool) {
	name := raw
	if suffix := "/" + ManifestFileName; strings.HasSuffix(raw, suffix) {
		name = strings.TrimSuffix(raw, suffix)
	}
	if !genDirRe.MatchString(name) {
		return "", false
	}
	return name, true
}

// CurrentGraphMtime resolves dir's active graph — segment-set aware — and
// returns its freshness mtime. #5915 J2 slice-3: every existence/freshness
// gate that used to os.Stat(CurrentGraphPath(dir)) directly only ever sees a
// flat .fb path, which is ABSENT for a segment-set repo (graph.<gen>/ dir +
// manifest.json, no flat .fb) — that stat silently reports "never indexed" /
// mtime-zero for a repo that is in fact freshly indexed. This is the shared,
// exported resolution those call sites route through instead of duplicating
// the descriptor-branch inline:
//
//   - GraphSingleFile (including the legacy flat fallback): the resolved .fb
//     file's own mtime — byte-identical to the pre-fix os.Stat behavior.
//   - GraphSegmentSet: the gen dir's manifest.json mtime, the atomic
//     commit point of a segment-set rebuild (verified newer than every
//     segment file it names) — mirrors internal/graph/groupalgo's
//     graphSourceMtime and cmd/grafel/daemon_tier.go's tierReloadCallback,
//     which use the same signal for cold-wake / overlay staleness.
//   - GraphAbsent, or a resolved path whose stat fails: ok=false.
//
// Lives here (not in internal/graph/groupalgo, which already has an
// unexported graphSourceMtime) because groupalgo imports internal/daemon,
// so internal/daemon call sites (deadref.go, algo/cache.go) cannot import
// groupalgo without a cycle; internal/graph has no such constraint and is
// already imported by every one of these sites.
func CurrentGraphMtime(dir string) (mtime time.Time, ok bool) {
	desc, err := CurrentGraphDescriptor(dir)
	if err != nil {
		return time.Time{}, false
	}
	var path string
	switch desc.Kind {
	case GraphSingleFile:
		path = desc.Path
	case GraphSegmentSet:
		path = filepath.Join(desc.GenDir, ManifestFileName)
	default:
		return time.Time{}, false
	}
	fi, statErr := os.Stat(path)
	if statErr != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}

// NextGen returns the next generation integer to write in dir: one greater
// than the maximum generation observed among the `current` pointer and every
// graph.<gen>.fb already present. Returns 1 for a dir that has never held a
// gen file. Scanning the directory (not merely trusting the pointer) keeps the
// sequence monotonic even if the pointer was manually deleted while gen files
// remain, so a stale reader never resolves a freshly-written gen to an older
// file.
// #5902: the segmented writer emits a generation as a graph.<gen>/ DIR (not a
// graph.<gen>.fb file), and points `current` at that dir. NextGen therefore
// scans the UNION of gen files and gen dirs — and reads the pointer via the
// segment-aware readPointerRaw so a segment-set `current` value is counted too
// — keeping the sequence monotonic across a mixed single-file/segment-set
// history. Trusting only the file-shaped pointer/entries (as before) would let
// a fresh segmented write collide with the immediately-previous gen dir.
func NextGen(dir string) uint64 {
	var maxGen uint64
	if raw, ok := readPointerRaw(dir); ok {
		if v, single := parseGen(raw); single && v > maxGen {
			maxGen = v
		} else if name, seg := parseSegPointer(raw); seg {
			if v, ok := parseGenDir(name); ok && v > maxGen {
				maxGen = v
			}
		}
	}
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				if v, ok := parseGenDir(e.Name()); ok && v > maxGen {
					maxGen = v
				}
				continue
			}
			if v, ok := parseGen(e.Name()); ok && v > maxGen {
				maxGen = v
			}
		}
	}
	return maxGen + 1
}

// pointerFlipRetries / pointerFlipRetryDelay bound the pointer-rename retry
// loop. On Unix the rename-over succeeds on the first try. On Windows, a
// concurrent resolver mid os.ReadFile of `current` can briefly hold it open
// and make the rename fail transiently with a sharing violation — the pointer
// is a tiny file read in microseconds, so a short bounded backoff rides that
// window out. This is NOT the mmap hazard (the pointer is never mapped); it is
// only the same brief open/read/close race the overlay swap already tolerates.
var (
	pointerFlipRetries    = 40
	pointerFlipRetryDelay = 5 * time.Millisecond
)

// WriteCurrentPointer atomically points <dir>/current at genName (a bare
// graph.<gen>.fb filename) via a sibling .tmp + rename. The pointer file is
// tiny and NEVER memory-mapped, so this rename-over is safe on every platform
// (it is not the ERROR_USER_MAPPED_FILE hazard the gen layout removes); a
// bounded retry absorbs the transient Windows reader-open race.
func WriteCurrentPointer(dir, genName string) error {
	if _, ok := parseGen(genName); !ok {
		return fmt.Errorf("graph.WriteCurrentPointer: invalid gen name %q", genName)
	}
	tmp := filepath.Join(dir, currentPointerName+".tmp")
	if err := os.WriteFile(tmp, []byte(genName+"\n"), 0o644); err != nil {
		return fmt.Errorf("graph.WriteCurrentPointer: write tmp: %w", err)
	}
	dst := filepath.Join(dir, currentPointerName)
	var err error
	for i := 0; i < pointerFlipRetries; i++ {
		if err = os.Rename(tmp, dst); err == nil {
			return nil
		}
		time.Sleep(pointerFlipRetryDelay)
	}
	os.Remove(tmp)
	return fmt.Errorf("graph.WriteCurrentPointer: rename: %w", err)
}

// WriteCurrentPointerRaw atomically points <dir>/current at name, where name
// may be EITHER a single-file gen ("graph.<gen>.fb") OR a segment-set gen dir
// ("graph.<gen>") or its manifest ("graph.<gen>/manifest.json"). It is the
// segment-aware superset of WriteCurrentPointer (which only accepts the
// single-file shape); the two share the same atomic tmp+rename + bounded retry.
// This is a format primitive for the FUTURE streaming writer (#5902) and the
// test fixtures — no existing producer calls it.
func WriteCurrentPointerRaw(dir, name string) error {
	_, single := parseGen(name)
	_, seg := parseSegPointer(name)
	if !single && !seg {
		return fmt.Errorf("graph.WriteCurrentPointerRaw: invalid pointer target %q", name)
	}
	tmp := filepath.Join(dir, currentPointerName+".tmp")
	if err := os.WriteFile(tmp, []byte(name+"\n"), 0o644); err != nil {
		return fmt.Errorf("graph.WriteCurrentPointerRaw: write tmp: %w", err)
	}
	dst := filepath.Join(dir, currentPointerName)
	var err error
	for i := 0; i < pointerFlipRetries; i++ {
		if err = os.Rename(tmp, dst); err == nil {
			return nil
		}
		time.Sleep(pointerFlipRetryDelay)
	}
	os.Remove(tmp)
	return fmt.Errorf("graph.WriteCurrentPointerRaw: rename: %w", err)
}

// GCStaleGens best-effort unlinks generation files older than the
// immediately-previous generation, keeping the current gen and the one before
// it. The previous gen is retained because serve/MCP may still have it mapped
// during a reload overlap (it swaps to the new gen on its next mtime-drift
// poll); deleting it out from under an active mmap would be unsafe on Unix and
// impossible on Windows.
//
// This is STRICTLY best-effort and MUST NOT affect the caller's success: a
// still-mapped gen on Windows fails to delete with a sharing violation, which
// is silently ignored and swept on a later write. A GC error never propagates.
// Returns the list of gen entries actually removed (for tests/observability).
//
// #5901: a generation may be EITHER a single-file graph.<gen>.fb OR a
// multi-segment gen DIR graph.<gen>/ (holding seg-NNNN.fb + manifest.json). GC
// sweeps both: a stale gen file is unlinked; a stale gen dir is removed
// recursively (os.RemoveAll — every segment plus the manifest). The keep-window
// (current + immediately-previous) is computed over the UNION of gen files and
// gen dirs, so a mixed-history dir (some gens single-file, some segmented)
// still retains exactly the two newest generations regardless of shape.
func GCStaleGens(dir string, currentGen uint64) []string {
	if currentGen <= 1 {
		return nil // nothing older than the immediately-previous gen exists
	}
	keepFrom := currentGen - 1 // keep currentGen and currentGen-1
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var removed []string
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() {
			// Multi-segment gen dir: graph.<gen>/ → rm -rf when stale.
			v, ok := parseGenDir(name)
			if !ok || v >= keepFrom {
				continue
			}
			if err := os.RemoveAll(filepath.Join(dir, name)); err == nil {
				removed = append(removed, name)
			}
			// On failure (e.g. Windows sharing violation on a still-mapped
			// segment) swallow and retry on the next write. Never fail.
			continue
		}
		// Single-file gen: graph.<gen>.fb → unlink when stale.
		v, ok := parseGen(name)
		if !ok || v >= keepFrom {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err == nil {
			removed = append(removed, name)
		}
		// On failure (e.g. Windows ERROR_SHARING_VIOLATION on a still-mapped
		// gen) swallow the error and try again on the next write. Never fail.
	}
	return removed
}

// parseGenDir returns the generation integer encoded in a bare gen-DIR name
// (graph.<gen>, no .fb suffix) and whether it matched. It is the segment-set
// counterpart to parseGen.
func parseGenDir(name string) (uint64, bool) {
	m := genDirRe.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// WriteGenGraph writes buf as a NEW generation file in dir, flips the `current`
// pointer to it, and best-effort GCs stale generations. It is the single
// producer primitive behind every writer (full-index and incremental): it
// never renames over an existing (possibly mapped) file — the gen filename is
// freshly allocated by NextGen, so its tmp+rename lands on a name nothing has
// open. Returns the absolute path of the gen file written, which callers pass
// to directory-keyed sidecar writers (WriteSidecar keys on filepath.Dir).
//
// The `current` pointer is flipped only AFTER the gen file is durably renamed
// into place, so a reader never resolves the pointer to a half-written file.
// A GC failure after the flip does not fail the write (see GCStaleGens).
func WriteGenGraph(dir string, buf []byte) (genPath string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("graph.WriteGenGraph: mkdir %s: %w", dir, err)
	}
	gen := NextGen(dir)
	name := GenFileName(gen)
	genPath = filepath.Join(dir, name)
	tmp := genPath + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return "", fmt.Errorf("graph.WriteGenGraph: write tmp: %w", err)
	}
	if err := os.Rename(tmp, genPath); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("graph.WriteGenGraph: rename gen: %w", err)
	}
	if err := WriteCurrentPointer(dir, name); err != nil {
		return "", fmt.Errorf("graph.WriteGenGraph: flip pointer: %w", err)
	}
	GCStaleGens(dir, gen) // best-effort; never fails the write
	return genPath, nil
}
