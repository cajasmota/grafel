package csharp_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/csharp"
)

// findBySubtype returns the first entity with the given subtype, or nil.
func findBySubtype(ents []types.EntityRecord, subtype string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == subtype {
			return &ents[i]
		}
	}
	return nil
}

func findRecurringByName(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == "recurring_job" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// --- Cron.* helper parse onto the recurring node --------------------------

func TestHangfireRecurringCronDaily(t *testing.T) {
	src := `RecurringJob.AddOrUpdate("daily-report", () => ReportService.Generate(), Cron.Daily);`
	ents := extractFull(t, "custom_csharp_hangfire", fi("Jobs.cs", "csharp", src))
	r := findRecurringByName(ents, "daily-report")
	if r == nil {
		t.Fatal("expected recurring_job entity 'daily-report'")
	}
	if got := r.Properties["cron_expression"]; got != "0 0 * * *" {
		t.Errorf("cron_expression = %q, want %q", got, "0 0 * * *")
	}
	if got := r.Properties["schedule_type"]; got != "cron" {
		t.Errorf("schedule_type = %q, want cron", got)
	}
}

func TestHangfireRecurringCronHourly(t *testing.T) {
	src := `RecurringJob.AddOrUpdate("hourly", () => Svc.Run(), Cron.Hourly);`
	ents := extractFull(t, "custom_csharp_hangfire", fi("Jobs.cs", "csharp", src))
	r := findRecurringByName(ents, "hourly")
	if r == nil {
		t.Fatal("expected recurring_job entity 'hourly'")
	}
	if got := r.Properties["cron_expression"]; got != "0 * * * *" {
		t.Errorf("cron_expression = %q, want %q", got, "0 * * * *")
	}
}

func TestHangfireRecurringCronRawString(t *testing.T) {
	src := `RecurringJob.AddOrUpdate("custom", () => Svc.Run(), "0 12 * * 1-5");`
	ents := extractFull(t, "custom_csharp_hangfire", fi("Jobs.cs", "csharp", src))
	r := findRecurringByName(ents, "custom")
	if r == nil {
		t.Fatal("expected recurring_job entity 'custom'")
	}
	if got := r.Properties["cron_expression"]; got != "0 12 * * 1-5" {
		t.Errorf("cron_expression = %q, want %q", got, "0 12 * * 1-5")
	}
	if got := r.Properties["schedule_type"]; got != "cron" {
		t.Errorf("schedule_type = %q, want cron", got)
	}
}

func TestHangfireRecurringTypedCron(t *testing.T) {
	src := `RecurringJob.AddOrUpdate<IReportService>("typed-rep", x => x.Generate(), Cron.Weekly);`
	ents := extractFull(t, "custom_csharp_hangfire", fi("Jobs.cs", "csharp", src))
	r := findRecurringByName(ents, "typed-rep")
	if r == nil {
		t.Fatal("expected recurring_job entity 'typed-rep'")
	}
	if got := r.Properties["cron_expression"]; got != "0 0 * * 0" {
		t.Errorf("cron_expression = %q, want %q", got, "0 0 * * 0")
	}
}

func TestHangfireRecurringCronIntervalLabelOnly(t *testing.T) {
	// Cron.MinuteInterval(n) depends on a runtime arg — schedule label only, no expr.
	src := `RecurringJob.AddOrUpdate("poll", () => Svc.Poll(), Cron.MinuteInterval(5));`
	ents := extractFull(t, "custom_csharp_hangfire", fi("Jobs.cs", "csharp", src))
	r := findRecurringByName(ents, "poll")
	if r == nil {
		t.Fatal("expected recurring_job entity 'poll'")
	}
	if got := r.Properties["cron_expression"]; got != "" {
		t.Errorf("cron_expression = %q, want empty for interval helper", got)
	}
	if got := r.Properties["schedule_type"]; got != "interval" {
		t.Errorf("schedule_type = %q, want interval", got)
	}
}

// --- Dynamic / non-literal enqueue + recurring (honest unresolved) ---------

func TestHangfireDynamicRecurringCapturedId(t *testing.T) {
	// job-id is a captured variable, lambda body is non-literal.
	src := `RecurringJob.AddOrUpdate(jobId, () => _service.Process(input), cronExpr);`
	ents := extractFull(t, "custom_csharp_hangfire", fi("Jobs.cs", "csharp", src))
	r := findBySubtype(ents, "recurring_job")
	if r == nil {
		t.Fatal("expected a dynamic recurring_job entity")
	}
	if got := r.Properties["pattern_type"]; got != "recurring_dynamic" {
		t.Errorf("pattern_type = %q, want recurring_dynamic", got)
	}
	if got := r.Properties["resolution"]; got != "unresolved" {
		t.Errorf("resolution = %q, want unresolved", got)
	}
}

func TestHangfireDynamicRecurringLiteralIdNonLiteralLambda(t *testing.T) {
	// id is literal but lambda body is a nested member access (a.b.Method),
	// which the literal Type.Method() pattern cannot resolve.
	src := `RecurringJob.AddOrUpdate("nightly", () => _ctx.Handler.Invoke(ctx), Cron.Daily);`
	ents := extractFull(t, "custom_csharp_hangfire", fi("Jobs.cs", "csharp", src))
	r := findRecurringByName(ents, "recurring:nightly")
	if r == nil {
		t.Fatal("expected dynamic recurring_job entity named recurring:nightly")
	}
	if got := r.Properties["resolution"]; got != "unresolved" {
		t.Errorf("resolution = %q, want unresolved", got)
	}
	// Cron should still parse even for a dynamic job target.
	if got := r.Properties["cron_expression"]; got != "0 0 * * *" {
		t.Errorf("cron_expression = %q, want %q", got, "0 0 * * *")
	}
}

func TestHangfireDynamicEnqueue(t *testing.T) {
	// Nested member-access enqueue (a.b.Method) — not a resolvable literal lambda.
	src := `BackgroundJob.Enqueue(() => _ctx.Processor.Handle(message));`
	ents := extractFull(t, "custom_csharp_hangfire", fi("Jobs.cs", "csharp", src))
	r := findBySubtype(ents, "task_enqueue")
	if r == nil {
		t.Fatal("expected a dynamic task_enqueue entity")
	}
	if got := r.Properties["pattern_type"]; got != "enqueue_dynamic" {
		t.Errorf("pattern_type = %q, want enqueue_dynamic", got)
	}
	if got := r.Properties["resolution"]; got != "unresolved" {
		t.Errorf("resolution = %q, want unresolved", got)
	}
}

func TestHangfireLiteralEnqueueNotMarkedDynamic(t *testing.T) {
	// A clean literal enqueue must NOT also emit a dynamic duplicate.
	src := `BackgroundJob.Enqueue(() => EmailService.Send(userId));`
	ents := extractFull(t, "custom_csharp_hangfire", fi("Jobs.cs", "csharp", src))
	for _, e := range ents {
		if e.Properties["pattern_type"] == "enqueue_dynamic" {
			t.Errorf("literal enqueue wrongly produced an enqueue_dynamic entity: %+v", e.Name)
		}
	}
}
