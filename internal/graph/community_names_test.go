package graph

import (
	"reflect"
	"strings"
	"testing"
)

func TestTokenize_CamelAndSnake(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []string
	}{
		{"OrderViewSet", []string{"order", "view", "set"}},
		{"order_view_set", []string{"order", "view", "set"}},
		{"order-view-set", []string{"order", "view", "set"}},
		{"HTTPServer", []string{"http", "server"}},
		{"data_sync_task", []string{"data", "sync", "task"}},
		{"app/orders/views.py", []string{"app", "orders", "views"}}, // py is a stopword
		{"v2Migrate", []string{"migrate"}},                          // "v2" -> "v","2" both filtered (single-char + digit-only filtered? digits kept but len>1)
	}
	for _, c := range cases {
		got := tokenize(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("tokenize(%q) = %v; want %v", c.in, got, c.want)
		}
	}
}

func TestTokenize_StopwordsAndShort(t *testing.T) {
	t.Parallel()
	got := tokenize("a_test_of_the_init_main")
	// every term is a stopword
	if len(got) != 0 {
		t.Errorf("tokenize stopword-only string: got %v want []", got)
	}
}

// Two communities sharing a generic vocabulary ("service") but each having a
// distinguishing term ("order" vs "compliance") should receive distinct
// auto_names that surface the distinguishing term.
func TestAssignCommunityNames_DistinguishesOverlappingVocab(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "a1", Name: "OrderService", QualifiedName: "shop.order.OrderService", SourceFile: "shop/order/service.py"},
		{ID: "a2", Name: "OrderRepository", QualifiedName: "shop.order.OrderRepository", SourceFile: "shop/order/repo.py"},
		{ID: "a3", Name: "OrderViewSet", QualifiedName: "shop.order.OrderViewSet", SourceFile: "shop/order/views.py"},

		{ID: "b1", Name: "ComplianceService", QualifiedName: "ops.compliance.ComplianceService", SourceFile: "ops/compliance/service.py"},
		{ID: "b2", Name: "ComplianceTask", QualifiedName: "ops.compliance.ComplianceTask", SourceFile: "ops/compliance/tasks.py"},
		{ID: "b3", Name: "ComplianceReport", QualifiedName: "ops.compliance.ComplianceReport", SourceFile: "ops/compliance/report.py"},
	}
	results := []CommunityResult{
		{ID: 0, Size: 3},
		{ID: 1, Size: 3},
	}
	commOf := map[string]int{
		"a1": 0, "a2": 0, "a3": 0,
		"b1": 1, "b2": 1, "b3": 1,
	}

	AssignCommunityNames(results, entities, commOf)

	if results[0].AutoName == "" || results[1].AutoName == "" {
		t.Fatalf("expected non-empty auto_names, got %+v", results)
	}
	if results[0].AutoName == results[1].AutoName {
		t.Fatalf("expected distinct auto_names; both got %q", results[0].AutoName)
	}
	if !strings.Contains(results[0].AutoName, "order") {
		t.Errorf("community 0 auto_name = %q; want it to contain 'order'", results[0].AutoName)
	}
	if !strings.Contains(results[1].AutoName, "compliance") {
		t.Errorf("community 1 auto_name = %q; want it to contain 'compliance'", results[1].AutoName)
	}
	// The shared term "service" should not be the only term picked — TF-IDF
	// must downweight terms appearing in every community.
	if results[0].AutoName == "service" || results[1].AutoName == "service" {
		t.Errorf("auto_name collapsed to shared stop-token 'service': %+v", results)
	}
}

func TestAssignCommunityNames_FallbackOnEmptyTokens(t *testing.T) {
	t.Parallel()
	// Entity with only stopword/single-char tokens.
	entities := []Entity{
		{ID: "x1", Name: "a", SourceFile: ""},
	}
	results := []CommunityResult{{ID: 7, Size: 1}}
	commOf := map[string]int{"x1": 7}
	AssignCommunityNames(results, entities, commOf)
	if results[0].AutoName != "community_7" {
		t.Errorf("fallback: got %q want %q", results[0].AutoName, "community_7")
	}
}

func TestAssignCommunityNames_Deterministic(t *testing.T) {
	t.Parallel()
	entities := []Entity{
		{ID: "a1", Name: "PaymentGateway", QualifiedName: "billing.PaymentGateway", SourceFile: "billing/gateway.go"},
		{ID: "a2", Name: "PaymentProcessor", QualifiedName: "billing.PaymentProcessor", SourceFile: "billing/processor.go"},
		{ID: "b1", Name: "InventoryStore", QualifiedName: "warehouse.InventoryStore", SourceFile: "warehouse/store.go"},
		{ID: "b2", Name: "InventoryAudit", QualifiedName: "warehouse.InventoryAudit", SourceFile: "warehouse/audit.go"},
	}
	commOf := map[string]int{"a1": 0, "a2": 0, "b1": 1, "b2": 1}

	var prev []string
	for i := 0; i < 5; i++ {
		results := []CommunityResult{{ID: 0}, {ID: 1}}
		AssignCommunityNames(results, entities, commOf)
		got := []string{results[0].AutoName, results[1].AutoName}
		if prev != nil && !reflect.DeepEqual(got, prev) {
			t.Fatalf("non-deterministic: prev=%v got=%v", prev, got)
		}
		prev = got
	}
}

func TestAssignCommunityNames_SkipsUnassignedNodes(t *testing.T) {
	t.Parallel()
	// commOf=-1 means the entity wasn't placed in any community; it must be
	// excluded from the corpus so its tokens don't pollute IDF.
	entities := []Entity{
		{ID: "a1", Name: "OrderViewSet", SourceFile: "order.py"},
		{ID: "ghost", Name: "DanglingNode", SourceFile: "dangling.py"},
	}
	results := []CommunityResult{{ID: 0, Size: 1}}
	commOf := map[string]int{"a1": 0, "ghost": -1}
	AssignCommunityNames(results, entities, commOf)
	if !strings.Contains(results[0].AutoName, "order") {
		t.Errorf("unassigned-node leakage: %q", results[0].AutoName)
	}
	if strings.Contains(results[0].AutoName, "dangling") {
		t.Errorf("unassigned node 'ghost' polluted community: %q", results[0].AutoName)
	}
}
