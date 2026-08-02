package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/atomicfile"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/indexer/diff"
)

// Worktree graph seeding (#5964, epic #5954).
//
// grafel stores one complete graph per (repo, ref) pair. Creating a worktree
// or a branch and indexing it therefore costs a FULL index of the whole
// corpus even when a single file differs from the parent — the dominant cost
// for the epic's driver, concurrent AI agents each working in their own
// worktree.
//
// Seeding copies the parent ref's graph generation AND its incremental diff
// manifest into the child's state dir, so the child's first pass is an
// ordinary incremental one: the existing content-hash + git machinery
// (internal/indexer/diff.FilterWithGit, plus the #5710 HEAD-advance range
// diff in internal/extractors.TryIncremental) recomputes the changed-file set
// from the child's WORKING TREE against the parent's baseline. That covers
// committed history, uncommitted edits and untracked files in one mechanism,
// because the baseline is content hashes rather than a commit range — an
// agent's in-progress edits, which are exactly what is not committed, are
// picked up without a special case.
//
// TWO THINGS MAKE THIS SAFE, and both are enforced here rather than assumed.
//
// 1. IDENTITY. graph.EntityID hashes the repo tag FIRST, and cmd/grafel.Index
//    defaults that tag to filepath.Base(repoPath) — the WORKTREE directory's
//    basename, which differs from the parent's. A seeded graph would otherwise
//    be a hybrid of two id spaces: parent-tagged carried-forward entities and
//    child-tagged freshly-extracted ones, with every cross-file edge between
//    them dangling. SeedWorktreeGraph therefore refuses to seed without an
//    explicit tag and records it as a PIN (WriteRepoTagPin) that the indexer
//    must honour on BOTH the seeded and the from-scratch path — otherwise the
//    two are not even comparable.
//
//    (The premise that entity ids are already stable across worktrees comes
//    from types.EntityRecord.ComputeID, which hashes OrgID+ProjectID+… and has
//    no repo tag. That is NOT the id function the graph is built with: every
//    producer calls graph.EntityID(i.repoTag, …). Traced, not assumed.)
//
// 2. PROVENANCE. The graph and the diff manifest MUST come from the same
//    parent generation. If they do not — a torn copy, or the parent finishing
//    a reindex mid-copy — a file that really differs can read as unchanged and
//    its stale entities survive into the child's graph: code that is not in
//    the child's tree, invisibly. So the copy is bracketed by a
//    generation-stability check, the result is stamped with a content digest
//    of every artifact copied, and the stamp is re-verified before the seed is
//    consumed (VerifySeededGraph). Anything that fails falls back to a full
//    index with a NAMED reason — never silently, because a seed that
//    systematically never fired would be invisible.
//
// The read path, storage format, cache keys, tier system and Pass-4 semantics
// are untouched: this is index-side only.

// SeedStampFileName is the basename of the provenance stamp written into a
// seeded child's state dir.
const SeedStampFileName = "seed-provenance.json"

// RepoTagPinFileName is the basename of the repo-tag pin. It is written for a
// worktree whether or not seeding succeeds, because a full index of the
// worktree must use the same tag a seeded index would — otherwise the two
// graphs have different entity ids and neither parity nor a later seed is
// possible.
const RepoTagPinFileName = "repo-tag"

// seedStampVersion is bumped when the stamp's JSON shape changes
// incompatibly. A stamp with an unknown version is treated as unverifiable
// (SeedFallbackStampMismatch), never as absent.
const seedStampVersion = 1

// SeedFallbackReason names WHY a seed did not happen, or why a seed already on
// disk must not be trusted. It is always logged. Fallback policy: on any
// non-empty reason the caller runs a full index and reports the reason —
// correctness first, and loudly, so a seed that never fires cannot hide.
type SeedFallbackReason string

const (
	// SeedFallbackParentPathUnresolved — no parent repo path could be
	// resolved for the worktree (neither recorded nor derivable).
	SeedFallbackParentPathUnresolved SeedFallbackReason = "parent_path_unresolved"
	// SeedFallbackRepoTagUnresolved — no repo tag to pin. Seeding without one
	// would mint a hybrid id space (see note 1 above).
	SeedFallbackRepoTagUnresolved SeedFallbackReason = "repo_tag_unresolved"
	// SeedFallbackParentNotIndexed — the parent ref's state dir holds no
	// graph. Nothing to seed from.
	SeedFallbackParentNotIndexed SeedFallbackReason = "parent_not_indexed"
	// SeedFallbackParentManifestAbsent — the parent has a graph but no diff
	// manifest, so there is no baseline for the child's incremental pass.
	// Seeding the graph alone would make every file read as changed: a full
	// reindex with extra copying.
	SeedFallbackParentManifestAbsent SeedFallbackReason = "parent_diff_manifest_absent"
	// SeedFallbackGenerationMoved — the parent's active generation changed
	// across the copy, or a verified seed's `current` pointer no longer names
	// the stamped generation. The copied artifacts may straddle two
	// generations.
	SeedFallbackGenerationMoved SeedFallbackReason = "parent_generation_moved"
	// SeedFallbackCopyFailed — an I/O error while copying.
	SeedFallbackCopyFailed SeedFallbackReason = "copy_failed"
	// SeedFallbackChildAlreadyIndexed — the child already has its own graph.
	// Seeding over it would discard newer work.
	SeedFallbackChildAlreadyIndexed SeedFallbackReason = "child_already_indexed"
	// SeedFallbackStampMismatch — a stamp is present but the artifacts on disk
	// do not hash to what it recorded. The seed is not provably tied to the
	// generation it claims.
	SeedFallbackStampMismatch SeedFallbackReason = "stamp_mismatch"
	// SeedFallbackSuperseded — BENIGN. The state dir's `current` pointer names
	// a generation STRICTLY NEWER than the one the stamp recorded: the child
	// has already built its own graph over the seed and the stamp is simply
	// stale. The caller must consume the stamp and carry on — it must NOT
	// discard, because the graph being pointed at is the child's own.
	//
	// This verdict exists because ConsumeSeedStamp is only reachable from the
	// paths that go looking for a stamp, and the scheduler skips the
	// incremental callback entirely when incremental reindexing is disabled
	// (GRAFEL_INCREMENTAL_REINDEX=0 — documented and supported). A full index
	// can therefore run over a seeded dir and leave the stamp behind. Reading
	// that stale stamp as "generation moved" and discarding would throw away a
	// full corpus reindex the child legitimately paid for: the exact cost this
	// feature exists to eliminate.
	SeedFallbackSuperseded SeedFallbackReason = "seed_superseded_by_child_graph"
)

// SeedVerdictIsBenign reports whether a reason returned by VerifySeededGraph
// describes a state dir that is FINE to keep using — as opposed to one whose
// graph cannot be trusted and must be discarded in favour of a full index.
//
// Callers must branch on this rather than on `reason != ""`, or a superseded
// (stale-stamp) dir will be treated as corrupt and its graph thrown away.
func SeedVerdictIsBenign(reason SeedFallbackReason) bool {
	return reason == "" || reason == SeedFallbackSuperseded
}

// SeedStamp is the provenance record written next to a seeded graph. It is
// what makes the seed provably tied to the parent generation it came from:
// Artifacts maps each copied file (relative to the state dir, forward-slash)
// to its SHA-256 at copy time, so VerifySeededGraph can prove the bytes on
// disk are the bytes that were copied, from the generation named by
// ParentPointer.
type SeedStamp struct {
	Version int `json:"version"`
	// ParentPath / ParentRef / ParentStateDir identify the source. ParentRef
	// is the parent's ACTUAL ref, never a hardcoded default — the bug that
	// killed the first clone-from-parent attempt (#3652).
	ParentPath     string `json:"parent_path"`
	ParentRef      string `json:"parent_ref"`
	ParentStateDir string `json:"parent_state_dir"`
	// ParentPointer is the parent's `current` pointer value at copy time (a
	// graph.<gen>.fb name, a graph.<gen> dir name, or the legacy flat
	// "graph.fb"). This is the generation the child's graph descends from.
	ParentPointer string `json:"parent_pointer"`
	// RepoTag is the tag the child's index MUST be pinned to.
	RepoTag string `json:"repo_tag"`
	// ChildPath / ChildRef identify the destination slot.
	ChildPath string `json:"child_path"`
	ChildRef  string `json:"child_ref"`
	// Artifacts maps state-dir-relative path → SHA-256 hex of the bytes
	// copied. Every entry is re-hashed by VerifySeededGraph.
	Artifacts   map[string]string `json:"artifacts"`
	BytesCopied int64             `json:"bytes_copied"`
	SeededAt    time.Time         `json:"seeded_at"`
}

// SeedRequest describes one seeding attempt.
type SeedRequest struct {
	// ParentPath is the parent repo's main checkout.
	ParentPath string
	// ParentRef is the parent's ACTUAL current ref. When empty the parent's
	// state dir is resolved via StateDirForRepo (which captures HEAD) — still
	// never a hardcoded "main".
	ParentRef string
	// ParentStateDir overrides the resolved parent state dir (tests).
	ParentStateDir string
	// ChildPath / ChildRef identify the worktree slot to seed.
	ChildPath string
	ChildRef  string
	// ChildStateDir overrides the resolved child state dir (tests).
	ChildStateDir string
	// RepoTag is the tag to pin the child's index to — the parent's slug.
	// Required.
	RepoTag string

	// afterCopyHook runs between the artifact copy and the
	// generation-stability re-check. Test-only seam for exercising "the
	// parent reindexed while the copy was in flight", which is otherwise a
	// genuine race and cannot be provoked deterministically.
	afterCopyHook func()
}

// SeedOutcome is the result of a seeding attempt. Seeded==false is a normal,
// expected outcome; Reason is then always non-empty and Detail always says
// what was actually observed.
type SeedOutcome struct {
	Seeded      bool
	Reason      SeedFallbackReason
	Detail      string
	BytesCopied int64
	Stamp       *SeedStamp
	// ParentStateDir / ChildStateDir are reported even on the fallback paths
	// so the log line can name the dirs that were checked.
	ParentStateDir string
	ChildStateDir  string
}

// SeedWorktreeGraph copies a parent ref's graph generation and diff manifest
// into a worktree's state dir, stamping the result with the parent generation
// it came from. See the package-level note above for why both halves matter.
//
// It never returns an error: every failure is a named SeedFallbackReason, so
// the caller's only decision is "seeded, or full index with this reason".
func SeedWorktreeGraph(req SeedRequest) SeedOutcome {
	out := SeedOutcome{}

	parentSD := req.ParentStateDir
	if parentSD == "" {
		if req.ParentPath == "" {
			out.Reason = SeedFallbackParentPathUnresolved
			out.Detail = "no parent repo path and no explicit parent state dir"
			return out
		}
		if req.ParentRef != "" {
			parentSD = StateDirForRepoRef(req.ParentPath, req.ParentRef)
		} else {
			// Resolves the parent's ACTUAL HEAD ref via gitmeta — #3652's bug
			// was resolving a hardcoded "main" here.
			parentSD = StateDirForRepo(req.ParentPath)
		}
	}
	if parentSD == "" {
		out.Reason = SeedFallbackParentPathUnresolved
		out.Detail = fmt.Sprintf("parent state dir unresolvable for %q ref %q", req.ParentPath, req.ParentRef)
		return out
	}
	out.ParentStateDir = parentSD

	childSD := req.ChildStateDir
	if childSD == "" {
		childSD = StateDirForRepoRef(req.ChildPath, req.ChildRef)
	}
	if childSD == "" {
		out.Reason = SeedFallbackCopyFailed
		out.Detail = "child state dir unresolvable"
		return out
	}
	out.ChildStateDir = childSD

	if req.RepoTag == "" {
		out.Reason = SeedFallbackRepoTagUnresolved
		out.Detail = fmt.Sprintf("no repo tag to pin for worktree %q; seeding would mint entity ids that disagree with the parent's", req.ChildPath)
		return out
	}

	// The child must not already own a graph — seeding over it would discard
	// newer work.
	if desc, err := graph.CurrentGraphDescriptor(childSD); err == nil && desc.Kind != graph.GraphAbsent {
		out.Reason = SeedFallbackChildAlreadyIndexed
		out.Detail = fmt.Sprintf("child state dir %s already holds a graph", childSD)
		return out
	}

	// Resolve the parent's active generation, twice: once now, once after the
	// copy. Both must agree.
	ptrBefore, desc, err := parentGeneration(parentSD)
	if err != nil {
		out.Reason = SeedFallbackParentNotIndexed
		out.Detail = fmt.Sprintf("parent state dir %s: %v", parentSD, err)
		return out
	}
	if desc.Kind == graph.GraphAbsent {
		out.Reason = SeedFallbackParentNotIndexed
		out.Detail = fmt.Sprintf("parent state dir %s holds no graph", parentSD)
		return out
	}
	manifestSrc := filepath.Join(parentSD, diff.ManifestFileName)
	if _, statErr := os.Stat(manifestSrc); statErr != nil {
		out.Reason = SeedFallbackParentManifestAbsent
		out.Detail = fmt.Sprintf("parent state dir %s has a graph but no %s: %v", parentSD, diff.ManifestFileName, statErr)
		return out
	}

	if err := os.MkdirAll(childSD, 0o755); err != nil {
		out.Reason = SeedFallbackCopyFailed
		out.Detail = fmt.Sprintf("mkdir child state dir %s: %v", childSD, err)
		return out
	}

	// Collect the artifacts to copy, relative to the state dir.
	rels, err := seedArtifactRels(parentSD, desc)
	if err != nil {
		out.Reason = SeedFallbackCopyFailed
		out.Detail = fmt.Sprintf("enumerate parent artifacts in %s: %v", parentSD, err)
		return out
	}
	rels = append(rels, diff.ManifestFileName)

	stamp := &SeedStamp{
		Version:        seedStampVersion,
		ParentPath:     req.ParentPath,
		ParentRef:      req.ParentRef,
		ParentStateDir: parentSD,
		ParentPointer:  ptrBefore,
		RepoTag:        req.RepoTag,
		ChildPath:      req.ChildPath,
		ChildRef:       req.ChildRef,
		Artifacts:      make(map[string]string, len(rels)),
		SeededAt:       time.Now().UTC(),
	}

	// Copy. The `current` pointer is deliberately NOT copied yet: until it
	// lands the child's dir does not resolve to a graph, so a crash or a
	// torn copy leaves inert bytes rather than a silently-wrong graph.
	//
	// KNOWN, ACCEPTED: a process crash between here and the stamp write
	// orphans a full graph generation on disk with neither a stamp nor a
	// pointer, so cleanupSeedArtifacts never runs and nothing later collects
	// it. Correctness is unaffected — an unpointed, unstamped file is inert
	// and the next pass runs a clean full index — but it is a disk leak
	// proportional to one graph. Reclaiming it belongs with the existing
	// generation GC (graph.IsGraphFileName already recognises these files),
	// not here.
	for _, rel := range rels {
		sum, n, cerr := copyArtifact(filepath.Join(parentSD, rel), filepath.Join(childSD, rel))
		if cerr != nil {
			cleanupSeedArtifacts(childSD, stamp.Artifacts, rels)
			out.Reason = SeedFallbackCopyFailed
			out.Detail = fmt.Sprintf("copy %s: %v", rel, cerr)
			return out
		}
		stamp.Artifacts[filepath.ToSlash(rel)] = sum
		stamp.BytesCopied += n
	}

	if req.afterCopyHook != nil {
		req.afterCopyHook()
	}

	// Generation-stability re-check. If the parent reindexed while we copied,
	// the graph and the manifest may be from different generations — the
	// silently-wrong case.
	ptrAfter, _, aerr := parentGeneration(parentSD)
	if aerr != nil || ptrAfter != ptrBefore {
		cleanupSeedArtifacts(childSD, stamp.Artifacts, rels)
		out.Reason = SeedFallbackGenerationMoved
		out.Detail = fmt.Sprintf("parent %s generation moved during copy: %q → %q (err=%v)", parentSD, ptrBefore, ptrAfter, aerr)
		return out
	}

	// Stamp BEFORE the pointer: a reader that finds a resolvable graph in a
	// seeded dir always finds a stamp to verify it against.
	if err := writeSeedStamp(childSD, stamp); err != nil {
		cleanupSeedArtifacts(childSD, stamp.Artifacts, rels)
		out.Reason = SeedFallbackCopyFailed
		out.Detail = fmt.Sprintf("write seed stamp: %v", err)
		return out
	}
	if err := WriteRepoTagPin(childSD, req.RepoTag); err != nil {
		cleanupSeedArtifacts(childSD, stamp.Artifacts, rels)
		_ = os.Remove(filepath.Join(childSD, SeedStampFileName))
		out.Reason = SeedFallbackCopyFailed
		out.Detail = fmt.Sprintf("write repo-tag pin: %v", err)
		return out
	}
	// Finally publish the pointer, making the seeded graph resolvable.
	if err := writePointerRaw(childSD, ptrBefore); err != nil {
		cleanupSeedArtifacts(childSD, stamp.Artifacts, rels)
		_ = os.Remove(filepath.Join(childSD, SeedStampFileName))
		out.Reason = SeedFallbackCopyFailed
		out.Detail = fmt.Sprintf("publish current pointer %q: %v", ptrBefore, err)
		return out
	}

	out.Seeded = true
	out.Stamp = stamp
	out.BytesCopied = stamp.BytesCopied
	return out
}

// VerifySeededGraph re-checks a seed already on disk before it is consumed.
//
// Returns (nil, "", nil) for a state dir that was never seeded — that is the
// overwhelming common case (every non-worktree repo) and must not be treated
// as a failure. For a seeded dir it re-hashes every stamped artifact and
// re-reads the `current` pointer; any disagreement yields a named reason and
// the caller must fall back to a full index.
func VerifySeededGraph(stateDir string) (*SeedStamp, SeedFallbackReason, error) {
	if stateDir == "" {
		return nil, "", nil
	}
	stamp, err := ReadSeedStamp(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", nil
		}
		// A stamp that exists but cannot be parsed is unverifiable, not
		// absent: refuse to trust the graph next to it.
		return nil, SeedFallbackStampMismatch, nil
	}
	if stamp.Version != seedStampVersion {
		return stamp, SeedFallbackStampMismatch, nil
	}
	// The pointer must still name the stamped generation — UNLESS it names a
	// strictly newer one, which means the child has already built its own
	// graph over the seed and the stamp is merely stale. Generation numbers
	// within one state dir are minted monotonically by the writer, so "newer
	// gen in this dir" is the child's own work, never the parent's.
	ptr, _, perr := parentGeneration(stateDir)
	if perr != nil {
		return stamp, SeedFallbackGenerationMoved, nil
	}
	if ptr != stamp.ParentPointer {
		if movedForward(stamp.ParentPointer, ptr) {
			return stamp, SeedFallbackSuperseded, nil
		}
		return stamp, SeedFallbackGenerationMoved, nil
	}
	// Every stamped artifact must hash to what was recorded. This is the
	// check a mutation that "trusts the stamp" would delete.
	for rel, want := range stamp.Artifacts {
		got, herr := hashFile(filepath.Join(stateDir, filepath.FromSlash(rel)))
		if herr != nil || got != want {
			return stamp, SeedFallbackStampMismatch, nil
		}
	}
	return stamp, "", nil
}

// DiscardSeed removes every artifact a seed placed in stateDir, plus the
// pointer that made them resolvable, so the next pass runs a clean full index.
// The repo-tag pin deliberately SURVIVES: a full index of the worktree must
// still use the parent's tag.
//
// It is a no-op on a dir that was never seeded — it never deletes a graph it
// did not put there.
func DiscardSeed(stateDir string) error {
	stamp, err := ReadSeedStamp(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		// Unparseable stamp: we cannot know which artifacts were seeded, but
		// we DO know this dir's graph is untrusted. Drop the pointer (making
		// it un-resolvable, so a full index runs) and the stamp.
		_ = os.Remove(filepath.Join(stateDir, graph.CurrentPointerName))
		return os.Remove(filepath.Join(stateDir, SeedStampFileName))
	}
	// REFUSAL. If the dir's active generation is strictly NEWER than the one
	// the stamp recorded, the graph being pointed at is the child's own — the
	// stamp is merely stale (see SeedFallbackSuperseded). Removing the pointer
	// here would make a full corpus reindex the child paid for unresolvable.
	// Drop only the stale stamp. This is deliberately enforced in DiscardSeed
	// itself rather than only at the call sites, so the data loss is
	// structurally impossible rather than merely unreached by today's callers.
	if ptr, _, perr := parentGeneration(stateDir); perr == nil && movedForward(stamp.ParentPointer, ptr) {
		if rerr := os.Remove(filepath.Join(stateDir, SeedStampFileName)); rerr != nil && !os.IsNotExist(rerr) {
			return rerr
		}
		return nil
	}
	// Remove the pointer FIRST so the dir stops resolving to the seeded graph
	// even if a later removal fails.
	if err := os.Remove(filepath.Join(stateDir, graph.CurrentPointerName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for rel := range stamp.Artifacts {
		p := filepath.Join(stateDir, filepath.FromSlash(rel))
		if err := os.RemoveAll(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Remove(filepath.Join(stateDir, SeedStampFileName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ConsumeSeedStamp removes the provenance stamp while leaving the seeded
// graph and manifest in place.
//
// It is called by the pass that has just VERIFIED the seed, at the moment the
// verification succeeds — not after the pass finishes. From that instant the
// pass owns the state dir and will write a new generation into it, at which
// point the stamp's recorded pointer no longer describes what is on disk. A
// stamp left behind would make the NEXT verification report
// generation_moved and DiscardSeed would then delete a graph the child
// legitimately built.
//
// Consuming early is also the safe choice under a crash: the seeded graph is a
// complete copy of the parent's, so an un-stamped state dir is just an
// ordinary already-indexed one and the next pass runs an ordinary incremental
// against it.
func ConsumeSeedStamp(stateDir string) error {
	if stateDir == "" {
		return nil
	}
	if err := os.Remove(filepath.Join(stateDir, SeedStampFileName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReadSeedStamp reads the provenance stamp from stateDir. The returned error
// wraps os.ErrNotExist when there is no stamp (i.e. the dir was never seeded).
func ReadSeedStamp(stateDir string) (*SeedStamp, error) {
	if stateDir == "" {
		return nil, fmt.Errorf("seed stamp: empty state dir: %w", os.ErrNotExist)
	}
	data, err := os.ReadFile(filepath.Join(stateDir, SeedStampFileName))
	if err != nil {
		return nil, err
	}
	var st SeedStamp
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("seed stamp: parse: %w", err)
	}
	return &st, nil
}

func writeSeedStamp(stateDir string, st *SeedStamp) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(filepath.Join(stateDir, SeedStampFileName), append(data, '\n'), 0o644)
}

// WriteRepoTagPin records the repo tag an index of stateDir's repo must use.
//
// It exists because cmd/grafel.Index defaults the tag to the repo directory's
// basename, which for a worktree is the worktree dir name, not the parent's
// slug — and graph.EntityID hashes that tag first. Both the seeded and the
// from-scratch index of a worktree must read this pin, or their graphs are not
// comparable and a seed can never be correct.
func WriteRepoTagPin(stateDir, tag string) error {
	if stateDir == "" || tag == "" {
		return fmt.Errorf("repo-tag pin: empty state dir or tag")
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	return atomicfile.WriteFile(filepath.Join(stateDir, RepoTagPinFileName), []byte(tag+"\n"), 0o644)
}

// ReadRepoTagPin returns the pinned repo tag for stateDir, or "" when there is
// none (every non-worktree repo — the caller then keeps today's default).
func ReadRepoTagPin(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(stateDir, RepoTagPinFileName))
	if err != nil {
		return ""
	}
	tag := strings.TrimSpace(string(data))
	// A pin is a bare tag, never a path: reject anything that could escape.
	if tag == "" || strings.ContainsAny(tag, "/\\") || strings.Contains(tag, "..") {
		return ""
	}
	return tag
}

// ---------------------------------------------------------------------------
// internals
// ---------------------------------------------------------------------------

// parentGeneration returns the raw `current` pointer value for stateDir plus
// the resolved descriptor. For a legacy flat graph.fb (no pointer) the
// sentinel "graph.fb" is returned, so the stamp always names something.
func parentGeneration(stateDir string) (string, graph.GraphDescriptor, error) {
	desc, err := graph.CurrentGraphDescriptor(stateDir)
	if err != nil {
		return "", desc, err
	}
	raw, rerr := os.ReadFile(filepath.Join(stateDir, graph.CurrentPointerName))
	if rerr == nil {
		if v := strings.TrimSpace(string(raw)); v != "" {
			return v, desc, nil
		}
	}
	if desc.Kind == graph.GraphSingleFile {
		return filepath.Base(desc.Path), desc, nil
	}
	return "", desc, nil
}

// seedGenRe extracts the generation number from any shape a `current` pointer
// can take: graph.<gen>.fb, graph.<gen>, or graph.<gen>/manifest.json.
var seedGenRe = regexp.MustCompile(`^graph\.(\d+)(?:\.fb|/manifest\.json)?$`)

// pointerGen parses a pointer value's generation number. The legacy flat
// "graph.fb" sentinel has no generation and reports ok=false.
func pointerGen(ptr string) (uint64, bool) {
	m := seedGenRe.FindStringSubmatch(strings.TrimSpace(ptr))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// movedForward reports whether `now` names a strictly newer generation than
// `stamped`, in the same state dir. Generation numbers are minted
// monotonically per dir by the writer, so this is the signal that the child
// built its own graph over the seed.
//
// Fails CLOSED: if either pointer has no parseable generation (the legacy flat
// layout, or a hostile value), this returns false and the caller treats the
// mismatch as untrusted rather than benign. Being wrong in that direction
// costs a full reindex; being wrong the other way loses a graph.
func movedForward(stamped, now string) bool {
	sg, sok := pointerGen(stamped)
	ng, nok := pointerGen(now)
	return sok && nok && ng > sg
}

// seedArtifactRels lists the graph artifacts to copy, relative to stateDir:
// the single generation file, or every file inside the generation directory
// for a segment set.
func seedArtifactRels(stateDir string, desc graph.GraphDescriptor) ([]string, error) {
	switch desc.Kind {
	case graph.GraphSingleFile:
		rel, err := filepath.Rel(stateDir, desc.Path)
		if err != nil {
			return nil, err
		}
		return []string{rel}, nil
	case graph.GraphSegmentSet:
		var rels []string
		genRel, err := filepath.Rel(stateDir, desc.GenDir)
		if err != nil {
			return nil, err
		}
		ents, err := os.ReadDir(desc.GenDir)
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			if e.IsDir() {
				// Fail LOUD rather than skipping. Gen dirs are flat today, so
				// this is unreachable — but if the layout ever nests, silently
				// skipping a subdirectory would copy a PARTIAL graph, stamp
				// only what was copied, pass verification, and leave the child
				// resolving an incomplete graph. Silently wrong is the failure
				// class this whole design is built against.
				return nil, fmt.Errorf("segment-set dir %s contains a subdirectory %q; seeding cannot prove it copied the whole graph", desc.GenDir, e.Name())
			}
			rels = append(rels, filepath.Join(genRel, e.Name()))
		}
		if len(rels) == 0 {
			return nil, fmt.Errorf("segment-set dir %s is empty", desc.GenDir)
		}
		return rels, nil
	default:
		return nil, fmt.Errorf("no graph to seed from")
	}
}

// copyArtifact copies src→dst, returning the SHA-256 of the bytes written and
// the byte count. The digest is computed from the same stream that is written,
// so it describes exactly what landed on disk.
func copyArtifact(src, dst string) (string, int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()

	tmp := dst + ".seedtmp"
	outF, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, cerr := io.Copy(io.MultiWriter(outF, h), in)
	if cerr != nil {
		outF.Close()
		_ = os.Remove(tmp)
		return "", 0, cerr
	}
	if serr := outF.Sync(); serr != nil {
		outF.Close()
		_ = os.Remove(tmp)
		return "", 0, serr
	}
	if cerr := outF.Close(); cerr != nil {
		_ = os.Remove(tmp)
		return "", 0, cerr
	}
	if rerr := os.Rename(tmp, dst); rerr != nil {
		_ = os.Remove(tmp)
		return "", 0, rerr
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// cleanupSeedArtifacts removes whatever a failed seed managed to write. It
// never touches the `current` pointer because a failed seed never publishes
// one.
func cleanupSeedArtifacts(stateDir string, done map[string]string, rels []string) {
	for rel := range done {
		_ = os.RemoveAll(filepath.Join(stateDir, filepath.FromSlash(rel)))
	}
	for _, rel := range rels {
		_ = os.Remove(filepath.Join(stateDir, rel+".seedtmp"))
	}
}

// writePointerRaw publishes a `current` pointer value verbatim. It exists
// because graph.WriteCurrentPointer only accepts the graph.<gen>.fb shape,
// while a seed must be able to republish a segment-set pointer (graph.<gen> or
// graph.<gen>/manifest.json) or a legacy flat sentinel unchanged. The value is
// never caller-supplied: it is the parent's own pointer, already validated by
// graph.CurrentGraphDescriptor having resolved it.
func writePointerRaw(stateDir, value string) error {
	if value == "" {
		return fmt.Errorf("empty pointer value")
	}
	if value == "graph.fb" {
		// Legacy flat layout: the copied graph.fb IS the resolution. Writing a
		// pointer naming it would not parse as a gen name.
		return nil
	}
	return atomicfile.WriteFile(filepath.Join(stateDir, graph.CurrentPointerName), []byte(value), 0o644)
}
