package dashboard

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func parseRepositoryTopologyQuery(r *http.Request) (repositoryTopologyQuery, error) {
	q := repositoryTopologyQuery{
		Channels:  []repositoryChannel{channelDubbo, channelHTTP, channelKafka, channelRabbitMQ},
		Direction: directionBoth,
		Depth:     1,
		Evidence:  []repositoryEvidence{evidenceConfirmed, evidenceInferred},
		MinCount:  1,
	}
	values := r.URL.Query()
	var err error
	if raw := values.Get("channels"); raw != "" {
		q.Channels, err = parseRepositoryChannels(raw)
		if err != nil {
			return repositoryTopologyQuery{}, err
		}
	}
	q.Repos = parseSortedStrings(values.Get("repos"))
	q.Focus = strings.TrimSpace(values.Get("focus"))
	q.Source = strings.TrimSpace(values.Get("source"))
	q.Target = strings.TrimSpace(values.Get("target"))
	q.Search = strings.TrimSpace(values.Get("q"))

	if raw := strings.TrimSpace(values.Get("direction")); raw != "" {
		q.Direction = repositoryDirection(raw)
		if !isRepositoryDirection(q.Direction) {
			return repositoryTopologyQuery{}, fmt.Errorf("invalid direction %q", raw)
		}
	}
	if raw := strings.TrimSpace(values.Get("depth")); raw != "" {
		q.Depth, err = strconv.Atoi(raw)
		if err != nil || q.Depth < 1 || q.Depth > 3 {
			return repositoryTopologyQuery{}, fmt.Errorf("invalid depth %q: must be 1..3", raw)
		}
	}
	if raw := values.Get("evidence"); raw != "" {
		q.Evidence, err = parseRepositoryEvidence(raw)
		if err != nil {
			return repositoryTopologyQuery{}, err
		}
	}
	if raw := strings.TrimSpace(values.Get("min_count")); raw != "" {
		q.MinCount, err = strconv.Atoi(raw)
		if err != nil || q.MinCount < 1 {
			return repositoryTopologyQuery{}, fmt.Errorf("invalid min_count %q: must be at least 1", raw)
		}
	}
	if q.Focus != "" && (q.Source != "" || q.Target != "") {
		return repositoryTopologyQuery{}, fmt.Errorf("focus and source/target path mode are mutually exclusive")
	}
	return q, nil
}

func parseRepositoryChannels(raw string) ([]repositoryChannel, error) {
	values := parseSortedStrings(raw)
	channels := make([]repositoryChannel, 0, len(values))
	for _, value := range values {
		channel := repositoryChannel(value)
		if !isRepositoryChannel(channel) {
			return nil, fmt.Errorf("invalid channel %q", value)
		}
		channels = append(channels, channel)
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("channels must not be empty")
	}
	return channels, nil
}

func parseRepositoryEvidence(raw string) ([]repositoryEvidence, error) {
	values := parseSortedStrings(raw)
	evidence := make([]repositoryEvidence, 0, len(values))
	for _, value := range values {
		state := repositoryEvidence(value)
		if !isRepositoryEvidence(state) {
			return nil, fmt.Errorf("invalid evidence %q", value)
		}
		evidence = append(evidence, state)
	}
	if len(evidence) == 0 {
		return nil, fmt.Errorf("evidence must not be empty")
	}
	return evidence, nil
}

func parseSortedStrings(raw string) []string {
	seen := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func isRepositoryChannel(channel repositoryChannel) bool {
	switch channel {
	case channelDubbo, channelHTTP, channelKafka, channelRabbitMQ, channelOther:
		return true
	default:
		return false
	}
}

func isRepositoryEvidence(evidence repositoryEvidence) bool {
	switch evidence {
	case evidenceConfirmed, evidenceInferred, evidenceDangling, evidenceAmbiguous, evidenceExternal:
		return true
	default:
		return false
	}
}

func isRepositoryDirection(direction repositoryDirection) bool {
	switch direction {
	case directionInbound, directionOutbound, directionBoth:
		return true
	default:
		return false
	}
}
