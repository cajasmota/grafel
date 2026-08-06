package main

// #6212, review follow-up — the two ORDER properties the walk stamp rests on.
//
// The end-to-end tests in manifest_walk_stamp_6212_test.go pin the coarse
// moment: the stamp comes from the walk, not from a re-hash at commit time. They
// cannot see either of the properties below, because both are about the order of
// operations INSIDE the per-file sequence, and from outside the loop a stamp
// taken from `content` and a stamp taken from a fresh os.ReadFile of the same
// path are indistinguishable — same value, different moment.
//
// So these drive classifyAndReadWithProgress directly and inject a working-tree
// write at a precise point via the osStat / osReadFile seams. No index run, no
// git fixture: a temp dir with two files, which keeps them cheap enough to run
// on their own.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func sha256OfBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// stampProbeIndexer builds a repo with one Go file and returns the indexer plus
// the repo root. The file is real so the classifier accepts it and the worker
// takes the read path.
func stampProbeIndexer(t *testing.T, body string) (*Indexer, string, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "grafelhome"))
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	rel := "victim.go"
	if err := os.WriteFile(filepath.Join(repo, rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := newTestIndexer(t, "stampprobe", []string{"extract"}, "")
	return idx, repo, rel
}

// TestWalkStamp_HashesTheBytesThePipelineReceived_NotAReReadOfThePath is F3.
//
// The stamp's meaning is "these bytes are what the graph was built from". The
// bytes the graph is built from are the ones os.ReadFile RETURNED — not whatever
// the path holds a moment later. Re-reading to hash reintroduces the entire
// #6212 defect in miniature: a window, and a stamp describing content no
// extractor ever saw.
//
// The seam returns bytes that are deliberately NOT on disk, so an implementation
// that re-reads stamps the disk's bytes and dies here, while producing an
// identical stamp to the correct one in every other test.
func TestWalkStamp_HashesTheBytesThePipelineReceived_NotAReReadOfThePath(t *testing.T) {
	const onDisk = "package victim\n\nfunc OnDisk() {}\n"
	const handedToPipeline = "package victim\n\nfunc HandedToThePipeline() int { return 7 }\n"

	idx, repo, rel := stampProbeIndexer(t, onDisk)

	prev := osReadFile
	osReadFile = func(name string) ([]byte, error) {
		if filepath.Base(name) == rel {
			return []byte(handedToPipeline), nil
		}
		return prev(name)
	}
	t.Cleanup(func() { osReadFile = prev })

	idx.classifyAndReadWithProgress(context.Background(), repo, []string{rel}, false, nil)

	got := idx.walkStamps[rel].SHA256
	if got == "" {
		t.Fatal("fixture is inert: no stamp was recorded for the victim file at all")
	}
	if got == sha256OfBytes([]byte(onDisk)) {
		t.Fatalf("the stamp is the hash of the bytes ON DISK, not of the bytes the pipeline was "+
			"handed. The extractors saw %q and the graph contains those entities, but the manifest "+
			"describes something else — a re-read of the path is the #6212 defect in miniature "+
			"(#6212)", handedToPipeline)
	}
	if want := sha256OfBytes([]byte(handedToPipeline)); got != want {
		t.Fatalf("stamp is %q, want the hash of the bytes the pipeline received (%q)", got, want)
	}
}

// TestWalkStamp_StatIsTakenBeforeTheRead is F5.
//
// size/mtime feed isChanged's fast path: when both match, the next pass skips
// the hash entirely and calls the file unchanged. So pairing POST-write metadata
// with PRE-write content is the one combination that loses an edit silently — it
// is the fast path lying, which no hash ever gets to correct.
//
// Taking the stat before the read makes the metadata at worst STALER than the
// content, which sends the next pass to the hash. This test makes a write land
// between the stat and the read: the read sees the new bytes, and the stamp must
// therefore carry the new hash beside the OLD mtime. An implementation that
// stats after reading records the new mtime, and the pair becomes self-
// consistent-but-wrong the moment anything else diverges.
func TestWalkStamp_StatIsTakenBeforeTheRead(t *testing.T) {
	const before = "package victim\n\nfunc Before() {}\n"
	const after = "package victim\n\nfunc After() int { return 99 }\n"

	idx, repo, rel := stampProbeIndexer(t, before)
	abs := filepath.Join(repo, rel)

	preInfo, err := os.Stat(abs)
	if err != nil {
		t.Fatal(err)
	}
	preMtime := preInfo.ModTime().UnixNano()

	prev := osStat
	var fired bool
	osStat = func(name string) (os.FileInfo, error) {
		info, serr := prev(name)
		if filepath.Base(name) == rel && !fired {
			fired = true
			// The write lands AFTER the stat was taken and BEFORE the read.
			if werr := os.WriteFile(abs, []byte(after), 0o644); werr != nil {
				t.Error(werr)
			}
		}
		return info, serr
	}
	t.Cleanup(func() { osStat = prev })

	idx.classifyAndReadWithProgress(context.Background(), repo, []string{rel}, false, nil)

	if !fired {
		t.Fatal("fixture is inert: the osStat seam never saw the victim file")
	}
	postInfo, err := os.Stat(abs)
	if err != nil {
		t.Fatal(err)
	}
	if postInfo.ModTime().UnixNano() == preMtime {
		t.Skip("filesystem mtime granularity did not separate the two writes; nothing to observe")
	}

	stamp := idx.walkStamps[rel]
	if stamp.SHA256 != sha256OfBytes([]byte(after)) {
		t.Fatalf("stamp hash is %q, want the hash of the bytes the read actually returned — the "+
			"write landed before the read, so those are the bytes the graph was built from", stamp.SHA256)
	}
	if stamp.Mtime != preMtime {
		t.Fatalf("stamp mtime is %d, want the PRE-write %d. The stat must be taken before the read: "+
			"post-write metadata beside pre-write content makes isChanged take its fast path and "+
			"call the file unchanged, and no hash ever gets to correct that (#6212)",
			stamp.Mtime, preMtime)
	}
}

// TestWalkStamp_FirstReadWins pins the recordWalkStamp PRIMITIVE.
//
// A .py file is read TWICE per run: once by the cross-file registry pre-pass in
// runPass1ExtractWithProgress (which feeds extractBaseClasses' cross-file
// INHERITS and the DRF bare-Route suppression) and once by the worker. Both
// reach the graph, so if a write lands between them the graph is built from two
// different versions of the file.
//
// Only the EARLIER stamp is safe. Stamping the later read hash-matches the disk
// on the next pass and freezes the registry's stale contribution — a Django
// models.py rewritten in that gap keeps a wrong INHERITS edge permanently.
// Stamping the earlier one makes the file read as changed and be re-extracted.
//
// This calls stampReadFile directly, so it pins the rule and NOT its caller. The
// pre-pass's own stamp call is pinned end to end by
// TestWalkStamp_PythonPrePassStampWinsOverTheWorkerRead below — deleting that
// call survives this test entirely.
func TestWalkStamp_FirstReadWins(t *testing.T) {
	idx, _, rel := stampProbeIndexer(t, "package victim\n")

	first := sha256OfBytes([]byte("the earlier read"))
	idx.stampReadFile(rel, []byte("the earlier read"), 16, 1111)
	idx.stampReadFile(rel, []byte("the later read"), 14, 2222)

	got := idx.walkStamps[rel]
	if got.SHA256 != first || got.Mtime != 1111 {
		t.Fatalf("stamp is %+v, want the FIRST read's (sha %q, mtime 1111). Last-write-wins lets the "+
			"worker's read overwrite the registry pre-pass's, so a write landing between the two is "+
			"hash-matched away and the registry's stale cross-file contribution becomes permanent "+
			"(#6212)", got, first)
	}
}

// TestWalkStamp_PythonPrePassStampWinsOverTheWorkerRead pins the CALLER — the
// stamp inside the registry pre-pass itself.
//
// The primitive above is first-wins, but that only matters if the pre-pass
// actually records a stamp. Delete its stampReadFile call and every .py file
// silently reverts to the worker's later read: nothing else in the suite
// notices, because with no write between the two reads both produce the same
// hash. The two implementations differ only when it counts.
//
// So this drives the real runPass1ExtractWithProgress over a .py fixture and
// makes the two reads return DIFFERENT bytes, via osReadFile. The pre-pass reads
// first and its bytes are what seeded the class registry, so its hash is the one
// that must survive. If the worker's read wins, the manifest hash-matches the
// disk on the next pass and the registry's stale cross-file INHERITS
// contribution is frozen in place permanently.
func TestWalkStamp_PythonPrePassStampWinsOverTheWorkerRead(t *testing.T) {
	const prePassBytes = "class Model:\n    pass\n\n\nclass FromThePrePass(Model):\n    pass\n"
	const workerBytes = "class Model:\n    pass\n\n\nclass FromTheWorker(Model):\n    pass\n"

	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "grafelhome"))
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	const rel = "models.py"
	if err := os.WriteFile(filepath.Join(repo, rel), []byte(prePassBytes), 0o644); err != nil {
		t.Fatal(err)
	}

	// Read 1 is the registry pre-pass, read 2 is the worker. A developer saving
	// the file in between is what this simulates.
	var reads int
	prev := osReadFile
	osReadFile = func(name string) ([]byte, error) {
		if filepath.Base(name) != rel {
			return prev(name)
		}
		reads++
		if reads == 1 {
			return []byte(prePassBytes), nil
		}
		return []byte(workerBytes), nil
	}
	t.Cleanup(func() { osReadFile = prev })

	idx := newTestIndexer(t, "pyprepass", nil, "")
	if _, _, err := idx.runPass1ExtractWithProgress(context.Background(), repo, []string{rel}, nil); err != nil {
		t.Fatalf("runPass1ExtractWithProgress: %v", err)
	}

	if reads < 2 {
		t.Fatalf("fixture is inert: the .py file was read %d time(s), so there were not two reads "+
			"to disagree about", reads)
	}

	got := idx.walkStamps[rel].SHA256
	if got == "" {
		t.Fatal("no stamp was recorded for the .py file at all")
	}
	if got == sha256OfBytes([]byte(workerBytes)) {
		t.Fatal("the stamp is the WORKER's read. The registry pre-pass read this file first and its " +
			"bytes are what seeded the cross-file class registry, so the graph's INHERITS edges " +
			"come from those bytes — but the manifest now describes the later ones and will " +
			"hash-match the disk next pass, freezing the registry's stale contribution permanently. " +
			"The pre-pass must stamp, and recordWalkStamp's first-wins rule must let it beat the " +
			"worker (#6212).")
	}
	if want := sha256OfBytes([]byte(prePassBytes)); got != want {
		t.Fatalf("stamp is %q, want the registry pre-pass's bytes (%q)", got, want)
	}
}
