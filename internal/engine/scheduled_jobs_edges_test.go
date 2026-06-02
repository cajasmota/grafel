// Tests for the scheduled-job entry-point detection pass (#728).
//
// Each framework has at least one test covering a happy-path detection.
// Tests call applyScheduledJobEdges directly (same pattern as
// kafka_edges_test.go) so they run without the full YAML-rule compiler.
package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
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
	if e.Properties["dispatch_method"] != "perform_async" {
		t.Errorf("expected dispatch_method=perform_async, got %q", e.Properties["dispatch_method"])
	}
	if e.Properties["worker_class"] != "EmailWorker" {
		t.Errorf("expected worker_class=EmailWorker, got %q", e.Properties["worker_class"])
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
		if r.Properties["framework"] == framework {
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
	if e.Properties["dispatch_method"] != "enqueue" {
		t.Errorf("expected dispatch_method=enqueue, got %q", e.Properties["dispatch_method"])
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
	if enq[0].Properties["dispatch_method"] != "enqueue_in" {
		t.Errorf("expected dispatch_method=enqueue_in, got %q", enq[0].Properties["dispatch_method"])
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
