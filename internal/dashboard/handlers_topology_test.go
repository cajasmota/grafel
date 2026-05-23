package dashboard

// handlers_topology_test.go — unit tests for the broadened collectTopology
// function (#946: Redis pub/sub, Redis Streams, serverless, async tasks).
// Extended in #1139: broker_canonical, owning_service, broker_groups.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ---------------------------------------------------------------------------
// classifyTopologyBucket — uses entity Name (not hashed ID)
// ---------------------------------------------------------------------------

func TestClassifyTopologyBucket(t *testing.T) {
	cases := []struct {
		kind   string
		name   string // entity Name, not hashed ID
		props  map[string]string
		expect string
	}{
		// Existing kinds — pass any name; classification is by kind
		{"MessageTopic", "UserCreated", nil, "topic"},
		{"Queue", "orders", map[string]string{"broker": "rabbitmq"}, "queue"},
		{"ChannelEvent", "chat-events", nil, "channel"},
		{"SCOPE.Queue", "some-queue", nil, "queue"},
		// #1116: Task / ScheduledJob entity kinds
		{"Task", "send_invoice", map[string]string{"framework": "celery"}, "queue"},
		{"SCOPE.Task", "process_order", map[string]string{"framework": "dramatiq"}, "queue"},
		{"ScheduledJob", "nightly_report", map[string]string{"framework": "celery_beat", "schedule": "0 0 * * *"}, "queue"},
		{"SCOPE.ScheduledJob", "cleanup_job", map[string]string{"framework": "bullmq"}, "queue"},
		// New Name-prefix classifications
		{"SCOPE.Queue", "channel:redis-pubsub:orders", nil, "channel"},
		{"SCOPE.Queue", "channel:redis-pubsub:notifications", map[string]string{"channel_type": "pubsub"}, "channel"},
		{"SCOPE.Queue", "stream:redis:events", nil, "queue"},
		{"SCOPE.Queue", "task:dramatiq:send_email", nil, "queue"},
		{"SCOPE.Queue", "task:rq:process_order", nil, "queue"},
		{"SCOPE.Queue", "task:hangfire:BackgroundJob", nil, "queue"},
		// ServerlessFunction: matched by kind
		{"SCOPE.ServerlessFunction", "aws-lambda:OrderProcessor", nil, "function"},
		{"SCOPE.ServerlessFunction", "gcp-cloudfunction:onUserCreate", nil, "function"},
		{"SCOPE.ServerlessFunction", "azure-function:HttpTrigger", nil, "function"},
		// Name-prefix serverless (when kind is already ServerlessFunction)
		{"SCOPE.ServerlessFunction", "aws-lambda:fn", nil, "function"},
		// Unrelated entities
		{"Function", "myFunc", nil, ""},
		{"Class", "MyClass", nil, ""},
	}

	for _, tc := range cases {
		got := classifyTopologyBucket(tc.kind, tc.name, tc.props)
		if got != tc.expect {
			t.Errorf("classifyTopologyBucket(%q, %q, %v) = %q, want %q", tc.kind, tc.name, tc.props, got, tc.expect)
		}
	}
}

// ---------------------------------------------------------------------------
// inferBrokerFromName
// ---------------------------------------------------------------------------

func TestInferBrokerFromName(t *testing.T) {
	cases := []struct {
		name   string
		expect string
	}{
		{"stream:redis:orders", "redis"},
		{"task:dramatiq:send_email", "dramatiq"},
		{"task:rq:process_order", "rq"},
		{"task:hangfire:Job.Execute", "hangfire"},
		{"task:quartz:MyJob", "quartz"},
		{"task:quartz.net:MyJob", "quartz"},
		{"task:unknown-framework:job", "task-queue"},
		{"aws-lambda:fn", ""},
		{"channel:redis-pubsub:orders", ""},
	}
	for _, tc := range cases {
		got := inferBrokerFromName(tc.name)
		if got != tc.expect {
			t.Errorf("inferBrokerFromName(%q) = %q, want %q", tc.name, got, tc.expect)
		}
	}
}

// ---------------------------------------------------------------------------
// collectTopology — Redis pub/sub channels
// ---------------------------------------------------------------------------

func TestCollectTopology_RedisPubSub(t *testing.T) {
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{
				// Entity ID is a hash (as stored by the engine); Name carries the semantic prefix.
				ID:         "abcd1234",
				Name:       "channel:redis-pubsub:notifications",
				Kind:       "SCOPE.Queue",
				SourceFile: "",
				Language:   "python",
				Properties: map[string]string{
					"broker":       "redis",
					"channel_type": "pubsub",
				},
			},
			{
				ID:         "fn:publisher",
				Name:       "publisher",
				Kind:       "SCOPE.Function",
				SourceFile: "app/notify.py",
			},
			{
				ID:         "fn:subscriber",
				Name:       "subscriber",
				Kind:       "SCOPE.Function",
				SourceFile: "app/handler.py",
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "fn:publisher", ToID: "abcd1234", Kind: "PUBLISHES_TO"},
			{ID: "r2", FromID: "fn:subscriber", ToID: "abcd1234", Kind: "SUBSCRIBES_TO"},
		},
	}
	grp := &DashGroup{
		Name:  "g",
		Repos: map[string]*DashRepo{"svc": {Slug: "svc", Doc: doc}},
	}

	topics, queues, channels, functions := collectTopology(grp)
	if len(topics) != 0 {
		t.Errorf("expected 0 topics, got %d", len(topics))
	}
	if len(queues) != 0 {
		t.Errorf("expected 0 queues, got %d", len(queues))
	}
	if len(functions) != 0 {
		t.Errorf("expected 0 functions, got %d", len(functions))
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	ch := channels[0]
	// Redis pub/sub channel_type is normalized to "redis_pubsub" for frontend
	// protocol matching (#946).
	if ch["channel_type"] != "redis_pubsub" {
		t.Errorf("channel_type = %q, want redis_pubsub", ch["channel_type"])
	}
	emitters, _ := ch["emitters"].([]string)
	subscribers, _ := ch["subscribers"].([]string)
	if len(emitters) != 1 {
		t.Errorf("expected 1 emitter, got %d", len(emitters))
	}
	if len(subscribers) != 1 {
		t.Errorf("expected 1 subscriber, got %d", len(subscribers))
	}
}

// ---------------------------------------------------------------------------
// collectTopology — Redis Streams
// ---------------------------------------------------------------------------

func TestCollectTopology_RedisStreams(t *testing.T) {
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{
				// ID is a hash; Name carries the semantic prefix.
				ID:         "efgh5678",
				Name:       "stream:redis:events",
				Kind:       "SCOPE.Queue",
				Properties: map[string]string{"broker": "redis", "channel_type": "stream"},
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "fn:producer", ToID: "efgh5678", Kind: "PUBLISHES_TO"},
			{ID: "r2", FromID: "fn:consumer", ToID: "efgh5678", Kind: "SUBSCRIBES_TO"},
		},
	}
	grp := &DashGroup{
		Name:  "g",
		Repos: map[string]*DashRepo{"svc": {Slug: "svc", Doc: doc}},
	}

	_, queues, _, _ := collectTopology(grp)
	if len(queues) != 1 {
		t.Fatalf("expected 1 queue, got %d", len(queues))
	}
	q := queues[0]
	if q["broker"] != "redis" {
		t.Errorf("broker = %v, want redis", q["broker"])
	}
	producers, _ := q["producers"].([]string)
	if len(producers) != 1 {
		t.Errorf("expected 1 producer, got %d", len(producers))
	}
}

// ---------------------------------------------------------------------------
// collectTopology — async tasks (dramatiq)
// ---------------------------------------------------------------------------

func TestCollectTopology_AsyncTasks(t *testing.T) {
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			// Dramatiq task (from #941 extractor — stored as SCOPE.Queue entity
			// with task: prefix in entity Name).
			{
				ID:         "ijkl9012",
				Name:       "task:dramatiq:send_email",
				Kind:       "SCOPE.Queue",
				Properties: map[string]string{"framework": "dramatiq", "broker": ""},
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "fn:caller", ToID: "ijkl9012", Kind: "PUBLISHES_TO"},
			{ID: "r2", FromID: "fn:worker", ToID: "ijkl9012", Kind: "SUBSCRIBES_TO"},
		},
	}
	grp := &DashGroup{
		Name:  "g",
		Repos: map[string]*DashRepo{"svc": {Slug: "svc", Doc: doc}},
	}

	_, queues, _, _ := collectTopology(grp)
	if len(queues) != 1 {
		t.Fatalf("expected 1 queue for task entity, got %d", len(queues))
	}
	q := queues[0]
	if q["framework"] != "dramatiq" {
		t.Errorf("framework = %v, want dramatiq", q["framework"])
	}
}

// ---------------------------------------------------------------------------
// collectTopology — Task (celery) + ScheduledJob entities (#1116)
// ---------------------------------------------------------------------------

// TestCollectTopology_CeleryTaskAndScheduledJob covers the vocabulary-mismatch
// fix from #1116: entities with kind=Task (from Celery/dramatiq/RQ extractors)
// and kind=ScheduledJob (from the scheduled-job pass) must appear in the queues
// bucket with the correct framework property. ScheduledJob entries must also
// carry scheduled:true and the schedule expression.
func TestCollectTopology_CeleryTaskAndScheduledJob(t *testing.T) {
	doc := &graph.Document{
		Repo: "client-fixture-a",
		Entities: []graph.Entity{
			// Task entity emitted by Celery extractor (kind has no SCOPE. prefix in
			// this fixture, matching what real custom extractors may emit).
			{
				ID:         "task:send_invoice",
				Name:       "send_invoice",
				Kind:       "Task",
				SourceFile: "worker/tasks.py",
				Language:   "python",
				Properties: map[string]string{
					"framework":    "celery",
					"pattern_type": "task",
				},
			},
			// ScheduledJob entity emitted by the scheduled-job pass (SCOPE. prefix).
			{
				ID:         "celery_beat:nightly_report",
				Name:       "nightly_report",
				Kind:       "SCOPE.ScheduledJob",
				SourceFile: "worker/beat.py",
				Language:   "python",
				Properties: map[string]string{
					"framework":    "celery_beat",
					"schedule":     "0 0 * * *",
					"pattern_type": "scheduled_job_synthesis",
				},
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "fn:api_handler", ToID: "task:send_invoice", Kind: "PUBLISHES_TO"},
			{ID: "r2", FromID: "fn:worker", ToID: "task:send_invoice", Kind: "SUBSCRIBES_TO"},
		},
	}
	grp := &DashGroup{
		Name:  "g",
		Repos: map[string]*DashRepo{"client-fixture-a": {Slug: "client-fixture-a", Doc: doc}},
	}

	_, queues, _, _ := collectTopology(grp)

	if len(queues) != 2 {
		t.Fatalf("expected 2 queue entries (1 Task + 1 ScheduledJob), got %d", len(queues))
	}

	// Find each entry by label.
	var taskEntry, scheduledEntry map[string]any
	for _, q := range queues {
		switch q["label"] {
		case "send_invoice":
			taskEntry = q
		case "nightly_report":
			scheduledEntry = q
		}
	}

	// Task entry checks.
	if taskEntry == nil {
		t.Fatal("Task entity 'send_invoice' not found in queues bucket")
	}
	if taskEntry["framework"] != "celery" {
		t.Errorf("Task framework = %v, want celery", taskEntry["framework"])
	}
	if _, hasScheduled := taskEntry["scheduled"]; hasScheduled {
		t.Errorf("Task entry should NOT have scheduled field, but it does")
	}
	producers, _ := taskEntry["producers"].([]string)
	consumers, _ := taskEntry["consumers"].([]string)
	if len(producers) != 1 {
		t.Errorf("expected 1 producer for Task, got %d", len(producers))
	}
	if len(consumers) != 1 {
		t.Errorf("expected 1 consumer for Task, got %d", len(consumers))
	}

	// ScheduledJob entry checks.
	if scheduledEntry == nil {
		t.Fatal("ScheduledJob entity 'nightly_report' not found in queues bucket")
	}
	if scheduledEntry["framework"] != "celery_beat" {
		t.Errorf("ScheduledJob framework = %v, want celery_beat", scheduledEntry["framework"])
	}
	if scheduledEntry["scheduled"] != true {
		t.Errorf("ScheduledJob entry should have scheduled=true, got %v", scheduledEntry["scheduled"])
	}
	if scheduledEntry["schedule"] != "0 0 * * *" {
		t.Errorf("ScheduledJob schedule = %v, want '0 0 * * *'", scheduledEntry["schedule"])
	}
}

// ---------------------------------------------------------------------------
// collectTopology — serverless functions (Lambda)
// ---------------------------------------------------------------------------

func TestCollectTopology_Serverless(t *testing.T) {
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{
				// Name carries the semantic aws-lambda: prefix.
				ID:         "mnop3456",
				Name:       "aws-lambda:OrderProcessor",
				Kind:       "SCOPE.ServerlessFunction",
				Properties: map[string]string{"provider": "aws-lambda", "function_name": "OrderProcessor"},
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "fn:api_handler", ToID: "mnop3456", Kind: "CALLS"},
			{ID: "r2", FromID: "fn:lambda_handler", ToID: "mnop3456", Kind: "HANDLES"},
		},
	}
	grp := &DashGroup{
		Name:  "g",
		Repos: map[string]*DashRepo{"svc": {Slug: "svc", Doc: doc}},
	}

	_, _, _, functions := collectTopology(grp)
	if len(functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(functions))
	}
	fn := functions[0]
	if fn["provider"] != "aws-lambda" {
		t.Errorf("provider = %v, want aws-lambda", fn["provider"])
	}
	invokers, _ := fn["invokers"].([]string)
	handlers, _ := fn["handlers"].([]string)
	if len(invokers) != 1 {
		t.Errorf("expected 1 invoker, got %d", len(invokers))
	}
	if len(handlers) != 1 {
		t.Errorf("expected 1 handler, got %d", len(handlers))
	}
}

// ---------------------------------------------------------------------------
// collectTopology — existing Kafka regression
// ---------------------------------------------------------------------------

func TestCollectTopology_KafkaRegression(t *testing.T) {
	doc := &graph.Document{
		Repo: "svc",
		Entities: []graph.Entity{
			{
				ID:         "UserCreatedTopic",
				Name:       "UserCreatedTopic",
				Kind:       "MessageTopic",
				Properties: map[string]string{"broker": "kafka"},
			},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "svc_producer", ToID: "UserCreatedTopic", Kind: "PUBLISHES_TO"},
			{ID: "r2", FromID: "svc_consumer", ToID: "UserCreatedTopic", Kind: "SUBSCRIBES_TO"},
		},
	}
	grp := &DashGroup{
		Name:  "g",
		Repos: map[string]*DashRepo{"svc": {Slug: "svc", Doc: doc}},
	}

	topics, queues, channels, functions := collectTopology(grp)
	if len(topics) != 1 {
		t.Fatalf("expected 1 kafka topic, got %d", len(topics))
	}
	if len(queues) != 0 {
		t.Errorf("expected 0 queues, got %d", len(queues))
	}
	if len(channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(channels))
	}
	if len(functions) != 0 {
		t.Errorf("expected 0 functions, got %d", len(functions))
	}
	if topics[0]["broker"] != "kafka" {
		t.Errorf("broker = %v, want kafka", topics[0]["broker"])
	}
}

// ---------------------------------------------------------------------------
// #1139: broker_canonical helper
// ---------------------------------------------------------------------------

func TestBrokerCanonical(t *testing.T) {
	cases := []struct {
		broker    string
		framework string
		want      string
	}{
		{"rabbitmq", "", "rabbitmq"},
		{"amqp", "", "rabbitmq"},
		{"redis", "", "redis"},
		{"sqs", "", "sqs"},
		{"aws-sqs", "", "sqs"},
		{"pubsub", "", "pubsub"},
		{"gcp-pubsub", "", "pubsub"},
		{"nats", "", "nats"},
		{"kafka", "", "kafka"},
		{"celery", "", "celery"},
		{"dramatiq", "", "dramatiq"},
		{"", "celery", "celery"},
		{"", "celery_beat", "celery"},
		{"", "dramatiq", "dramatiq"},
		{"", "rq", "rq"},
		{"", "sidekiq", "sidekiq"},
		{"", "bullmq", "bullmq"},
		{"", "bull", "bullmq"},
		{"", "hangfire", "hangfire"},
		{"", "quartz", "quartz"},
		{"", "quartz.net", "quartz"},
		{"", "", "unknown"},
		{"custom-broker", "", "custom-broker"},
	}
	for _, tc := range cases {
		got := brokerCanonical(tc.broker, tc.framework)
		if got != tc.want {
			t.Errorf("brokerCanonical(%q, %q) = %q, want %q", tc.broker, tc.framework, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// #1139: owningService helper
// ---------------------------------------------------------------------------

func TestOwningService(t *testing.T) {
	if got := owningService(map[string]string{"service": "orders-svc"}, "repo-a"); got != "orders-svc" {
		t.Errorf("expected orders-svc, got %q", got)
	}
	if got := owningService(map[string]string{}, "repo-a"); got != "repo-a" {
		t.Errorf("expected repo-a fallback, got %q", got)
	}
	if got := owningService(nil, "repo-b"); got != "repo-b" {
		t.Errorf("expected repo-b fallback on nil props, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// #1139: broker_groups — 2 brokers, 5 topics each
// ---------------------------------------------------------------------------

func TestCollectTopologyResponse_BrokerGroups_TwoBrokers(t *testing.T) {
	// Build a group with 2 repos, each contributing 5 topics under a different broker.
	makeTopics := func(repo, broker string, n int) []graph.Entity {
		ents := make([]graph.Entity, n)
		for i := 0; i < n; i++ {
			ents[i] = graph.Entity{
				ID:         graph.EntityID(repo, "MessageTopic", string(rune('A'+i)), ""),
				Name:       string(rune('A' + i)),
				Kind:       "MessageTopic",
				Properties: map[string]string{"broker": broker},
			}
		}
		return ents
	}
	makeRels := func(entities []graph.Entity, producerID, consumerID string) []graph.Relationship {
		var rels []graph.Relationship
		for _, e := range entities {
			rels = append(rels,
				graph.Relationship{ID: "p-" + e.ID, FromID: producerID, ToID: e.ID, Kind: "PUBLISHES_TO"},
				graph.Relationship{ID: "c-" + e.ID, FromID: e.ID, ToID: consumerID, Kind: "SUBSCRIBES_TO"},
			)
		}
		return rels
	}

	rabbitTopics := makeTopics("svc-a", "rabbitmq", 5)
	sqsTopics := makeTopics("svc-b", "sqs", 5)

	grp := &DashGroup{
		Name: "g",
		Repos: map[string]*DashRepo{
			"svc-a": {Slug: "svc-a", Doc: &graph.Document{
				Repo:     "svc-a",
				Entities: rabbitTopics,
				Relationships: makeRels(rabbitTopics, "svc-a::producer", "svc-a::consumer"),
			}},
			"svc-b": {Slug: "svc-b", Doc: &graph.Document{
				Repo:     "svc-b",
				Entities: sqsTopics,
				Relationships: makeRels(sqsTopics, "svc-b::producer", "svc-b::consumer"),
			}},
		},
	}

	resp := collectTopologyResponse(grp, "", nil)

	if len(resp.BrokerGroups) != 2 {
		t.Fatalf("expected 2 broker_groups, got %d: %v", len(resp.BrokerGroups), resp.BrokerGroups)
	}

	// Groups are sorted alphabetically: rabbitmq < sqs.
	bg0 := resp.BrokerGroups[0]
	bg1 := resp.BrokerGroups[1]

	if bg0.Broker != "rabbitmq" {
		t.Errorf("broker_groups[0].broker = %q, want rabbitmq", bg0.Broker)
	}
	if bg0.Count != 5 {
		t.Errorf("broker_groups[0].count = %d, want 5", bg0.Count)
	}
	if bg1.Broker != "sqs" {
		t.Errorf("broker_groups[1].broker = %q, want sqs", bg1.Broker)
	}
	if bg1.Count != 5 {
		t.Errorf("broker_groups[1].count = %d, want 5", bg1.Count)
	}

	// All topics have both producer and consumer → all active.
	if bg0.HealthSummary.Active != 5 {
		t.Errorf("rabbitmq health_summary.active = %d, want 5", bg0.HealthSummary.Active)
	}
	if bg0.OrphanPublishers != 0 || bg0.OrphanSubscribers != 0 {
		t.Errorf("unexpected orphans in rabbitmq group")
	}

	// Per-entry broker_canonical and owning_service fields.
	for _, entry := range resp.Topics {
		if _, ok := entry["broker_canonical"]; !ok {
			t.Errorf("topic entry missing broker_canonical field")
		}
		if _, ok := entry["owning_service"]; !ok {
			t.Errorf("topic entry missing owning_service field")
		}
	}
}

// ---------------------------------------------------------------------------
// #1139: broker_groups — cross-repo topic via CrossRepoLink
// ---------------------------------------------------------------------------

func TestCollectTopologyResponse_BrokerGroups_CrossRepo(t *testing.T) {
	topicID := graph.EntityID("svc-a", "MessageTopic", "OrderPlaced", "")
	prefixedID := dashPrefixedID("svc-a", topicID)

	grp := &DashGroup{
		Name: "g",
		Repos: map[string]*DashRepo{
			"svc-a": {Slug: "svc-a", Doc: &graph.Document{
				Repo: "svc-a",
				Entities: []graph.Entity{
					{ID: topicID, Name: "OrderPlaced", Kind: "MessageTopic",
						Properties: map[string]string{"broker": "rabbitmq"}},
				},
				Relationships: []graph.Relationship{
					{ID: "p1", FromID: "svc-a::producer", ToID: topicID, Kind: "PUBLISHES_TO"},
					{ID: "c1", FromID: topicID, ToID: "svc-b::consumer", Kind: "SUBSCRIBES_TO"},
				},
			}},
		},
		// Cross-repo link: svc-b consumer subscribes to svc-a topic.
		Links: []CrossRepoLink{
			{Source: prefixedID, Target: "svc-b::consumer", Kind: "SUBSCRIBES_TO"},
		},
	}

	resp := collectTopologyResponse(grp, "", nil)

	if len(resp.BrokerGroups) != 1 {
		t.Fatalf("expected 1 broker_group, got %d", len(resp.BrokerGroups))
	}
	bg := resp.BrokerGroups[0]
	if bg.Broker != "rabbitmq" {
		t.Errorf("broker = %q, want rabbitmq", bg.Broker)
	}
	if bg.CrossRepoTopicCount != 1 {
		t.Errorf("cross_repo_topic_count = %d, want 1", bg.CrossRepoTopicCount)
	}
}

// ---------------------------------------------------------------------------
// #1139: broker_canonical = 'celery' for framework=celery entity
// ---------------------------------------------------------------------------

func TestCollectTopologyResponse_BrokerGroups_CeleryFramework(t *testing.T) {
	grp := &DashGroup{
		Name: "g",
		Repos: map[string]*DashRepo{
			"worker": {Slug: "worker", Doc: &graph.Document{
				Repo: "worker",
				Entities: []graph.Entity{
					{
						ID:   "task:celery:send_invoice",
						Name: "send_invoice",
						Kind: "Task",
						Properties: map[string]string{
							"framework": "celery",
						},
					},
				},
			}},
		},
	}

	resp := collectTopologyResponse(grp, "", nil)

	// Task entities go into queues bucket.
	if len(resp.Queues) != 1 {
		t.Fatalf("expected 1 queue, got %d", len(resp.Queues))
	}
	entry := resp.Queues[0]
	if entry["broker_canonical"] != "celery" {
		t.Errorf("broker_canonical = %v, want celery", entry["broker_canonical"])
	}
	if entry["owning_service"] != "worker" {
		t.Errorf("owning_service = %v, want worker (repo slug fallback)", entry["owning_service"])
	}

	if len(resp.BrokerGroups) != 1 {
		t.Fatalf("expected 1 broker_group for celery, got %d", len(resp.BrokerGroups))
	}
	bg := resp.BrokerGroups[0]
	if bg.Broker != "celery" {
		t.Errorf("broker_groups[0].broker = %q, want celery", bg.Broker)
	}
}

// ---------------------------------------------------------------------------
// #1695 — Kafka topic cross-repo dedup: ONE node per (broker, topic_name)
// ---------------------------------------------------------------------------

// TestCollectTopology_KafkaTopicDedup is the regression test for #1695.
//
// The same Kafka topic "payments.settled" appears as a SCOPE.MessageTopic
// entity in 8 different repos (each with its own graph-stamped entity ID).
// Before this fix, collectTopologyResponse emitted 8 separate topic entries.
// After this fix, it must emit exactly ONE entry with:
//   - all producers merged across repos
//   - all consumers merged across repos
//   - appears_in listing all 8 repos
func TestCollectTopology_KafkaTopicDedup(t *testing.T) {
	// Simulate 8 repos each carrying a SCOPE.MessageTopic for "kafka:payments.settled"
	// with their own graph-stamped entity IDs (as happens in production).
	repos := map[string]*DashRepo{}
	repoSlugs := []string{
		"orders", "payments", "notifications", "billing",
		"analytics", "fraud", "shipping", "reporting",
	}
	for _, slug := range repoSlugs {
		// Mimic graph.EntityID: each repo produces a different stamped ID.
		entityID := graph.EntityID(slug, "SCOPE.MessageTopic", "kafka:payments.settled", "")
		var rels []graph.Relationship
		// Some repos publish, some subscribe, some both.
		switch slug {
		case "payments":
			rels = append(rels, graph.Relationship{
				ID: "pub-" + slug, FromID: "PaymentService", ToID: entityID, Kind: "PUBLISHES_TO",
			})
		case "orders":
			rels = append(rels, graph.Relationship{
				ID: "sub-" + slug, FromID: entityID, ToID: "OrderHandler", Kind: "SUBSCRIBES_TO",
			})
		default:
			rels = append(rels, graph.Relationship{
				ID: "sub-" + slug, FromID: entityID, ToID: slug + "Handler", Kind: "SUBSCRIBES_TO",
			})
		}
		repos[slug] = &DashRepo{
			Slug: slug,
			Doc: &graph.Document{
				Repo: slug,
				Entities: []graph.Entity{
					{
						ID:   entityID,
						Name: "kafka:payments.settled",
						Kind: "SCOPE.MessageTopic",
						Properties: map[string]string{
							"broker":       "kafka",
							"topic_name":   "payments.settled",
							"pattern_type": "kafka_synthesis",
						},
					},
				},
				Relationships: rels,
			},
		}
	}

	grp := &DashGroup{Name: "g", Repos: repos}
	resp := collectTopologyResponse(grp, "", nil)

	// #1695 core assertion: exactly ONE topic entry for payments.settled.
	if len(resp.Topics) != 1 {
		t.Fatalf("#1695 regression: expected 1 merged topic entry for payments.settled, got %d (was one per repo before fix)", len(resp.Topics))
	}

	entry := resp.Topics[0]

	// Label must be the canonical topic name.
	if entry["label"] != "kafka:payments.settled" {
		t.Errorf("label = %q, want kafka:payments.settled", entry["label"])
	}

	// All 8 repos must appear in appears_in.
	appearsIn, _ := entry["appears_in"].([]string)
	if len(appearsIn) != 8 {
		t.Errorf("appears_in has %d repos, want 8: %v", len(appearsIn), appearsIn)
	}

	// Producers: only "payments" repo publishes → 1 producer.
	producers, _ := entry["producers"].([]string)
	if len(producers) != 1 {
		t.Errorf("producers: got %d, want 1: %v", len(producers), producers)
	}

	// Consumers: 7 other repos subscribe → 7 consumers.
	consumers, _ := entry["consumers"].([]string)
	if len(consumers) != 7 {
		t.Errorf("consumers: got %d, want 7: %v", len(consumers), consumers)
	}

	// broker_groups must have exactly one kafka entry with count=1 (not 8).
	if len(resp.BrokerGroups) != 1 {
		t.Fatalf("expected 1 broker_group, got %d", len(resp.BrokerGroups))
	}
	bg := resp.BrokerGroups[0]
	if bg.Broker != "kafka" {
		t.Errorf("broker_groups[0].broker = %q, want kafka", bg.Broker)
	}
	if bg.Count != 1 {
		t.Errorf("broker_groups[0].count = %d, want 1 (not 8)", bg.Count)
	}
	// The merged topic has both producers and consumers → Active=1, not Orphan.
	if bg.HealthSummary.Active != 1 {
		t.Errorf("health_summary.active = %d, want 1", bg.HealthSummary.Active)
	}
}
