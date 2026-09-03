package graph

// kindvocab.go — reading back the entity-kind vocabulary version a stored
// graph was written under (#6779).
//
// Renaming or retiring an entity kind does not break any format. The stored
// graph.fb stays readable, its FormatVersion stays current, ReindexRequiredReason
// keeps answering "no reindex needed" — and every consumer filtering on the new
// kind spelling gets an empty result set from a graph that looks healthy.
// That silence is the defect; this file is how a reader can see it.

import (
	"os"
	"path/filepath"

	"github.com/cajasmota/grafel/internal/types"
)

// KindVocabularyState is the answer to "what vocabulary does the graph stored
// in this state dir speak?".
//
// It has THREE values on purpose. "This graph is current" and "there is no
// graph here at all" are different facts about a repo, and a check that
// collapses them re-creates the very failure #6779 exists to fix: a report
// that cannot distinguish a healthy answer from an absent one. (The same
// collapse #6757 arm C had to undo between `Scanned` and `Clean`.)
type KindVocabularyState string

const (
	// KindVocabularyNoGraph — this state dir holds no graph. Nothing has been
	// indexed here (or a `reset` removed it), so there is no vocabulary to be
	// current or stale, and telling the user to reindex would be advice about
	// a graph that does not exist.
	KindVocabularyNoGraph KindVocabularyState = "no-graph"

	// KindVocabularyCurrent — a graph is stored here and it was written under
	// this build's kind vocabulary (or a newer one). Its kind strings mean
	// what this binary thinks they mean.
	KindVocabularyCurrent KindVocabularyState = "current"

	// KindVocabularyOlder — a graph is stored here and it was written under an
	// OLDER kind vocabulary. Its entities may still be spelled with kinds this
	// build has renamed or retired, so queries filtering on the new spellings
	// can silently return nothing. The fix is a reindex, which the USER
	// chooses to run: nothing in grafel migrates or reindexes on this signal.
	KindVocabularyOlder KindVocabularyState = "older"
)

// KindVocabularyStateForDir reports which entity-kind vocabulary the graph
// stored in dir (a repo's .grafel state directory) was written under, together
// with the version actually stamped on disk (0 when the graph predates the
// stamp, and meaningless when the state is KindVocabularyNoGraph).
//
// The two inputs are read INDEPENDENTLY, and that independence is the whole
// design:
//
//   - whether a graph EXISTS is answered by the same resolution the loader
//     uses (CurrentGraphDescriptor for graph.fb / segment sets, plus the
//     graph.json fallback), never by the sidecar. A stale graph-stats.json
//     left behind by a removed graph must not be reported as a stale-
//     vocabulary graph.
//   - which VOCABULARY it speaks is answered by the sidecar stamp, never by
//     the graph, because the graph does not record it and cannot.
//
// Deriving both from one signal is what would collapse the three states into
// two. It is cheap enough to call on every doctor run: a stat plus a small
// JSON read, with no entity materialization.
func KindVocabularyStateForDir(dir string) (state KindVocabularyState, stored int) {
	if !storedGraphExists(dir) {
		return KindVocabularyNoGraph, 0
	}
	side, err := LoadSidecar(dir)
	if err != nil || side == nil {
		// A graph with no readable sidecar cannot prove it is current, and the
		// honest reading of an unprovable stamp is the same as an absent one:
		// every graph written before this mechanism existed is genuinely on an
		// older vocabulary.
		return KindVocabularyOlder, 0
	}
	stored = side.KindVocabularyVersion
	if stored >= types.KindVocabularyVersion {
		// Strictly-newer means the graph came from a build ahead of this one.
		// Reindexing with THIS binary would move it backwards, so there is
		// nothing to report.
		return KindVocabularyCurrent, stored
	}
	return KindVocabularyOlder, stored
}

// storedGraphExists reports whether dir actually holds a graph, using the same
// resolution order LoadGraphFromDir uses: a `current`-pointed generation file
// or segment set first, then the legacy flat graph.fb, then graph.json.
func storedGraphExists(dir string) bool {
	if dir == "" {
		return false
	}
	if desc, err := CurrentGraphDescriptor(dir); err == nil && desc.Kind != GraphAbsent {
		return true
	}
	if fi, err := os.Stat(filepath.Join(dir, "graph.json")); err == nil && !fi.IsDir() {
		return true
	}
	return false
}
