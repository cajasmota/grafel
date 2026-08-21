package engine

import (
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// #6417 — a `url_mount_point` synthetic's record ID is `http:ANY:<canonical>:mount`,
// which carries no file qualification. The Django pass (ApplyDjangoNestedURLConf)
// dedupes mount records against a `seen` map whose lifetime spans EVERY parent
// file in the pass, so when two different urls.py files mount the SAME prefix
// only the first-scanned file's mount survives. The second file's declaration is
// dropped outright, and which file wins the surviving attribution depends purely
// on the order `parentFiles` arrives in — that is the reported `SourceFile` flap.
//
// The fix is NOT to file-qualify the Name/ID string: graph.EntityID already
// hashes SourceFile (internal/graph/graph.go:259), so two records that share a
// Name but differ in SourceFile are already distinct graph entities. Changing the
// Name would rewrite the id of every existing mount entity on reindex for no
// identity gain. The fix is to stop the cross-file dedup from collapsing them.

// mountRecords returns the `url_mount_point` records in `recs`, in input order.
func mountRecords(recs []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range recs {
		if e.Properties != nil && e.Properties["pattern_type"] == "url_mount_point" {
			out = append(out, e)
		}
	}
	return out
}

func mountSourceFiles(recs []types.EntityRecord) []string {
	var out []string
	for _, e := range mountRecords(recs) {
		out = append(out, e.SourceFile)
	}
	sort.Strings(out)
	return out
}

// twoDjangoMountFiles is a repo where two distinct URLconf roots mount the same
// "/api" prefix — the exact shape from the issue report.
func twoDjangoMountFiles() fileMap {
	return fileMap{
		"app/urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/", include("nonexistent.users")),
]
`,
		"admin/urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/", include("nonexistent.admin")),
]
`,
	}
}

// TestIssue6417_DjangoMount_TwoFilesSamePrefix_BothSurvive pins that BOTH
// mounting files keep their own mount entity. Before the fix exactly one
// record is emitted.
func TestIssue6417_DjangoMount_TwoFilesSamePrefix_BothSurvive(t *testing.T) {
	files := twoDjangoMountFiles()
	got := ApplyDjangoNestedURLConf([]string{"app/urls.py", "admin/urls.py"}, files.reader)

	mounts := mountRecords(got)
	if len(mounts) != 2 {
		t.Fatalf("#6417: expected 2 url_mount_point records (one per mounting file); got %d (%v)",
			len(mounts), mountSourceFiles(got))
	}
	want := []string{"admin/urls.py", "app/urls.py"}
	if sf := mountSourceFiles(got); sf[0] != want[0] || sf[1] != want[1] {
		t.Errorf("#6417: mount SourceFiles = %v, want %v", sf, want)
	}
	for _, m := range mounts {
		if m.Properties["url_prefix"] != "/api" {
			t.Errorf("#6417: url_prefix = %q, want /api (the linker harvests this property, not the ID)",
				m.Properties["url_prefix"])
		}
	}
}

// TestIssue6417_DjangoMount_AttributionDoesNotFlapWithFileOrder is the direct
// pin on the reported symptom: the surviving attribution must not depend on the
// order parentFiles arrives in. Before the fix, the two orders yield
// {app/urls.py} and {admin/urls.py} respectively.
func TestIssue6417_DjangoMount_AttributionDoesNotFlapWithFileOrder(t *testing.T) {
	files := twoDjangoMountFiles()
	forward := mountSourceFiles(ApplyDjangoNestedURLConf([]string{"app/urls.py", "admin/urls.py"}, files.reader))
	reverse := mountSourceFiles(ApplyDjangoNestedURLConf([]string{"admin/urls.py", "app/urls.py"}, files.reader))

	if len(forward) != len(reverse) {
		t.Fatalf("#6417: mount attribution flaps with file order: forward=%v reverse=%v", forward, reverse)
	}
	for i := range forward {
		if forward[i] != reverse[i] {
			t.Fatalf("#6417: mount attribution flaps with file order: forward=%v reverse=%v", forward, reverse)
		}
	}
}

// TestIssue6417_DjangoMount_DistinctGraphIdentityWithoutRenaming pins WHY the
// Name is deliberately left un-file-qualified: graph.EntityID already folds
// SourceFile into the hash, so the two same-prefix mounts are distinct entities
// while their Name (and therefore every existing mount entity's id, when a
// prefix is mounted once) stays byte-identical across the fix.
func TestIssue6417_DjangoMount_DistinctGraphIdentityWithoutRenaming(t *testing.T) {
	files := twoDjangoMountFiles()
	mounts := mountRecords(ApplyDjangoNestedURLConf([]string{"app/urls.py", "admin/urls.py"}, files.reader))
	if len(mounts) != 2 {
		t.Fatalf("#6417: expected 2 mount records, got %d", len(mounts))
	}
	for _, m := range mounts {
		if m.Name != "http:ANY:/api:mount" {
			t.Fatalf("#6417: mount Name = %q, want the un-file-qualified %q "+
				"(file-qualifying it would rewrite every existing mount entity's id on reindex)",
				m.Name, "http:ANY:/api:mount")
		}
	}
	a := graph.EntityID("repo", mounts[0].Kind, mounts[0].Name, mounts[0].SourceFile)
	b := graph.EntityID("repo", mounts[1].Kind, mounts[1].Name, mounts[1].SourceFile)
	if a == b {
		t.Errorf("#6417: the two mount records collapse to one graph entity id %s", a)
	}
}

// TestIssue6417_FastAPIMount_TwoFilesSamePrefix_BothSurvive pins the same
// invariant on the FastAPI emitter, whose dedup map is already per-file. This is
// a regression guard, not a bug reproduction.
func TestIssue6417_FastAPIMount_TwoFilesSamePrefix_BothSurvive(t *testing.T) {
	const main = `
from fastapi import FastAPI
app = FastAPI()
app.include_router(users.router, prefix="/api")
`
	const admin = `
from fastapi import FastAPI
admin_app = FastAPI()
admin_app.include_router(admin.router, prefix="/api")
`
	a := fastapiMountPointSynthetics(main, "app/main.py")
	b := fastapiMountPointSynthetics(admin, "app/admin.py")
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("#6417: expected 1 mount synthetic per file; got %d and %d", len(a), len(b))
	}
	if a[0].SourceFile != "app/main.py" || b[0].SourceFile != "app/admin.py" {
		t.Fatalf("#6417: mount SourceFile misattributed: %q, %q", a[0].SourceFile, b[0].SourceFile)
	}
	idA := graph.EntityID("repo", a[0].Kind, a[0].Name, a[0].SourceFile)
	idB := graph.EntityID("repo", b[0].Kind, b[0].Name, b[0].SourceFile)
	if idA == idB {
		t.Errorf("#6417: FastAPI mounts from two files collapse to one graph entity id %s", idA)
	}
}

// TestIssue6417_DjangoMount_TwoPrefixesInOneFile_BothEmitted pins that the
// per-file dedup key is the MOUNT ID, not the file. Keying it by relPath would
// emit one mount per file and silently lose every additional prefix a root
// declares — the ordinary Django shape.
func TestIssue6417_DjangoMount_TwoPrefixesInOneFile_BothEmitted(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/", include("nonexistent.api")),
    path("admin/", include("nonexistent.admin")),
]
`,
	}
	mounts := mountRecords(ApplyDjangoNestedURLConf([]string{"urls.py"}, files.reader))
	if len(mounts) != 2 {
		t.Fatalf("#6417: expected 2 url_mount_point records (one per prefix); got %d", len(mounts))
	}
	got := []string{mounts[0].Properties["url_prefix"], mounts[1].Properties["url_prefix"]}
	sort.Strings(got)
	if got[0] != "/admin" || got[1] != "/api" {
		t.Errorf("#6417: url_prefixes = %v, want [/admin /api]", got)
	}
}

// TestIssue6417_DjangoMount_SamePrefixTwiceInOneFile_DedupedToOne pins the one
// shape where the id collapse the issue alleged is REAL: two include() calls on
// the same prefix in the same file produce records with identical Name AND
// identical SourceFile, so graph.EntityID cannot tell them apart. The per-file
// dedup must suppress the second — deleting the dedup would push a genuine
// id-collision into the store.
func TestIssue6417_DjangoMount_SamePrefixTwiceInOneFile_DedupedToOne(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/", include("nonexistent.users")),
    path("api/", include("nonexistent.orders")),
]
`,
	}
	mounts := mountRecords(ApplyDjangoNestedURLConf([]string{"urls.py"}, files.reader))
	if len(mounts) != 1 {
		ids := make([]string, 0, len(mounts))
		for _, m := range mounts {
			ids = append(ids, graph.EntityID("repo", m.Kind, m.Name, m.SourceFile))
		}
		t.Fatalf("#6417: expected 1 url_mount_point for a prefix mounted twice in one file "+
			"(identical Name+SourceFile => identical graph.EntityID); got %d with ids %v",
			len(mounts), ids)
	}
}

// TestIssue6417_DjangoMount_DuplicateParentFileIsIdempotent pins that scanning
// the same parent file twice does not emit two records that collapse to one
// graph.EntityID. Pre-#6417 the pass-wide `seen` map suppressed this as a side
// effect; the per-file dedup no longer can, so the pass guards relPath itself.
func TestIssue6417_DjangoMount_DuplicateParentFileIsIdempotent(t *testing.T) {
	files := fileMap{
		"urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/", include("nonexistent.users")),
]
`,
	}
	once := ApplyDjangoNestedURLConf([]string{"urls.py"}, files.reader)
	twice := ApplyDjangoNestedURLConf([]string{"urls.py", "urls.py"}, files.reader)
	if len(once) != len(twice) {
		t.Fatalf("#6417: duplicate parentFiles is not idempotent: %d records vs %d",
			len(once), len(twice))
	}
}

// TestIssue6417_DjangoMount_URLPrefixIsCanonical pins that the property the
// linker harvests (`url_prefix`) carries the SAME normalisation as the dedup key
// (`canonical`). Two spellings of one parameterised prefix dedup to a single
// mount id, so they must also contribute a single member to the linker's
// per-repo prefix set — emitting the raw source spelling put an unmatchable
// `<int:version>` candidate at the front of the longest-first probe order.
func TestIssue6417_DjangoMount_URLPrefixIsCanonical(t *testing.T) {
	files := fileMap{
		"a/urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/<int:version>/", include("nonexistent.a")),
]
`,
		"b/urls.py": `
from django.urls import path, include

urlpatterns = [
    path("api/{version}/", include("nonexistent.b")),
]
`,
	}
	mounts := mountRecords(ApplyDjangoNestedURLConf([]string{"a/urls.py", "b/urls.py"}, files.reader))
	if len(mounts) != 2 {
		t.Fatalf("#6417: expected 2 mount records (one per file); got %d", len(mounts))
	}
	prefixes := map[string]bool{}
	for _, m := range mounts {
		if m.Properties["url_prefix"] != m.Properties["path"] {
			t.Errorf("#6417: url_prefix %q != canonical path %q — the linker harvests url_prefix, "+
				"so a raw spelling adds a set member the mount id already deduped away",
				m.Properties["url_prefix"], m.Properties["path"])
		}
		prefixes[m.Properties["url_prefix"]] = true
	}
	if len(prefixes) != 1 {
		t.Errorf("#6417: two spellings of one prefix contributed %d distinct url_prefix values %v, want 1",
			len(prefixes), prefixes)
	}
}
