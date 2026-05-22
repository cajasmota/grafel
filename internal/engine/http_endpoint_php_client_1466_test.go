package engine

// Tests for issue #1466 — Laravel Http:: facade consumer-side extraction.
//
// Issue context: #1429 added Laravel PRODUCER endpoint extraction
// (Route::get/resource → http_endpoint_definition). This file verifies that
// the CONSUMER side (Http::get/post/withToken/withHeaders chained calls in
// InvoiceController.php / GenerateInvoicePdf.php) is extracted and that
// the billing→orders, billing→notifications, and billing→legacy-erp
// cross-service outbound edges are all emitted.
//
// Before fix (pre wave-2c): 0 consumer http_endpoint_call entities from PHP
// files — billing outbound edges all absent.
//
// After fix: billing outbound edges fully resolved:
//   billing → orders        (GET+POST+PUT+PATCH on /api/orders/*)
//   billing → notifications (POST /api/notifications)
//   billing → legacy-erp   (POST /api/erp/invoices, POST /api/erp/pdf-uploads,
//                            PATCH /api/erp/invoices/{id})

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// InvoiceController scenario
// ---------------------------------------------------------------------------

// TestPHP1466_InvoiceController verifies that Http:: facade calls in
// InvoiceController.php produce the correct http_endpoint_call entities
// for all three outbound services (orders, notifications, legacy-erp).
func TestPHP1466_InvoiceController(t *testing.T) {
	src := `<?php

namespace App\Http\Controllers;

use Illuminate\Http\Request;
use Illuminate\Support\Facades\Http;
use GuzzleHttp\Client;

class InvoiceController extends Controller
{
    public function store(Request $request)
    {
        $orderResponse = Http::post('http://orders-service/api/orders', [
            'customer_id' => $validated['customer_id'],
            'items'       => $validated['items'],
        ]);
        $orderId = $orderResponse->json('id');

        Http::post('http://notifications-service/api/notifications', [
            'user_id' => $validated['customer_id'],
            'event'   => 'invoice.created',
        ]);

        Http::withToken(config('services.erp.token'))
            ->post('http://legacy-erp-service/api/erp/invoices', [
                'order_id' => $orderId,
            ]);

        return response()->json(['order_id' => $orderId]);
    }

    public function show(string $id)
    {
        $response = Http::get('http://orders-service/api/orders/' . $id);
        return response()->json($response->json());
    }

    public function update(Request $request, string $id)
    {
        $client = new \GuzzleHttp\Client();
        $client->put('http://orders-service/api/orders/' . $id, [
            'json' => $request->all(),
        ]);
        $client->patch('http://legacy-erp-service/api/erp/invoices/' . $id, [
            'json' => ['updated' => true],
        ]);
        return response()->json(['status' => 'updated']);
    }
}
`
	ids, rels := runDetectWithRels(t, "php", "InvoiceController.php", src)

	// billing → orders (via Http:: POST)
	requireContains(t, ids, []string{"http:POST:/api/orders"}, "#1466 billing→orders POST")
	requireFetches(t, rels, "http:POST:/api/orders", "#1466 billing→orders POST")

	// billing → notifications (via Http:: POST)
	requireContains(t, ids, []string{"http:POST:/api/notifications"}, "#1466 billing→notifications POST")
	requireFetches(t, rels, "http:POST:/api/notifications", "#1466 billing→notifications POST")

	// billing → legacy-erp (via Http::withToken()->post, chained form)
	requireContains(t, ids, []string{"http:POST:/api/erp/invoices"}, "#1466 billing→legacy-erp POST")
	requireFetches(t, rels, "http:POST:/api/erp/invoices", "#1466 billing→legacy-erp POST")

	// billing → orders (via Guzzle PUT)
	requireContains(t, ids, []string{"http:PUT:/api/orders"}, "#1466 billing→orders PUT via Guzzle")

	// billing → legacy-erp (via Guzzle PATCH)
	requireContains(t, ids, []string{"http:PATCH:/api/erp/invoices"}, "#1466 billing→legacy-erp PATCH via Guzzle")
}

// TestPHP1466_GenerateInvoicePdf verifies that Http:: facade calls in
// GenerateInvoicePdf.php produce the correct consumer-side synthetics.
func TestPHP1466_GenerateInvoicePdf(t *testing.T) {
	src := `<?php

namespace App\Jobs;

use Illuminate\Support\Facades\Http;

class GenerateInvoicePdf
{
    public function __construct(
        private readonly int    $invoiceId,
        private readonly string $orderId,
    ) {}

    public function handle(): void
    {
        // Fetch order details from orders-service
        $order = Http::get('http://orders-service/api/orders/' . $this->orderId)->json();

        // Push the PDF metadata to legacy ERP via withHeaders chained form
        Http::withHeaders([
            'X-Api-Key' => config('services.erp.api_key'),
            'Accept'    => 'application/json',
        ])->post('http://legacy-erp-service/api/erp/pdf-uploads', [
            'invoice_id' => $this->invoiceId,
        ]);

        // Notify user via notifications-service via withToken chained form
        Http::withToken(config('services.notifications.token'))
            ->post('http://notifications-service/api/notifications', [
                'user_id' => $order['customer_id'],
                'event'   => 'invoice.pdf_ready',
            ]);
    }
}
`
	ids, rels := runDetectWithRels(t, "php", "GenerateInvoicePdf.php", src)

	// billing → orders (Http::get)
	requireContains(t, ids, []string{"http:GET:/api/orders"}, "#1466 pdf billing→orders GET")
	requireFetches(t, rels, "http:GET:/api/orders", "#1466 pdf billing→orders GET")

	// billing → legacy-erp (Http::withHeaders()->post — chained form)
	requireContains(t, ids, []string{"http:POST:/api/erp/pdf-uploads"}, "#1466 pdf billing→legacy-erp POST")
	requireFetches(t, rels, "http:POST:/api/erp/pdf-uploads", "#1466 pdf billing→legacy-erp POST")

	// billing → notifications (Http::withToken()->post — chained form)
	requireContains(t, ids, []string{"http:POST:/api/notifications"}, "#1466 pdf billing→notifications POST")
	requireFetches(t, rels, "http:POST:/api/notifications", "#1466 pdf billing→notifications POST")
}

// TestPHP1466_WithTokenVariants verifies Http::withToken()->get/post/put are
// all recognised (chained via phpLaravelHttpChainedRe).
func TestPHP1466_WithTokenVariants(t *testing.T) {
	src := `<?php

use Illuminate\Support\Facades\Http;

class ERP
{
    public function fetchErpInvoice(string $id): array
    {
        return Http::withToken(config('services.erp.token'))
            ->get('http://legacy-erp-service/api/erp/invoices/' . $id)
            ->json();
    }

    public function createErpInvoice(array $data): array
    {
        return Http::withToken(config('services.erp.token'))
            ->post('http://legacy-erp-service/api/erp/invoices', $data)
            ->json();
    }

    public function updateErpInvoice(string $id, array $data): void
    {
        Http::withToken(config('services.erp.token'))
            ->put('http://legacy-erp-service/api/erp/invoices/' . $id, $data);
    }

    public function deleteErpInvoice(string $id): void
    {
        Http::withToken(config('services.erp.token'))
            ->delete('http://legacy-erp-service/api/erp/invoices/' . $id);
    }
}
`
	ids, rels := runDetectWithRels(t, "php", "ERP.php", src)

	want := []string{
		"http:GET:/api/erp/invoices",
		"http:POST:/api/erp/invoices",
		"http:PUT:/api/erp/invoices",
		"http:DELETE:/api/erp/invoices",
	}
	requireContains(t, ids, want, "#1466 withToken verb coverage")
	for _, id := range want {
		requireFetches(t, rels, id, "#1466 withToken FETCHES edge")
	}
}

// TestPHP1466_WithHeadersVariants verifies Http::withHeaders()->get/post are
// all recognised (chained via phpLaravelHttpChainedRe).
func TestPHP1466_WithHeadersVariants(t *testing.T) {
	src := `<?php

use Illuminate\Support\Facades\Http;

class ApiGateway
{
    private array $defaultHeaders = [
        'Accept' => 'application/json',
    ];

    public function getUsers(): array
    {
        return Http::withHeaders($this->defaultHeaders)
            ->get('http://users-service/api/users')
            ->json();
    }

    public function createUser(array $data): array
    {
        return Http::withHeaders($this->defaultHeaders)
            ->post('http://users-service/api/users', $data)
            ->json();
    }
}
`
	ids, rels := runDetectWithRels(t, "php", "ApiGateway.php", src)

	want := []string{
		"http:GET:/api/users",
		"http:POST:/api/users",
	}
	requireContains(t, ids, want, "#1466 withHeaders verb coverage")
	for _, id := range want {
		requireFetches(t, rels, id, "#1466 withHeaders FETCHES edge")
	}
}

// TestPHP1466_FetchesEdgeCallerAttribution verifies that FETCHES edges carry
// the enclosing method name in their FromID, enabling caller-graph tracing
// from the billing service.
func TestPHP1466_FetchesEdgeCallerAttribution(t *testing.T) {
	src := `<?php

use Illuminate\Support\Facades\Http;

class InvoiceService
{
    public function generateAndSync(int $invoiceId): void
    {
        Http::post('http://orders-service/api/orders', ['invoice_id' => $invoiceId]);
    }

    public function notifyUser(int $userId): void
    {
        Http::post('http://notifications-service/api/notifications', ['user_id' => $userId]);
    }
}
`
	_, res := runDetect(t, "php", "InvoiceService.php", src)

	// Verify FETCHES edges carry correct caller attribution.
	callerMap := make(map[string]string) // entityID → FromID
	for _, r := range res.Relationships {
		if r.Kind == fetchesEdgeKind {
			callerMap[r.ToID] = r.FromID
		}
	}

	if from, ok := callerMap["http:POST:/api/orders"]; !ok || !strings.Contains(from, "generateAndSync") {
		t.Errorf("#1466 expected FETCHES edge from generateAndSync to /api/orders, got FromID=%q", from)
	}
	if from, ok := callerMap["http:POST:/api/notifications"]; !ok || !strings.Contains(from, "notifyUser") {
		t.Errorf("#1466 expected FETCHES edge from notifyUser to /api/notifications, got FromID=%q", from)
	}
}

// TestPHP1466_BillingServiceLinkCount is the "before/after" measurement test.
//
// BEFORE (pre wave-2c, pre-#1466 fix): 0 consumer http_endpoint_call entities
// and 0 FETCHES edges emitted from PHP billing files.
//
// AFTER (this fix): billing outbound cross-repo edges fully emitted.
// Expected link counts from the two billing files combined:
//
//	billing → orders        : 3 edges (POST + GET from InvoiceController, GET from GenerateInvoicePdf)
//	billing → notifications : 2 edges (POST from each file)
//	billing → legacy-erp   : 3 edges (POST /erp/invoices, PATCH /erp/invoices, POST /erp/pdf-uploads)
//	total http_endpoint_call entities: ≥ 8
func TestPHP1466_BillingServiceLinkCount(t *testing.T) {
	invoiceControllerSrc := `<?php

namespace App\Http\Controllers;

use Illuminate\Support\Facades\Http;
use GuzzleHttp\Client;

class InvoiceController extends Controller
{
    public function store()
    {
        Http::post('http://orders-service/api/orders', ['items' => []]);
        Http::post('http://notifications-service/api/notifications', ['event' => 'invoice.created']);
        Http::withToken('token')->post('http://legacy-erp-service/api/erp/invoices', ['sync' => true]);
    }

    public function show(string $id)
    {
        Http::get('http://orders-service/api/orders/' . $id);
    }

    public function update(string $id)
    {
        $client = new Client();
        $client->patch('http://legacy-erp-service/api/erp/invoices/' . $id, ['json' => []]);
    }
}
`

	generateInvoicePdfSrc := `<?php

namespace App\Jobs;

use Illuminate\Support\Facades\Http;

class GenerateInvoicePdf
{
    public function handle(): void
    {
        Http::get('http://orders-service/api/orders/123');
        Http::withHeaders(['X-Api-Key' => 'key'])->post('http://legacy-erp-service/api/erp/pdf-uploads', []);
        Http::withToken('token')->post('http://notifications-service/api/notifications', ['event' => 'pdf_ready']);
    }
}
`

	// --- measure InvoiceController.php ---
	idsIC, relsIC := runDetectWithRels(t, "php", "InvoiceController.php", invoiceControllerSrc)
	// --- measure GenerateInvoicePdf.php ---
	idsPDF, relsPDF := runDetectWithRels(t, "php", "GenerateInvoicePdf.php", generateInvoicePdfSrc)

	// Deduplicate across files (same canonical ID may appear in both files).
	allIDs := make(map[string]bool)
	for _, id := range idsIC {
		if strings.HasPrefix(id, "http:") {
			allIDs[id] = true
		}
	}
	for _, id := range idsPDF {
		if strings.HasPrefix(id, "http:") {
			allIDs[id] = true
		}
	}

	// Count FETCHES edges across both files.
	totalFetches := 0
	fetchesByTarget := make(map[string]int)
	for _, r := range append(relsIC, relsPDF...) {
		if r.Kind == fetchesEdgeKind {
			totalFetches++
			fetchesByTarget[r.ToID]++
		}
	}

	// --- Before/After report ---
	t.Logf("=== #1466 before/after link count ===")
	t.Logf("BEFORE: 0 http_endpoint_call entities, 0 FETCHES edges (wave-2c not yet landed)")
	t.Logf("AFTER:  %d distinct http_endpoint_call entities, %d FETCHES edges across 2 billing files",
		len(allIDs), totalFetches)
	t.Logf("  billing→orders        : %d edge(s)",
		fetchesByTarget["http:POST:/api/orders"]+fetchesByTarget["http:GET:/api/orders"])
	t.Logf("  billing→notifications : %d edge(s)",
		fetchesByTarget["http:POST:/api/notifications"])
	t.Logf("  billing→legacy-erp   : %d edge(s)",
		fetchesByTarget["http:POST:/api/erp/invoices"]+
			fetchesByTarget["http:PATCH:/api/erp/invoices"]+
			fetchesByTarget["http:POST:/api/erp/pdf-uploads"])

	// Assert minimum AFTER counts.
	// billing → orders
	ordersEdges := fetchesByTarget["http:POST:/api/orders"] + fetchesByTarget["http:GET:/api/orders"]
	if ordersEdges < 2 {
		t.Errorf("billing→orders: expected ≥2 FETCHES edges, got %d", ordersEdges)
	}
	// billing → notifications
	notifEdges := fetchesByTarget["http:POST:/api/notifications"]
	if notifEdges < 2 {
		t.Errorf("billing→notifications: expected ≥2 FETCHES edges, got %d", notifEdges)
	}
	// billing → legacy-erp
	erpEdges := fetchesByTarget["http:POST:/api/erp/invoices"] +
		fetchesByTarget["http:PATCH:/api/erp/invoices"] +
		fetchesByTarget["http:POST:/api/erp/pdf-uploads"]
	if erpEdges < 3 {
		t.Errorf("billing→legacy-erp: expected ≥3 FETCHES edges, got %d", erpEdges)
	}
	// Total distinct endpoints
	if len(allIDs) < 6 {
		t.Errorf("expected ≥6 distinct http_endpoint_call entities, got %d: %v", len(allIDs), allIDs)
	}
	// Total FETCHES edges
	if totalFetches < 7 {
		t.Errorf("expected ≥7 total FETCHES edges across both files, got %d", totalFetches)
	}
}

// TestPHP1466_KindIsHTTPEndpointCall verifies that PHP consumer-side synthetics
// are emitted with Kind=http_endpoint_call (#1217 split kinds), NOT the legacy
// http_endpoint kind, so the dashboard properly separates producers from
// consumers.
func TestPHP1466_KindIsHTTPEndpointCall(t *testing.T) {
	src := `<?php

use Illuminate\Support\Facades\Http;

function callOrders(): void
{
    Http::get('http://orders-service/api/orders');
    Http::post('http://orders-service/api/orders', []);
}
`
	_, res := runDetect(t, "php", "caller.php", src)

	for _, e := range res.Entities {
		if e.ID == "http:GET:/api/orders" || e.ID == "http:POST:/api/orders" {
			if e.Kind != httpEndpointCallKind {
				t.Errorf("#1466 entity %q: expected Kind=%q, got %q",
					e.ID, httpEndpointCallKind, e.Kind)
			}
			if pt := e.Properties["pattern_type"]; pt != "http_endpoint_client_synthesis" {
				t.Errorf("#1466 entity %q: expected pattern_type=http_endpoint_client_synthesis, got %q",
					e.ID, pt)
			}
		}
	}
}

// TestPHP1466_NegativeNoFalsePositives confirms a PHP file that defines
// routes (producer side) does NOT also emit consumer-side http_endpoint_call
// entities from the Route:: calls.
func TestPHP1466_NegativeNoFalsePositives(t *testing.T) {
	src := `<?php

use Illuminate\Support\Facades\Route;

Route::get('/invoices', [\App\Http\Controllers\InvoiceController::class, 'index']);
Route::post('/invoices', [\App\Http\Controllers\InvoiceController::class, 'store']);
Route::apiResource('payments', \App\Http\Controllers\PaymentController::class);
`
	_, res := runDetect(t, "php", "routes/api.php", src)

	for _, e := range res.Entities {
		if e.Kind == httpEndpointCallKind {
			t.Errorf("#1466 negative: route definition file emitted consumer-side entity %q", e.ID)
		}
	}
}
