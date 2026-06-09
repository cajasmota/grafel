package ingest

import "testing"

func buildIndex() map[string][]NameTarget {
	return IndexNames([]NameTuple{
		{Name: "OrderService", QualifiedName: "orders/order.OrderService", ID: "id_orderservice", Kind: "SCOPE.Class"},
		{Name: "placeOrder", QualifiedName: "orders/order.OrderService.placeOrder", ID: "id_placeorder", Kind: "SCOPE.Function"},
		{Name: "validateOrder", QualifiedName: "orders/order.validateOrder", ID: "id_validate", Kind: "SCOPE.Function"},
		// Two distinct entities sharing a name -> ambiguous, must not link.
		{Name: "Handler", QualifiedName: "a.Handler", ID: "id_handler_a", Kind: "SCOPE.Class"},
		{Name: "Handler", QualifiedName: "b.Handler", ID: "id_handler_b", Kind: "SCOPE.Class"},
		// Short name -> never linkable.
		{Name: "db", QualifiedName: "x.db", ID: "id_db", Kind: "SCOPE.Variable"},
	})
}

func TestLinkMentions_ExactMatchLinks(t *testing.T) {
	idx := buildIndex()
	sections := []Section{
		{HeadingText: "Intro", Body: "The OrderService runs placeOrder and then validateOrder.\n"},
	}
	got := LinkMentions(sections, idx)

	want := map[string]string{
		"OrderService":  "id_orderservice",
		"placeOrder":    "id_placeorder",
		"validateOrder": "id_validate",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d mentions, want %d: %+v", len(got), len(want), got)
	}
	for _, m := range got {
		if want[m.Token] != m.TargetID {
			t.Errorf("token %q -> %q, want %q", m.Token, m.TargetID, want[m.Token])
		}
	}
}

func TestLinkMentions_DropsCommonWordsAndKeywords(t *testing.T) {
	idx := buildIndex()
	// "type", "this", "value", "data", "file", "test" are skip-words; a token
	// like "order" (common) and "the" never appear as entity names anyway.
	sections := []Section{
		{HeadingText: "Notes", Body: "This type of data in the file is used for the test value.\n"},
	}
	got := LinkMentions(sections, idx)
	if len(got) != 0 {
		t.Fatalf("expected 0 links from common/keyword-only prose, got %+v", got)
	}
}

func TestLinkMentions_SubstringDoesNotLink(t *testing.T) {
	idx := buildIndex()
	// "OrderServiceFactory" must NOT match the entity "OrderService" — exact
	// whole-token match only.
	sections := []Section{
		{HeadingText: "X", Body: "The OrderServiceFactory builds things.\n"},
	}
	got := LinkMentions(sections, idx)
	if len(got) != 0 {
		t.Fatalf("substring wrongly linked: %+v", got)
	}
}

func TestLinkMentions_AmbiguousNameDropped(t *testing.T) {
	idx := buildIndex()
	sections := []Section{
		{HeadingText: "X", Body: "The Handler dispatches requests.\n"},
	}
	got := LinkMentions(sections, idx)
	for _, m := range got {
		if m.Token == "Handler" {
			t.Fatalf("ambiguous name 'Handler' was linked to %q (must be dropped)", m.TargetID)
		}
	}
	if len(got) != 0 {
		t.Fatalf("expected no links, got %+v", got)
	}
}

func TestLinkMentions_ShortTokenNeverLinks(t *testing.T) {
	idx := buildIndex()
	sections := []Section{{HeadingText: "X", Body: "open the db now.\n"}}
	if got := LinkMentions(sections, idx); len(got) != 0 {
		t.Fatalf("short token 'db' linked: %+v", got)
	}
}

func TestLinkMentions_DedupesWithinSection(t *testing.T) {
	idx := buildIndex()
	sections := []Section{
		{HeadingText: "X", Body: "placeOrder, placeOrder, and again placeOrder.\n"},
	}
	got := LinkMentions(sections, idx)
	if len(got) != 1 {
		t.Fatalf("expected 1 deduped mention, got %d: %+v", len(got), got)
	}
}
