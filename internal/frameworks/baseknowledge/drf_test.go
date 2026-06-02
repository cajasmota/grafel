package baseknowledge

import (
	"reflect"
	"testing"
)

// reg returns a registry containing only the DRF pack so tests are
// independent of process-global registration order.
func reg() *Registry { return NewRegistry(drfPack{}) }

// TestModelViewSetInheritsFiveCRUDVerbsWithStatuses asserts the headline
// contract: a ModelViewSet exposes the 6 CRUD verbs, each attributed to
// the right defining mixin with the right default status. (ModelViewSet =
// list + retrieve + create + update + partial_update + destroy.)
func TestModelViewSetInheritsCRUDVerbsWithStatuses(t *testing.T) {
	r := reg()

	want := []struct {
		verb     string
		method   string // HTTP method
		status   int
		defining string
	}{
		{"create", "POST", 201, "rest_framework.mixins.CreateModelMixin"},
		{"retrieve", "GET", 200, "rest_framework.mixins.RetrieveModelMixin"},
		{"update", "PUT", 200, "rest_framework.mixins.UpdateModelMixin"},
		{"partial_update", "PATCH", 200, "rest_framework.mixins.UpdateModelMixin"},
		{"destroy", "DELETE", 204, "rest_framework.mixins.DestroyModelMixin"},
		{"list", "GET", 200, "rest_framework.mixins.ListModelMixin"},
	}

	for _, w := range want {
		m, ok := r.Member("ModelViewSet", w.verb)
		if !ok {
			t.Fatalf("ModelViewSet missing inherited verb %q", w.verb)
		}
		if m.HTTPVerb != w.method {
			t.Errorf("verb %q: HTTPVerb = %q, want %q", w.verb, m.HTTPVerb, w.method)
		}
		if m.DefaultStatus != w.status {
			t.Errorf("verb %q: DefaultStatus = %d, want %d", w.verb, m.DefaultStatus, w.status)
		}
		if m.DefiningClass != w.defining {
			t.Errorf("verb %q: DefiningClass = %q, want %q", w.verb, m.DefiningClass, w.defining)
		}
		if !m.PermissionApplicable {
			t.Errorf("verb %q: expected PermissionApplicable true (DRF applies perms to every handler)", w.verb)
		}
	}

	// Exactly the 6 CRUD verbs, nothing extra.
	got := r.MembersOf("ModelViewSet")
	if len(got) != 6 {
		t.Errorf("ModelViewSet member count = %d, want 6 (got %v)", len(got), keys(got))
	}
}

// TestUpdateIsValid400Fact asserts the #278 fact: update / partial_update /
// create carry the implicit 400-on-invalid contract via
// is_valid(raise_exception=True).
func TestUpdateIsValid400Fact(t *testing.T) {
	r := reg()
	for _, verb := range []string{"create", "update", "partial_update"} {
		m, ok := r.Member("ModelViewSet", verb)
		if !ok {
			t.Fatalf("missing %q", verb)
		}
		if !contains(m.ErrorStatuses, 400) {
			t.Errorf("verb %q: ErrorStatuses %v missing 400 (the #278 is_valid fact)", verb, m.ErrorStatuses)
		}
		if m.Behaviour == "" {
			t.Errorf("verb %q: expected a behaviour note describing is_valid(raise_exception=True)", verb)
		}
	}
	// retrieve / destroy must NOT claim a 400 — they don't validate input.
	for _, verb := range []string{"retrieve", "destroy"} {
		m, _ := r.Member("ModelViewSet", verb)
		if contains(m.ErrorStatuses, 400) {
			t.Errorf("verb %q should not carry a 400 contract", verb)
		}
	}
}

// TestListPaginationApplicable asserts only `list` advertises pagination.
func TestListPaginationApplicable(t *testing.T) {
	r := reg()
	if m, _ := r.Member("ModelViewSet", "list"); !m.PaginationApplicable {
		t.Error("list should advertise PaginationApplicable")
	}
	for _, verb := range []string{"retrieve", "create", "update", "partial_update", "destroy"} {
		if m, _ := r.Member("ModelViewSet", verb); m.PaginationApplicable {
			t.Errorf("verb %q should not advertise pagination", verb)
		}
	}
}

// TestReadOnlyModelViewSetIsListRetrieveOnly guards the substring trap.
func TestReadOnlyModelViewSetIsListRetrieveOnly(t *testing.T) {
	r := reg()
	got := r.MembersOf("ReadOnlyModelViewSet")
	want := map[string]bool{"list": true, "retrieve": true}
	if len(got) != len(want) {
		t.Fatalf("ReadOnlyModelViewSet members = %v, want list+retrieve", keys(got))
	}
	for n := range want {
		if _, ok := got[n]; !ok {
			t.Errorf("ReadOnlyModelViewSet missing %q", n)
		}
	}
}

// TestMixinsDefineTheirOwnVerbs asserts each mixin contributes exactly its
// verb(s) with the right status, independent of any viewset.
func TestMixinsDefineTheirOwnVerbs(t *testing.T) {
	r := reg()
	cases := map[string]map[string]int{
		"CreateModelMixin":   {"create": 201},
		"RetrieveModelMixin": {"retrieve": 200},
		"UpdateModelMixin":   {"update": 200, "partial_update": 200},
		"DestroyModelMixin":  {"destroy": 204},
		"ListModelMixin":     {"list": 200},
	}
	for mixin, verbs := range cases {
		ms := r.MembersOf(mixin)
		if len(ms) != len(verbs) {
			t.Errorf("%s members = %v, want %v", mixin, keys(ms), verbs)
		}
		for v, status := range verbs {
			m, ok := ms[v]
			if !ok {
				t.Errorf("%s missing verb %q", mixin, v)
				continue
			}
			if m.DefaultStatus != status {
				t.Errorf("%s.%s status = %d, want %d", mixin, v, m.DefaultStatus, status)
			}
		}
	}
}

// TestFQNAndLeafLookup asserts lookup works by both dotted FQN and leaf.
func TestFQNAndLeafLookup(t *testing.T) {
	r := reg()
	if _, ok := r.Lookup("rest_framework.viewsets.ModelViewSet"); !ok {
		t.Error("FQN lookup of ModelViewSet failed")
	}
	if _, ok := r.Lookup("ModelViewSet"); !ok {
		t.Error("leaf lookup of ModelViewSet failed")
	}
	if _, ok := r.Lookup("rest_framework.mixins.CreateModelMixin"); !ok {
		t.Error("FQN lookup of CreateModelMixin failed")
	}
	if _, ok := r.Lookup("totally.unknown.Base"); ok {
		t.Error("unknown base should not resolve")
	}
}

// TestMemberNamesUnion reproduces the old cbvBaseInheritedMethods union
// semantics: combining mixins yields the union of their verbs, sorted.
func TestMemberNamesUnion(t *testing.T) {
	r := reg()
	got := r.MemberNames("ListModelMixin", "CreateModelMixin", "RetrieveModelMixin", "UpdateModelMixin", "DestroyModelMixin")
	want := []string{"create", "destroy", "list", "partial_update", "retrieve", "update"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MemberNames union = %v, want %v", got, want)
	}
}

// TestGenericViewSetHasNoVerbs asserts the action-only hosts are known but
// empty (so they participate in cbv_bases without adding CRUD verbs).
func TestGenericViewSetHasNoVerbs(t *testing.T) {
	r := reg()
	for _, base := range []string{"ViewSet", "GenericViewSet", "GenericAPIView"} {
		c, ok := r.Lookup(base)
		if !ok {
			t.Fatalf("%s should be a known base", base)
		}
		if len(c.Members) != 0 {
			t.Errorf("%s should contribute no verbs, got %v", base, keys(c.Members))
		}
	}
}

// TestDjangoCBVVerbsHaveNoFabricatedStatus asserts the never-fabricate
// rule: Django generic CBV handlers carry the verb but StatusUnknown.
func TestDjangoCBVVerbsHaveNoFabricatedStatus(t *testing.T) {
	r := reg()
	m, ok := r.Member("ListView", "get")
	if !ok {
		t.Fatal("ListView.get missing")
	}
	if m.DefaultStatus != StatusUnknown {
		t.Errorf("ListView.get DefaultStatus = %d, want StatusUnknown (no fabricated status)", m.DefaultStatus)
	}
	if m.HTTPVerb != "GET" {
		t.Errorf("ListView.get HTTPVerb = %q, want GET", m.HTTPVerb)
	}
}

// --- test helpers -------------------------------------------------------

func keys(m map[string]Member) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func contains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
