package links

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/cajasmota/grafel/internal/safeio"
)

// safe_source_read.go — the link pass's single entry point for reading a
// file out of a SCANNED SOURCE TREE (#6823).
//
// WHY THIS EXISTS. #6416: a FIFO with an indexed extension — `mkfifo
// config.js`, one unprivileged command — parks whichever goroutine opens it
// in open(2) forever. No timeout, no error, no log line. The fix for that
// reached internal/engine/http_endpoint_substrate_fold.go, whose header
// spells out the reasoning; it did not reach this package, which held ELEVEN
// copies of the same `os.ReadFile(filepath.Join(srcRoot, rel))` read across
// eleven passes. The link pass re-opens source files by path in group-link,
// long after the indexing walk's irregular-file filter has run, so every one
// of those copies had the same exposure through a different entry point.
//
// It is one helper rather than eleven hardened call sites because eleven
// copies is how the divergence happened in the first place.
//
// WHAT IT DOES NOT COVER. Reads of grafel's OWN artefacts — links.json, the
// resolves-to / drift sidecars, the string-scan cache, candidate documents —
// deliberately stay on os.ReadFile. Those paths are constructed by grafel
// inside a grafel-owned directory rather than taken from a scanned tree, so
// they are not plantable by whoever owns the repo being indexed, and their
// callers depend on distinguishing fs.ErrNotExist from a real failure.

// maxSourceFileBytes caps a single source file read by the link pass.
//
// THE CHOICE, and its cost, stated rather than inherited silently. This is
// the same 1 MiB the engine twin uses (substrateMaxFileBytes), and it is
// deliberately the same value: #6823 exists because the two resolvers
// diverged once already, and a second axis of divergence — one twin bounded
// at 1 MiB, the other at something else — would be a new instance of the bug
// being fixed, not a fix for it.
//
// The cost is real and is inherited whole. safeio.ReadFile TRUNCATES at the
// bound (io.LimitReader) rather than failing, so a declaring file larger than
// 1 MiB is parsed from its first megabyte only: a constant declared past that
// point is silently not found, with no telemetry. #6450 recorded exactly that
// as a permanent capability loss on the engine path. This path now shares it.
// The trade is accepted because a bound is not optional — safeio needs one, a
// character device never reaches EOF — and because base-URL constants,
// import lines and handler bodies live in small modules; a source file over
// 1 MiB is generated or minified, not hand-written.
const maxSourceFileBytes int64 = 1 << 20

// stringScanMaxFileBytes bounds the string-literal scan instead. That pass
// already discards any file larger than 4 MiB after reading it whole, so the
// bound is set one byte past that threshold: a file over the limit still
// arrives over-length and is still discarded by the existing check, making
// this a pure memory saving with no behavioural change. Every file the pass
// accepts today is read whole today.
const stringScanMaxFileBytes int64 = 4*1024*1024 + 1

// readSourceFile reads abs, which must be a path inside a scanned source
// tree, refusing anything that is not a regular file and reading at most
// maxBytes.
//
// FollowSymlinks, matching the engine twin: the indexing walk mints file
// entities for a symlink to a regular file, so rejecting symlinks outright
// would delete legitimate coverage. The policy judges the TARGET, so a
// symlink pointing at a FIFO is still refused.
//
// The bound is a parameter rather than a package default because the two
// callers that need a different one should have to say so at the call site.
func readSourceFile(abs string, maxBytes int64) ([]byte, error) {
	return safeio.ReadFile(abs, safeio.FollowSymlinks, maxBytes)
}

// probeSourceFile answers "would readSourceFile accept abs?" without reading
// it: same package, same symlink policy, same two-layer type gate — it just
// closes the descriptor instead of draining it.
//
// It exists for the string-scan cache (#6857). A cache hit there is validated
// on mtime+size, which says the file has not changed; it does not say the path
// still holds a regular file that opens. Returning cached extractions on that
// evidence alone makes "the contents were classified" an inference, at the one
// site in this package whose contract is that a read failure is loud. This is
// the cheapest way to keep that contract on the cache path: one open(2) and a
// close, not a re-read and a re-hash of the file the cache exists to avoid
// reading.
//
// The bound readSourceFile takes is absent on purpose — nothing is read, so
// there is nothing to bound, and safeio's own stat gate plus O_NONBLOCK are
// what keep the open itself from parking on a FIFO.
func probeSourceFile(abs string) error {
	f, err := safeio.Open(abs, safeio.FollowSymlinks)
	if err != nil {
		return err
	}
	return f.Close()
}

// noteUnreadableSource records one scanned-source read that a SUPPLEMENTARY
// link pass could not complete, and returns how much it adds to that pass's
// PassResult.UnreadableSourceFiles (1 when it counts, 0 when it does not).
// Written as a return value so a call site reads
// `unreadable += noteUnreadableSource(...)` on the line above the `continue`
// it already had.
//
// WHY THE SKIP STAYS (#6839 group C). Nine of this package's eleven hardened
// reads sit in passes whose failure mode is ABSENCE — a property not stamped,
// a sidecar row not written, an edge not emitted. Losing one file thins the
// annotation; it never makes the artefact assert something false. That is a
// materially weaker degradation than reachability.go's (the one site that
// wrote `reachable="false"` off a file it could not read, fixed in #6863),
// and it does not justify giving a best-effort pass an abort path it has
// never had. safeio's package doc (see internal/safeio/safeio.go) forbids the
// SILENCE, not the non-abort: the skip has to be "bounded and REPORTED".
// Skipping is therefore the deliberate choice at every one of these sites,
// and this function is the report that makes it legitimate.
//
// WHAT IT DOES NOT COUNT. fs.ErrNotExist: under a source root that exists, a
// file the graph names but disk does not hold is a KNOWN fact rather than a
// hidden one — nothing about its contents was concealed from the pass, and
// group-link routinely runs over graphs indexed from a tree that has since
// changed. reachability.go additionally stats the source ROOT before granting
// that exemption, because a root that MOVED makes every file report
// ErrNotExist while the code exists; these passes do not carry that guard
// because the consequence there is a whole repo stamped dead, and here it is
// only annotations that were already best-effort.
//
// The consequence, stated rather than left to be discovered: a source root that
// has MOVED makes every file report ErrNotExist, so group C reports ZERO for
// that repo while producing nothing. That is the one scenario in which this
// field is silent by design — the loud signal for it is reachability's
// degraded_repos, which stats the root.
//
// The count reaches an operator through PassResult.UnreadableSourceFiles and
// the `unreadable_source_files` field of <group>-link-pass-stats.json. No MCP
// tool projects that field today — it is an operator-facing record, stated
// here rather than implied to have a consumer.
func noteUnreadableSource(pass, repo, file string, err error) int {
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	fmt.Fprintf(os.Stderr,
		"grafel: warning: %s: %s: source file %s unreadable (%v); file skipped, "+
			"this pass's output for it is missing rather than empty\n",
		pass, repo, file, err)
	return 1
}
