package csharp_test

// #6741 arm 3 — the C# job passes (Hangfire, Quartz.NET) claimed to emit
// PRODUCES edges and emitted none: they set `edge_kind: "PRODUCES"` as an
// entity PROPERTY, which is inert metadata, not a relationship. Both golden
// fixtures say in their own descriptions that they exist "to verify
// PRODUCES/CONSUMES edge emission", and the graph contained zero such edges.
//
// These tests pin the real edges. Per ADR-0028 §1 the shape is the one-hop
// degenerate form (framework producer entity → the job class it dispatches,
// carrying the `task:<framework>:<id>` address as a property), the same form
// java/quartz.go emits and arm 2 kept.

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// extract6741 runs one registered C# extractor and returns the full
// EntityRecords. The package's shared `extract` helper summarises entities into
// (Kind, Subtype, Name) and DROPS Relationships — which is exactly what this
// file has to assert on.
func extract6741(t *testing.T, name, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("%s not registered", name)
	}
	recs, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "csharp", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return recs
}

// producesTargets returns the ToID of every PRODUCES relationship carried by
// an entity named `from`, across all extracted records.
func producesTargets(ents []types.EntityRecord, from string) []string {
	var out []string
	for _, e := range ents {
		if e.Name != from {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindProduces) {
				out = append(out, r.ToID)
			}
		}
	}
	return out
}

// allProduces returns every PRODUCES edge as "from -> toID".
func allProduces(ents []types.EntityRecord) []string {
	var out []string
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindProduces) {
				out = append(out, e.Name+" -> "+r.ToID)
			}
		}
	}
	return out
}

func relProp(t *testing.T, ents []types.EntityRecord, from, key string) string {
	t.Helper()
	for _, e := range ents {
		if e.Name != from {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindProduces) {
				if v := r.Properties.Get(key); v != "" {
					return v
				}
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Quartz.NET
// ---------------------------------------------------------------------------

// TestQuartzNetJobBuilderProducesCrossFile is the fixture shape: the scheduler
// lives in Startup.cs and the IJob implementations in jobs/ReportJob.cs, so the
// producer cannot see a same-file job_class. It targets the resolvable class
// stub `Class:<Name>` — the established C# cross-file convention (the same one
// object_mapping.go's MAPS_TO edges use), which the resolver's by-name pass
// binds to the separately-extracted class entity wherever it is declared.
func TestQuartzNetJobBuilderProducesCrossFile(t *testing.T) {
	src := `
using Quartz;

namespace App
{
    public class Startup
    {
        public async Task ConfigureScheduler(IScheduler scheduler)
        {
            var reportJob = JobBuilder.Create<ReportJob>()
                .WithIdentity("report-job", "reports")
                .Build();

            var emailJob = JobBuilder.Create<EmailJob>()
                .WithIdentity("email-job")
                .Build();
        }
    }
}
`
	ents := extract6741(t, "custom_csharp_quartz_net", "Startup.cs", src)

	got := producesTargets(ents, "JobBuilder.Create<ReportJob>")
	if len(got) != 1 || got[0] != "Class:ReportJob" {
		t.Errorf("JobBuilder.Create<ReportJob> PRODUCES targets = %v, want [Class:ReportJob]", got)
	}
	got = producesTargets(ents, "JobBuilder.Create<EmailJob>")
	if len(got) != 1 || got[0] != "Class:EmailJob" {
		t.Errorf("JobBuilder.Create<EmailJob> PRODUCES targets = %v, want [Class:EmailJob]", got)
	}
	if got := relProp(t, ents, "JobBuilder.Create<ReportJob>", "task_id"); got != "task:quartz.net:ReportJob" {
		t.Errorf("task_id on the PRODUCES edge = %q, want task:quartz.net:ReportJob", got)
	}
}

// TestQuartzNetJobBuilderPairsOnlyItsOwnJobType is the PERMISSIVE-direction pin.
// A pass that paired a producer with ANY job class in scope — rather than the
// one its generic type argument names — would mint an edge between unrelated
// jobs, and every test above would still pass because both edges exist.
func TestQuartzNetJobBuilderPairsOnlyItsOwnJobType(t *testing.T) {
	src := `
using Quartz;

public class Startup
{
    public void Configure()
    {
        var a = JobBuilder.Create<ReportJob>().Build();
        var b = JobBuilder.Create<EmailJob>().Build();
    }
}
`
	ents := extract6741(t, "custom_csharp_quartz_net", "Startup.cs", src)

	got := allProduces(ents)
	want := map[string]bool{
		"JobBuilder.Create<ReportJob> -> Class:ReportJob": true,
		"JobBuilder.Create<EmailJob> -> Class:EmailJob":   true,
	}
	if len(got) != len(want) {
		t.Fatalf("PRODUCES edges = %v, want exactly %d (one per builder, each to its OWN type)", got, len(want))
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected PRODUCES edge %q — a builder must pair only with the job type it names", g)
		}
	}
}

// TestQuartzNetProducesScopedToJobBuilders is the second PERMISSIVE pin. The
// trigger and schedule_job entities ALSO carry the inert `edge_kind: PRODUCES`
// property, so a pass that emitted an edge for every entity carrying that
// property — instead of scoping to the job_builder subtype, which is the only
// one that names a job type — would mint edges out of nodes that dispatch no
// identifiable job.
func TestQuartzNetProducesScopedToJobBuilders(t *testing.T) {
	src := `
using Quartz;

public class Startup
{
    public async Task Configure(IScheduler scheduler)
    {
        var trigger = TriggerBuilder.Create()
            .WithIdentity("report-trigger")
            .Build();

        await scheduler.ScheduleJob(reportJob, trigger);
    }
}
`
	ents := extract6741(t, "custom_csharp_quartz_net", "Startup.cs", src)

	// Sanity: the trigger and schedule_job producers ARE extracted here, so the
	// assertion below is not vacuous.
	sawTrigger, sawSchedule := false, false
	for _, e := range ents {
		switch e.Subtype {
		case "trigger":
			sawTrigger = true
		case "schedule_job":
			sawSchedule = true
		}
	}
	if !sawTrigger || !sawSchedule {
		t.Fatalf("fixture premise broken: trigger=%v schedule_job=%v — both must be extracted for this test to mean anything", sawTrigger, sawSchedule)
	}
	if got := allProduces(ents); len(got) != 0 {
		t.Errorf("PRODUCES edges = %v, want none — no JobBuilder.Create<T> names a job type in this file", got)
	}
}

// TestQuartzNetSameFileJobClassNotDoubleEdged: the target address is the same
// `Class:<Name>` stub whether the job class is declared in this file or another
// — the resolver binds it by name either way — so a same-file declaration must
// not earn a SECOND edge for the same hop (ADR-0028 §3: one hop, one pair).
func TestQuartzNetSameFileJobClassNotDoubleEdged(t *testing.T) {
	src := `
using Quartz;

public class ReportJob : IJob
{
    public async Task Execute(IJobExecutionContext context) { }
}

public class Startup
{
    public void Configure()
    {
        var a = JobBuilder.Create<ReportJob>().Build();
    }
}
`
	ents := extract6741(t, "custom_csharp_quartz_net", "All.cs", src)

	// Premise: the same-file job_class IS extracted here, so a same-file arm,
	// had one been written, would have fired.
	sawJobClass := false
	for _, e := range ents {
		if e.Subtype == "job_class" && e.Name == "ReportJob" {
			sawJobClass = true
		}
	}
	if !sawJobClass {
		t.Fatal("fixture premise broken: no same-file job_class ReportJob extracted")
	}
	got := producesTargets(ents, "JobBuilder.Create<ReportJob>")
	if len(got) != 1 || got[0] != "Class:ReportJob" {
		t.Fatalf("PRODUCES targets = %v, want exactly [Class:ReportJob] (one hop, one edge)", got)
	}
}

// ---------------------------------------------------------------------------
// Hangfire
// ---------------------------------------------------------------------------

// TestHangfireEnqueueProduces pins the producer→job-type edge for each of the
// literal Hangfire dispatch shapes.
func TestHangfireEnqueueProduces(t *testing.T) {
	src := `
using Hangfire;

public class OrderController
{
    public void PlaceOrder(int orderId)
    {
        BackgroundJob.Enqueue(() => EmailService.SendConfirmation(orderId));
    }

    public void ConfigureRecurring()
    {
        RecurringJob.AddOrUpdate("daily-cleanup", () => CleanupService.Run(), Cron.Daily);
        BackgroundJob.Enqueue<IEmailService>(x => x.SendNewsletter());
    }
}
`
	ents := extract6741(t, "custom_csharp_hangfire", "OrderController.cs", src)

	cases := []struct{ from, to string }{
		{"EmailService.SendConfirmation", "Class:EmailService"},
		{"IEmailService.SendNewsletter", "Class:IEmailService"},
		{"daily-cleanup", "Class:CleanupService"},
	}
	for _, c := range cases {
		got := producesTargets(ents, c.from)
		if len(got) != 1 || got[0] != c.to {
			t.Errorf("%s PRODUCES targets = %v, want [%s]", c.from, got, c.to)
		}
	}
	if got := relProp(t, ents, "EmailService.SendConfirmation", "task_id"); got != "task:hangfire:EmailService.SendConfirmation" {
		t.Errorf("task_id on the PRODUCES edge = %q, want task:hangfire:EmailService.SendConfirmation", got)
	}
}

// TestHangfireProducesPairsOnlyItsOwnJobType is the PERMISSIVE pin for Hangfire:
// two dispatch sites in one file must not cross-pair.
func TestHangfireProducesPairsOnlyItsOwnJobType(t *testing.T) {
	src := `
using Hangfire;

public class OrderController
{
    public void PlaceOrder(int orderId)
    {
        BackgroundJob.Enqueue(() => EmailService.SendConfirmation(orderId));
        BackgroundJob.Schedule(() => AuditService.Record(orderId), TimeSpan.FromMinutes(5));
    }
}
`
	ents := extract6741(t, "custom_csharp_hangfire", "OrderController.cs", src)

	got := allProduces(ents)
	want := map[string]bool{
		"EmailService.SendConfirmation -> Class:EmailService": true,
		"AuditService.Record -> Class:AuditService":           true,
	}
	if len(got) != len(want) {
		t.Fatalf("PRODUCES edges = %v, want exactly %d (one per dispatch, each to its OWN type)", got, len(want))
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected PRODUCES edge %q — a dispatch must pair only with the type it names", g)
		}
	}
}

// TestHangfireDynamicProducerEmitsNoProduces: the unresolved-producer fallbacks
// (sections 8/9) carry `edge_kind: PRODUCES` too but resolved no job type, so
// there is nothing honest to point at. A pass keyed on the inert property
// rather than on a resolved job_type would fabricate an edge here.
func TestHangfireDynamicProducerEmitsNoProduces(t *testing.T) {
	src := `
using Hangfire;

public class OrderController
{
    public void Configure(Action work)
    {
        BackgroundJob.Enqueue(work);
    }
}
`
	ents := extract6741(t, "custom_csharp_hangfire", "OrderController.cs", src)

	sawDynamic := false
	for _, e := range ents {
		if e.Subtype == "task_enqueue" {
			sawDynamic = true
		}
	}
	if !sawDynamic {
		t.Fatal("fixture premise broken: no task_enqueue producer extracted, so the assertion below is vacuous")
	}
	if got := allProduces(ents); len(got) != 0 {
		t.Errorf("PRODUCES edges = %v, want none — the dispatch target is not statically resolvable", got)
	}
}

// TestHangfireSameFileJobClassNotDoubleEdged: `BackgroundJob.Enqueue<EmailJob>`
// alongside `class EmailJob : IBackgroundJob` — one hop, one edge, on the same
// `Class:<Name>` address the cross-file case uses.
func TestHangfireSameFileJobClassNotDoubleEdged(t *testing.T) {
	src := `
using Hangfire;

public class EmailJob : IBackgroundJob
{
    public async Task Execute(PerformContext ctx) { }
}

public class OrderController
{
    public void PlaceOrder()
    {
        BackgroundJob.Enqueue<EmailJob>(x => x.Execute(null));
    }
}
`
	ents := extract6741(t, "custom_csharp_hangfire", "All.cs", src)

	sawJobClass := false
	for _, e := range ents {
		if e.Subtype == "job_class" && e.Name == "EmailJob" {
			sawJobClass = true
		}
	}
	if !sawJobClass {
		t.Fatal("fixture premise broken: no same-file job_class EmailJob extracted")
	}
	got := producesTargets(ents, "EmailJob.Execute")
	if len(got) != 1 || got[0] != "Class:EmailJob" {
		t.Fatalf("PRODUCES targets = %v, want exactly [Class:EmailJob] (one hop, one edge)", got)
	}
}
