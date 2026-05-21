package dashboard

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestV2Envelope_OKShape(t *testing.T) {
	payload := map[string]string{"key": "value"}
	env := v2OK(payload)
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := got["data"]; !ok {
		t.Error("envelope missing 'data' field")
	}
	if _, ok := got["ok"]; !ok {
		t.Error("envelope missing 'ok' field")
	}
}

func TestV2Envelope_ErrorShape(t *testing.T) {
	env := v2Err("not_found", "group not registered")
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := got["error"]; !ok {
		t.Error("error envelope missing 'error' field")
	}
	if _, ok := got["ok"]; !ok {
		t.Error("error envelope missing 'ok' field")
	}
	var errField map[string]string
	if err := json.Unmarshal(got["error"], &errField); err != nil {
		t.Fatalf("error field: %v", err)
	}
	if errField["code"] != "not_found" {
		t.Errorf("want code=not_found, got %q", errField["code"])
	}
	if errField["message"] != "group not registered" {
		t.Errorf("want message='group not registered', got %q", errField["message"])
	}
}

func TestV2Pagination_Defaults(t *testing.T) {
	p := parsePagination(nil, 0)
	if p.Limit != 50 {
		t.Errorf("default limit: want 50, got %d", p.Limit)
	}
	if p.Offset != 0 {
		t.Errorf("default offset: want 0, got %d", p.Offset)
	}
}

func TestV2Pagination_ClampLimit(t *testing.T) {
	q := url.Values{}
	q.Set("limit", "9999")
	p := parsePagination(q, 0)
	if p.Limit != 500 {
		t.Errorf("clamped limit: want 500, got %d", p.Limit)
	}
}

func TestV2Pagination_Envelope(t *testing.T) {
	items := []string{"a", "b", "c"}
	pag := V2Pagination{Limit: 10, Offset: 0, Total: 3}
	env := v2Page(items, pag)
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := got["data"]; !ok {
		t.Error("missing data")
	}
	if _, ok := got["pagination"]; !ok {
		t.Error("missing pagination")
	}
}
