package dashboard

import (
	"reflect"
	"testing"
)

func TestIsGraphQLEndpoint(t *testing.T) {
	cases := []struct {
		name    string
		subtype string
		props   map[string]string
		want    bool
	}{
		{"java dgs resolver subtype", "graphql_resolver", nil, true},
		{"verb GRAPHQL", "endpoint", map[string]string{"verb": "GRAPHQL"}, true},
		{"http_method GRAPHQL lowercase", "endpoint", map[string]string{"http_method": "graphql"}, true},
		{"plain http endpoint", "endpoint", map[string]string{"verb": "POST"}, false},
		{"no props", "function", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isGraphQLEndpoint(c.subtype, c.props); got != c.want {
				t.Fatalf("isGraphQLEndpoint(%q,%v) = %v, want %v", c.subtype, c.props, got, c.want)
			}
		})
	}
}

func TestParseEffectConfidences(t *testing.T) {
	got := parseEffectConfidences("db_read=0.95,http_out=1.00,mutation=0.80")
	want := map[string]float64{"db_read": 0.95, "http_out": 1.00, "mutation": 0.80}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseEffectConfidences = %v, want %v", got, want)
	}

	// Malformed / empty segments are skipped, not fatal.
	got = parseEffectConfidences(",db_write=0.5,broken,trailing=")
	if len(got) != 1 || got["db_write"] != 0.5 {
		t.Fatalf("parseEffectConfidences(malformed) = %v, want {db_write:0.5}", got)
	}

	if got := parseEffectConfidences(""); len(got) != 0 {
		t.Fatalf("parseEffectConfidences(\"\") = %v, want empty", got)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "x", "y"); got != "x" {
		t.Fatalf("firstNonEmpty = %q, want x", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("firstNonEmpty(empties) = %q, want empty", got)
	}
}
