package php_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/php"
)

// extractFull returns the full EntityRecords (with properties) for prop-asserting
// tests of the Laravel queue producer/consumer extraction (#4920).
func extractFull(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

func findEntity(ents []types.EntityRecord, kind, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// TestLaravelQueueConsumerAttribution: a queued Job class carries job_class +
// the file-scoped $queue attribution on both the SCOPE.Service and its handle().
func TestLaravelQueueConsumerAttribution(t *testing.T) {
	src := `<?php
class SendInvoice implements ShouldQueue
{
    public $queue = 'invoices';

    public function handle()
    {
        // ...
    }
}
`
	ents := extractFull(t, "custom_php_laravel", fi("SendInvoice.php", "php", src))

	job := findEntity(ents, "SCOPE.Service", "SendInvoice")
	if job == nil {
		t.Fatal("expected SCOPE.Service SendInvoice job class")
	}
	if job.Properties["job_class"] != "SendInvoice" {
		t.Errorf("job_class = %q, want SendInvoice", job.Properties["job_class"])
	}
	if job.Properties["queue"] != "invoices" {
		t.Errorf("queue = %q, want invoices", job.Properties["queue"])
	}

	handle := findEntity(ents, "SCOPE.Operation", "handle")
	if handle == nil {
		t.Fatal("expected SCOPE.Operation handle")
	}
	if handle.Properties["queue"] != "invoices" {
		t.Errorf("handle queue = %q, want invoices", handle.Properties["queue"])
	}
}

// TestLaravelQueueProducerStatic: the static-dispatch producer idiom converges
// with the consumer by job-class name.
func TestLaravelQueueProducerStatic(t *testing.T) {
	src := `<?php
class OrderController
{
    public function store()
    {
        SendInvoice::dispatch($order);
        ProcessPayment::dispatchSync($payment);
    }
}
`
	ents := extractFull(t, "custom_php_laravel", fi("OrderController.php", "php", src))

	d1 := findEntity(ents, "SCOPE.Operation", "SendInvoice.dispatch")
	if d1 == nil {
		t.Fatal("expected SendInvoice.dispatch producer op")
	}
	if d1.Properties["job_class"] != "SendInvoice" || d1.Properties["dispatch_method"] != "dispatch" {
		t.Errorf("got job_class=%q dispatch_method=%q", d1.Properties["job_class"], d1.Properties["dispatch_method"])
	}
	if d1.Properties["provenance"] != "INFERRED_FROM_LARAVEL_JOB_DISPATCH" {
		t.Errorf("provenance = %q", d1.Properties["provenance"])
	}

	d2 := findEntity(ents, "SCOPE.Operation", "ProcessPayment.dispatch")
	if d2 == nil {
		t.Fatal("expected ProcessPayment.dispatch producer op")
	}
	if d2.Properties["dispatch_method"] != "dispatchSync" {
		t.Errorf("dispatch_method = %q, want dispatchSync", d2.Properties["dispatch_method"])
	}
}

// TestLaravelQueueProducerHelper: the dispatch(new Job) helper and Bus::dispatch
// facade forms both resolve to the job class (and Bus is never a target).
func TestLaravelQueueProducerHelper(t *testing.T) {
	src := `<?php
dispatch(new SendInvoice($order));
Bus::dispatch(new ProcessPayment($payment));
`
	ents := extractFull(t, "custom_php_laravel", fi("helper.php", "php", src))

	if findEntity(ents, "SCOPE.Operation", "SendInvoice.dispatch") == nil {
		t.Error("expected SendInvoice.dispatch from dispatch(new ...) helper")
	}
	if findEntity(ents, "SCOPE.Operation", "ProcessPayment.dispatch") == nil {
		t.Error("expected ProcessPayment.dispatch from Bus::dispatch(new ...)")
	}
	if findEntity(ents, "SCOPE.Operation", "Bus.dispatch") != nil {
		t.Error("Bus facade must not be emitted as a dispatch target")
	}
}

// TestLaravelQueueProducerConverges: producer dispatch and consumer service share
// the job-class identity (the value of the extraction).
func TestLaravelQueueProducerConverges(t *testing.T) {
	src := `<?php
class SendInvoice implements ShouldQueue {
    public function handle() {}
}
SendInvoice::dispatch($order);
`
	ents := extractFull(t, "custom_php_laravel", fi("mixed.php", "php", src))

	svc := findEntity(ents, "SCOPE.Service", "SendInvoice")
	prod := findEntity(ents, "SCOPE.Operation", "SendInvoice.dispatch")
	if svc == nil || prod == nil {
		t.Fatalf("expected both consumer service and producer op, got svc=%v prod=%v", svc != nil, prod != nil)
	}
	if svc.Properties["job_class"] != prod.Properties["job_class"] {
		t.Errorf("producer/consumer job_class mismatch: %q vs %q",
			prod.Properties["job_class"], svc.Properties["job_class"])
	}
}
