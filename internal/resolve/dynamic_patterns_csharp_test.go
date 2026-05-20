package resolve

import "testing"

// TestDynamicPatterns_Catalog_CSharp verifies that C#/Razor dynamic-dispatch
// patterns classify correctly (Refs #44).
func TestDynamicPatterns_Catalog_CSharp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		lang string
		stub string
		want bool
	}{
		// ---- Quartz.NET / Hangfire C# fluent builder + generic factory (issue #44) ---
		{"quartz_jobbuilder_create_generic", "csharp", `JobBuilder.Create<ReportJob>`, true},
		{"quartz_jobbuilder_create_email", "csharp", `JobBuilder.Create<EmailJob>`, true},
		{"quartz_triggerbuilder_create_generic", "csharp", `TriggerBuilder.Create<DailyTrigger>`, true},
		{"hangfire_backgroundjob_enqueue_generic", "csharp", `BackgroundJob.Enqueue<IEmailService>`, true},
		{"hangfire_recurringjob_addorupdate_generic", "csharp", `RecurringJob.AddOrUpdate<IReportService>`, true},
		{"quartz_withidentity", "csharp", `WithIdentity`, true},
		{"quartz_startnow", "csharp", `StartNow`, true},
		// Cross-language gate: Quartz.NET patterns MUST NOT fire for non-C# languages.
		{"quartz_withidentity_go_neg", "go", `WithIdentity`, false},
		{"quartz_startnow_python_neg", "python", `StartNow`, false},
		{"quartz_withidentity_java_neg", "java", `WithIdentity`, false},
		{"quartz_generic_factory_go_neg", "go", `JobBuilder.Create<ReportJob>`, false},
		{"quartz_generic_factory_python_neg", "python", `BackgroundJob.Enqueue<IEmailService>`, false},
		{"quartz_generic_factory_ts_neg", "typescript", `JobBuilder.Create<EmailJob>`, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isDynamicPatternLang(tc.stub, tc.lang)
			if got != tc.want {
				t.Fatalf("isDynamicPatternLang(%q, lang=%q) = %v, want %v", tc.stub, tc.lang, got, tc.want)
			}
		})
	}
}
