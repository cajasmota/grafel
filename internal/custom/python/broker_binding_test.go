package python_test

// broker_binding_test.go — tests for Issue #3074
// Covers broker_binding + result_backend_binding (celery),
// broker_binding + retry_policy_extraction (dramatiq),
// broker_binding + retry_policy + schedule_extraction (rq).

import "testing"

// ============================================================================
// Celery — broker_binding
// ============================================================================

func TestCelery_BrokerBinding_ConfAssign(t *testing.T) {
	src := `from celery import Celery

app = Celery("myapp")
app.conf.broker_url = "amqp://guest:guest@localhost//"
`
	ents := extract(t, "python_celery", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "broker_binding" && e.Props["framework"] == "celery" {
			if e.Props["broker_url"] != "amqp://guest:guest@localhost//" {
				t.Errorf("expected broker_url amqp://guest:guest@localhost//, got %q", e.Props["broker_url"])
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected broker_binding entity from app.conf.broker_url assign")
	}
}

func TestCelery_BrokerBinding_EnvVar(t *testing.T) {
	src := `CELERY_BROKER_URL = "redis://localhost:6379/0"
`
	ents := extract(t, "python_celery", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "broker_binding" && e.Props["broker_url"] == "redis://localhost:6379/0" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected broker_binding entity from CELERY_BROKER_URL env-var style")
	}
}

func TestCelery_BrokerBinding_Constructor(t *testing.T) {
	src := `app = Celery("tasks", broker="redis://localhost:6379/1")
`
	ents := extract(t, "python_celery", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "broker_binding" && e.Props["broker_url"] == "redis://localhost:6379/1" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected broker_binding entity from Celery(broker=...) constructor")
	}
}

// ============================================================================
// Celery — result_backend_binding
// ============================================================================

func TestCelery_ResultBackendBinding_ConfAssign(t *testing.T) {
	src := `app.conf.result_backend = "redis://localhost:6379/0"
`
	ents := extract(t, "python_celery", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "result_backend_binding" && e.Props["framework"] == "celery" {
			if e.Props["result_backend"] != "redis://localhost:6379/0" {
				t.Errorf("expected result_backend redis://localhost:6379/0, got %q", e.Props["result_backend"])
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected result_backend_binding entity from app.conf.result_backend assign")
	}
}

func TestCelery_ResultBackendBinding_EnvVar(t *testing.T) {
	src := `CELERY_RESULT_BACKEND = "db+sqlite:///results.db"
`
	ents := extract(t, "python_celery", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "result_backend_binding" && e.Props["result_backend"] == "db+sqlite:///results.db" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected result_backend_binding entity from CELERY_RESULT_BACKEND env-var style")
	}
}

func TestCelery_ResultBackendBinding_Constructor(t *testing.T) {
	src := `app = Celery("tasks", backend="redis://localhost:6379/2")
`
	ents := extract(t, "python_celery", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "result_backend_binding" && e.Props["result_backend"] == "redis://localhost:6379/2" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected result_backend_binding entity from Celery(backend=...) constructor")
	}
}

// ============================================================================
// Dramatiq — broker_binding
// ============================================================================

func TestDramatiq_BrokerBinding_SetBroker(t *testing.T) {
	src := `import dramatiq
from dramatiq.brokers.rabbitmq import RabbitmqBroker

rabbitmq_broker = RabbitmqBroker(url="amqp://localhost")
dramatiq.set_broker(rabbitmq_broker)
`
	ents := extract(t, "python_dramatiq", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "broker_binding" && e.Props["framework"] == "dramatiq" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected broker_binding entity from dramatiq.set_broker(...)")
	}
}

func TestDramatiq_BrokerBinding_InlineBroker(t *testing.T) {
	src := `import dramatiq
from dramatiq.brokers.redis import RedisBroker

dramatiq.set_broker(RedisBroker(url="redis://localhost:6379"))
`
	ents := extract(t, "python_dramatiq", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "broker_binding" && e.Props["broker_class"] == "RedisBroker" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected broker_binding entity with broker_class=RedisBroker")
	}
}

// ============================================================================
// Dramatiq — retry_policy_extraction
// ============================================================================

func TestDramatiq_RetryPolicy_MaxRetries(t *testing.T) {
	src := `import dramatiq

@dramatiq.actor(max_retries=5)
def send_email(recipient):
    pass
`
	ents := extract(t, "python_dramatiq", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "retry_policy" && e.Props["framework"] == "dramatiq" {
			if e.Props["max_retries"] != "5" {
				t.Errorf("expected max_retries=5, got %q", e.Props["max_retries"])
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected retry_policy entity from @dramatiq.actor(max_retries=5)")
	}
}

func TestDramatiq_RetryPolicy_ZeroRetries(t *testing.T) {
	src := `@dramatiq.actor(max_retries=0)
def idempotent_task():
    pass
`
	ents := extract(t, "python_dramatiq", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "retry_policy" && e.Props["max_retries"] == "0" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected retry_policy entity with max_retries=0")
	}
}

// ============================================================================
// RQ — broker_binding (Redis connection)
// ============================================================================

func TestRQ_BrokerBinding_RedisHost(t *testing.T) {
	src := `from rq import Queue, Worker
from redis import Redis

conn = Redis(host="redis.example.com", port=6379)
q = Queue(connection=conn)
`
	ents := extract(t, "python_rq", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "broker_binding" && e.Props["framework"] == "rq" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected broker_binding entity from Redis(host=...) connection")
	}
}

func TestRQ_BrokerBinding_RedisFromURL(t *testing.T) {
	src := `from rq import Queue
from redis import Redis

conn = Redis.from_url("redis://myredis:6379/0")
q = Queue(connection=conn)
`
	ents := extract(t, "python_rq", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "broker_binding" && e.Props["redis_url"] == "redis://myredis:6379/0" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected broker_binding entity from Redis.from_url(...)")
	}
}

// ============================================================================
// RQ — retry_policy_extraction
// ============================================================================

func TestRQ_RetryPolicy_RetryMax(t *testing.T) {
	src := `from rq import Queue, Retry
from redis import Redis

conn = Redis(host="localhost")
q = Queue(connection=conn)
q.enqueue(my_task, retry=Retry(max=3))
`
	ents := extract(t, "python_rq", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "retry_policy" && e.Props["framework"] == "rq" {
			if e.Props["max_retries"] != "3" {
				t.Errorf("expected max_retries=3, got %q", e.Props["max_retries"])
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected retry_policy entity from Retry(max=3)")
	}
}

// ============================================================================
// RQ — schedule_extraction (rq-scheduler)
// ============================================================================

func TestRQ_ScheduleExtraction_EnqueueAt(t *testing.T) {
	src := `from rq_scheduler import Scheduler
from redis import Redis
import datetime

conn = Redis(host="localhost")
scheduler = Scheduler(connection=conn)
scheduler.enqueue_at(datetime.datetime(2026, 1, 1, 12), my_job)
`
	ents := extract(t, "python_rq", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "scheduled_job" && e.Props["schedule_type"] == "enqueue_at" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected scheduled_job entity from scheduler.enqueue_at(...)")
	}
}

func TestRQ_ScheduleExtraction_EnqueueIn(t *testing.T) {
	src := `from rq_scheduler import Scheduler
from redis import Redis

conn = Redis(host="localhost")
scheduler = Scheduler(connection=conn)
scheduler.enqueue_in(timedelta(hours=1), my_job)
`
	ents := extract(t, "python_rq", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "scheduled_job" && e.Props["schedule_type"] == "enqueue_in" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected scheduled_job entity from scheduler.enqueue_in(...)")
	}
}

func TestRQ_ScheduleExtraction_Cron(t *testing.T) {
	src := `from rq_scheduler import Scheduler
from redis import Redis

conn = Redis(host="localhost")
scheduler = Scheduler(connection=conn)
scheduler.cron("0 */6 * * *", func=cleanup_job, id="cleanup")
`
	ents := extract(t, "python_rq", src)
	found := false
	for _, e := range ents {
		if e.Subtype == "scheduled_job" && e.Props["schedule_type"] == "cron" {
			if e.Props["cron_expr"] != "0 */6 * * *" {
				t.Errorf("expected cron_expr '0 */6 * * *', got %q", e.Props["cron_expr"])
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected scheduled_job entity from scheduler.cron(...)")
	}
}

// ============================================================================
// Negative tests — no false positives when broker patterns absent
// ============================================================================

func TestCelery_NoBrokerBinding_WhenAbsent(t *testing.T) {
	src := `from celery import shared_task

@shared_task
def my_task():
    pass
`
	ents := extract(t, "python_celery", src)
	for _, e := range ents {
		if e.Subtype == "broker_binding" || e.Subtype == "result_backend_binding" {
			t.Errorf("unexpected broker entity %q in plain task file", e.Name)
		}
	}
}

func TestDramatiq_NoBrokerBinding_WhenAbsent(t *testing.T) {
	src := `import dramatiq

@dramatiq.actor
def process():
    pass
`
	ents := extract(t, "python_dramatiq", src)
	for _, e := range ents {
		if e.Subtype == "broker_binding" {
			t.Errorf("unexpected broker_binding entity in plain actor file")
		}
	}
}

func TestRQ_NoBrokerBinding_WhenNoImport(t *testing.T) {
	// Without rq import, broker binding should not be emitted.
	src := `conn = Redis(host="localhost")
`
	ents := extract(t, "python_rq", src)
	for _, e := range ents {
		if e.Subtype == "broker_binding" {
			t.Errorf("unexpected broker_binding entity without rq import")
		}
	}
}
