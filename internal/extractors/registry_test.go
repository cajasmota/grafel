package extractors

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---- helpers ----------------------------------------------------------------

// mockExtractor is a simple test extractor that returns a fixed result or panics.
type mockExtractor struct {
	language string
	records  []types.EntityRecord
	err      error
	panic    bool
}

func (m *mockExtractor) Language() string { return m.language }

func (m *mockExtractor) Extract(_ context.Context, _ FileInput) ([]types.EntityRecord, error) {
	if m.panic {
		panic("mock extractor panic")
	}
	return m.records, m.err
}

// cleanRegistry snapshots the current global registry, clears it for the
// duration of the test, and schedules a restore via t.Cleanup so that
// real-extractor registrations (from registry_gen.go init functions) are
// intact when other test packages run in the same binary.
func cleanRegistry(t *testing.T) {
	t.Helper()
	t.Cleanup(extractor.SnapshotForTesting())
	extractor.ClearForTesting()
}

// newTestTracer builds a recording OTel tracer and returns it with the span exporter.
func newTestTracer() (trace.Tracer, *tracetest.SpanRecorder) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	return tp.Tracer("test"), rec
}

// ---- tests ------------------------------------------------------------------

func TestRegisterAndGet(t *testing.T) {
	cleanRegistry(t)
	ext := &mockExtractor{language: "python"}
	Register("python", ext)

	got, ok := Get("python")
	if !ok {
		t.Fatal("expected extractor to be registered for python")
	}
	if got.Language() != "python" {
		t.Errorf("expected language python, got %s", got.Language())
	}
}

func TestGetUnregisteredLanguage(t *testing.T) {
	cleanRegistry(t)

	_, ok := Get("cobol")
	if ok {
		t.Fatal("expected false for unregistered language cobol")
	}
}

func TestExtractDispatchesToRegisteredExtractor(t *testing.T) {
	cleanRegistry(t)
	want := []types.EntityRecord{{Name: "Foo", Kind: "function", SourceFile: "foo.go"}}
	Register("go", &mockExtractor{language: "go", records: want})

	ctx := context.Background()
	got, err := Extract(ctx, FileInput{Path: "foo.go", Language: "go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d records, got %d", len(want), len(got))
	}
	if got[0].Name != want[0].Name {
		t.Errorf("expected record name %q, got %q", want[0].Name, got[0].Name)
	}
}

func TestExtractUnknownLanguageReturnsErrNoExtractor(t *testing.T) {
	cleanRegistry(t)

	ctx := context.Background()
	_, err := Extract(ctx, FileInput{Path: "x.xyz", Language: "xyz"})
	if err == nil {
		t.Fatal("expected error for unknown language")
	}
	if !errors.Is(err, ErrNoExtractorForLanguage) {
		t.Errorf("expected ErrNoExtractorForLanguage, got: %v", err)
	}
}

func TestExtractPanicRecoveredAsError(t *testing.T) {
	cleanRegistry(t)
	Register("rust", &mockExtractor{language: "rust", panic: true})

	ctx := context.Background()
	_, err := Extract(ctx, FileInput{Path: "main.rs", Language: "rust"})
	if err == nil {
		t.Fatal("expected error from panicking extractor")
	}
	want := "extractor panicked: mock extractor panic"
	if err.Error() != want {
		t.Errorf("expected error %q, got %q", want, err.Error())
	}
}

func TestExtractPanicSpanHasErrorAttribute(t *testing.T) {
	cleanRegistry(t)
	Register("rust", &mockExtractor{language: "rust", panic: true})

	tr, rec := newTestTracer()
	SetTracer(tr)
	defer SetTracer(nil)

	ctx := context.Background()
	_, _ = Extract(ctx, FileInput{Path: "main.rs", Language: "rust"})

	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}
	span := spans[len(spans)-1]
	if span.Name() != "extractor.dispatch" {
		t.Errorf("expected span name extractor.dispatch, got %s", span.Name())
	}
	if span.Status().Code.String() != "Error" {
		t.Errorf("expected span status Error, got %s", span.Status().Code.String())
	}
}

func TestExtractSuccessSpanEmitted(t *testing.T) {
	cleanRegistry(t)
	records := []types.EntityRecord{{Name: "Bar", Kind: "class", SourceFile: "bar.py"}}
	Register("python", &mockExtractor{language: "python", records: records})

	tr, rec := newTestTracer()
	SetTracer(tr)
	defer SetTracer(nil)

	ctx := context.Background()
	_, err := Extract(ctx, FileInput{Path: "bar.py", Language: "python"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}
	span := spans[len(spans)-1]
	if span.Name() != "extractor.dispatch" {
		t.Errorf("expected span name extractor.dispatch, got %s", span.Name())
	}

	// Verify required attributes are present.
	attrMap := spanAttrMap(span.Attributes())
	checkAttr(t, attrMap, "language", "python")
	checkAttr(t, attrMap, "file", "bar.py")
	if _, ok := attrMap["duration_ms"]; !ok {
		t.Error("expected duration_ms attribute on span")
	}
	if _, ok := attrMap["entity_count"]; !ok {
		t.Error("expected entity_count attribute on span")
	}
}

func TestListReturnsSortedLanguages(t *testing.T) {
	cleanRegistry(t)
	for _, lang := range []string{"typescript", "go", "python", "java", "rust"} {
		Register(lang, &mockExtractor{language: lang})
	}

	got := List()
	want := []string{"go", "java", "python", "rust", "typescript"}
	if len(got) != len(want) {
		t.Fatalf("expected %d languages, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("position %d: expected %q, got %q", i, w, got[i])
		}
	}
}

func TestListEmptyRegistry(t *testing.T) {
	cleanRegistry(t)
	got := List()
	if len(got) != 0 {
		t.Errorf("expected empty list, got %v", got)
	}
}

func TestConcurrentRegisterAndGet(t *testing.T) {
	cleanRegistry(t)

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n * 2)

	for i := range n {
		lang := fmt.Sprintf("lang%d", i)
		go func(l string) {
			defer wg.Done()
			Register(l, &mockExtractor{language: l})
		}(lang)
		go func(l string) {
			defer wg.Done()
			Get(l) // may or may not find it — race check only
		}(lang)
	}

	wg.Wait()
}

// ---- utilities --------------------------------------------------------------

func spanAttrMap(attrs []attribute.KeyValue) map[string]attribute.Value {
	m := make(map[string]attribute.Value, len(attrs))
	for _, kv := range attrs {
		m[string(kv.Key)] = kv.Value
	}
	return m
}

func checkAttr(t *testing.T, m map[string]attribute.Value, key, want string) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("expected attribute %q on span", key)
		return
	}
	if got := v.AsString(); got != want {
		t.Errorf("attribute %q: expected %q, got %q", key, want, got)
	}
}
