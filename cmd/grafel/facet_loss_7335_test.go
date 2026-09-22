package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/cross/ormlink"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Issue #7335 — buildDocument's #4406 dedup keys on
// graph.EntityID(repo, Kind, Name, SourceFile), which is blind to Subtype AND
// StartLine, and takes the first writer. The loser is not a duplicate row
// anybody can find later: it is a MISSING entity. These tests grade the
// diagnostic that makes that loss observable and attributable.
//
// Every "does not fire" test below carries a POSITIVE CONTROL in the same
// function: the same fixture with the by-design repair defeated, which MUST
// fire. Without that, a silent verdict is indistinguishable from an instrument
// that never ran — which is the exact failure mode #7335 is about.

// facetLossOnly returns the single attributed record, failing when the count is
// not exactly one. Asserting the content of what was found is the point; a
// count floor would be satisfied by the wrong answer.
func facetLossOnly(t *testing.T, s facetLossStats) facetLossRecord {
	t.Helper()
	if s.Conflicts != 1 || len(s.Records) != 1 || s.LostRecords != 1 || s.LostEntities != 1 {
		t.Fatalf("facetLoss = %d conflicts / %d lost_records / %d lost_entities / %d attributed; "+
			"want exactly 1 of each (records: %+v)",
			s.Conflicts, s.LostRecords, s.LostEntities, len(s.Records), s.Records)
	}
	return s.Records[0]
}

// A `consumes_api` carrier and the file entity for the same file differ in
// NOTHING but Subtype: same Kind, same Name (the file path), same SourceFile,
// no line. Every sort key ties, so which facet survives is pure emission order
// — and the survivor's Properties["subtype"] already says "file", so even the
// property gap-fill cannot record that a consumes_api facet ever existed.
func TestFacetLoss_ConsumesAPIVsFileEntity_IsAttributed(t *testing.T) {
	const (
		repo = "facetloss_repo"
		file = "src/api/client.ts"
	)
	fileEntity := types.EntityRecord{
		Kind: "SCOPE.Component", Name: file, SourceFile: file,
		Subtype:    "file",
		Language:   "typescript",
		Properties: map[string]string{"subtype": "file"},
	}
	consumer := types.EntityRecord{
		Kind: "SCOPE.Component", Name: file, SourceFile: file,
		Subtype:  "consumes_api",
		Language: "typescript",
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{fileEntity, consumer}
	doc := idx.buildDocument(&pass1, nil, nil, nil)

	got := facetLossOnly(t, idx.facetLoss)
	want := facetLossRecord{
		Repo:       repo,
		EntityID:   graph.EntityID(repo, "SCOPE.Component", file, file),
		Kind:       "SCOPE.Component",
		Name:       file,
		SourceFile: file,
		Field:      "subtype",
		Survivor:   "file",
		Dropped:    "consumes_api",
	}
	if got != want {
		t.Errorf("attributed record =\n  %+v\nwant\n  %+v", got, want)
	}
	if n := idx.facetLoss.ByField["subtype"]; n != 1 {
		t.Errorf("ByField[subtype] = %d; want 1", n)
	}

	// The loss is real: the graph holds ONE node, and its subtype is the
	// survivor's. That is the destroyed facet the diagnostic just named.
	e := findEntity(t, doc.Entities, want.EntityID)
	if e.Subtype != "file" {
		t.Errorf("survivor Subtype = %q; want %q", e.Subtype, "file")
	}
	if v, ok := e.PropLookup("subtype"); !ok || v != "file" {
		t.Errorf("survivor Properties[subtype] = %q (present=%v); the property gap-fill cannot "+
			"recover the lost consumes_api facet, which is why the diagnostic must", v, ok)
	}
}

// The #6440 shape: two declarations of one name in one file at different lines
// hash to one id. That collision destroyed a node AND minted a phantom
// self-CALLS edge, and nothing counted it.
func TestFacetLoss_OverloadCollision_ReportsBothLines(t *testing.T) {
	const (
		repo = "facetloss_repo"
		file = "StructPtr.vb"
		name = "StructPtr.Dispose"
	)
	withArg := types.EntityRecord{
		Kind: "SCOPE.Function", Name: name, SourceFile: file,
		Subtype: "method", StartLine: 41, EndLine: 48,
	}
	noArg := types.EntityRecord{
		Kind: "SCOPE.Function", Name: name, SourceFile: file,
		Subtype: "method", StartLine: 61, EndLine: 64,
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{withArg, noArg}
	idx.buildDocument(&pass1, nil, nil, nil)

	if idx.facetLoss.Conflicts != 2 {
		t.Fatalf("facetLoss.Conflicts = %d; want 2 (start_line and end_line both conflict): %+v",
			idx.facetLoss.Conflicts, idx.facetLoss.Records)
	}
	byField := map[string]facetLossRecord{}
	for _, r := range idx.facetLoss.Records {
		byField[r.Field] = r
	}
	start, ok := byField["start_line"]
	if !ok {
		t.Fatalf("no start_line conflict recorded: %+v", idx.facetLoss.Records)
	}
	if start.Survivor != "41" || start.Dropped != "61" {
		t.Errorf("start_line survivor/dropped = %q/%q; want 41/61 — both declarations' lines "+
			"must be named, or the report cannot point at the second overload",
			start.Survivor, start.Dropped)
	}
	if start.Name != name || start.SourceFile != file || start.Kind != "SCOPE.Function" || start.Repo != repo {
		t.Errorf("attribution = %+v; want repo/kind/name/file %s/%s/%s/%s",
			start, repo, "SCOPE.Function", name, file)
	}
	if end := byField["end_line"]; end.Survivor != "48" || end.Dropped != "64" {
		t.Errorf("end_line survivor/dropped = %q/%q; want 48/64", end.Survivor, end.Dropped)
	}
	// Subtype is identical on both records: a facet that is not lost must not
	// be reported, or the instrument is noise.
	if _, reported := byField["subtype"]; reported {
		t.Errorf("subtype reported for two records that agree on it: %+v", idx.facetLoss.Records)
	}
}

// BY-DESIGN, REPAIRED: the #6275 ormlink sentinel. The sentinel's synthetic
// Subtype/QualifiedName are blanked before the gap-fill so the real class's
// values are promoted — the facet is NOT lost, and the diagnostic must stay
// silent. The positive control defeats the repair by giving the first writer a
// non-sentinel subtype; the identical collision then fires.
func TestFacetLoss_SilentOnOrmlinkSentinelRepair(t *testing.T) {
	const (
		repo = "facetloss_repo"
		file = "com/example/demo/model/User.java"
		name = "User"
	)
	realClass := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype:       "class",
		QualifiedName: "com.example.demo.model.User",
		StartLine:     7, EndLine: 20,
	}
	sentinel := func(subtype string) types.EntityRecord {
		return types.EntityRecord{
			Kind: "SCOPE.Component", Name: name, SourceFile: file,
			Subtype:       subtype,
			QualifiedName: "scope:ormmodel:" + file + "#" + name,
			StartLine:     0, EndLine: 0,
		}
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{sentinel(ormlink.SubtypeSentinel), realClass}
	doc := idx.buildDocument(&pass1, nil, nil, nil)
	if idx.facetLoss.Conflicts != 0 {
		t.Errorf("sentinel drop reported %d losses; the #6275 repair promotes the real class's "+
			"subtype and qualified_name, so nothing is lost: %+v", idx.facetLoss.Conflicts, idx.facetLoss.Records)
	}
	if e := findEntity(t, doc.Entities, graph.EntityID(repo, "SCOPE.Component", name, file)); e.Subtype != "class" {
		t.Fatalf("precondition broken: survivor Subtype = %q; want class (the #6275 repair)", e.Subtype)
	}

	// POSITIVE CONTROL — same pair, repair not applicable (first writer is not
	// a sentinel). The collision is otherwise identical, and it must fire.
	ctl := &Indexer{repoTag: repo}
	ctlPass1 := []types.EntityRecord{sentinel("orm_model"), realClass}
	ctl.buildDocument(&ctlPass1, nil, nil, nil)
	if ctl.facetLoss.Conflicts == 0 {
		t.Fatal("positive control did not fire: the silence above proves nothing, because an " +
			"inert diagnostic reports zero for this fixture too")
	}
	got := map[string][2]string{}
	for _, r := range ctl.facetLoss.Records {
		got[r.Field] = [2]string{r.Survivor, r.Dropped}
	}
	if v := got["subtype"]; v != [2]string{"orm_model", "class"} {
		t.Errorf("control subtype survivor/dropped = %v; want [orm_model class]", v)
	}
	if v := got["qualified_name"]; v != [2]string{"scope:ormmodel:" + file + "#" + name, "com.example.demo.model.User"} {
		t.Errorf("control qualified_name survivor/dropped = %v; want the sentinel anchor then the real FQN", v)
	}
}

// BY-DESIGN, PREVENTED: the #1613 class-hierarchy shadow. The fold runs BEFORE
// the assembly loop and drops the shadow (re-homing its edges), so the pair
// never collides at the dedup site and there is nothing to report. The
// positive control disables the fold via the documented escape hatch — the very
// same records then collide and the diagnostic fires, which is what proves the
// silence above is the fold's doing and not the instrument's.
func TestFacetLoss_SilentOnClassHierarchyShadowFold(t *testing.T) {
	const (
		repo = "facetloss_repo"
		file = "app/models/order.py"
		name = "Order"
	)
	shadow := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype:   "class",
		StartLine: 1, EndLine: 1,
		Properties: map[string]string{"provenance": "INFERRED_FROM_CLASS_HIERARCHY"},
	}
	astNode := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype:   "class",
		StartLine: 12, EndLine: 40,
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{shadow, astNode}
	idx.buildDocument(&pass1, nil, nil, nil)
	if idx.facetLoss.Conflicts != 0 {
		t.Errorf("hierarchy-shadow drop reported %d losses; foldClassHierarchyShadows drops the "+
			"shadow before assembly and re-homes its edges, so this is a by-design drop: %+v",
			idx.facetLoss.Conflicts, idx.facetLoss.Records)
	}

	// POSITIVE CONTROL — same two records, fold disabled.
	t.Setenv("GRAFEL_DISABLE_1613_FOLD", "1")
	ctl := &Indexer{repoTag: repo}
	ctlPass1 := []types.EntityRecord{shadow, astNode}
	ctl.buildDocument(&ctlPass1, nil, nil, nil)
	if ctl.facetLoss.Conflicts == 0 {
		t.Fatal("positive control did not fire with the #1613 fold disabled: the silence above " +
			"proves nothing about the fold")
	}
	for _, r := range ctl.facetLoss.Records {
		if r.Field == "start_line" && (r.Survivor != "1" || r.Dropped != "12") {
			t.Errorf("control start_line survivor/dropped = %q/%q; want 1/12", r.Survivor, r.Dropped)
		}
	}
}

// CONTROL for the classification's other direction: a collision whose dropped
// record carries ONLY fields the survivor left empty is fully repaired by the
// gap-fill, so it is not a loss and must not be reported. If this fires, the
// detector is reporting every drop, which is the noise failure mode.
func TestFacetLoss_SilentWhenGapFillRecoversEverything(t *testing.T) {
	const (
		repo = "facetloss_repo"
		file = "src/svc.go"
		name = "Svc"
	)
	first := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		Subtype: "struct", StartLine: 10, EndLine: 14,
	}
	second := types.EntityRecord{
		Kind: "SCOPE.Component", Name: name, SourceFile: file,
		QualifiedName: "pkg/svc.Svc",
		Signature:     "type Svc struct",
	}

	idx := &Indexer{repoTag: repo}
	pass1 := []types.EntityRecord{first, second}
	doc := idx.buildDocument(&pass1, nil, nil, nil)
	if idx.facetLoss.Conflicts != 0 {
		t.Errorf("fully-recoverable drop reported %d losses: %+v", idx.facetLoss.Conflicts, idx.facetLoss.Records)
	}
	e := findEntity(t, doc.Entities, graph.EntityID(repo, "SCOPE.Component", name, file))
	if e.QualifiedName != "pkg/svc.Svc" || e.Signature != "type Svc struct" {
		t.Fatalf("precondition broken: gap-fill did not carry the dropped record's fields "+
			"(qname=%q sig=%q)", e.QualifiedName, e.Signature)
	}
}

// The report is the operator-facing half of the instrument: it must name the
// pair, not just a count, and it must say nothing at all when there is nothing
// to say (an unconditional "0 losses" line is indistinguishable from a
// diagnostic that never ran).
func TestFacetLoss_ReportNamesThePairAndIsSilentWhenClean(t *testing.T) {
	var clean bytes.Buffer
	(&facetLossStats{}).report(&clean)
	if clean.Len() != 0 {
		t.Errorf("clean report wrote %q; want nothing", clean.String())
	}

	s := &facetLossStats{}
	s.observe("r1", "deadbeef", &types.EntityRecord{
		Kind: "SCOPE.Component", Name: "src/api/client.ts", SourceFile: "src/api/client.ts",
	}, []facetLossRecord{{Field: "subtype", Survivor: "file", Dropped: "consumes_api"}})
	var out bytes.Buffer
	s.report(&out)
	got := out.String()
	for _, want := range []string{
		"assembly-facet-loss: drops=1 lost_records=1 lost_entities=1 conflicts=1 attributed=1 by_field: subtype=1\n",
		`assembly-facet-loss:   repo=r1 id=deadbeef kind=SCOPE.Component name=src/api/client.ts file=src/api/client.ts field=subtype survivor="file" dropped="consumes_api"` + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing line\n  %q\ngot\n%s", want, got)
		}
	}
}

// The attribution is capped; the COUNT is not. A cap that silently truncated
// the total would turn the report into a floor, and a floor is satisfied by
// the wrong answers.
func TestFacetLoss_CapBoundsAttributionNotTheCount(t *testing.T) {
	s := &facetLossStats{}
	rec := &types.EntityRecord{Kind: "K", Name: "N", SourceFile: "f"}
	for i := 0; i < facetLossSampleCap+7; i++ {
		s.observe("r", "id", rec, []facetLossRecord{{Field: "subtype", Survivor: "a", Dropped: "b"}})
	}
	if s.Conflicts != facetLossSampleCap+7 {
		t.Errorf("Conflicts = %d; want %d — the counter must not be capped", s.Conflicts, facetLossSampleCap+7)
	}
	if s.LostRecords != facetLossSampleCap+7 {
		t.Errorf("LostRecords = %d; want %d — the destroyed-thing counter must not be capped either",
			s.LostRecords, facetLossSampleCap+7)
	}
	if s.Drops != facetLossSampleCap+7 {
		t.Errorf("Drops = %d; want %d", s.Drops, facetLossSampleCap+7)
	}
	if s.LostEntities != 1 {
		t.Errorf("LostEntities = %d; want 1 — every observe used the same id, and conflating "+
			"distinct ids with repeated losses on one id is exactly the conflation this split fixes",
			s.LostEntities)
	}
	if s.ByField["subtype"] != facetLossSampleCap+7 {
		t.Errorf("ByField[subtype] = %d; want %d", s.ByField["subtype"], facetLossSampleCap+7)
	}
	if len(s.Records) != facetLossSampleCap {
		t.Errorf("attributed records = %d; want the cap %d", len(s.Records), facetLossSampleCap)
	}
}

// Stats belong to ONE buildDocument call. A field that accumulated across calls
// would attribute one run's losses to the next.
func TestFacetLoss_ResetPerBuildDocument(t *testing.T) {
	const repo = "facetloss_repo"
	colliding := []types.EntityRecord{
		{Kind: "SCOPE.Component", Name: "A", SourceFile: "a.ts", Subtype: "file"},
		{Kind: "SCOPE.Component", Name: "A", SourceFile: "a.ts", Subtype: "consumes_api"},
	}
	idx := &Indexer{repoTag: repo}
	p1 := append([]types.EntityRecord(nil), colliding...)
	idx.buildDocument(&p1, nil, nil, nil)
	if idx.facetLoss.Conflicts != 1 {
		t.Fatalf("precondition: first run Total = %d; want 1", idx.facetLoss.Conflicts)
	}
	p2 := []types.EntityRecord{{Kind: "SCOPE.Component", Name: "B", SourceFile: "b.ts", Subtype: "file"}}
	idx.buildDocument(&p2, nil, nil, nil)
	if idx.facetLoss.Conflicts != 0 || len(idx.facetLoss.Records) != 0 ||
		idx.facetLoss.LostRecords != 0 || idx.facetLoss.LostEntities != 0 || idx.facetLoss.Drops != 0 {
		t.Errorf("second (clean) run carries drops=%d lost_records=%d lost_entities=%d "+
			"conflicts=%d / %d records from the first",
			idx.facetLoss.Drops, idx.facetLoss.LostRecords, idx.facetLoss.LostEntities,
			idx.facetLoss.Conflicts, len(idx.facetLoss.Records))
	}
}
