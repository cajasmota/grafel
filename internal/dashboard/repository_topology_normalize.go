package dashboard

import (
	"sort"
	"strings"
)

type repositoryRelationship struct {
	ID         string
	SourceRepo string
	TargetRepo string
	Channel    repositoryChannel
	Evidence   repositoryEvidence
	Identifier string
	Label      string
	SearchText string
	Link       CrossRepoLink
}

func repositoryFromPrefixedEntityID(id string, knownRepos map[string]struct{}) (string, bool) {
	best := ""
	for repo := range knownRepos {
		if len(repo) <= len(best) || !strings.HasPrefix(id, repo+"::") {
			continue
		}
		best = repo
	}
	return best, best != ""
}

func normalizeRepositoryChannel(link CrossRepoLink) repositoryChannel {
	if channel := repositoryChannel(strings.ToLower(strings.TrimSpace(link.Channel))); isRepositoryChannel(channel) {
		return channel
	}
	method := strings.ToLower(strings.TrimSpace(link.Method))
	kind := strings.ToLower(strings.TrimSpace(link.Kind))
	identifier := strings.ToLower(strings.TrimSpace(link.Identifier))
	combined := method + " " + kind + " " + identifier

	switch {
	case method == "dubbo", strings.HasPrefix(identifier, "dubbo:"), strings.Contains(combined, " dubbo"):
		return channelDubbo
	case method == "http", method == "http_self", strings.HasPrefix(identifier, "http:"), strings.Contains(combined, " http"):
		return channelHTTP
	case strings.Contains(combined, "kafka"):
		return channelKafka
	case strings.Contains(combined, "rabbit"), strings.Contains(combined, "amqp"):
		return channelRabbitMQ
	}

	properties := lowerRepositoryProperties(link.Properties)
	switch {
	case strings.Contains(properties["broker"], "kafka"), properties["protocol"] == "kafka":
		return channelKafka
	case strings.Contains(properties["broker"], "rabbit"), properties["protocol"] == "amqp",
		properties["routing_key"] != "", properties["queue"] != "", properties["exchange"] != "":
		return channelRabbitMQ
	default:
		return channelOther
	}
}

func normalizeRepositoryEvidence(link CrossRepoLink, sourceKnown, targetKnown bool) repositoryEvidence {
	if !sourceKnown || !targetKnown {
		return evidenceDangling
	}
	confidence := strings.ToLower(strings.TrimSpace(link.Properties["confidence"]))
	switch confidence {
	case "inferred", "heuristic":
		return evidenceInferred
	default:
		return evidenceConfirmed
	}
}

func normalizeRepositoryRelationship(link CrossRepoLink, knownRepos map[string]struct{}) (repositoryRelationship, bool) {
	sourceRepo, sourceKnown := repositoryFromPrefixedEntityID(link.Source, knownRepos)
	if !sourceKnown {
		return repositoryRelationship{}, false
	}
	targetRepo, targetKnown := repositoryFromPrefixedEntityID(link.Target, knownRepos)
	channel := normalizeRepositoryChannel(link)
	evidence := normalizeRepositoryEvidence(link, sourceKnown, targetKnown)
	label := strings.TrimSpace(link.Identifier)
	if label == "" {
		label = strings.TrimSpace(link.Method)
	}
	if label == "" {
		label = strings.TrimSpace(link.Kind)
	}
	return repositoryRelationship{
		ID:         repositoryRelationshipID(link),
		SourceRepo: sourceRepo,
		TargetRepo: targetRepo,
		Channel:    channel,
		Evidence:   evidence,
		Identifier: strings.TrimSpace(link.Identifier),
		Label:      label,
		SearchText: repositoryRelationshipSearchText(link, sourceRepo, targetRepo, channel),
		Link:       link,
	}, true
}

func repositoryRelationshipID(link CrossRepoLink) string {
	return strings.Join([]string{
		strings.TrimSpace(link.Source),
		strings.TrimSpace(link.Target),
		strings.TrimSpace(link.Method),
		strings.TrimSpace(link.Kind),
		strings.TrimSpace(link.Identifier),
	}, "|")
}

func repositoryRelationshipSearchText(link CrossRepoLink, sourceRepo, targetRepo string, channel repositoryChannel) string {
	parts := []string{
		sourceRepo, targetRepo, string(channel), link.Source, link.Target,
		link.SourceName, link.SourceQualifiedName, link.SourceFile,
		link.TargetName, link.TargetQualifiedName, link.TargetFile,
		link.Kind, link.Method, link.Identifier,
	}
	keys := make([]string, 0, len(link.Properties))
	for key := range link.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key, link.Properties[key])
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func lowerRepositoryProperties(properties map[string]string) map[string]string {
	lower := make(map[string]string, len(properties))
	for key, value := range properties {
		lower[strings.ToLower(strings.TrimSpace(key))] = strings.ToLower(strings.TrimSpace(value))
	}
	return lower
}
