package csharp_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/csharp"
)

// findTrigger returns the first trigger entity, or nil.
func findTrigger(ents []types.EntityRecord) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == "trigger" {
			return &ents[i]
		}
	}
	return nil
}

func TestQuartzNetCronScheduleParse(t *testing.T) {
	src := `
var trigger = TriggerBuilder.Create()
    .WithIdentity("nightly", "reports")
    .WithCronSchedule("0 0 2 * * ?")
    .Build();
`
	ents := extractFull(t, "custom_csharp_quartz_net", fi("Sched.cs", "csharp", src))
	tr := findTrigger(ents)
	if tr == nil {
		t.Fatal("expected a trigger entity")
	}
	if got := tr.Properties["cron_expression"]; got != "0 0 2 * * ?" {
		t.Errorf("cron_expression = %q, want %q", got, "0 0 2 * * ?")
	}
	if got := tr.Properties["schedule_type"]; got != "cron" {
		t.Errorf("schedule_type = %q, want cron", got)
	}
	if got := tr.Properties["trigger_group"]; got != "reports" {
		t.Errorf("trigger_group = %q, want reports", got)
	}
	if got := tr.Properties["trigger_name"]; got != "nightly" {
		t.Errorf("trigger_name = %q, want nightly", got)
	}
}

func TestQuartzNetCronScheduleBuilder(t *testing.T) {
	src := `
var trigger = TriggerBuilder.Create()
    .WithSchedule(CronScheduleBuilder.CronSchedule("0 0/15 * * * ?"))
    .Build();
`
	ents := extractFull(t, "custom_csharp_quartz_net", fi("Sched.cs", "csharp", src))
	tr := findTrigger(ents)
	if tr == nil {
		t.Fatal("expected a trigger entity")
	}
	if got := tr.Properties["cron_expression"]; got != "0 0/15 * * * ?" {
		t.Errorf("cron_expression = %q, want %q", got, "0 0/15 * * * ?")
	}
	if got := tr.Properties["schedule_type"]; got != "cron" {
		t.Errorf("schedule_type = %q, want cron", got)
	}
}

func TestQuartzNetSimpleScheduleIntervalSeconds(t *testing.T) {
	src := `
var trigger = TriggerBuilder.Create()
    .WithIdentity("poller")
    .WithSimpleSchedule(x => x.WithIntervalInSeconds(40).RepeatForever())
    .Build();
`
	ents := extractFull(t, "custom_csharp_quartz_net", fi("Sched.cs", "csharp", src))
	tr := findTrigger(ents)
	if tr == nil {
		t.Fatal("expected a trigger entity")
	}
	if got := tr.Properties["schedule_type"]; got != "simple" {
		t.Errorf("schedule_type = %q, want simple", got)
	}
	if got := tr.Properties["interval_seconds"]; got != "40" {
		t.Errorf("interval_seconds = %q, want 40", got)
	}
	if got := tr.Properties["repeat_forever"]; got != "true" {
		t.Errorf("repeat_forever = %q, want true", got)
	}
	// no cron on a simple schedule
	if got := tr.Properties["cron_expression"]; got != "" {
		t.Errorf("cron_expression = %q, want empty", got)
	}
}

func TestQuartzNetSimpleScheduleIntervalMinutes(t *testing.T) {
	src := `
var trigger = TriggerBuilder.Create()
    .WithSimpleSchedule(x => x.WithIntervalInMinutes(5))
    .Build();
`
	ents := extractFull(t, "custom_csharp_quartz_net", fi("Sched.cs", "csharp", src))
	tr := findTrigger(ents)
	if tr == nil {
		t.Fatal("expected a trigger entity")
	}
	if got := tr.Properties["interval_seconds"]; got != "300" {
		t.Errorf("interval_seconds = %q, want 300 (5 min)", got)
	}
}

func TestQuartzNetSimpleScheduleTimeSpanInterval(t *testing.T) {
	src := `
var trigger = TriggerBuilder.Create()
    .WithSimpleSchedule(x => x.WithInterval(TimeSpan.FromHours(2)))
    .Build();
`
	ents := extractFull(t, "custom_csharp_quartz_net", fi("Sched.cs", "csharp", src))
	tr := findTrigger(ents)
	if tr == nil {
		t.Fatal("expected a trigger entity")
	}
	if got := tr.Properties["interval_seconds"]; got != "7200" {
		t.Errorf("interval_seconds = %q, want 7200 (2h)", got)
	}
}

func TestQuartzNetJobKeyGroup(t *testing.T) {
	src := `
var job = JobBuilder.Create<SendEmailJob>()
    .WithIdentity("emailJob", "notifications")
    .Build();
`
	ents := extractFull(t, "custom_csharp_quartz_net", fi("Sched.cs", "csharp", src))
	var jb *types.EntityRecord
	for i := range ents {
		if ents[i].Subtype == "job_builder" {
			jb = &ents[i]
		}
	}
	if jb == nil {
		t.Fatal("expected a job_builder entity")
	}
	if got := jb.Properties["job_group"]; got != "notifications" {
		t.Errorf("job_group = %q, want notifications", got)
	}
}

// Two triggers in one file must not bleed schedule props into each other.
func TestQuartzNetTriggerScheduleIsolation(t *testing.T) {
	src := `
var t1 = TriggerBuilder.Create().WithIdentity("a").WithCronSchedule("0 0 1 * * ?").Build();
var t2 = TriggerBuilder.Create().WithIdentity("b").WithSimpleSchedule(x => x.WithIntervalInSeconds(10)).Build();
`
	ents := extractFull(t, "custom_csharp_quartz_net", fi("Sched.cs", "csharp", src))
	var cron, simple *types.EntityRecord
	for i := range ents {
		if ents[i].Subtype != "trigger" {
			continue
		}
		switch ents[i].Properties["schedule_type"] {
		case "cron":
			cron = &ents[i]
		case "simple":
			simple = &ents[i]
		}
	}
	if cron == nil || simple == nil {
		t.Fatalf("expected one cron and one simple trigger, got cron=%v simple=%v", cron, simple)
	}
	if cron.Properties["interval_seconds"] != "" {
		t.Errorf("cron trigger leaked interval_seconds=%q", cron.Properties["interval_seconds"])
	}
	if simple.Properties["cron_expression"] != "" {
		t.Errorf("simple trigger leaked cron_expression=%q", simple.Properties["cron_expression"])
	}
}
