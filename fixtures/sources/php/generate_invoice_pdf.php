<?php

namespace App\Jobs;

use Illuminate\Bus\Queueable;
use Illuminate\Contracts\Queue\ShouldQueue;
use Illuminate\Support\Facades\Http;

/**
 * GenerateInvoicePdf — queued job that generates a PDF for an invoice
 * and pushes metadata to the legacy ERP system and notifications service.
 *
 * Cross-repo outbound edges this file is expected to produce:
 *   billing → legacy-erp   (Http::withHeaders()->post /api/erp/pdf-uploads)
 *   billing → notifications (Http::withToken()->post  /api/notifications)
 *   billing → orders        (Http::get /api/orders/{order_id})
 */
class GenerateInvoicePdf implements ShouldQueue
{
    use Queueable;

    public function __construct(
        private readonly int    $invoiceId,
        private readonly string $orderId,
    ) {}

    /**
     * Execute the job.
     */
    public function handle(): void
    {
        // Fetch order details from orders-service
        $order = Http::get('http://orders-service/api/orders/' . $this->orderId)->json();

        // Generate PDF (local)
        $pdfPath = $this->renderPdf($order);

        // Push the PDF metadata to legacy ERP
        Http::withHeaders([
            'X-Api-Key'    => config('services.erp.api_key'),
            'Accept'       => 'application/json',
        ])->post('http://legacy-erp-service/api/erp/pdf-uploads', [
            'invoice_id' => $this->invoiceId,
            'order_id'   => $this->orderId,
            'pdf_path'   => $pdfPath,
        ]);

        // Notify user via notifications-service
        Http::withToken(config('services.notifications.token'))
            ->post('http://notifications-service/api/notifications', [
                'user_id' => $order['customer_id'],
                'event'   => 'invoice.pdf_ready',
                'payload' => ['invoice_id' => $this->invoiceId],
            ]);
    }

    private function renderPdf(array $order): string
    {
        // Stub: real implementation renders a Blade → PDF
        return '/storage/invoices/' . $this->invoiceId . '.pdf';
    }
}
