package mcp

import (
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbreader"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
)

// F3 of ADR-0027: the mmap-backed zero-copy read path. These tests prove the
// mmap views exist, are CORRECT (byte-identical to the heap path — the parity
// test), guard the empty-ByteVector panic, and are lifetime-safe under -race
// (no zero-copy string read after its handle is munmapped). They MUST run under
// -race: unsafe aliasing + borrow lifetime is exactly what -race guards.

// TestZeroCopyStringEmptyByteVector is the direct empty-ByteVector test
// (ADR-0027 §plan): unsafe.String(&bv[0], 0) panics on a zero-length slice; the
// len==0 guard must return "" for both a nil and a present-but-empty vector.
func TestZeroCopyStringEmptyByteVector(t *testing.T) {
	if got := zeroCopyString(nil); got != "" {
		t.Errorf("zeroCopyString(nil) = %q, want \"\"", got)
	}
	if got := zeroCopyString([]byte{}); got != "" {
		t.Errorf("zeroCopyString([]byte{}) = %q, want \"\"", got)
	}
	// A present, non-empty vector must alias its bytes verbatim.
	if got := zeroCopyString([]byte("sig")); got != "sig" {
		t.Errorf("zeroCopyString(\"sig\") = %q, want \"sig\"", got)
	}
}

// parityDoc builds a Document exercising every field the views expose plus the
// empty-ByteVector cases that WILL hit real data: an entity with empty
// Signature and empty Subtype, an entity with no module, a relationship with
// and without the tunneled "id" property, and multi/zero property sets.
func parityDoc() *graph.Document {
	e0 := graph.Entity{
		ID: "r::pkg.Foo", Name: "Foo", QualifiedName: "pkg.Foo", Kind: "type",
		Subtype: "struct", SourceFile: "pkg/foo.go", Language: "go",
		Signature: "type Foo struct{}", StartLine: 10,
	}
	// "empty" is a present KEY with an empty ("") VALUE — a zero-length (present)
	// ByteVector on the value path, exercising the zeroCopyString len==0 guard for
	// values (distinct from the empty-scalar Signature/Subtype cases on e1). Its
	// mmap-vs-heap PropGet/PropLookup/PropRange parity is asserted in
	// assertEntityViewEqual's probe list.
	e0.PropsReplace(map[string]string{"module": "pkg", "visibility": "public", "empty": ""})

	// Empty Signature AND empty Subtype — the zero-length (present) ByteVector
	// path. No module property either.
	e1 := graph.Entity{
		ID: "r::pkg.bar", Name: "bar", QualifiedName: "pkg.bar", Kind: "func",
		Subtype: "", SourceFile: "pkg/bar.go", Language: "go", Signature: "",
	}

	// No properties at all → Properties() must be nil on both sides.
	e2 := graph.Entity{
		ID: "r::pkg.Baz", Name: "Baz", QualifiedName: "pkg.Baz", Kind: "const",
		SourceFile: "pkg/baz.go", Language: "go",
	}

	r0 := graph.Relationship{FromID: "r::pkg.Foo", ToID: "r::pkg.bar", Kind: "calls"}
	r0.PropsReplace(map[string]string{"id": "edge-0001", "line": "12"})

	// Relationship without an id property → ID() must be "" on both sides.
	r1 := graph.Relationship{FromID: "r::pkg.bar", ToID: "r::pkg.Baz", Kind: "references"}

	return &graph.Document{
		Entities:      []graph.Entity{e0, e1, e2},
		Relationships: []graph.Relationship{r0, r1},
	}
}

// writeParityGraph writes parityDoc() to a temp graph.fb and returns its path.
func writeParityGraph(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.fb")
	if err := fbwriter.WriteAtomic(path, parityDoc()); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	return path
}

// TestMMapViewParityWithHeap is the key F3 correctness test: build the hot index
// both ways over the SAME graph.fb and assert byte-identical results for every
// hot-index query (byID/byLabel/byQName) and every view accessor, for every
// entity AND relationship. Runs the mmap zero-copy path under -race.
func TestMMapViewParityWithHeap(t *testing.T) {
	path := writeParityGraph(t)
	dir := filepath.Dir(path)

	// OFF side: the heap Document, loaded FROM the same graph.fb (so both sides
	// go through the identical FB decode — a fair parity comparison).
	heapDoc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("LoadGraphFromDir: %v", err)
	}

	// ON side: an mmap reader wrapped in a MapHandle, exactly as reload publishes.
	rdr, err := fbreader.Open(path)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	handle := newMapHandle(rdr)
	handle.repo = "r"
	defer handle.closeOnce()

	heapIdx := buildHotIndex(handle, docEntityViewSource{doc: heapDoc})
	mmapIdx := buildHotIndex(handle, mmapEntityViewSource{handle: handle})

	// Entity parity: for every entity id, both indexes resolve to a view whose
	// every accessor is byte-identical.
	for i := range heapDoc.Entities {
		id := heapDoc.Entities[i].ID
		hv, hok := heapIdx.entityByID(id)
		mv, mok := mmapIdx.entityByID(id)
		if !hok || !mok {
			t.Fatalf("entity %q: byID present heap=%v mmap=%v", id, hok, mok)
		}
		assertEntityViewEqual(t, id, hv, mv)

		// byQName parity (case-insensitive key).
		qn := heapDoc.Entities[i].QualifiedName
		hq, hqok := heapIdx.entityByQName(qn)
		mq, mqok := mmapIdx.entityByQName(qn)
		if hqok != mqok {
			t.Fatalf("entity %q: byQName present heap=%v mmap=%v", id, hqok, mqok)
		}
		if hqok {
			assertEntityViewEqual(t, id+"[qname]", hq, mq)
		}
	}

	// byLabel parity (including the ambiguous-label multiplicity).
	labels := map[string]bool{}
	for i := range heapDoc.Entities {
		labels[heapDoc.Entities[i].Name] = true
	}
	for lbl := range labels {
		hl := idsOfViews(heapIdx.entitiesByLabel(lbl))
		ml := idsOfViews(mmapIdx.entitiesByLabel(lbl))
		if len(hl) != len(ml) {
			t.Fatalf("byLabel %q: heap %v vs mmap %v", lbl, hl, ml)
		}
		for k := range hl {
			if hl[k] != ml[k] {
				t.Fatalf("byLabel %q: heap %v vs mmap %v", lbl, hl, ml)
			}
		}
	}

	// Relationship parity: iterate both readers/docs in vector order and compare
	// every RelationshipView accessor.
	if rdr.RelationshipCount() != len(heapDoc.Relationships) {
		t.Fatalf("relationship count: mmap=%d heap=%d", rdr.RelationshipCount(), len(heapDoc.Relationships))
	}
	for i := range heapDoc.Relationships {
		hv := graph.RelationshipViewOf(&heapDoc.Relationships[i])
		mv := mmapRelationshipView{r: rdr.RelationshipAt(i)}
		assertRelViewEqual(t, i, hv, mv)
	}
}

func assertEntityViewEqual(t *testing.T, id string, want, got graph.EntityView) {
	t.Helper()
	if want.ID() != got.ID() {
		t.Errorf("%s ID: heap %q mmap %q", id, want.ID(), got.ID())
	}
	if want.Kind() != got.Kind() {
		t.Errorf("%s Kind: heap %q mmap %q", id, want.Kind(), got.Kind())
	}
	if want.Name() != got.Name() {
		t.Errorf("%s Name: heap %q mmap %q", id, want.Name(), got.Name())
	}
	if want.QualifiedName() != got.QualifiedName() {
		t.Errorf("%s QualifiedName: heap %q mmap %q", id, want.QualifiedName(), got.QualifiedName())
	}
	if want.Subtype() != got.Subtype() {
		t.Errorf("%s Subtype: heap %q mmap %q", id, want.Subtype(), got.Subtype())
	}
	if want.SourceFile() != got.SourceFile() {
		t.Errorf("%s SourceFile: heap %q mmap %q", id, want.SourceFile(), got.SourceFile())
	}
	if want.Language() != got.Language() {
		t.Errorf("%s Language: heap %q mmap %q", id, want.Language(), got.Language())
	}
	if want.Signature() != got.Signature() {
		t.Errorf("%s Signature: heap %q mmap %q", id, want.Signature(), got.Signature())
	}
	// W2 (ADR-0027): full read-only property surface parity — the mmap view's
	// PropGet/PropLookup/PropLen/PropRange/PropsSnapshot must be byte-identical to
	// the heap *graph.Entity's, incl. the module scalar/property fold.
	assertPropReadParity(t, id, want, got, []string{"module", "visibility", "empty", "id", "__absent__"})
}

// propReader is the read-only property surface shared by graph.EntityView and
// graph.RelationshipView (W2). Its method set is identical to graph.Entity's /
// Relationship's, which is the whole point: parity is asserted method-for-method.
type propReader interface {
	PropGet(key string) string
	PropLookup(key string) (string, bool)
	PropLen() int
	PropRange(f func(k, v string) bool)
	PropsSnapshot() map[string]string
}

// assertPropReadParity asserts every read-only property method returns a
// byte-identical result between the heap view (want) and the mmap view (got):
// PropLen, PropsSnapshot (deep-equal incl. nil-when-empty), PropRange (same
// key/value pairs in the same order), and PropGet/PropLookup over probeKeys.
func assertPropReadParity(t *testing.T, tag string, want, got propReader, probeKeys []string) {
	t.Helper()
	if want.PropLen() != got.PropLen() {
		t.Errorf("%s PropLen: heap %d mmap %d", tag, want.PropLen(), got.PropLen())
	}
	ws, gs := want.PropsSnapshot(), got.PropsSnapshot()
	if (ws == nil) != (gs == nil) {
		t.Errorf("%s PropsSnapshot nilness: heap nil=%v mmap nil=%v", tag, ws == nil, gs == nil)
	}
	if len(ws) != len(gs) {
		t.Errorf("%s PropsSnapshot len: heap %v mmap %v", tag, ws, gs)
	}
	for k, wv := range ws {
		if gv := gs[k]; gv != wv {
			t.Errorf("%s PropsSnapshot[%q]: heap %q mmap %q", tag, k, wv, gv)
		}
	}
	// PropRange must visit the same pairs in the same (sorted-key) order.
	type kv struct{ K, V string }
	var wr, gr []kv
	want.PropRange(func(k, v string) bool { wr = append(wr, kv{k, v}); return true })
	got.PropRange(func(k, v string) bool { gr = append(gr, kv{k, v}); return true })
	if len(wr) != len(gr) {
		t.Errorf("%s PropRange len: heap %v mmap %v", tag, wr, gr)
	} else {
		for i := range wr {
			if wr[i] != gr[i] {
				t.Errorf("%s PropRange[%d]: heap %v mmap %v", tag, i, wr[i], gr[i])
			}
		}
	}
	for _, k := range probeKeys {
		if want.PropGet(k) != got.PropGet(k) {
			t.Errorf("%s PropGet(%q): heap %q mmap %q", tag, k, want.PropGet(k), got.PropGet(k))
		}
		wv, wok := want.PropLookup(k)
		gv, gok := got.PropLookup(k)
		if wok != gok || wv != gv {
			t.Errorf("%s PropLookup(%q): heap (%q,%v) mmap (%q,%v)", tag, k, wv, wok, gv, gok)
		}
	}
}

func assertRelViewEqual(t *testing.T, i int, want, got graph.RelationshipView) {
	t.Helper()
	if want.ID() != got.ID() {
		t.Errorf("rel[%d] ID: heap %q mmap %q", i, want.ID(), got.ID())
	}
	if want.FromID() != got.FromID() {
		t.Errorf("rel[%d] FromID: heap %q mmap %q", i, want.FromID(), got.FromID())
	}
	if want.ToID() != got.ToID() {
		t.Errorf("rel[%d] ToID: heap %q mmap %q", i, want.ToID(), got.ToID())
	}
	if want.Kind() != got.Kind() {
		t.Errorf("rel[%d] Kind: heap %q mmap %q", i, want.Kind(), got.Kind())
	}
	// W2: full read-only property surface parity, incl. the id-tunneled "id"
	// property (which also backs ID()).
	assertPropReadParity(t, "rel["+strconv.Itoa(i)+"]", want, got, []string{"id", "line", "__absent__"})
}

func idsOfViews(vs []graph.EntityView) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.ID())
	}
	sort.Strings(out)
	return out
}

// TestDefaultServeFromMMapForOS pins the platform default (ADR-0027 mmap cutover,
// epic #5850): ON on every platform, including Windows, now that the
// generation-file layout removed the mapped-graph.fb rename hazard
// (ERROR_USER_MAPPED_FILE on os.Rename).
func TestDefaultServeFromMMapForOS(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if !defaultServeFromMMapForOS(goos) {
			t.Errorf("defaultServeFromMMapForOS(%q) = false, want true (default ON)", goos)
		}
	}
}

// TestParseServeFromMMapFlag pins the flag parser (the flag is read once at load
// from this pure function). Explicit tokens force ON/OFF on every platform;
// unset/""/malformed fall back to the platform default (defaultServeFromMMapForOS).
func TestParseServeFromMMapFlag(t *testing.T) {
	// Explicit truthy tokens force ON regardless of platform (Windows opt-in).
	for _, on := range []string{"1", "true", "TRUE", "yes", "on", " On "} {
		if !parseServeFromMMapFlag(on) {
			t.Errorf("parseServeFromMMapFlag(%q) = false, want true", on)
		}
	}
	// Explicit off tokens force OFF regardless of platform (opt-out everywhere).
	for _, off := range []string{"0", "false", "FALSE", "no", "off", " Off "} {
		if parseServeFromMMapFlag(off) {
			t.Errorf("parseServeFromMMapFlag(%q) = true, want false", off)
		}
	}
	// unset/""/malformed follow the current platform's default, so this assertion
	// stays correct on whatever OS CI runs it (ON on darwin/linux, OFF on Windows).
	want := defaultServeFromMMapForOS(runtime.GOOS)
	for _, def := range []string{"", "   ", "nope", "maybe"} {
		if got := parseServeFromMMapFlag(def); got != want {
			t.Errorf("parseServeFromMMapFlag(%q) = %v, want %v (platform default for %s)",
				def, got, want, runtime.GOOS)
		}
	}
}

// TestEntityViewSourceForSelectsByFlag proves the flag routes the hot-index build
// to the right source: mmap-backed when ON, heap-Document when OFF, per repo.
func TestEntityViewSourceForSelectsByFlag(t *testing.T) {
	path := writeParityGraph(t)
	rdr, err := fbreader.Open(path)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	handle := newMapHandle(rdr)
	handle.repo = "r"
	defer handle.closeOnce()
	b := &groupBorrow{handles: []*MapHandle{handle}}
	doc := &graph.Document{}

	prev := serveFromMMapEnabled
	defer func() { serveFromMMapEnabled = prev }()

	serveFromMMapEnabled = false
	if _, ok := b.entityViewSourceFor("r", doc).(docEntityViewSource); !ok {
		t.Errorf("flag OFF: source = %T, want docEntityViewSource", b.entityViewSourceFor("r", doc))
	}
	serveFromMMapEnabled = true
	if _, ok := b.entityViewSourceFor("r", doc).(mmapEntityViewSource); !ok {
		t.Errorf("flag ON: source = %T, want mmapEntityViewSource", b.entityViewSourceFor("r", doc))
	}
}

// TestMMapHotIndexReadsAreLifetimeSafeUnderReload is the F3 lifetime-race test
// (reuses the F1/F2 reload-loop harness shape). N borrowers each capture a REAL
// mmap handle under s.mu, build a hot index over the mmap zero-copy source, and
// READ zero-copy strings through it while a reloader retires handles in a tight
// loop. Under -race it proves no zero-copy string is read after its handle is
// munmapped: the captured handle stays mapped for the whole borrow (refs>0 →
// deferred unmap), and every retired mapping is unmapped exactly once.
func TestMMapHotIndexReadsAreLifetimeSafeUnderReload(t *testing.T) {
	path := writeParityGraph(t)

	s := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{}},
	}})
	lr := &LoadedRepo{Repo: "r"}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	// Each handle wraps a REAL mmap of the same graph.fb, so reads alias live
	// mapped bytes; a premature munmap under -race is a use-after-unmap.
	var closersMu sync.Mutex
	var readers []*fbreader.Reader
	newHandle := func() *MapHandle {
		rdr, err := fbreader.Open(path)
		if err != nil {
			t.Errorf("fbreader.Open: %v", err)
			return nil
		}
		closersMu.Lock()
		readers = append(readers, rdr)
		closersMu.Unlock()
		h := newMapHandle(rdr)
		h.releaseGap = runtime.Gosched // widen the rebound TOCTOU window
		return h
	}

	s.mu.Lock()
	lr.publishHandle(newHandle())
	s.mu.Unlock()

	var readFault atomic.Int64
	stop := make(chan struct{})

	var wgR sync.WaitGroup
	wgR.Add(1)
	go func() {
		defer wgR.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			h := newHandle()
			if h == nil {
				return
			}
			s.mu.Lock()
			lr.publishHandle(h) // publishes successor, retires predecessor
			s.mu.Unlock()
		}
	}()

	var wgB sync.WaitGroup
	for i := 0; i < 8; i++ {
		wgB.Add(1)
		go func() {
			defer wgB.Done()
			for j := 0; j < 500; j++ {
				b := s.borrowGroup("g")
				if b == nil {
					readFault.Add(1)
					continue
				}
				// Build the hot index over the mmap zero-copy source, keyed off
				// the captured handle, and READ zero-copy strings through it.
				hi := b.buildHotIndex("r", mmapEntityViewSource{handle: b.Handle("r")})
				v, ok := hi.entityByID("r::pkg.Foo")
				if !ok {
					readFault.Add(1)
					b.Release()
					continue
				}
				// Force actual reads of aliased mmap bytes (cold + hot fields).
				if v.Name() != "Foo" || v.Signature() != "type Foo struct{}" ||
					v.SourceFile() != "pkg/foo.go" {
					readFault.Add(1)
				}
				if mod, _ := v.PropLookup("module"); mod != "pkg" {
					readFault.Add(1)
				}
				if v.PropGet("module") != "pkg" || v.PropLen() != 3 {
					readFault.Add(1)
				}
				// PropRange reads zero-copy value strings through the borrow.
				v.PropRange(func(k, val string) bool {
					if k == "module" && val != "pkg" {
						readFault.Add(1)
					}
					return true
				})
				b.Release()
			}
		}()
	}

	wgB.Wait()
	close(stop)
	wgR.Wait()

	s.mu.Lock()
	lr.retireHandle()
	s.mu.Unlock()

	if f := readFault.Load(); f != 0 {
		t.Fatalf("read faults (wrong result or use-after-unmap): %d", f)
	}
}

// TestMMapPropsSnapshotSurvivesRelease is the load-bearing lifetime test for the
// mmap*View.PropsSnapshot HEAP-COPY contract: the returned map (unlike the
// zero-copy scalar/PropGet/PropRange accessors) must remain byte-correct AFTER
// the borrow is released AND the underlying mmap is retired+munmapped. It retains
// an entity AND a relationship snapshot under a real borrow, Release()s the
// borrow, retireHandle()s the mapping (with refs drained, retire munmaps it NOW —
// see MapHandle.retire), forces a GC, THEN reads the retained maps. If
// PropsSnapshot aliased the mmap (zeroCopyString instead of string(...)) this is
// a use-after-unmap: SIGBUS/SIGSEGV or garbage under -race. Heap-copied, it
// survives. Adversarial — meant to run under -race -count=50.
func TestMMapPropsSnapshotSurvivesRelease(t *testing.T) {
	path := writeParityGraph(t)

	s := NewState(&Registry{Groups: map[string]RegistryGroup{
		"g": {Repos: map[string]RegistryRepo{}},
	}})
	lr := &LoadedRepo{Repo: "r"}
	s.groups["g"] = &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"r": lr}}

	rdr, err := fbreader.Open(path)
	if err != nil {
		t.Fatalf("fbreader.Open: %v", err)
	}
	s.mu.Lock()
	lr.publishHandle(newMapHandle(rdr))
	s.mu.Unlock()

	// Capture the snapshots strictly UNDER the borrow (the only window in which
	// the mmap is guaranteed mapped).
	b := s.borrowGroup("g")
	if b == nil {
		t.Fatal("borrowGroup returned nil")
	}
	h := b.Handle("r")
	if h == nil {
		t.Fatal("Handle(r) returned nil")
	}
	hi := b.buildHotIndex("r", mmapEntityViewSource{handle: h})
	ev, ok := hi.entityByID("r::pkg.Foo")
	if !ok {
		t.Fatal("entityByID(r::pkg.Foo) miss")
	}
	rv := mmapRelationshipView{r: h.Reader().RelationshipAt(0)}

	entSnap := ev.PropsSnapshot()
	relSnap := rv.PropsSnapshot()

	// Drop the borrow, then retire the handle: refs is drained, so retire munmaps
	// the mapping immediately. Any lingering alias into it is now use-after-unmap.
	b.Release()
	s.mu.Lock()
	lr.retireHandle()
	s.mu.Unlock()

	// Churn the heap so a stale alias is unlikely to still find intact bytes.
	runtime.GC()

	// The retained snapshots must still be byte-correct AFTER the munmap. Reading
	// an aliased key/value here would fault; heap-copied strings are safe.
	wantEnt := map[string]string{"module": "pkg", "visibility": "public", "empty": ""}
	if len(entSnap) != len(wantEnt) {
		t.Fatalf("entity snapshot = %v, want %v", entSnap, wantEnt)
	}
	for k, want := range wantEnt {
		if got, ok := entSnap[k]; !ok || got != want {
			t.Errorf("entity snapshot[%q] = (%q,%v) after munmap, want (%q,true) (use-after-unmap?)", k, got, ok, want)
		}
	}
	wantRel := map[string]string{"id": "edge-0001", "line": "12"}
	if len(relSnap) != len(wantRel) {
		t.Fatalf("rel snapshot = %v, want %v", relSnap, wantRel)
	}
	for k, want := range wantRel {
		if got, ok := relSnap[k]; !ok || got != want {
			t.Errorf("rel snapshot[%q] = (%q,%v) after munmap, want (%q,true) (use-after-unmap?)", k, got, ok, want)
		}
	}
}
