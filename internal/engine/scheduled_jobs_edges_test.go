// Tests for the scheduled-job entry-point detection pass (#728).
//
// Each framework has at least one test covering a happy-path detection.
// Tests call applyScheduledJobEdges directly (same pattern as
// kafka_edges_test.go) so they run without the full YAML-rule compiler.
package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// runScheduledDetect is a lightweight in-process driver.
func runScheduledDetect(t *testing.T, lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applyScheduledJobEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

func scheduledJobsByFramework(ents []types.EntityRecord, framework string) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range ents {
		if e.Kind == scheduledJobKind && e.Properties["framework"] == framework {
			out = append(out, e)
		}
	}
	return out
}

func triggersEdges(rels []types.RelationshipRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rels {
		if r.Kind == triggersEdgeKind {
			out = append(out, r)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Python — Celery task decorator
// ---------------------------------------------------------------------------

func TestScheduledJobs_PyCelery_TaskDecorator(t *testing.T) {
	src := `import celery
app = celery.Celery('tasks', broker='redis://localhost')

@app.task
def send_daily_report():
    pass
`
	ents, rels := runScheduledDetect(t, "python", "tasks.py", src)
	jobs := scheduledJobsByFramework(ents, "celery")
	if len(jobs) == 0 {
		t.Fatalf("expected at least 1 celery ScheduledJob entity, got 0 (entities=%v)", ents)
	}
	found := false
	for _, j := range jobs {
		if j.Properties["handler"] == "send_daily_report" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected handler=send_daily_report in %v", jobs)
	}
	edges := triggersEdges(rels)
	if len(edges) == 0 {
		t.Fatalf("expected TRIGGERS edge, got none")
	}
}

// ---------------------------------------------------------------------------
// Python — Celery beat_schedule config dictionary
// ---------------------------------------------------------------------------

func TestScheduledJobs_PyCeleryBeat_ScheduleDict(t *testing.T) {
	src := `from celery.schedules import crontab

beat_schedule = {
    'generate-report': {
        'task': 'app.tasks.generate_report',
        'schedule': crontab(hour=0, minute=0),
    },
}
`
	ents, rels := runScheduledDetect(t, "python", "celeryconfig.py", src)
	jobs := scheduledJobsByFramework(ents, "celery_beat")
	if len(jobs) == 0 {
		t.Fatalf("expected at least 1 celery_beat ScheduledJob entity, got 0")
	}
	taskPath := jobs[0].Properties["task_path"]
	if taskPath != "app.tasks.generate_report" {
		t.Errorf("task_path = %q, want app.tasks.generate_report", taskPath)
	}
	_ = rels
}

// ---------------------------------------------------------------------------
// Python — APScheduler
// ---------------------------------------------------------------------------

func TestScheduledJobs_PyAPScheduler_CronJob(t *testing.T) {
	src := `from apscheduler.schedulers.background import BackgroundScheduler

scheduler = BackgroundScheduler()
scheduler.add_job(cleanup_old_records, trigger='cron', hour=2, minute=30)
scheduler.start()
`
	ents, rels := runScheduledDetect(t, "python", "scheduler.py", src)
	jobs := scheduledJobsByFramework(ents, "apscheduler")
	if len(jobs) == 0 {
		t.Fatalf("expected at least 1 apscheduler ScheduledJob entity, got 0")
	}
	if jobs[0].Properties["handler"] != "cleanup_old_records" {
		t.Errorf("handler = %q, want cleanup_old_records", jobs[0].Properties["handler"])
	}
	edges := triggersEdges(rels)
	if len(edges) == 0 {
		t.Fatalf("expected TRIGGERS edge from apscheduler job, got none")
	}
}

// ---------------------------------------------------------------------------
// Python — schedule library
// ---------------------------------------------------------------------------

func TestScheduledJobs_PyScheduleLib(t *testing.T) {
	src := `import schedule
import time

def send_heartbeat():
    requests.get('http://monitor/ping')

schedule.every(10).minutes.do(send_heartbeat)
`
	ents, rels := runScheduledDetect(t, "python", "heartbeat.py", src)
	jobs := scheduledJobsByFramework(ents, "schedule_lib")
	if len(jobs) == 0 {
		t.Fatalf("expected at least 1 schedule_lib ScheduledJob, got 0")
	}
	if jobs[0].Properties["schedule"] != "every(10).minutes" {
		t.Errorf("schedule = %q, want every(10).minutes", jobs[0].Properties["schedule"])
	}
	_ = rels
}

// ---------------------------------------------------------------------------
// Node — node-cron
// ---------------------------------------------------------------------------

func TestScheduledJobs_NodeCron_Schedule(t *testing.T) {
	src := `const cron = require('node-cron');

cron.schedule('0 0 * * *', sendNightlyDigest);
`
	ents, rels := runScheduledDetect(t, "javascript", "cron.js", src)
	jobs := scheduledJobsByFramework(ents, "node_cron")
	if len(jobs) == 0 {
		t.Fatalf("expected at least 1 node_cron ScheduledJob, got 0")
	}
	if jobs[0].Properties["schedule"] != "0 0 * * *" {
		t.Errorf("schedule = %q, want 0 0 * * *", jobs[0].Properties["schedule"])
	}
	_ = rels
}

// ---------------------------------------------------------------------------
// Node — bull/bullmq repeat job
// ---------------------------------------------------------------------------

func TestScheduledJobs_NodeBull_RepeatCron(t *testing.T) {
	src := `const Queue = require('bull');
const emailQueue = new Queue('emails');

emailQueue.add('sendWeeklyReport', { to: 'all' }, {
  repeat: { cron: '0 9 * * 1' }
});
`
	ents, rels := runScheduledDetect(t, "javascript", "queue.js", src)
	jobs := scheduledJobsByFramework(ents, "bullmq")
	if len(jobs) == 0 {
		t.Fatalf("expected at least 1 bullmq ScheduledJob, got 0")
	}
	if jobs[0].Properties["schedule"] != "0 9 * * 1" {
		t.Errorf("schedule = %q, want 0 9 * * 1", jobs[0].Properties["schedule"])
	}
	_ = rels
}

// ---------------------------------------------------------------------------
// Java — Spring @Scheduled
// ---------------------------------------------------------------------------

func TestScheduledJobs_SpringScheduled_Cron(t *testing.T) {
	src := `package com.example;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

@Component
public class ReportService {

    @Scheduled(cron = "0 0 2 * * *")
    public void generateNightlyReport() {
        // ...
    }
}
`
	ents, rels := runScheduledDetect(t, "java", "ReportService.java", src)
	jobs := scheduledJobsByFramework(ents, "spring_scheduled")
	if len(jobs) == 0 {
		t.Fatalf("expected at least 1 spring_scheduled ScheduledJob, got 0")
	}
	sched := jobs[0].Properties["schedule"]
	if !strings.Contains(sched, "0 0 2 * * *") {
		t.Errorf("schedule %q does not contain cron expression", sched)
	}
	edges := triggersEdges(rels)
	if len(edges) == 0 {
		t.Fatalf("expected TRIGGERS edge from Spring @Scheduled job, got none")
	}
}

// ---------------------------------------------------------------------------
// Java — Spring @Scheduled with fixedRate
// ---------------------------------------------------------------------------

func TestScheduledJobs_SpringScheduled_FixedRate(t *testing.T) {
	src := `import org.springframework.scheduling.annotation.Scheduled;

public class HealthChecker {
    @Scheduled(fixedRate = 30000)
    public void checkHealth() {}
}
`
	ents, rels := runScheduledDetect(t, "java", "HealthChecker.java", src)
	jobs := scheduledJobsByFramework(ents, "spring_scheduled")
	if len(jobs) == 0 {
		t.Fatalf("expected fixedRate Spring job, got 0")
	}
	_ = rels
}

// ---------------------------------------------------------------------------
// Java — Quartz
// ---------------------------------------------------------------------------

func TestScheduledJobs_JavaQuartz(t *testing.T) {
	src := `import org.quartz.*;

JobDetail job = JobBuilder.newJob(EmailJob.class)
    .withIdentity("emailJob").build();

Trigger trigger = TriggerBuilder.newTrigger()
    .withSchedule(CronScheduleBuilder.cronSchedule("0 0 0 * * ?"))
    .build();

scheduler.scheduleJob(job, trigger);
`
	ents, rels := runScheduledDetect(t, "java", "JobScheduler.java", src)
	jobs := scheduledJobsByFramework(ents, "quartz")
	if len(jobs) == 0 {
		t.Fatalf("expected at least 1 quartz ScheduledJob, got 0")
	}
	if jobs[0].Properties["handler"] != "EmailJob" {
		t.Errorf("handler = %q, want EmailJob", jobs[0].Properties["handler"])
	}
	_ = rels
}

// ---------------------------------------------------------------------------
// Go — robfig/cron
// ---------------------------------------------------------------------------

func TestScheduledJobs_GoCron_AddFunc(t *testing.T) {
	src := `package main

import (
    "github.com/robfig/cron/v3"
)

func main() {
    c := cron.New()
    c.AddFunc("0 0 * * *", cleanupExpiredSessions)
    c.Start()
}
`
	ents, rels := runScheduledDetect(t, "go", "main.go", src)
	jobs := scheduledJobsByFramework(ents, "go_cron")
	if len(jobs) == 0 {
		t.Fatalf("expected at least 1 go_cron ScheduledJob, got 0")
	}
	if jobs[0].Properties["schedule"] != "0 0 * * *" {
		t.Errorf("schedule = %q, want 0 0 * * *", jobs[0].Properties["schedule"])
	}
	edges := triggersEdges(rels)
	if len(edges) == 0 {
		t.Fatalf("expected TRIGGERS edge for go_cron, got none")
	}
}

// ---------------------------------------------------------------------------
// Go — asynq (hibiken/asynq) task queue (#4923)
// ---------------------------------------------------------------------------

func TestScheduledJobs_GoAsynq_HandlerAndEnqueueConverge(t *testing.T) {
	src := `package main

import "github.com/hibiken/asynq"

func registerHandlers(mux *asynq.ServeMux) {
    mux.HandleFunc("email:send", handleEmailSend)
    mux.Handle("image:resize", asynq.HandlerFunc(handleResize))
}

func DispatchWelcomeEmail() error {
    task := asynq.NewTask("email:send", payload)
    _, err := client.Enqueue(task)
    return err
}
`
	ents, rels := runScheduledDetect(t, "go", "tasks.go", src)

	jobs := scheduledJobsByFramework(ents, "asynq")
	if len(jobs) != 2 {
		t.Fatalf("expected 2 asynq ScheduledJob entities (email:send, image:resize), got %d", len(jobs))
	}
	byType := map[string]types.EntityRecord{}
	for _, j := range jobs {
		byType[j.Properties["task_type"]] = j
	}
	if _, ok := byType["email:send"]; !ok {
		t.Fatalf("missing asynq job for task_type email:send")
	}
	if _, ok := byType["image:resize"]; !ok {
		t.Fatalf("missing asynq job for task_type image:resize")
	}

	// TRIGGERS edge: HandleFunc handler is resolved to handleEmailSend.
	trig := triggersEdges(rels)
	foundTrigger := false
	for _, e := range trig {
		if e.ToID == "Function:handleEmailSend" && e.Properties.Get("framework") == "asynq" {
			foundTrigger = true
		}
	}
	if !foundTrigger {
		t.Errorf("expected TRIGGERS edge asynq job -> Function:handleEmailSend, got %+v", trig)
	}

	// ENQUEUES edge: producer (asynq.NewTask) converges on the same task-type
	// node from its enclosing function.
	enq := enqueuesEdges(rels)
	foundEnqueue := false
	for _, e := range enq {
		if e.ToID == scheduledJobKind+":asynq:email:send" &&
			e.FromID == "SCOPE.Operation:DispatchWelcomeEmail" &&
			e.Properties.Get("framework") == "asynq" {
			foundEnqueue = true
		}
	}
	if !foundEnqueue {
		t.Errorf("expected ENQUEUES edge DispatchWelcomeEmail -> asynq:email:send, got %+v", enq)
	}
}

func TestScheduledJobs_GoAsynq_NoPhantomWhenHandlerUnknown(t *testing.T) {
	// A NewTask producer for a task type that has no HandleFunc registration in
	// any indexed file must NOT emit a phantom ENQUEUES edge.
	src := `package main

import "github.com/hibiken/asynq"

func Dispatch() {
    task := asynq.NewTask("unregistered:type", nil)
    client.Enqueue(task)
}
`
	_, rels := runScheduledDetect(t, "go", "producer.go", src)
	for _, e := range enqueuesEdges(rels) {
		if e.Properties.Get("framework") == "asynq" {
			t.Errorf("expected no asynq ENQUEUES edge for unregistered task type, got %+v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Kubernetes CronJob YAML
// ---------------------------------------------------------------------------

func TestScheduledJobs_K8sCronJob_YAML(t *testing.T) {
	src := `apiVersion: batch/v1
kind: CronJob
metadata:
  name: report-generator
spec:
  schedule: "0 2 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: reporter
            image: myapp/reporter:latest
`
	ents, rels := runScheduledDetect(t, "", "k8s/cronjob.yaml", src)
	jobs := scheduledJobsByFramework(ents, "kubernetes_cronjob")
	if len(jobs) == 0 {
		t.Fatalf("expected at least 1 kubernetes_cronjob ScheduledJob, got 0")
	}
	if jobs[0].Properties["schedule"] != "0 2 * * *" {
		t.Errorf("schedule = %q, want 0 2 * * *", jobs[0].Properties["schedule"])
	}
	_ = rels
}

// ---------------------------------------------------------------------------
// GitHub Actions schedule trigger
// ---------------------------------------------------------------------------

func TestScheduledJobs_GitHubActionsSchedule(t *testing.T) {
	src := `name: Nightly Build

on:
  schedule:
    - cron: '0 3 * * *'

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
`
	ents, rels := runScheduledDetect(t, "", ".github/workflows/nightly.yml", src)
	jobs := scheduledJobsByFramework(ents, "github_actions_schedule")
	if len(jobs) == 0 {
		t.Fatalf("expected at least 1 github_actions_schedule ScheduledJob, got 0")
	}
	if jobs[0].Properties["schedule"] != "0 3 * * *" {
		t.Errorf("schedule = %q, want 0 3 * * *", jobs[0].Properties["schedule"])
	}
	_ = rels
}

// ---------------------------------------------------------------------------
// Dedup: same job registered twice should yield only one entity
// ---------------------------------------------------------------------------

func TestScheduledJobs_Dedup(t *testing.T) {
	src := `import schedule

def do_work(): pass

schedule.every(5).minutes.do(do_work)
schedule.every(5).minutes.do(do_work)
`
	ents, _ := runScheduledDetect(t, "python", "worker.py", src)
	jobs := scheduledJobsByFramework(ents, "schedule_lib")
	if len(jobs) != 1 {
		t.Errorf("expected 1 deduplicated job entity, got %d", len(jobs))
	}
}

// ---------------------------------------------------------------------------
// Non-match: file with no scheduler content emits nothing
// ---------------------------------------------------------------------------

func TestScheduledJobs_NoMatch(t *testing.T) {
	src := `package main

import "fmt"

func main() {
    fmt.Println("hello world")
}
`
	ents, rels := runScheduledDetect(t, "go", "main.go", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("expected no entities/rels for unrelated file, got %d/%d", len(ents), len(rels))
	}
}

// ---------------------------------------------------------------------------
// Ruby — Sidekiq workers + sidekiq-cron + ENQUEUES edges (#3700)
// ---------------------------------------------------------------------------

func enqueuesEdges(rels []types.RelationshipRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range rels {
		if r.Kind == string(types.RelationshipKindEnqueues) {
			out = append(out, r)
		}
	}
	return out
}

// A Sidekiq worker class becomes a ScheduledJob with a TRIGGERS edge to its
// perform method, and a `Worker.perform_async` dispatch site emits an ENQUEUES
// edge from the enclosing method to the worker job.
func TestScheduledJobs_RubySidekiq_EnqueuesEdge(t *testing.T) {
	src := `class EmailWorker
  include Sidekiq::Worker

  def perform(user_id)
    # send the email
  end
end

class SignupService
  def register(user)
    EmailWorker.perform_async(user.id)
  end
end
`
	ents, rels := runScheduledDetect(t, "ruby", "app/workers/email_worker.rb", src)

	jobs := scheduledJobsByFramework(ents, "sidekiq")
	if len(jobs) != 1 {
		t.Fatalf("expected exactly 1 sidekiq ScheduledJob, got %d (%v)", len(jobs), ents)
	}
	job := jobs[0]
	if job.Name != "sidekiq:EmailWorker" {
		t.Errorf("expected job ID sidekiq:EmailWorker, got %q", job.Name)
	}
	if job.Properties["handler"] != "perform" {
		t.Errorf("expected handler=perform, got %q", job.Properties["handler"])
	}

	// TRIGGERS: job -> perform handler.
	var gotTrigger bool
	for _, e := range triggersEdges(rels) {
		if e.FromID == scheduledJobKind+":sidekiq:EmailWorker" && e.ToID == "Function:perform" {
			gotTrigger = true
		}
	}
	if !gotTrigger {
		t.Errorf("expected TRIGGERS edge sidekiq:EmailWorker -> Function:perform, got %v", triggersEdges(rels))
	}

	// ENQUEUES: caller `register` -> worker job.
	enq := enqueuesEdges(rels)
	if len(enq) != 1 {
		t.Fatalf("expected exactly 1 ENQUEUES edge, got %d (%v)", len(enq), enq)
	}
	e := enq[0]
	if e.FromID != "SCOPE.Operation:register" {
		t.Errorf("expected ENQUEUES from SCOPE.Operation:register, got %q", e.FromID)
	}
	if e.ToID != scheduledJobKind+":sidekiq:EmailWorker" {
		t.Errorf("expected ENQUEUES to %s:sidekiq:EmailWorker, got %q", scheduledJobKind, e.ToID)
	}
	if e.Properties.Get("dispatch_method") != "perform_async" {
		t.Errorf("expected dispatch_method=perform_async, got %q", e.Properties.Get("dispatch_method"))
	}
	if e.Properties.Get("worker_class") != "EmailWorker" {
		t.Errorf("expected worker_class=EmailWorker, got %q", e.Properties.Get("worker_class"))
	}
}

// sidekiq-cron declares a recurring job carrying a cron expression; it reuses
// the worker job ID so the scheduled job and the dispatch target are one node.
func TestScheduledJobs_RubySidekiqCron_Schedule(t *testing.T) {
	src := `class CleanupWorker
  include Sidekiq::Job

  def perform
    Account.stale.delete_all
  end
end

Sidekiq::Cron::Job.create(
  name: 'nightly-cleanup',
  cron: '0 0 * * *',
  class: 'CleanupWorker'
)
`
	ents, _ := runScheduledDetect(t, "ruby", "config/schedule.rb", src)

	var cronJob *types.EntityRecord
	for i := range ents {
		if ents[i].Properties["framework"] == "sidekiq_cron" {
			cronJob = &ents[i]
		}
	}
	if cronJob == nil {
		t.Fatalf("expected a sidekiq_cron ScheduledJob, got %v", ents)
	}
	if cronJob.Properties["schedule"] != "0 0 * * *" {
		t.Errorf("expected cron schedule '0 0 * * *', got %q", cronJob.Properties["schedule"])
	}
	if cronJob.Name != "sidekiq:CleanupWorker" {
		t.Errorf("expected cron job to reuse worker ID sidekiq:CleanupWorker, got %q", cronJob.Name)
	}
}

// Negative: a plain Ruby class with no Sidekiq include must not produce any
// job entity or enqueue edge.
func TestScheduledJobs_RubyPlainClass_NoJob(t *testing.T) {
	src := `class Calculator
  def add(a, b)
    a + b
  end
end
`
	ents, rels := runScheduledDetect(t, "ruby", "lib/calculator.rb", src)
	for _, e := range ents {
		if e.Kind == scheduledJobKind {
			t.Errorf("expected no ScheduledJob entity for plain class, got %v", e)
		}
	}
	if len(enqueuesEdges(rels)) != 0 {
		t.Errorf("expected no ENQUEUES edges for plain class, got %v", enqueuesEdges(rels))
	}
}

// ---------------------------------------------------------------------------
// Ruby — Resque jobs + ENQUEUES edges (#3628 area)
// ---------------------------------------------------------------------------

// enqueuesByFramework filters ENQUEUES edges by their framework property.
func enqueuesByFramework(rels []types.RelationshipRecord, framework string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range enqueuesEdges(rels) {
		if r.Properties.Get("framework") == framework {
			out = append(out, r)
		}
	}
	return out
}

// A Resque job class (@queue + def self.perform) becomes a ScheduledJob with a
// TRIGGERS edge to perform, and `Resque.enqueue(Job)` emits an ENQUEUES edge
// from the enclosing method to the job — joined on resque:<Job>.
func TestScheduledJobs_RubyResque_EnqueuesEdge(t *testing.T) {
	src := `class EmailJob
  @queue = :emails

  def self.perform(user_id)
    # send the email
  end
end

class SignupService
  def register(user)
    Resque.enqueue(EmailJob, user.id)
  end
end
`
	ents, rels := runScheduledDetect(t, "ruby", "app/jobs/email_job.rb", src)

	jobs := scheduledJobsByFramework(ents, "resque")
	if len(jobs) != 1 {
		t.Fatalf("expected exactly 1 resque ScheduledJob, got %d (%v)", len(jobs), ents)
	}
	if jobs[0].Name != "resque:EmailJob" {
		t.Errorf("expected job ID resque:EmailJob, got %q", jobs[0].Name)
	}
	if jobs[0].Properties["queue_name"] != "emails" {
		t.Errorf("expected queue_name=emails, got %q", jobs[0].Properties["queue_name"])
	}

	// TRIGGERS: job -> perform handler.
	var gotTrigger bool
	for _, e := range triggersEdges(rels) {
		if e.FromID == scheduledJobKind+":resque:EmailJob" && e.ToID == "Function:perform" {
			gotTrigger = true
		}
	}
	if !gotTrigger {
		t.Errorf("expected TRIGGERS resque:EmailJob -> Function:perform, got %v", triggersEdges(rels))
	}

	// ENQUEUES: caller `register` -> job, joined on resque:EmailJob.
	enq := enqueuesByFramework(rels, "resque")
	if len(enq) != 1 {
		t.Fatalf("expected exactly 1 resque ENQUEUES edge, got %d (%v)", len(enq), enq)
	}
	e := enq[0]
	if e.FromID != "SCOPE.Operation:register" {
		t.Errorf("expected ENQUEUES from SCOPE.Operation:register, got %q", e.FromID)
	}
	if e.ToID != scheduledJobKind+":resque:EmailJob" {
		t.Errorf("expected ENQUEUES to %s:resque:EmailJob, got %q", scheduledJobKind, e.ToID)
	}
	if e.Properties.Get("dispatch_method") != "enqueue" {
		t.Errorf("expected dispatch_method=enqueue, got %q", e.Properties.Get("dispatch_method"))
	}
}

// Resque.enqueue_in(sec, Job, …) — the job class is the 2nd positional arg.
func TestScheduledJobs_RubyResque_EnqueueIn(t *testing.T) {
	src := `class ReportJob
  @queue = "reports"
  def self.perform(id); end
end

class Caller
  def schedule
    Resque.enqueue_in(60, ReportJob, 7)
  end
end
`
	_, rels := runScheduledDetect(t, "ruby", "app/jobs/report_job.rb", src)
	enq := enqueuesByFramework(rels, "resque")
	if len(enq) != 1 {
		t.Fatalf("expected 1 resque ENQUEUES edge, got %d (%v)", len(enq), enq)
	}
	if enq[0].ToID != scheduledJobKind+":resque:ReportJob" {
		t.Errorf("expected ENQUEUES to resque:ReportJob, got %q", enq[0].ToID)
	}
	if enq[0].Properties.Get("dispatch_method") != "enqueue_in" {
		t.Errorf("expected dispatch_method=enqueue_in, got %q", enq[0].Properties.Get("dispatch_method"))
	}
}

// Negative: a Resque.enqueue dispatch whose job class is NOT a known job (no
// @queue/self.perform def in scope) must not fabricate an ENQUEUES edge.
func TestScheduledJobs_RubyResque_UnknownJob_NoEdge(t *testing.T) {
	src := `class Caller
  def go
    Resque.enqueue(SomeUnindexedJob, 1)
  end
end
`
	_, rels := runScheduledDetect(t, "ruby", "app/caller.rb", src)
	if got := enqueuesByFramework(rels, "resque"); len(got) != 0 {
		t.Errorf("expected no resque ENQUEUES edge for unknown job, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Python — RQ enqueue→handler ENQUEUES (#3628 area)
// ---------------------------------------------------------------------------

// queue.enqueue(my_func) links the producer's enclosing function to the
// consumer Function:my_func.
func TestScheduledJobs_PyRQ_EnqueueRef(t *testing.T) {
	src := `from rq import Queue
from redis import Redis
from workers.email import send_email

q = Queue("emails", connection=Redis())

def notify_user(user_id):
    q.enqueue(send_email, "user@example.com", "hi")
`
	_, rels := runScheduledDetect(t, "python", "api/notifications.py", src)
	enq := enqueuesByFramework(rels, "rq")
	if len(enq) != 1 {
		t.Fatalf("expected exactly 1 rq ENQUEUES edge, got %d (%v)", len(enq), enq)
	}
	e := enq[0]
	if e.FromID != "SCOPE.Operation:notify_user" {
		t.Errorf("expected ENQUEUES from notify_user, got %q", e.FromID)
	}
	if e.ToID != "Function:send_email" {
		t.Errorf("expected ENQUEUES to Function:send_email, got %q", e.ToID)
	}
}

// queue.enqueue_call(func="workers.email.generate_report") resolves the dotted
// string to the consumer's short name.
func TestScheduledJobs_PyRQ_EnqueueCallString(t *testing.T) {
	src := `import rq

def request_report(report_id):
    report_queue.enqueue_call(func="workers.email.generate_report", args=[report_id])
`
	_, rels := runScheduledDetect(t, "python", "api/reports.py", src)
	enq := enqueuesByFramework(rels, "rq")
	if len(enq) != 1 {
		t.Fatalf("expected exactly 1 rq ENQUEUES edge, got %d (%v)", len(enq), enq)
	}
	if enq[0].FromID != "SCOPE.Operation:request_report" {
		t.Errorf("expected ENQUEUES from request_report, got %q", enq[0].FromID)
	}
	if enq[0].ToID != "Function:generate_report" {
		t.Errorf("expected ENQUEUES to Function:generate_report, got %q", enq[0].ToID)
	}
}

// Negative: a `.enqueue` call in a file with no rq import must not emit an
// edge (guards against generic queue objects in non-RQ code).
func TestScheduledJobs_PyRQ_NoImport_NoEdge(t *testing.T) {
	src := `def handler():
    some_other_queue.enqueue(do_work, 1)
`
	_, rels := runScheduledDetect(t, "python", "svc/worker.py", src)
	if got := enqueuesByFramework(rels, "rq"); len(got) != 0 {
		t.Errorf("expected no rq ENQUEUES edge without rq import, got %v", got)
	}
}

// Negative: enqueue with a dynamic (non-identifier) callable must not fabricate
// an edge — honest-partial on dynamic dispatch.
func TestScheduledJobs_PyRQ_DynamicCallable_NoEdge(t *testing.T) {
	src := `from rq import Queue

def dispatch(name):
    q.enqueue(getattr(mod, name), 1)
`
	_, rels := runScheduledDetect(t, "python", "svc/dyn.py", src)
	// The captured token `getattr` is immediately followed by `(`, so it is a
	// nested call (dynamic dispatch), not a callable reference — no edge.
	if got := enqueuesByFramework(rels, "rq"); len(got) != 0 {
		t.Errorf("expected no rq ENQUEUES edge for dynamic callable, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// #3628 area — breadth additions: node-schedule, APScheduler decorator,
// whenever, rufus-scheduler, Hangfire RecurringJob.
// ---------------------------------------------------------------------------

// triggersEdgeFor returns the TRIGGERS edge whose FromID names jobID (suffix
// match on the canonical "SCOPE.ScheduledJob:<jobID>" FromID), or nil.
func triggersEdgeFor(rels []types.RelationshipRecord, jobID string) *types.RelationshipRecord {
	want := scheduledJobKind + ":" + jobID
	for i := range rels {
		if rels[i].Kind == triggersEdgeKind && rels[i].FromID == want {
			return &rels[i]
		}
	}
	return nil
}

// jobWithSchedule returns the first ScheduledJob of framework whose schedule
// property equals want, or nil.
func jobWithSchedule(ents []types.EntityRecord, framework, want string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == scheduledJobKind && e.Properties["framework"] == framework &&
			e.Properties["schedule"] == want {
			return e
		}
	}
	return nil
}

// Node — node-schedule: schedule.scheduleJob('0 2 * * *', cleanup) →
// ScheduledJob(schedule='0 2 * * *') TRIGGERS cleanup.
func TestScheduledJobs_NodeSchedule_StringCron(t *testing.T) {
	src := `const schedule = require('node-schedule');
schedule.scheduleJob('0 2 * * *', cleanup);
`
	ents, rels := runScheduledDetect(t, "javascript", "jobs.js", src)
	job := jobWithSchedule(ents, "node_schedule", "0 2 * * *")
	if job == nil {
		t.Fatalf("expected node_schedule job with schedule='0 2 * * *', got %v",
			scheduledJobsByFramework(ents, "node_schedule"))
	}
	if job.Properties["handler"] != "cleanup" {
		t.Errorf("expected handler=cleanup, got %q", job.Properties["handler"])
	}
	if e := triggersEdgeFor(rels, "node_schedule:jobs.js:0 2 * * *"); e == nil {
		t.Errorf("expected TRIGGERS edge to cleanup, got none in %v", triggersEdges(rels))
	} else if e.ToID != "Function:cleanup" {
		t.Errorf("expected TRIGGERS → Function:cleanup, got %q", e.ToID)
	}
}

// Node — node-schedule inline function literal: handler is anonymous → job
// node still emitted with the schedule, but no TRIGGERS edge.
func TestScheduledJobs_NodeSchedule_InlineFunc(t *testing.T) {
	src := `schedule.scheduleJob('30 1 * * *', function() { doWork(); });`
	ents, rels := runScheduledDetect(t, "typescript", "cron.ts", src)
	job := jobWithSchedule(ents, "node_schedule", "30 1 * * *")
	if job == nil {
		t.Fatalf("expected node_schedule job node even for inline fn")
	}
	if job.Properties["handler"] != "" {
		t.Errorf("expected empty handler for inline fn, got %q", job.Properties["handler"])
	}
	if len(triggersEdges(rels)) != 0 {
		t.Errorf("expected no TRIGGERS edge for anonymous handler, got %v", triggersEdges(rels))
	}
}

// Python — APScheduler decorator: @scheduler.scheduled_job('cron', hour=2) on
// nightly() → schedule carries cron kwargs, TRIGGERS nightly.
func TestScheduledJobs_PyAPScheduler_Decorator(t *testing.T) {
	src := `from apscheduler.schedulers.background import BackgroundScheduler
scheduler = BackgroundScheduler()

@scheduler.scheduled_job('cron', hour=2, minute=30)
def nightly():
    pass
`
	ents, rels := runScheduledDetect(t, "python", "sched.py", src)
	jobs := scheduledJobsByFramework(ents, "apscheduler")
	var job *types.EntityRecord
	for i := range jobs {
		if jobs[i].Properties["handler"] == "nightly" {
			job = &jobs[i]
		}
	}
	if job == nil {
		t.Fatalf("expected apscheduler job for handler=nightly, got %v", jobs)
	}
	if !strings.Contains(job.Properties["schedule"], "hour=2") ||
		!strings.HasPrefix(job.Properties["schedule"], "cron(") {
		t.Errorf("expected schedule like cron(hour=2, minute=30), got %q", job.Properties["schedule"])
	}
	if job.Properties["trigger_type"] != "cron" {
		t.Errorf("expected trigger_type=cron, got %q", job.Properties["trigger_type"])
	}
	if e := triggersEdgeFor(rels, "apscheduler:sched.py:nightly"); e == nil || e.ToID != "Function:nightly" {
		t.Errorf("expected TRIGGERS → Function:nightly, got %v", e)
	}
}

// Python — APScheduler interval decorator: schedule='interval(minutes=5)'.
func TestScheduledJobs_PyAPScheduler_DecoratorInterval(t *testing.T) {
	src := `@sched.scheduled_job('interval', minutes=5)
def poll():
    pass
`
	ents, _ := runScheduledDetect(t, "python", "poll.py", src)
	jobs := scheduledJobsByFramework(ents, "apscheduler")
	if len(jobs) == 0 {
		t.Fatalf("expected an apscheduler interval job, got none")
	}
	got := jobs[0].Properties["schedule"]
	if !strings.Contains(got, "interval") || !strings.Contains(got, "minutes=5") {
		t.Errorf("expected interval(minutes=5) schedule, got %q", got)
	}
}

// Ruby — whenever: every '0 0 * * *' do; runner 'Report.generate'; end →
// schedule='0 0 * * *' TRIGGERS Report.generate.
func TestScheduledJobs_RubyWhenever_Cron(t *testing.T) {
	src := `every '0 0 * * *' do
  runner 'Report.generate'
end

every 1.day, at: '4:30 am' do
  rake 'db:cleanup'
end
`
	ents, _ := runScheduledDetect(t, "ruby", "config/schedule.rb", src)
	job := jobWithSchedule(ents, "whenever", "'0 0 * * *'")
	if job == nil {
		t.Fatalf("expected whenever job schedule='0 0 * * *', got %v",
			scheduledJobsByFramework(ents, "whenever"))
	}
	if job.Properties["handler"] != "Report.generate" {
		t.Errorf("expected handler=Report.generate, got %q", job.Properties["handler"])
	}
	// Second block: interval schedule + rake handler.
	jobs := scheduledJobsByFramework(ents, "whenever")
	foundCleanup := false
	for _, j := range jobs {
		if j.Properties["handler"] == "db:cleanup" {
			foundCleanup = true
		}
	}
	if !foundCleanup {
		t.Errorf("expected a whenever job with handler=db:cleanup, got %v", jobs)
	}
}

// Negative: a generic `every` outside config/schedule.rb must not fabricate a
// whenever job (e.g. Enumerable#every in app code).
func TestScheduledJobs_RubyWhenever_NonScheduleFile_NoJob(t *testing.T) {
	src := `every 3.times do
  puts "hi"
end
`
	ents, _ := runScheduledDetect(t, "ruby", "app/models/widget.rb", src)
	if got := scheduledJobsByFramework(ents, "whenever"); len(got) != 0 {
		t.Errorf("expected no whenever jobs outside schedule.rb, got %v", got)
	}
}

// Ruby — rufus-scheduler: scheduler.cron '0 22 * * *' do nightly_backup end →
// schedule='cron 0 22 * * *' TRIGGERS nightly_backup.
func TestScheduledJobs_RubyRufus_Cron(t *testing.T) {
	src := `require 'rufus-scheduler'
scheduler = Rufus::Scheduler.new
scheduler.cron '0 22 * * *' do
  nightly_backup
end
`
	ents, rels := runScheduledDetect(t, "ruby", "jobs.rb", src)
	jobs := scheduledJobsByFramework(ents, "rufus_scheduler")
	if len(jobs) == 0 {
		t.Fatalf("expected a rufus job, got none")
	}
	job := jobs[0]
	if job.Properties["schedule"] != "cron 0 22 * * *" {
		t.Errorf("expected schedule='cron 0 22 * * *', got %q", job.Properties["schedule"])
	}
	if job.Properties["handler"] != "nightly_backup" {
		t.Errorf("expected handler=nightly_backup, got %q", job.Properties["handler"])
	}
	if e := triggersEdgeFor(rels, "rufus:jobs.rb:0 22 * * *"); e == nil || e.ToID != "Function:nightly_backup" {
		t.Errorf("expected TRIGGERS → Function:nightly_backup, got %v", e)
	}
}

// .NET — Hangfire RecurringJob static lambda with Cron.* factory:
// RecurringJob.AddOrUpdate("daily", () => Reports.Generate(), Cron.Daily) →
// schedule='Cron.Daily' TRIGGERS Generate.
func TestScheduledJobs_CSharpHangfire_RecurringCronFactory(t *testing.T) {
	src := `using Hangfire;
public class Startup {
  public void Configure() {
    RecurringJob.AddOrUpdate("daily-report", () => Reports.Generate(), Cron.Daily);
  }
}
`
	ents, rels := runScheduledDetect(t, "csharp", "Startup.cs", src)
	job := jobWithSchedule(ents, "hangfire", "Cron.Daily")
	if job == nil {
		t.Fatalf("expected hangfire job schedule='Cron.Daily', got %v",
			scheduledJobsByFramework(ents, "hangfire"))
	}
	if job.Properties["handler"] != "Generate" {
		t.Errorf("expected handler=Generate, got %q", job.Properties["handler"])
	}
	if job.Properties["job_name"] != "daily-report" {
		t.Errorf("expected job_name=daily-report, got %q", job.Properties["job_name"])
	}
	if e := triggersEdgeFor(rels, "hangfire_recurring:daily-report"); e == nil || e.ToID != "Function:Generate" {
		t.Errorf("expected TRIGGERS → Function:Generate, got %v", e)
	}
}

// .NET — Hangfire RecurringJob with literal cron string + typed lambda:
// RecurringJob.AddOrUpdate<IPurger>("purge", x => x.Run(), "0 2 * * *").
func TestScheduledJobs_CSharpHangfire_RecurringLiteralCronTyped(t *testing.T) {
	src := `RecurringJob.AddOrUpdate<IPurger>("purge", x => x.Run(), "0 2 * * *");`
	ents, rels := runScheduledDetect(t, "csharp", "Jobs.cs", src)
	job := jobWithSchedule(ents, "hangfire", "\"0 2 * * *\"")
	if job == nil {
		t.Fatalf("expected hangfire job with literal cron schedule, got %v",
			scheduledJobsByFramework(ents, "hangfire"))
	}
	if job.Properties["handler"] != "Run" {
		t.Errorf("expected handler=Run, got %q", job.Properties["handler"])
	}
	if e := triggersEdgeFor(rels, "hangfire_recurring:purge"); e == nil || e.ToID != "Function:Run" {
		t.Errorf("expected TRIGGERS → Function:Run, got %v", e)
	}
}
