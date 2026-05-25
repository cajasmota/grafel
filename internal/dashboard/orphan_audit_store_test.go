package dashboard

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/quality/audit"
)

func TestOrphanAuditStore_RoundTrip(t *testing.T) {
	root := t.TempDir()
	group := "g1"

	if _, ok := loadOrphanAudit(root, group); ok {
		t.Fatal("expected no persisted audit before any run")
	}

	reply := OrphanAuditReply{
		Group:       group,
		HasRun:      true,
		AuditedAt:   "2026-05-22T00:00:00Z",
		HealthScore: 72,
		Total:       OrphanTotals{Entities: 100, Orphans: 10, OrphanRate: 0.1},
	}
	if err := saveOrphanAudit(root, group, reply); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, ok := loadOrphanAudit(root, group)
	if !ok {
		t.Fatal("expected persisted audit after save")
	}
	if !got.HasRun || got.HealthScore != 72 || got.Total.Entities != 100 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestOrphanAuditPath_NoTraversal(t *testing.T) {
	root := filepath.FromSlash("/root")
	want := filepath.Join(root, "orphan-audits", "passwd.json")
	p := orphanAuditPath(root, "../../etc/passwd")
	if p != want {
		t.Fatalf("path traversal not sanitised: got %q want %q", p, want)
	}
}

// buildOrphanAuditReply must report REAL per-kind totals (not the orphan count)
// and derive a composite health score + fidelity from measured rates (#1574).
func TestBuildOrphanAuditReply_PerKindAndScores(t *testing.T) {
	repos := []*audit.RepoReport{
		{
			Path:     "/x/repoA",
			Entities: 200,
			Orphans:  20,
			TopKinds: []audit.KVCount{
				{Key: "Function", Count: 120},
				{Key: "Class", Count: 80},
			},
			TopOrphanKinds: []audit.KVCount{
				{Key: "Function", Count: 15},
				{Key: "Class", Count: 5},
			},
			ImportsTotal: 100,
			ImportsToIDFormat: map[audit.ImportFormat]int{
				audit.ImportFormatHex: 70, // 30 unresolved → bug_rate 30% → fidelity 0.70
			},
		},
	}
	r := buildOrphanAuditReply("g", repos)

	if r.Total.Entities != 200 || r.Total.Orphans != 20 {
		t.Fatalf("totals wrong: %+v", r.Total)
	}
	// Per-kind must carry TOTAL entities, not orphan counts.
	var fn *KindStat
	for i := range r.PerKind {
		if r.PerKind[i].Kind == "Function" {
			fn = &r.PerKind[i]
		}
	}
	if fn == nil {
		t.Fatal("Function kind missing")
	}
	if fn.Entities != 120 || fn.Orphans != 15 {
		t.Fatalf("per-kind not real: entities=%d orphans=%d (want 120/15)", fn.Entities, fn.Orphans)
	}
	if fn.Count != fn.Entities {
		t.Fatalf("back-compat Count should mirror Entities: %d vs %d", fn.Count, fn.Entities)
	}
	// Fidelity = 100 − bug_rate(30) = 0.70.
	if r.Fidelity == nil || *r.Fidelity < 0.69 || *r.Fidelity > 0.71 {
		t.Fatalf("fidelity wrong: %v (want ~0.70)", r.Fidelity)
	}
	// Health is the composite, not an avg risk score; with orphan 10% + bug 30%
	// it must be well below a naive 85.
	if r.HealthScore >= 85 {
		t.Fatalf("health score should reflect real orphan+bug rates, got %d", r.HealthScore)
	}
}
