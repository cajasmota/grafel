package csharp_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/csharp"
)

// findResilience returns the first resilience_policy entity of the given
// resilience_type, or nil.
func findResilience(ents []types.EntityRecord, resType string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == "resilience_policy" && ents[i].Properties["resilience_type"] == resType {
			return &ents[i]
		}
	}
	return nil
}

func TestPolly_V7Retry(t *testing.T) {
	src := `
var policy = Policy
    .Handle<HttpRequestException>()
    .Retry(3);
`
	ents := extractFull(t, "custom_csharp_polly", fi("Resilience.cs", "csharp", src))
	r := findResilience(ents, "retry")
	if r == nil {
		t.Fatal("expected a retry resilience_policy")
	}
	if got := r.Properties["retry_count"]; got != "3" {
		t.Errorf("retry_count = %q, want 3", got)
	}
	if got := r.Properties["handled_exception"]; got != "HttpRequestException" {
		t.Errorf("handled_exception = %q, want HttpRequestException", got)
	}
	if got := r.Properties["api_version"]; got != "v7" {
		t.Errorf("api_version = %q, want v7", got)
	}
	if got := r.Properties["framework"]; got != "polly" {
		t.Errorf("framework = %q, want polly", got)
	}
}

func TestPolly_V7WaitAndRetryAsync(t *testing.T) {
	src := `
var policy = Policy
    .Handle<SqlException>()
    .WaitAndRetryAsync(5, attempt => TimeSpan.FromSeconds(attempt));
`
	ents := extractFull(t, "custom_csharp_polly", fi("R.cs", "csharp", src))
	r := findResilience(ents, "retry")
	if r == nil {
		t.Fatal("expected a retry resilience_policy")
	}
	if got := r.Properties["retry_count"]; got != "5" {
		t.Errorf("retry_count = %q, want 5", got)
	}
}

func TestPolly_V7CircuitBreaker(t *testing.T) {
	src := `
var cb = Policy
    .Handle<BrokenCircuitException>()
    .CircuitBreaker(2, TimeSpan.FromMinutes(1));
`
	ents := extractFull(t, "custom_csharp_polly", fi("R.cs", "csharp", src))
	r := findResilience(ents, "circuit_breaker")
	if r == nil {
		t.Fatal("expected a circuit_breaker resilience_policy")
	}
	if got := r.Properties["break_threshold"]; got != "2" {
		t.Errorf("break_threshold = %q, want 2", got)
	}
	if got := r.Properties["break_seconds"]; got != "60" {
		t.Errorf("break_seconds = %q, want 60", got)
	}
}

func TestPolly_V7Timeout(t *testing.T) {
	src := `var t = Policy.Timeout(TimeSpan.FromSeconds(10));`
	ents := extractFull(t, "custom_csharp_polly", fi("R.cs", "csharp", src))
	r := findResilience(ents, "timeout")
	if r == nil {
		t.Fatal("expected a timeout resilience_policy")
	}
	if got := r.Properties["timeout_seconds"]; got != "10" {
		t.Errorf("timeout_seconds = %q, want 10", got)
	}
}

func TestPolly_V7Bulkhead(t *testing.T) {
	src := `var b = Policy.Bulkhead(maxParallelization: 12);`
	ents := extractFull(t, "custom_csharp_polly", fi("R.cs", "csharp", src))
	r := findResilience(ents, "bulkhead")
	if r == nil {
		t.Fatal("expected a bulkhead resilience_policy")
	}
	if got := r.Properties["max_parallel"]; got != "12" {
		t.Errorf("max_parallel = %q, want 12", got)
	}
}

func TestPolly_V7Fallback(t *testing.T) {
	src := `
var f = Policy<int>
    .Handle<Exception>()
    .Fallback(-1);
`
	ents := extractFull(t, "custom_csharp_polly", fi("R.cs", "csharp", src))
	r := findResilience(ents, "fallback")
	if r == nil {
		t.Fatal("expected a fallback resilience_policy")
	}
	if got := r.Properties["handled_exception"]; got != "Exception" {
		t.Errorf("handled_exception = %q, want Exception", got)
	}
}

func TestPolly_V8Pipeline(t *testing.T) {
	src := `
var pipeline = new ResiliencePipelineBuilder()
    .AddRetry(new RetryStrategyOptions { MaxRetryAttempts = 4 })
    .AddTimeout(TimeSpan.FromSeconds(30))
    .Build();
`
	ents := extractFull(t, "custom_csharp_polly", fi("R.cs", "csharp", src))
	r := findResilience(ents, "retry")
	if r == nil {
		t.Fatal("expected a retry resilience_policy")
	}
	if got := r.Properties["api_version"]; got != "v8" {
		t.Errorf("api_version = %q, want v8", got)
	}
	if got := r.Properties["retry_count"]; got != "4" {
		t.Errorf("retry_count = %q, want 4", got)
	}
	to := findResilience(ents, "timeout")
	if to == nil {
		t.Fatal("expected a timeout resilience_policy")
	}
	if got := to.Properties["timeout_seconds"]; got != "30" {
		t.Errorf("timeout_seconds = %q, want 30", got)
	}
}

func TestPolly_V8CircuitBreakerOptions(t *testing.T) {
	src := `
var p = new ResiliencePipelineBuilder<string>()
    .AddCircuitBreaker(new CircuitBreakerStrategyOptions
    {
        FailureRatio = 0.5,
        BreakDuration = TimeSpan.FromMinutes(2)
    })
    .Build();
`
	ents := extractFull(t, "custom_csharp_polly", fi("R.cs", "csharp", src))
	r := findResilience(ents, "circuit_breaker")
	if r == nil {
		t.Fatal("expected a circuit_breaker resilience_policy")
	}
	if got := r.Properties["break_threshold"]; got != "0.5" {
		t.Errorf("break_threshold = %q, want 0.5", got)
	}
	if got := r.Properties["break_seconds"]; got != "120" {
		t.Errorf("break_seconds = %q, want 120", got)
	}
}

func TestPolly_AddPolicyHandler(t *testing.T) {
	src := `
services.AddHttpClient("github")
    .AddPolicyHandler(retryPolicy);
`
	ents := extractFull(t, "custom_csharp_polly", fi("R.cs", "csharp", src))
	r := findResilience(ents, "http_client_policy")
	if r == nil {
		t.Fatal("expected an http_client_policy resilience_policy")
	}
	if got := r.Properties["binding"]; got != "add_policy_handler" {
		t.Errorf("binding = %q, want add_policy_handler", got)
	}
	if got := r.Properties["http_client"]; got != "github" {
		t.Errorf("http_client = %q, want github", got)
	}
}

func TestPolly_AddResilienceHandler(t *testing.T) {
	src := `
services.AddHttpClient("orders")
    .AddResilienceHandler("pipeline", b => b.AddRetry(new RetryStrategyOptions()));
`
	ents := extractFull(t, "custom_csharp_polly", fi("R.cs", "csharp", src))
	r := findResilience(ents, "http_client_policy")
	if r == nil {
		t.Fatal("expected an http_client_policy resilience_policy")
	}
	if got := r.Properties["binding"]; got != "add_resilience_handler" {
		t.Errorf("binding = %q, want add_resilience_handler", got)
	}
	if got := r.Properties["http_client"]; got != "orders" {
		t.Errorf("http_client = %q, want orders", got)
	}
}

// Negative: a file with no Polly idiom yields nothing.
func TestPolly_NoMatch(t *testing.T) {
	src := `public class Foo { public void Bar() { var x = 1; } }`
	ents := extractFull(t, "custom_csharp_polly", fi("Foo.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

// Honest-partial: a config-driven retry count is omitted rather than guessed.
func TestPolly_PartialRetryCount(t *testing.T) {
	src := `
var policy = Policy
    .Handle<Exception>()
    .Retry(maxRetries);
`
	ents := extractFull(t, "custom_csharp_polly", fi("R.cs", "csharp", src))
	r := findResilience(ents, "retry")
	if r == nil {
		t.Fatal("expected a retry resilience_policy")
	}
	if got, ok := r.Properties["retry_count"]; ok {
		t.Errorf("retry_count should be omitted (variable arg), got %q", got)
	}
}
