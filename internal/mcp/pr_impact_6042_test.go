// pr_impact_6042_test.go — issue #6042: an add-only PR must get a merge-risk
// verdict instead of a decline, and the payload must make an INFERRED verdict
// impossible to mistake for a measured one.
//
// THE OVERLAY IN THESE FIXTURES IS PRODUCTION-SHAPED. It covers exactly the
// entities that existed at the last group index (svc:A, svc:B, svc:C) and
// nothing else. Entities that live only on a feature ref are absent from it by
// construction — that is the entire premise of this issue, and a fixture that
// stamped them in would certify a behaviour the code does not have.
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/graph/fbwriter"
	"github.com/cajasmota/grafel/internal/graph/groupalgo"
	"github.com/cajasmota/grafel/internal/registry"
	"github.com/cajasmota/grafel/internal/testsupport"
	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

type prImpact6042Env struct {
	srv         *Server
	overlayPath string
	curMtimes   map[string]int64
}

func prImpact6042Ent(id, file, module string) graph.Entity {
	e := graph.Entity{ID: id, Name: id, Kind: "function", SourceFile: file, Language: "go"}
	if module != "" {
		e = e.WithProperties(map[string]string{"module": module})
	}
	return e
}

// setupPRImpact6042 writes the ref graphs for one repo. The indexed group union
// (what the overlay knows about) is main:
//
//	svc:A, svc:B  — core/*.go, module "core", community 7
//	svc:C         — util/c.go, module "util",  community 9
//
// Every other ref ADDS exactly one entity that the overlay cannot possibly know
// about, differing only in which inference signal is available to it.
func setupPRImpact6042(t *testing.T) prImpact6042Env {
	t.Helper()
	testsupport.IsolateHome(t)
	root := t.TempDir()
	t.Setenv("GRAFEL_HOME", filepath.Join(root, "home"))
	t.Setenv("GRAFEL_DAEMON_ROOT", filepath.Join(root, "daemon"))

	repoPath := filepath.Join(root, "svc")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	base := func() []graph.Entity {
		return []graph.Entity{
			prImpact6042Ent("svc:A", "core/a.go", "core"),
			prImpact6042Ent("svc:B", "core/b.go", "core"),
			prImpact6042Ent("svc:C", "util/c.go", "util"),
			prImpact6042Ent("svc:D", "extra/d.go", "extra"),
		}
	}
	// mk builds a ref graph: the base entities plus zero or one added entity,
	// with the given outbound CALLS edges.
	mk := func(added graph.Entity, calls ...string) *graph.Document {
		d := &graph.Document{Version: 1, Repo: "svc", Entities: base()}
		if added.ID != "" {
			d.Entities = append(d.Entities, added)
			for _, target := range calls {
				d.Relationships = append(d.Relationships,
					graph.Relationship{FromID: added.ID, ToID: target, Kind: "CALLS"})
			}
		}
		return d
	}
	// modify perturbs one base entity's signature so DiffDocs classifies it as
	// MODIFIED — a MEASURED changed entity, since the overlay covers it.
	modify := func(d *graph.Document, idx int, sig string) *graph.Document {
		d.Entities[idx].Signature = sig
		return d
	}

	refs := map[string]*graph.Document{
		"main": mk(graph.Entity{}),
		// Signal 1: the added entity lives in an ALREADY-PLACED file.
		"inFile": mk(prImpact6042Ent("svc:NewInA", "core/a.go", "core")),
		// Signal 3: brand-new file, brand-new module, but it calls two placed
		// entities that agree on community 7.
		"viaTargets": mk(prImpact6042Ent("svc:NewT", "newpkg/t.go", "newpkg"), "svc:A", "svc:B"),
		// Nothing to infer from: new file, new module, no edges.
		"isolated": mk(prImpact6042Ent("svc:NewI", "lonely/i.go", "lonely")),
		// Measured control arms: both modify an overlay-covered entity in
		// community 7, so their overlap is observed on BOTH sides.
		"modA": modify(mk(graph.Entity{}), 0, "func A(x int)"),
		"modB": modify(mk(graph.Entity{}), 1, "func B(x int)"),
		// The D1 shape: each ref has a MEASURED community of its own (9 / 5) plus
		// an added entity inferred into 7. Their ONLY overlap is 7 — the inferred
		// one — so the conflict is manufactured while every aggregate flag reads
		// "measured".
		"mixedA": modify(mk(prImpact6042Ent("svc:NewMA", "core/a.go", "core")), 2, "func C(x int)"),
		"mixedB": modify(mk(prImpact6042Ent("svc:NewMB", "core/b.go", "core")), 3, "func D(x int)"),
	}
	for ref, doc := range refs {
		dir := daemon.StateDirForRepoRef(repoPath, ref)
		if err := fbwriter.WriteAtomic(filepath.Join(dir, "graph.fb"), doc); err != nil {
			t.Fatalf("write graph.fb for %s: %v", ref, err)
		}
	}
	if err := fbwriter.WriteAtomic(
		filepath.Join(daemon.StateDirForRepo(repoPath), "graph.fb"), mk(graph.Entity{})); err != nil {
		t.Fatalf("write HEAD graph.fb: %v", err)
	}

	cfgPath, err := registry.ConfigPathFor("acme")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	cfg := &registry.GroupConfig{Name: "acme", Repos: []registry.Repo{{Slug: "svc", Path: repoPath}}}
	if err := registry.SaveGroupConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save group config: %v", err)
	}
	if err := registry.AddGroup("acme", cfgPath); err != nil {
		t.Fatalf("add group: %v", err)
	}
	overlayPath, err := groupalgo.OverlayPath("acme")
	if err != nil {
		t.Fatalf("overlay path: %v", err)
	}
	cur, err := groupalgo.CurrentSourceMtimes("acme")
	if err != nil {
		t.Fatalf("current mtimes: %v", err)
	}
	env := prImpact6042Env{
		srv:         newTestServer(t, &graph.Document{Repo: "svc"}),
		overlayPath: overlayPath,
		curMtimes:   cur,
	}
	env.writeOverlay(t)
	return env
}

// writeOverlay covers ONLY the last-indexed group union. svc:NewInA, svc:NewT
// and svc:NewI are deliberately absent — production cannot place them, and this
// issue is entirely about that state.
func (e prImpact6042Env) writeOverlay(t *testing.T) {
	t.Helper()
	ov := &groupalgo.Overlay{
		AlgoVersion:  groupalgo.OverlayAlgoVersion,
		Group:        "acme",
		ComputedAt:   time.Now().UTC().Add(-time.Hour),
		SourceMtimes: e.curMtimes,
		Results: map[string]groupalgo.EntityOverlay{
			"svc:A": {CommunityID: 7, PageRank: 0.1},
			"svc:B": {CommunityID: 7, PageRank: 0.1},
			"svc:C": {CommunityID: 9, PageRank: 0.1},
			"svc:D": {CommunityID: 5, PageRank: 0.1},
		},
		Communities: []graph.CommunityResult{
			{ID: 5, Size: 1, AutoName: "extra"},
			{ID: 7, Size: 2, AutoName: "core"},
			{ID: 9, Size: 1, AutoName: "util"},
		},
	}
	if err := groupalgo.WriteOverlayTo(e.overlayPath, ov); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
}

func (e prImpact6042Env) conflicts(t *testing.T, refs ...string) *mcpapi.CallToolResult {
	t.Helper()
	rs := make([]any, len(refs))
	for i, r := range refs {
		rs[i] = r
	}
	return callHandlerResult(t, e.srv.handlePRImpact, map[string]any{
		"group": "acme", "repo": "svc", "base": "main", "refs": rs,
	})
}

func (e prImpact6042Env) single(t *testing.T, head string) *mcpapi.CallToolResult {
	t.Helper()
	return callHandlerResult(t, e.srv.handlePRImpact, map[string]any{
		"group": "acme", "repo": "svc", "base": "main", "head": head,
	})
}

type prImpact6042Payload struct {
	RiskyPairCount            int      `json:"risky_pair_count"`
	CommunityDataAvailable    bool     `json:"community_data_available"`
	CommunityDataInferredOnly bool     `json:"community_data_inferred_only"`
	InferredEntityCount       int      `json:"inferred_entity_count"`
	InferredOnlyPairCount     int      `json:"inferred_only_pair_count"`
	CommunityDataNote         string   `json:"community_data_note"`
	RefsWithInferred          []string `json:"refs_with_inferred_community_data"`
	ChangedCount              int      `json:"changed_count"`
	ChangedUncovered          int      `json:"changed_entities_without_community"`
	ChangedOverlay            int      `json:"changed_entities_with_overlay_community"`
	ChangedInferred           int      `json:"changed_entities_with_inferred_community"`
	RiskPairs                 []struct {
		SharedCommunities         []int `json:"shared_communities"`
		InferredSharedCommunities []int `json:"inferred_shared_communities"`
		InferredOnly              bool  `json:"inferred_only"`
	} `json:"risk_pairs"`
	PerRef []struct {
		Ref                 string `json:"ref"`
		ImpactedCommunities []int  `json:"impacted_communities"`
		InferredCommunities []int  `json:"inferred_communities"`
		ChangedOverlay      int    `json:"changed_entities_with_overlay_community"`
		ChangedInferred     int    `json:"changed_entities_with_inferred_community"`
		Uncovered           int    `json:"changed_entities_without_community"`
	} `json:"per_ref"`
	ChangedEntities []struct {
		ID                 string `json:"id"`
		CommunityID        int    `json:"community_id"`
		CommunitySource    string `json:"community_source"`
		CommunityInference *struct {
			Signals []string `json:"signals"`
			Support int      `json:"support"`
			Sample  int      `json:"sample"`
		} `json:"community_inference"`
	} `json:"changed_entities"`
	ImpactedCommunities []struct {
		CommunityID  int  `json:"community_id"`
		InferredOnly bool `json:"inferred_only"`
	} `json:"impacted_communities"`
}

func must6042Payload(t *testing.T, res *mcpapi.CallToolResult) prImpact6042Payload {
	t.Helper()
	if res == nil || res.IsError {
		t.Fatalf("expected a successful result, got: %s", resultText(res))
	}
	var p prImpact6042Payload
	if err := json.Unmarshal([]byte(resultText(res)), &p); err != nil {
		t.Fatalf("unmarshal payload: %v\n%s", err, resultText(res))
	}
	return p
}

// THE #6042 test. Two refs that only ADD entities — the shape #6006 declined —
// now produce a verdict, because both added entities can be inferred into
// community 7 from their placed neighbours. The verdict must be flagged as
// resting entirely on inference.
func TestPRImpact6042_AddOnlyRefsGetAnInferredVerdict(t *testing.T) {
	env := setupPRImpact6042(t)

	res := env.conflicts(t, "inFile", "viaTargets")
	p := must6042Payload(t, res)

	if p.RiskyPairCount != 1 || len(p.RiskPairs) != 1 ||
		len(p.RiskPairs[0].SharedCommunities) != 1 || p.RiskPairs[0].SharedCommunities[0] != 7 {
		t.Fatalf("both refs infer into community 7; want 1 risky pair sharing [7], got %+v", p)
	}
	if !p.CommunityDataAvailable {
		t.Errorf("an inferred placement is still a placement; community_data_available = false")
	}
	if !p.CommunityDataInferredOnly {
		t.Errorf("NOTHING in this verdict was measured; community_data_inferred_only = false — " +
			"the caller cannot tell this from a verdict the group partition actually produced")
	}
	if p.InferredEntityCount != 2 {
		t.Errorf("inferred_entity_count = %d, want 2", p.InferredEntityCount)
	}
	if len(p.PerRef) != 2 {
		t.Fatalf("want 2 per-ref entries, got %+v", p.PerRef)
	}
	for _, r := range p.PerRef {
		if r.ChangedInferred != 1 || r.ChangedOverlay != 0 || r.Uncovered != 0 {
			t.Errorf("per_ref %s = overlay %d / inferred %d / none %d, want 0/1/0",
				r.Ref, r.ChangedOverlay, r.ChangedInferred, r.Uncovered)
		}
		// Conflicts mode emits no changed_entities, so this is the ONLY place a
		// caller can see which communities this ref reached by inference.
		if len(r.InferredCommunities) != 1 || r.InferredCommunities[0] != 7 {
			t.Errorf("per_ref %s inferred_communities = %v, want [7]", r.Ref, r.InferredCommunities)
		}
	}
	// The pair itself must be marked, not just the verdict.
	if !p.RiskPairs[0].InferredOnly || len(p.RiskPairs[0].InferredSharedCommunities) != 1 {
		t.Errorf("the shared community is inferred on both sides; risk pair must say so: %+v",
			p.RiskPairs[0])
	}
	if p.InferredOnlyPairCount != 1 {
		t.Errorf("inferred_only_pair_count = %d, want 1", p.InferredOnlyPairCount)
	}
	// Structured flags are the contract, but the prose note is what stops an
	// agent reading the verdict as a measurement.
	if !strings.Contains(strings.ToLower(p.CommunityDataNote), "inferred") {
		t.Errorf("community_data_note missing or unhelpful: %q", p.CommunityDataNote)
	}
	if !reflect.DeepEqual(p.RefsWithInferred, []string{"inFile", "viaTargets"}) {
		t.Errorf("refs_with_inferred_community_data = %v, want [inFile viaTargets]", p.RefsWithInferred)
	}
}

// THE D1 REGRESSION, end to end. Both refs modify an overlay-covered entity, so
// both carry MEASURED communities and every aggregate flag reads "measured" —
// community_data_inferred_only is legitimately false. But their measured
// communities (9 and 5) do not overlap: the ONLY shared community is 7, which
// each ref reaches solely through an entity the partition has never seen.
//
// The reported conflict is therefore entirely manufactured, and a whole-verdict
// flag cannot say so. Without per-pair provenance an agent reads a fabricated
// conflict as measured — the #6006 defect class at the granularity a merge
// decision is actually made at.
func TestPRImpact6042_ConflictRestingOnlyOnInferenceIsMarkedPerPair(t *testing.T) {
	env := setupPRImpact6042(t)

	p := must6042Payload(t, env.conflicts(t, "mixedA", "mixedB"))

	if p.RiskyPairCount != 1 || len(p.RiskPairs) != 1 {
		t.Fatalf("want exactly 1 risky pair, got %+v", p)
	}
	pair := p.RiskPairs[0]
	if !reflect.DeepEqual(pair.SharedCommunities, []int{7}) {
		t.Fatalf("fixture must overlap on community 7 only, got %v — the test proves nothing otherwise",
			pair.SharedCommunities)
	}
	// The precondition that makes this dangerous: the verdict looks measured.
	if p.CommunityDataInferredOnly {
		t.Fatalf("precondition: both refs modify overlay-covered entities, so the verdict "+
			"is not inferred-only; got %+v", p)
	}
	for _, r := range p.PerRef {
		if r.ChangedOverlay != 1 || r.ChangedInferred != 1 {
			t.Fatalf("precondition: per_ref %s must have 1 measured + 1 inferred entity, got %+v", r.Ref, r)
		}
	}

	if !pair.InferredOnly {
		t.Errorf("every shared community is reached only by inference, so this reported conflict " +
			"is DEDUCED — risk_pairs[].inferred_only = false lets an agent read it as measured")
	}
	if !reflect.DeepEqual(pair.InferredSharedCommunities, []int{7}) {
		t.Errorf("inferred_shared_communities = %v, want [7]", pair.InferredSharedCommunities)
	}
	if p.InferredOnlyPairCount != 1 {
		t.Errorf("inferred_only_pair_count = %d, want 1", p.InferredOnlyPairCount)
	}
	// And the note must fire even though the verdict-level flag is false.
	if !strings.Contains(strings.ToLower(p.CommunityDataNote), "deduced") {
		t.Errorf("a manufactured conflict must be called out in prose too; note = %q", p.CommunityDataNote)
	}
}

// The measured control arm for the pair marker: two refs that overlap on a
// community they BOTH really touch must not be marked deduced. Without this,
// "inferred_only: true" everywhere would satisfy the test above while carrying
// no information at all.
func TestPRImpact6042_MeasuredConflictIsNotMarkedInferred(t *testing.T) {
	env := setupPRImpact6042(t)

	// modA modifies svc:A and modB modifies svc:B — both overlay-covered, both in
	// community 7. Nothing here is inferred on either side.
	p := must6042Payload(t, env.conflicts(t, "modA", "modB"))
	if p.RiskyPairCount != 1 {
		t.Fatalf("want 1 risky pair on community 7, got %+v", p)
	}
	if p.RiskPairs[0].InferredOnly || len(p.RiskPairs[0].InferredSharedCommunities) != 0 {
		t.Errorf("both refs reach community 7 by MEASUREMENT; the overlap is real: %+v", p.RiskPairs[0])
	}
	if p.InferredOnlyPairCount != 0 || p.InferredEntityCount != 0 {
		t.Errorf("inferred_only_pair_count = %d / inferred_entity_count = %d, want 0/0",
			p.InferredOnlyPairCount, p.InferredEntityCount)
	}
	if p.CommunityDataNote != "" {
		t.Errorf("a fully measured conflict must carry no inference note: %q", p.CommunityDataNote)
	}
	for _, r := range p.PerRef {
		if len(r.InferredCommunities) != 0 {
			t.Errorf("per_ref %s inferred_communities = %v, want []", r.Ref, r.InferredCommunities)
		}
	}
}

// One inferred SIDE is enough to make the overlap deduced: modA really touches
// community 7, but mixedA only gets there through an entity the partition has
// never seen, so the claim "these two refs collide in community 7" is half
// guess. It must be marked even though one side is solid.
func TestPRImpact6042_OneInferredSideStillMarksTheConflict(t *testing.T) {
	env := setupPRImpact6042(t)

	p := must6042Payload(t, env.conflicts(t, "modA", "mixedA"))
	if p.RiskyPairCount != 1 || !reflect.DeepEqual(p.RiskPairs[0].SharedCommunities, []int{7}) {
		t.Fatalf("fixture must produce exactly one overlap, on community 7: %+v", p.RiskPairs)
	}
	if !p.RiskPairs[0].InferredOnly {
		t.Errorf("mixedA reaches community 7 only by inference, so the OVERLAP was never observed: %+v",
			p.RiskPairs[0])
	}
}

// The control arm that proves the marker discriminates: a ref that MODIFIES an
// overlay-covered entity is measured, and must NOT be flagged as inferred.
func TestPRImpact6042_MeasuredVerdictIsNotFlaggedAsInferred(t *testing.T) {
	env := setupPRImpact6042(t)

	p := must6042Payload(t, env.conflicts(t, "modA", "inFile"))
	if !p.CommunityDataAvailable {
		t.Fatalf("modA touches an overlay-covered entity; community data must be available")
	}
	if p.CommunityDataInferredOnly {
		t.Errorf("modA's placement was MEASURED; community_data_inferred_only must be false")
	}
	if p.InferredEntityCount != 1 {
		t.Errorf("inferred_entity_count = %d, want 1 (inFile's added entity)", p.InferredEntityCount)
	}
}

// #6006's decline path must survive. A ref whose added entity has no placed
// neighbours at all cannot be inferred, and one uninferrable ref still taints
// the whole merge-risk verdict.
func TestPRImpact6042_UninferrableRefStillDeclines(t *testing.T) {
	env := setupPRImpact6042(t)

	res := env.conflicts(t, "isolated", "inFile")
	if res == nil {
		t.Fatal("nil result")
	}
	text := resultText(res)
	if !res.IsError {
		t.Fatalf("the isolated ref cannot be placed or inferred, so merge risk was not computed; "+
			"must not return a payload: %s", text)
	}
	if !strings.Contains(text, "isolated") {
		t.Errorf("error must name the ref that could not be placed, got: %s", text)
	}
	// The explanation must say inference was attempted — otherwise a caller reads
	// this as "grafel never tried" and reindexes for nothing.
	if !strings.Contains(strings.ToLower(text), "infer") {
		t.Errorf("error must say that inference from placed neighbours was attempted and failed, got: %s", text)
	}
	var payload map[string]any
	if json.Unmarshal([]byte(text), &payload) == nil {
		if v, ok := payload["risky_pair_count"]; ok {
			t.Errorf("uncomputed merge risk still emitted risky_pair_count=%v", v)
		}
	}
}

// Single mode must label EVERY changed entity with how it was placed, and carry
// the same verdict-level marker.
func TestPRImpact6042_SingleModeLabelsInferencePerEntity(t *testing.T) {
	env := setupPRImpact6042(t)

	// Inferred: the added entity sits in an already-placed file.
	p := must6042Payload(t, env.single(t, "inFile"))
	if p.ChangedCount != 1 {
		t.Fatalf("fixture must change exactly one entity, got %+v", p.ChangedEntities)
	}
	got := p.ChangedEntities[0]
	if got.ID != "svc:NewInA" || got.CommunityID != 7 || got.CommunitySource != "inferred" {
		t.Errorf("changed entity = %+v, want svc:NewInA / community 7 / source inferred", got)
	}
	// The margin must survive to the wire: "inferred" alone cannot tell a file
	// consensus from a coin flip.
	if got.CommunityInference == nil || len(got.CommunityInference.Signals) == 0 ||
		got.CommunityInference.Sample == 0 {
		t.Errorf("community_inference missing from the payload: %+v", got.CommunityInference)
	}
	if !strings.Contains(strings.ToLower(p.CommunityDataNote), "inferred") {
		t.Errorf("single mode must carry the prose note when the whole verdict is inferred; got %q",
			p.CommunityDataNote)
	}
	// The impacted community carries its own inference marker too.
	if len(p.ImpactedCommunities) != 1 || !p.ImpactedCommunities[0].InferredOnly {
		t.Errorf("impacted_communities must mark community 7 as inference-only: %+v",
			p.ImpactedCommunities)
	}
	if !p.CommunityDataAvailable || !p.CommunityDataInferredOnly {
		t.Errorf("want available=true inferred_only=true, got %v/%v",
			p.CommunityDataAvailable, p.CommunityDataInferredOnly)
	}
	if p.ChangedInferred != 1 || p.ChangedOverlay != 0 || p.ChangedUncovered != 0 {
		t.Errorf("counts = overlay %d / inferred %d / none %d, want 0/1/0",
			p.ChangedOverlay, p.ChangedInferred, p.ChangedUncovered)
	}

	// Measured: the modified entity is in the overlay.
	m := must6042Payload(t, env.single(t, "modA"))
	if len(m.ChangedEntities) == 0 || m.ChangedEntities[0].CommunitySource != "overlay" {
		t.Errorf("modA's changed entity should be sourced from the overlay, got %+v", m.ChangedEntities)
	}
	if m.CommunityDataInferredOnly {
		t.Errorf("modA is measured; community_data_inferred_only must be false")
	}

	// Neither: nothing to infer from. Single mode still returns a payload (the
	// blast radius is valid) but must not claim any placement.
	i := must6042Payload(t, env.single(t, "isolated"))
	if len(i.ChangedEntities) == 0 || i.ChangedEntities[0].CommunitySource != "none" {
		t.Errorf("the isolated added entity has no placed neighbours; want source none, got %+v",
			i.ChangedEntities)
	}
	if i.CommunityDataAvailable || i.CommunityDataInferredOnly {
		t.Errorf("nothing was placed or inferred; want available=false inferred_only=false, got %v/%v",
			i.CommunityDataAvailable, i.CommunityDataInferredOnly)
	}
	if i.ChangedUncovered != 1 {
		t.Errorf("changed_entities_without_community = %d, want 1", i.ChangedUncovered)
	}
}

// Inference reads the graph the handler already loaded and the overlay it
// already parsed — it must not add an overlay read per call.
func TestPRImpact6042_InferenceDoesNotReparseTheOverlay(t *testing.T) {
	env := setupPRImpact6042(t)

	_ = env.conflicts(t, "inFile", "viaTargets") // prime
	overlayCacheMu.Lock()
	before := overlayCacheHits
	overlayCacheMu.Unlock()

	for i := 0; i < 2; i++ {
		_ = must6042Payload(t, env.conflicts(t, "inFile", "viaTargets"))
	}

	overlayCacheMu.Lock()
	got := overlayCacheHits - before
	overlayCacheMu.Unlock()
	if got != 2 {
		t.Errorf("expected 2 overlay cache hits across 2 repeat calls, got %d", got)
	}
}
