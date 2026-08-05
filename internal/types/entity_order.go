package types

import "sort"

// SortEntityRecordsCanonical orders an EntityRecord slice by the tuple
// (Kind, Name, SourceFile, StartLine, EndLine, Signature) — issue #481's
// canonical order.
//
// WHY IT LIVES HERE. Several downstream passes disambiguate identically-named
// entities by FIRST-WRITER-WINS over the slice they are handed
// (resolve.BuildIndex, the seenEntity/seenRel dedup in buildDocument,
// engine.ResolveHTTPEndpointHandlers' globalIdx / globalMulti /
// sameFileBareIdx, engine.ApplyResponseShapesCorpus' handler index). Whoever
// comes first in the slice therefore DECIDES the graph, so a producer that
// hands those passes a differently-ordered slice can bind a different entity
// than the full rebuild does — a mis-bind, which is the class every count-based
// metric reports as healthy (#6123).
//
// There were two copies of this comparator before #6150 (cmd/grafel's
// determinism.go and internal/daemon/extract's coordinator.go) and the
// incremental producer needed a third, which is exactly the drift #5974 fixed
// for the emission sort by moving it into one shared place. This is that place
// for the canonical order: it is the lowest package all three producers already
// depend on, so no import cycle is possible.
//
// sort.SliceStable, not sort.Slice: records comparing equal on all six fields
// keep their arrival order, so the answer stays a function of the input rather
// than of the sort's internal pivot choices.
func SortEntityRecordsCanonical(rs []EntityRecord) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := &rs[i], &rs[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.SourceFile != b.SourceFile {
			return a.SourceFile < b.SourceFile
		}
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		if a.EndLine != b.EndLine {
			return a.EndLine < b.EndLine
		}
		return a.Signature < b.Signature
	})
}
