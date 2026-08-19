package generated

import (
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/cajasmota/grafel/internal/types"
)

// Set resolves, once per file, whether a repository-relative path is
// machine-generated, and stamps entity records accordingly.
//
// # Why this exists at all
//
// The stamp originally lived only in extractors.safeExtract, described as "the
// one chokepoint all three production paths funnel through". That was true of
// PASS 1 and false of the pipeline. Two further passes emit EntityRecords on
// both production paths and never touch safeExtract:
//
//	pass 2.5 (framework rules)   index.go: i.detector.Detect
//	                             subproc.go: detector.Detect
//	pass 3   (cross-language)    index.go: e.Extractor.Extract
//	                             subproc.go: ce.Extractor.Extract
//
// input.Path is the generated file's relpath in every one of those, so their
// entities were attributed to a generated file and carried no flag. Concretely
// *.pb.gw.go has its own entry in pathRules, yet the grpc-gateway endpoint
// entities _cross_endpoint extracts from it were never stamped and never
// demoted — and the same held for jOOQ/sqlc ORM links via _cross_ormlink.
// Those are the exact categories #6329 was filed about.
//
// # Why the resolution is lazy
//
// Marker detection needs file CONTENT, and the seam that covers every pass on
// both paths (mergePassRecords) runs after releaseClassifiedASTs has dropped
// it. Re-reading the head — at most headScanBytes, once per distinct file that
// produced an entity — is the price of having ONE seam instead of one per
// pass. The alternative, pre-populating from the classified slice, is only
// possible on the in-process path, so it would reintroduce a second code path
// on exactly the axis this is fixing.
//
// Path rules are evaluated first and need no IO, so the read happens only for
// files no filename rule already decided.
type Set struct {
	// root is the absolute repository root. An empty root disables the
	// content read entirely, leaving the filename rules — that is the shape
	// unit tests that call buildDocument directly get, and it degrades rather
	// than lying.
	root string

	mu sync.Mutex
	m  map[string]Detection
}

// NewSet returns a Set resolving relative paths against absRepo.
func NewSet(absRepo string) *Set {
	return &Set{root: absRepo, m: make(map[string]Detection)}
}

// Lookup resolves rel, memoising the answer.
func (s *Set) Lookup(rel string) Detection {
	if rel == "" {
		return Detection{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.m[rel]; ok {
		return d
	}
	d := FromPath(rel)
	if !d.Generated && s.root != "" {
		d = FromContent(readHead(filepath.Join(s.root, filepath.FromSlash(rel))))
	}
	s.m[rel] = d
	return d
}

// PropSetter is anything carrying the property-setter shape shared by
// graph.Entity and the record types. It is declared STRUCTURALLY so this
// package — a leaf detector — does not have to import internal/graph.
type PropSetter interface {
	PropSet(key, val string)
}

// StampEntities marks every entity whose SourceFile is machine-generated.
//
// It takes the source file and the setter separately so the caller can pass
// &doc.Entities[k] without this package importing internal/graph.
func (s *Set) StampEntities(n int, at func(i int) (string, PropSetter)) {
	for i := 0; i < n; i++ {
		rel, e := at(i)
		d := s.Lookup(rel)
		if !d.Generated || e == nil {
			continue
		}
		e.PropSet(types.EntityGeneratedProperty, "true")
		e.PropSet(types.EntityGeneratedByProperty, d.Rule)
	}
}

// StampRecord applies one Detection to one record. It is the ONLY place the
// two property keys are written, so both seams agree by construction.
//
// A record that carried no Properties map does not gain an empty one when the
// file is not generated — several downstream checks are written as
// `len(e.Properties) == 0`, so allocating unconditionally would change what
// they see on every file in the repository. Callers therefore check
// Detection.Generated before calling this.
func StampRecord(r *types.EntityRecord, d Detection) {
	if !d.Generated {
		return
	}
	if r.Properties == nil {
		r.Properties = make(map[string]string, 2)
	}
	r.Properties[types.EntityGeneratedProperty] = "true"
	r.Properties[types.EntityGeneratedByProperty] = d.Rule
}

// readHead reads at most headScanBytes from path. Any error yields nil, which
// FromContent treats as "not generated" — the fail-safe direction.
func readHead(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, headScanBytes)
	n, err := f.Read(buf)
	if n <= 0 || (err != nil && err != io.EOF) {
		return nil
	}
	return buf[:n]
}
