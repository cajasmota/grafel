package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseRepositoryTopologyQueryDefaults(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/repository-topology/sample", nil)
	got, err := parseRepositoryTopologyQuery(req)
	if err != nil {
		t.Fatal(err)
	}
	wantChannels := []repositoryChannel{channelDubbo, channelHTTP, channelKafka, channelRabbitMQ}
	if !reflect.DeepEqual(got.Channels, wantChannels) {
		t.Fatalf("channels = %#v, want %#v", got.Channels, wantChannels)
	}
	wantEvidence := []repositoryEvidence{evidenceConfirmed, evidenceInferred}
	if !reflect.DeepEqual(got.Evidence, wantEvidence) {
		t.Fatalf("evidence = %#v, want %#v", got.Evidence, wantEvidence)
	}
	if got.Direction != directionBoth || got.Depth != 1 || got.MinCount != 1 {
		t.Fatalf("unexpected defaults: %#v", got)
	}
}

func TestParseRepositoryTopologyQueryNormalizesValidValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/repository-topology/sample?channels=other,dubbo,dubbo&repos=rules,client,rules&focus=client&direction=outbound&depth=3&evidence=external,confirmed&min_count=2&q=%20OrderService%20", nil)
	got, err := parseRepositoryTopologyQuery(req)
	if err != nil {
		t.Fatal(err)
	}
	if want := []repositoryChannel{channelDubbo, channelOther}; !reflect.DeepEqual(got.Channels, want) {
		t.Fatalf("channels = %#v, want %#v", got.Channels, want)
	}
	if want := []string{"client", "rules"}; !reflect.DeepEqual(got.Repos, want) {
		t.Fatalf("repos = %#v, want %#v", got.Repos, want)
	}
	if want := []repositoryEvidence{evidenceConfirmed, evidenceExternal}; !reflect.DeepEqual(got.Evidence, want) {
		t.Fatalf("evidence = %#v, want %#v", got.Evidence, want)
	}
	if got.Focus != "client" || got.Direction != directionOutbound || got.Depth != 3 || got.MinCount != 2 || got.Search != "OrderService" {
		t.Fatalf("unexpected normalized query: %#v", got)
	}
}

func TestParseRepositoryTopologyQueryRejectsInvalidValues(t *testing.T) {
	cases := []string{
		"?channels=smtp",
		"?direction=sideways",
		"?depth=0",
		"?depth=4",
		"?min_count=0",
		"?evidence=guessed",
		"?source=a&target=b&focus=c",
		"?target=b&focus=c",
	}
	for _, suffix := range cases {
		t.Run(suffix, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v2/repository-topology/g"+suffix, nil)
			if _, err := parseRepositoryTopologyQuery(req); err == nil {
				t.Fatalf("expected error for %s", suffix)
			}
		})
	}
}

func TestParseRepositoryTopologyQueryAllowsIncompletePathSelection(t *testing.T) {
	for _, suffix := range []string{"?source=a", "?target=b"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/repository-topology/g"+suffix, nil)
		if _, err := parseRepositoryTopologyQuery(req); err != nil {
			t.Fatalf("unexpected error for %s: %v", suffix, err)
		}
	}
}
