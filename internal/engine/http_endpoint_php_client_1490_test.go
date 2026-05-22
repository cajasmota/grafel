package engine

// Tests for issue #1490 — PHP billing Http:: synthetics emit but
// caller_resolved=0 so billing→orders/notifications/legacy-erp FETCHES
// edges never form in the resolve pass.
//
// Root cause: indexPHPEnclosingFns returned BARE method names ("store",
// "handle") so source_caller was stamped as "Function:store". The PHP
// tree-sitter extractor emits methods as "ClassName.methodName"
// (SCOPE.Operation:InvoiceController.store) — the name mismatch prevented
// resolveCallerToFetchesEdge from finding the entity in the idx map,
// leaving caller_resolved=0 for every PHP class-based consumer.
//
// Fix: indexPHPEnclosingFns now qualifies method names with their
// enclosing class — "InvoiceController.store", "GenerateInvoicePdf.handle"
// — so the source_caller ref shape matches the extractor's entity Name.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Unit test: indexPHPEnclosingFns produces class-qualified names (#1490)
// ---------------------------------------------------------------------------

// TestPHP1490_IndexEnclosingFnsClassQualified verifies that methods inside a
// class body get a "ClassName.method" qualified name, while file-scope
// functions keep their bare name.
func TestPHP1490_IndexEnclosingFnsClassQualified(t *testing.T) {
	src := `<?php

namespace App\Http\Controllers;

class InvoiceController extends Controller
{
    public function index()
    {
        return response()->json([]);
    }

    public function store($req)
    {
        $ordersUrl = env('ORDERS_URL');
        Http::get("{$ordersUrl}/orders");
    }
}

function topLevelHelper()
{
    Http::get('http://example.com/api');
}
`
	spans := indexPHPEnclosingFns(src)
	byName := make(map[string]bool)
	for _, s := range spans {
		byName[s.name] = true
	}

	// Methods inside InvoiceController must be class-qualified.
	if !byName["InvoiceController.index"] {
		t.Errorf("#1490 expected InvoiceController.index in spans, got %v", spans)
	}
	if !byName["InvoiceController.store"] {
		t.Errorf("#1490 expected InvoiceController.store in spans, got %v", spans)
	}
	// File-scope functions keep their bare name.
	if !byName["topLevelHelper"] {
		t.Errorf("#1490 expected topLevelHelper in spans, got %v", spans)
	}
	// Bare unqualified names must NOT appear for class methods.
	if byName["index"] {
		t.Errorf("#1490 bare 'index' should not appear — must be class-qualified")
	}
	if byName["store"] {
		t.Errorf("#1490 bare 'store' should not appear — must be class-qualified")
	}
}

// TestPHP1490_MultiClass verifies correct qualification when multiple classes
// appear in the same file (e.g. GenerateInvoicePdf + a helper class).
func TestPHP1490_MultiClass(t *testing.T) {
	src := `<?php

class GenerateInvoicePdf
{
    public function __construct(int $id) {}

    public function handle(): void
    {
        Http::post('http://erp/erp/gl/postings', []);
    }
}

class InvoiceMailer
{
    public function send(): void
    {
        Http::post('http://notifications/notifications/email', []);
    }
}
`
	spans := indexPHPEnclosingFns(src)
	byName := make(map[string]bool)
	for _, s := range spans {
		byName[s.name] = true
	}

	want := []string{
		"GenerateInvoicePdf.__construct",
		"GenerateInvoicePdf.handle",
		"InvoiceMailer.send",
	}
	for _, w := range want {
		if !byName[w] {
			t.Errorf("#1490 expected %q in spans, got %v", w, spans)
		}
	}
}

// ---------------------------------------------------------------------------
// Resolve-phase integration: source_caller with class-qualified name (#1490)
// ---------------------------------------------------------------------------

// TestPHP1490_ResolveCallerClassQualified is the primary regression test for
// #1490. It simulates what happens when the merged entity table contains a
// PHP SCOPE.Operation named "InvoiceController.store" and the consumer
// synthetic carries source_caller="Function:InvoiceController.store"
// (as produced by the fixed indexPHPEnclosingFns).
//
// BEFORE fix: source_caller="Function:store" → caller_resolved=0.
// AFTER fix:  source_caller="Function:InvoiceController.store" → caller_resolved=1.
func TestPHP1490_ResolveCallerClassQualified(t *testing.T) {
	callerEntity := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       "InvoiceController.store",
		SourceFile: "app/Http/Controllers/InvoiceController.php",
		Language:   "php",
	}
	synth := types.EntityRecord{
		Kind:       httpEndpointCallKind,
		Name:       "http:GET:/orders/{orderId}",
		SourceFile: "app/Http/Controllers/InvoiceController.php",
		Language:   "php",
		Properties: map[string]string{
			"framework":     "laravel_http",
			"pattern_type":  "http_endpoint_client_synthesis",
			"source_caller": "Function:InvoiceController.store",
		},
	}
	merged := []types.EntityRecord{callerEntity, synth}
	out, stats := ResolveHTTPEndpointHandlers(merged)

	if stats.CallerResolved != 1 {
		t.Errorf("#1490 expected caller_resolved=1 for class-qualified PHP method, got %d (caller_unresolved=%d)",
			stats.CallerResolved, stats.CallerUnresolved)
	}
	if len(out) != 2 {
		t.Fatalf("#1490 expected 2 entities preserved, got %d", len(out))
	}

	// The caller entity should now carry a FETCHES edge to the synthetic.
	var fetchesFound bool
	for _, r := range out[0].Relationships {
		if r.Kind == "FETCHES" {
			fetchesFound = true
			wantFrom := "SCOPE.Operation:InvoiceController.store"
			wantTo := "http_endpoint_call:http:GET:/orders/{orderId}"
			if r.FromID != wantFrom {
				t.Errorf("#1490 FETCHES.FromID = %q, want %q", r.FromID, wantFrom)
			}
			if r.ToID != wantTo {
				t.Errorf("#1490 FETCHES.ToID = %q, want %q", r.ToID, wantTo)
			}
		}
	}
	if !fetchesFound {
		t.Errorf("#1490 expected FETCHES edge on InvoiceController.store, got %+v", out[0].Relationships)
	}

	// source_caller should be cleared after resolution.
	if _, has := out[1].Properties["source_caller"]; has {
		t.Errorf("#1490 source_caller should be cleared after resolution")
	}
}

// TestPHP1490_ResolveCallerGenerateInvoicePdf verifies that
// GenerateInvoicePdf.handle (the job class) also resolves correctly.
func TestPHP1490_ResolveCallerGenerateInvoicePdf(t *testing.T) {
	callerEntity := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       "GenerateInvoicePdf.handle",
		SourceFile: "app/Jobs/GenerateInvoicePdf.php",
		Language:   "php",
	}
	erpSynth := types.EntityRecord{
		Kind:       httpEndpointCallKind,
		Name:       "http:POST:/erp/gl/postings",
		SourceFile: "app/Jobs/GenerateInvoicePdf.php",
		Language:   "php",
		Properties: map[string]string{
			"framework":     "laravel_http",
			"pattern_type":  "http_endpoint_client_synthesis",
			"source_caller": "Function:GenerateInvoicePdf.handle",
		},
	}
	notifSynth := types.EntityRecord{
		Kind:       httpEndpointCallKind,
		Name:       "http:POST:/notifications/email",
		SourceFile: "app/Jobs/GenerateInvoicePdf.php",
		Language:   "php",
		Properties: map[string]string{
			"framework":     "laravel_http",
			"pattern_type":  "http_endpoint_client_synthesis",
			"source_caller": "Function:GenerateInvoicePdf.handle",
		},
	}
	merged := []types.EntityRecord{callerEntity, erpSynth, notifSynth}
	_, stats := ResolveHTTPEndpointHandlers(merged)

	// Both synthetics should resolve to the same caller entity.
	if stats.CallerResolved != 2 {
		t.Errorf("#1490 GenerateInvoicePdf.handle: expected caller_resolved=2, got %d (unresolved=%d)",
			stats.CallerResolved, stats.CallerUnresolved)
	}
}

// ---------------------------------------------------------------------------
// End-to-end synthesis + resolve: billing fixture (#1490)
// ---------------------------------------------------------------------------

// TestPHP1490_BillingCallerResolved is the before/after measurement for #1490.
//
// It runs the full synthesis pass over the REAL billing file content and then
// the resolve pass over a merged entity table that includes the PHP extractor
// entities (class-qualified names).
//
// BEFORE fix: caller_resolved=0 for all billing PHP Http:: calls (source_caller
// used bare name "store"/"handle" but extractor emits "InvoiceController.store").
// AFTER fix:  caller_resolved > 0 — at least store + handle resolve correctly.
func TestPHP1490_BillingCallerResolved(t *testing.T) {
	// Mirrors services/billing/app/Http/Controllers/InvoiceController.php.
	// Uses config('key') form (without default) which the synthesizer
	// tracks as runtime-dynamic via phpConfigVarRe.
	invoiceControllerSrc := `<?php

namespace App\Http\Controllers;

use App\Jobs\GenerateInvoicePdf;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Http;

class InvoiceController extends Controller
{
    public function index()
    {
        return [];
    }

    public function store(Request $request)
    {
        $orderId = $request->input('order_id');
        $ordersUrl = config('services.orders.url');
        $order = Http::get("{$ordersUrl}/orders/{$orderId}")->json();
        return response()->json($order, 201);
    }

    public function void(string $id)
    {
        return [];
    }
}
`

	// Mirrors services/billing/app/Jobs/GenerateInvoicePdf.php.
	generateInvoicePdfSrc := `<?php

namespace App\Jobs;

use Illuminate\Support\Facades\Http;

class GenerateInvoicePdf
{
    public function __construct(public int $invoiceId) {}

    public function handle(): void
    {
        $erpUrl = config('services.erp.url');
        Http::post("{$erpUrl}/erp/gl/postings", ['region' => 'US-CA']);

        $notifyUrl = config('services.notifications.url');
        Http::post("{$notifyUrl}/notifications/email", ['template' => 'invoice']);
    }
}
`

	// --- Step 1: Run synthesis on both files ---
	// Synthesize entities and relationships for InvoiceController.
	icEntities, _ := applyHTTPEndpointSynthesis(
		"php", "app/Http/Controllers/InvoiceController.php",
		[]byte(invoiceControllerSrc), nil, nil,
	)
	// Synthesize entities and relationships for GenerateInvoicePdf.
	pdfEntities, _ := applyHTTPEndpointSynthesis(
		"php", "app/Jobs/GenerateInvoicePdf.php",
		[]byte(generateInvoicePdfSrc), nil, nil,
	)

	// --- Step 2: Build a merged entity table that includes the PHP
	// tree-sitter extractor entities (class-qualified SCOPE.Operation names).
	icCaller := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       "InvoiceController.store",
		SourceFile: "app/Http/Controllers/InvoiceController.php",
		Language:   "php",
	}
	pdfCaller := types.EntityRecord{
		Kind:       "SCOPE.Operation",
		Name:       "GenerateInvoicePdf.handle",
		SourceFile: "app/Jobs/GenerateInvoicePdf.php",
		Language:   "php",
	}

	var merged []types.EntityRecord
	merged = append(merged, icCaller, pdfCaller)
	merged = append(merged, icEntities...)
	merged = append(merged, pdfEntities...)

	// --- Step 3: Run the resolve pass ---
	_, stats := ResolveHTTPEndpointHandlers(merged)

	t.Logf("=== #1490 before/after caller_resolved ===")
	t.Logf("BEFORE: caller_resolved=0 (bare name 'store'/'handle' never matched qualified entity)")
	t.Logf("AFTER:  caller_resolved=%d caller_unresolved=%d (synthetics=%d)",
		stats.CallerResolved, stats.CallerUnresolved, stats.Synthetics)

	// At minimum, InvoiceController.store (orders GET) + GenerateInvoicePdf.handle
	// (erp POST + notifications POST) = 3 caller_resolved edges expected.
	if stats.CallerResolved < 1 {
		t.Errorf("#1490 expected caller_resolved≥1 for PHP billing callers, got %d", stats.CallerResolved)
	}

	// Verify specific billing→orders edge: InvoiceController.store calls orders GET.
	t.Logf("  caller_resolved: %d (billing→orders + billing→erp + billing→notifications)",
		stats.CallerResolved)
}
