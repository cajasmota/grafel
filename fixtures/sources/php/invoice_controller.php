<?php

namespace App\Http\Controllers;

use Illuminate\Http\Request;
use Illuminate\Support\Facades\Http;
use GuzzleHttp\Client;

/**
 * InvoiceController — billing service controller that calls orders,
 * notifications, and legacy-erp services via Http:: facade and Guzzle.
 *
 * Cross-repo outbound edges this file is expected to produce:
 *   billing → orders        (Http::get  /api/orders/{id})
 *   billing → orders        (Http::post /api/orders)
 *   billing → notifications (Http::post /api/notifications)
 *   billing → legacy-erp   (Http::post /api/erp/invoices)
 */
class InvoiceController extends Controller
{
    /**
     * Store a new invoice and place an order in the orders service.
     */
    public function store(Request $request): \Illuminate\Http\JsonResponse
    {
        $validated = $request->validate([
            'customer_id' => 'required|integer',
            'items'       => 'required|array',
        ]);

        // Place order in orders-service via Http:: facade
        $orderResponse = Http::post('http://orders-service/api/orders', [
            'customer_id' => $validated['customer_id'],
            'items'       => $validated['items'],
        ]);

        $orderId = $orderResponse->json('id');

        // Notify the notifications-service
        Http::post('http://notifications-service/api/notifications', [
            'user_id' => $validated['customer_id'],
            'event'   => 'invoice.created',
            'payload' => ['order_id' => $orderId],
        ]);

        // Sync the new invoice to the legacy ERP system
        Http::withToken(config('services.erp.token'))
            ->post('http://legacy-erp-service/api/erp/invoices', [
                'order_id'    => $orderId,
                'customer_id' => $validated['customer_id'],
                'sync'        => true,
            ]);

        return response()->json(['order_id' => $orderId, 'status' => 'created'], 201);
    }

    /**
     * Show a single invoice by fetching the associated order from orders-service.
     */
    public function show(string $id): \Illuminate\Http\JsonResponse
    {
        $response = Http::get('http://orders-service/api/orders/' . $id);

        if (!$response->successful()) {
            return response()->json(['error' => 'Order not found'], 404);
        }

        return response()->json($response->json());
    }

    /**
     * Update invoice and re-sync to ERP via Guzzle directly.
     */
    public function update(Request $request, string $id): \Illuminate\Http\JsonResponse
    {
        $client = new Client();

        // Update order in orders-service via Guzzle
        $client->put('http://orders-service/api/orders/' . $id, [
            'json' => $request->all(),
        ]);

        // Patch the ERP record
        $client->patch('http://legacy-erp-service/api/erp/invoices/' . $id, [
            'json' => ['updated' => true],
        ]);

        return response()->json(['status' => 'updated']);
    }
}
