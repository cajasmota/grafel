package patterns

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// queueDetector discovers event queue producers and consumers.
// Matches Python queue_detector.py.
type queueDetector struct{}

var queueImportTokens = []string{
	"kafka", "pika", "boto3", "redis", "celery",
	"kafkajs", "bullmq", "sarama",
	"github.com/ibm/sarama", "github.com/shopify/sarama",
	"spring-kafka", "org.springframework.kafka",
	"javax.jms", "jakarta.jms", "events",
}

var queueSignals = []struct {
	re      *regexp.Regexp
	sdk     string
	relType string
}{
	{regexp.MustCompile(`\bKafkaProducer\b`), "KafkaProducer", "WRITES_TO"},
	{regexp.MustCompile(`\.send\s*\(`), "KafkaProducer.send", "WRITES_TO"},
	{regexp.MustCompile(`\bKafkaConsumer\b`), "KafkaConsumer", "READS_FROM"},
	{regexp.MustCompile(`\.basic_publish\s*\(`), "pika.basic_publish", "WRITES_TO"},
	{regexp.MustCompile(`\.basic_consume\s*\(`), "pika.basic_consume", "READS_FROM"},
	{regexp.MustCompile(`\.send_message\s*\(`), "sqs.send_message", "WRITES_TO"},
	{regexp.MustCompile(`\.receive_message\s*\(`), "sqs.receive_message", "READS_FROM"},
	{regexp.MustCompile(`\.publish\s*\(`), "redis.publish", "WRITES_TO"},
	{regexp.MustCompile(`\.subscribe\s*\(`), "redis.subscribe", "READS_FROM"},
	{regexp.MustCompile(`@(?:app|shared_task)\b`), "celery.task", "WRITES_TO"},
	{regexp.MustCompile(`\bKafkaTemplate\b.*\.send\s*\(`), "spring.KafkaTemplate", "WRITES_TO"},
	{regexp.MustCompile(`@KafkaListener\b`), "spring.KafkaListener", "READS_FROM"},
	{regexp.MustCompile(`\bproducer\.send\s*\(`), "kafkajs.producer", "WRITES_TO"},
	{regexp.MustCompile(`\bconsumer\.subscribe\s*\(`), "kafkajs.consumer", "READS_FROM"},
	{regexp.MustCompile(`\bQueue\.add\s*\(`), "bullmq.Queue", "WRITES_TO"},
	{regexp.MustCompile(`\bnew\s+Worker\s*\(`), "bullmq.Worker", "READS_FROM"},
	{regexp.MustCompile(`\.SendMessage\s*\(`), "sarama.SendMessage", "WRITES_TO"},
	{regexp.MustCompile(`\bConsumerGroup\b`), "sarama.ConsumerGroup", "READS_FROM"},
}

var queueTopicRE = regexp.MustCompile(`["']([A-Za-z0-9_\-\.]+(?:[-_][A-Za-z0-9_]+)*)["']`)

func (q *queueDetector) Category() string { return "event_queue" }

func (q *queueDetector) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	for _, tok := range queueImportTokens {
		if strings.Contains(srcLower, strings.ToLower(tok)) {
			return true
		}
	}
	return false
}

func (q *queueDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	for _, sig := range queueSignals {
		for idx, m := range sig.re.FindAllStringIndex(src, -1) {
			// Try to extract topic name from surrounding context
			start := m[0]
			end := m[1]
			if end+100 < len(src) {
				end = m[1] + 100
			} else {
				end = len(src)
			}
			context := src[start:end]
			topic := sig.sdk
			if tm := queueTopicRE.FindStringSubmatch(context); tm != nil {
				topic = tm[1]
			}

			key := fmt.Sprintf("%s:%s:%d", sig.sdk, topic, idx)
			if seen[key] {
				continue
			}
			seen[key] = true

			results = append(results, makeEntity(filePath,
				fmt.Sprintf("queue_%s_%s", strings.ReplaceAll(sig.sdk, ".", "_"), topic),
				"SCOPE.Queue", sig.relType, language,
				lineOf(src, m[0]),
				map[string]string{
					"kind":          "event_queue",
					"relationship":  sig.relType,
					"sdk":           sig.sdk,
					"topic_or_queue": topic,
				}))
		}
	}

	return results
}

func init() {
	Register(&queueDetector{})
}
