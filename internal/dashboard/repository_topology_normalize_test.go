package dashboard

import (
	"strings"
	"testing"
)

func TestNormalizeRepositoryRelationshipExtractsRepos(t *testing.T) {
	link := CrossRepoLink{
		Source:     "client::svc:RuleClient",
		Target:     "rule::svc:RuleFacade",
		Channel:    "dubbo",
		Identifier: "dubbo:com.acme.RuleFacade|||",
		Confidence: 1,
		Properties: map[string]string{"confidence": "resolved"},
	}
	got, ok := normalizeRepositoryRelationship(link, repositorySet("client", "rule"))
	if !ok || got.SourceRepo != "client" || got.TargetRepo != "rule" || got.Channel != channelDubbo {
		t.Fatalf("unexpected: %#v ok=%v", got, ok)
	}
	if got.Evidence != evidenceConfirmed {
		t.Fatalf("evidence = %q, want confirmed", got.Evidence)
	}
}

func TestNormalizeRepositoryRelationshipUsesLongestRepositoryPrefix(t *testing.T) {
	link := CrossRepoLink{Source: "client-api::client", Target: "rule::server", Method: "http"}
	got, ok := normalizeRepositoryRelationship(link, repositorySet("client", "client-api", "rule"))
	if !ok || got.SourceRepo != "client-api" {
		t.Fatalf("unexpected: %#v ok=%v", got, ok)
	}
}

func TestNormalizeRepositoryChannelUsesPersistedMetadata(t *testing.T) {
	tests := []struct {
		name string
		link CrossRepoLink
		want repositoryChannel
	}{
		{name: "structured dubbo", link: CrossRepoLink{Channel: "dubbo"}, want: channelDubbo},
		{name: "http method", link: CrossRepoLink{Method: "http"}, want: channelHTTP},
		{name: "http identifier", link: CrossRepoLink{Identifier: "http:GET:/orders"}, want: channelHTTP},
		{name: "kafka method", link: CrossRepoLink{Method: "kafka_topic"}, want: channelKafka},
		{name: "kafka broker", link: CrossRepoLink{Properties: map[string]string{"broker": "kafka"}}, want: channelKafka},
		{name: "rabbit identifier", link: CrossRepoLink{Identifier: "rabbitmq:payments.created"}, want: channelRabbitMQ},
		{name: "amqp routing key", link: CrossRepoLink{Properties: map[string]string{"routing_key": "payments.created"}}, want: channelRabbitMQ},
		{name: "fallback", link: CrossRepoLink{Method: "label_match"}, want: channelOther},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeRepositoryChannel(test.link); got != test.want {
				t.Fatalf("channel = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeRepositoryEvidenceUsesPersistedConfidence(t *testing.T) {
	tests := []struct {
		name        string
		properties  map[string]string
		sourceKnown bool
		targetKnown bool
		want        repositoryEvidence
	}{
		{name: "resolved", properties: map[string]string{"confidence": "resolved"}, sourceKnown: true, targetKnown: true, want: evidenceConfirmed},
		{name: "missing marker defaults resolved", sourceKnown: true, targetKnown: true, want: evidenceConfirmed},
		{name: "inferred", properties: map[string]string{"confidence": "inferred"}, sourceKnown: true, targetKnown: true, want: evidenceInferred},
		{name: "heuristic", properties: map[string]string{"confidence": "heuristic"}, sourceKnown: true, targetKnown: true, want: evidenceInferred},
		{name: "missing target", sourceKnown: true, targetKnown: false, want: evidenceDangling},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			link := CrossRepoLink{Properties: test.properties}
			if got := normalizeRepositoryEvidence(link, test.sourceKnown, test.targetKnown); got != test.want {
				t.Fatalf("evidence = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeRepositoryRelationshipPreservesUnresolvedTarget(t *testing.T) {
	link := CrossRepoLink{Source: "client::client", Target: "missing::server", Method: "http", Identifier: "http:GET:/orders"}
	got, ok := normalizeRepositoryRelationship(link, repositorySet("client", "rule"))
	if !ok || got.SourceRepo != "client" || got.TargetRepo != "" || got.Evidence != evidenceDangling {
		t.Fatalf("unexpected: %#v ok=%v", got, ok)
	}
	searchText := strings.ToLower(got.SearchText)
	if !strings.Contains(searchText, "missing::server") || !strings.Contains(searchText, "http:get:/orders") {
		t.Fatalf("search text did not preserve unresolved metadata: %q", got.SearchText)
	}
}

func TestNormalizeRepositoryRelationshipRejectsUnknownSource(t *testing.T) {
	link := CrossRepoLink{Source: "missing::client", Target: "rule::server", Method: "http"}
	if _, ok := normalizeRepositoryRelationship(link, repositorySet("client", "rule")); ok {
		t.Fatal("expected unknown source repository to be rejected")
	}
}

func repositorySet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
