package dashboard

// handlers_topology_test.go — unit tests for the broadened collectTopology
// function (#946: Redis pub/sub, Redis Streams, serverless, async tasks).

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
