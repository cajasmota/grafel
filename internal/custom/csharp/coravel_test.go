package csharp_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/csharp"
)

// findCoravelSubtype returns the first entity of the given subtype, or nil.
func findCoravelSubtype(ents []types.EntityRecord, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

func TestCoravel_Invocable(t *testing.T) {
	src := `
public class SendNewsletter : IInvocable
{
    public Task Invoke() => Task.CompletedTask;
}
`
	ents := extractFull(t, "custom_csharp_coravel", fi("SendNewsletter.cs", "csharp", src))
	inv := findCoravelSubtype(ents, "invocable")
	if inv == nil {
		t.Fatal("expected an invocable entity")
	}
	if inv.Name != "SendNewsletter" {
		t.Errorf("name = %q, want SendNewsletter", inv.Name)
	}
	if got := inv.Properties["task_id"]; got != "task:coravel:SendNewsletter" {
		t.Errorf("task_id = %q, want task:coravel:SendNewsletter", got)
	}
	if got := inv.Properties["edge_kind"]; got != "CONSUMES" {
		t.Errorf("edge_kind = %q, want CONSUMES", got)
	}
	if inv.Kind != "SCOPE.Service" {
		t.Errorf("kind = %q, want SCOPE.Service", inv.Kind)
	}
}

func TestCoravel_ScheduleEveryMinute(t *testing.T) {
	src := `
scheduler.Schedule<SendNewsletter>().EveryMinute();
`
	ents := extractFull(t, "custom_csharp_coravel", fi("Sched.cs", "csharp", src))
	s := findCoravelSubtype(ents, "schedule")
	if s == nil {
		t.Fatal("expected a schedule entity")
	}
	if got := s.Properties["invocable"]; got != "SendNewsletter" {
		t.Errorf("invocable = %q, want SendNewsletter", got)
	}
	if got := s.Properties["schedule_type"]; got != "interval" {
		t.Errorf("schedule_type = %q, want interval", got)
	}
	if got := s.Properties["frequency"]; got != "EveryMinute" {
		t.Errorf("frequency = %q, want EveryMinute", got)
	}
	if got := s.Properties["interval_seconds"]; got != "60" {
		t.Errorf("interval_seconds = %q, want 60", got)
	}
	if got := s.Properties["task_id"]; got != "task:coravel:SendNewsletter" {
		t.Errorf("task_id = %q, want task:coravel:SendNewsletter", got)
	}
	if got := s.Properties["edge_kind"]; got != "PRODUCES" {
		t.Errorf("edge_kind = %q, want PRODUCES", got)
	}
}

func TestCoravel_ScheduleCron(t *testing.T) {
	src := `scheduler.Schedule<Cleanup>().Cron("*/5 * * * *");`
	ents := extractFull(t, "custom_csharp_coravel", fi("Sched.cs", "csharp", src))
	s := findCoravelSubtype(ents, "schedule")
	if s == nil {
		t.Fatal("expected a schedule entity")
	}
	if got := s.Properties["schedule_type"]; got != "cron" {
		t.Errorf("schedule_type = %q, want cron", got)
	}
	if got := s.Properties["cron_expression"]; got != "*/5 * * * *" {
		t.Errorf("cron_expression = %q, want */5 * * * *", got)
	}
}

func TestCoravel_ScheduleDailyAt(t *testing.T) {
	src := `scheduler.Schedule<Report>().DailyAt("13:00");`
	ents := extractFull(t, "custom_csharp_coravel", fi("Sched.cs", "csharp", src))
	s := findCoravelSubtype(ents, "schedule")
	if s == nil {
		t.Fatal("expected a schedule entity")
	}
	if got := s.Properties["schedule_type"]; got != "daily" {
		t.Errorf("schedule_type = %q, want daily", got)
	}
	if got := s.Properties["daily_at"]; got != "13:00" {
		t.Errorf("daily_at = %q, want 13:00", got)
	}
}

func TestCoravel_ScheduleEveryNMinutes(t *testing.T) {
	src := `scheduler.Schedule<Sync>().Every5Minutes();`
	ents := extractFull(t, "custom_csharp_coravel", fi("Sched.cs", "csharp", src))
	s := findCoravelSubtype(ents, "schedule")
	if s == nil {
		t.Fatal("expected a schedule entity")
	}
	if got := s.Properties["interval_seconds"]; got != "300" {
		t.Errorf("interval_seconds = %q, want 300", got)
	}
	if got := s.Properties["frequency"]; got != "Every5Minutes" {
		t.Errorf("frequency = %q, want Every5Minutes", got)
	}
}

func TestCoravel_ScheduleAnonymousHourly(t *testing.T) {
	src := `scheduler.Schedule(() => Console.WriteLine("tick")).Hourly();`
	ents := extractFull(t, "custom_csharp_coravel", fi("Sched.cs", "csharp", src))
	s := findCoravelSubtype(ents, "schedule")
	if s == nil {
		t.Fatal("expected a schedule entity")
	}
	if got := s.Properties["frequency"]; got != "Hourly" {
		t.Errorf("frequency = %q, want Hourly", got)
	}
	if got := s.Properties["interval_seconds"]; got != "3600" {
		t.Errorf("interval_seconds = %q, want 3600", got)
	}
}

func TestCoravel_QueueInvocable(t *testing.T) {
	src := `_queue.QueueInvocable<GrabDataFromApi>();`
	ents := extractFull(t, "custom_csharp_coravel", fi("Q.cs", "csharp", src))
	q := findCoravelSubtype(ents, "queue")
	if q == nil {
		t.Fatal("expected a queue entity")
	}
	if got := q.Properties["invocable"]; got != "GrabDataFromApi" {
		t.Errorf("invocable = %q, want GrabDataFromApi", got)
	}
	if got := q.Properties["task_id"]; got != "task:coravel:GrabDataFromApi" {
		t.Errorf("task_id = %q, want task:coravel:GrabDataFromApi", got)
	}
}

func TestCoravel_Mail(t *testing.T) {
	src := `await _mailer.SendAsync(new WelcomeMailable(user));`
	ents := extractFull(t, "custom_csharp_coravel", fi("M.cs", "csharp", src))
	m := findCoravelSubtype(ents, "mail")
	if m == nil {
		t.Fatal("expected a mail entity")
	}
	if got := m.Properties["mailable"]; got != "WelcomeMailable" {
		t.Errorf("mailable = %q, want WelcomeMailable", got)
	}
}

// Consumer + producer converge on the same task:coravel:<T> id.
func TestCoravel_ConsumerProducerJoin(t *testing.T) {
	src := `
public class SendNewsletter : IInvocable
{
    public Task Invoke() => Task.CompletedTask;
}

public static void Configure(IServiceProvider services)
{
    services.UseScheduler(scheduler =>
    {
        scheduler.Schedule<SendNewsletter>().Daily();
    });
}
`
	ents := extractFull(t, "custom_csharp_coravel", fi("App.cs", "csharp", src))
	inv := findCoravelSubtype(ents, "invocable")
	s := findCoravelSubtype(ents, "schedule")
	if inv == nil || s == nil {
		t.Fatalf("expected both invocable and schedule, got inv=%v sched=%v", inv != nil, s != nil)
	}
	if inv.Properties["task_id"] != s.Properties["task_id"] {
		t.Errorf("task_id mismatch: invocable=%q schedule=%q",
			inv.Properties["task_id"], s.Properties["task_id"])
	}
	if got := s.Properties["schedule_type"]; got != "daily" {
		t.Errorf("schedule_type = %q, want daily", got)
	}
}

// Negative: a Send(new Foo()) that is not a *Mailable is not a mail surface.
func TestCoravel_MailNonMailableSkipped(t *testing.T) {
	src := `scheduler.Schedule<X>().EveryMinute(); channel.Send(new Message());`
	ents := extractFull(t, "custom_csharp_coravel", fi("M.cs", "csharp", src))
	if m := findCoravelSubtype(ents, "mail"); m != nil {
		t.Errorf("expected no mail entity for non-Mailable Send, got %q", m.Name)
	}
}

func TestCoravel_NoMatch(t *testing.T) {
	src := `public class Foo { public void Bar() {} }`
	ents := extractFull(t, "custom_csharp_coravel", fi("Foo.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
