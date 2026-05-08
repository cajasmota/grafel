package enrichers

// EventFlowEnricher links producer services to consumer services via shared topics.
// Port of Python event_flow_enricher.py (MX-694).
// Only exact topic matching implemented — LLM wildcard path not ported.

import "fmt"

// PublishesToEdge is a PUBLISHES_TO edge from the messaging extractor.
type PublishesToEdge struct {
	ProducerServiceID string
	TopicName         string
	EdgeID            string
}

// SubscribesToEdge is a SUBSCRIBES_TO edge from the messaging extractor.
type SubscribesToEdge struct {
	ConsumerServiceID string
	TopicName         string
	EdgeID            string
}

// EventFlowChain is an emitted event_flow chain entity.
type EventFlowChain struct {
	Kind               string
	Topic              string
	ProducerServiceID  string
	ConsumerServiceID  string
	PublishesToEdgeID  string
	SubscribesToEdgeID string
	Confidence         string
	ChainID            string
}

var wildcardChars = map[rune]bool{'*': true, '?': true, '#': true}

func isWildcardTopic(topic string) bool {
	for _, ch := range topic {
		if wildcardChars[ch] {
			return true
		}
	}
	return false
}

func makeChainID(topic, producerID, consumerID string) string {
	return fmt.Sprintf("event_flow:%s:%s:%s", topic, producerID, consumerID)
}

// EnrichEventFlow emits EventFlowChain records for exact topic matches.
func EnrichEventFlow(publishesTo []PublishesToEdge, subscribesTo []SubscribesToEdge) []EventFlowChain {
	if len(publishesTo) == 0 || len(subscribesTo) == 0 {
		return nil
	}
	pubByTopic := make(map[string][]PublishesToEdge)
	for _, p := range publishesTo {
		if !isWildcardTopic(p.TopicName) {
			pubByTopic[p.TopicName] = append(pubByTopic[p.TopicName], p)
		}
	}
	subByTopic := make(map[string][]SubscribesToEdge)
	for _, s := range subscribesTo {
		if !isWildcardTopic(s.TopicName) {
			subByTopic[s.TopicName] = append(subByTopic[s.TopicName], s)
		}
	}
	var chains []EventFlowChain
	for topic, pubs := range pubByTopic {
		subs, ok := subByTopic[topic]
		if !ok {
			continue
		}
		for _, pub := range pubs {
			for _, sub := range subs {
				chains = append(chains, EventFlowChain{
					Kind:               "event_flow",
					Topic:              topic,
					ProducerServiceID:  pub.ProducerServiceID,
					ConsumerServiceID:  sub.ConsumerServiceID,
					PublishesToEdgeID:  pub.EdgeID,
					SubscribesToEdgeID: sub.EdgeID,
					Confidence:         "exact",
					ChainID:            makeChainID(topic, pub.ProducerServiceID, sub.ConsumerServiceID),
				})
			}
		}
	}
	return chains
}
