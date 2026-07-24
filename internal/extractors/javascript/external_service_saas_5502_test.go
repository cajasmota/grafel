package javascript_test

// external_service_saas_5502_test.go — the shared common-TS-SaaS-SDK
// allow-list (#5502, epic #5479). The detector machinery is unchanged: the
// import-gated `new Ctor()` + receiver pass and default-import receiver-call
// pass already emit DEPENDS_ON_SERVICE edges to a converged SCOPE.ExternalService
// node. #5502 EXTENDS the shared service dictionary
// (extractor.ServiceForImportSource) with the common SaaS SDKs — one allow-list,
// not per-SDK sprawl — so calls into Stripe / Slack / PostHog / Knock / Plaid /
// HubSpot / Linear / Mapbox / Contentful / UploadThing / Cal.com / Vercel-AI /
// OpenFeature surface as external-service dependencies tagged with the service.
//
// Honest-partial / precision-first (preserved): the SDK IMPORT must be present
// for any edge — a bare `stripe`-named local that was never imported from the
// `stripe` package emits NOTHING.

import (
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// jsSvcOp returns the operation property recorded on the DEPENDS_ON_SERVICE edge
// (fromName -> service), or "" if there is no such edge / no operation.
func jsSvcOp(recs []types.EntityRecord, fromName, service string) string {
	want := extreg.ExternalServiceTargetID(service)
	for i := range recs {
		if recs[i].Name != fromName {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindDependsOnService) && r.ToID == want {
				return r.Properties.Get("operation")
			}
		}
	}
	return ""
}

// TestSaaS_StripeChargesCreate: `new Stripe()` + `stripe.charges.create(...)`
// → stripe, operation charges.create.
func TestSaaS_StripeChargesCreate_5502(t *testing.T) {
	src := []byte(`import Stripe from "stripe";

function pay(amount) {
  const stripe = new Stripe("sk_test");
  return stripe.charges.create({ amount });
}
`)
	recs := extract(t, src, "typescript", parseTS(t, src))
	if !jsSvcEdge(recs, "pay", "stripe") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(pay -> stripe)")
	}
	if op := jsSvcOp(recs, "pay", "stripe"); op != "charges.create" {
		t.Errorf("expected operation charges.create, got %q", op)
	}
	if id, n := jsSvcNode(recs, "stripe"); id == "" || n != 1 {
		t.Errorf("expected exactly 1 stripe node, got id=%q n=%d", id, n)
	}
}

// TestSaaS_SlackPostMessage: `new WebClient()` from @slack/web-api +
// `slack.chat.postMessage(...)` → slack, op chat.postMessage.
func TestSaaS_SlackPostMessage_5502(t *testing.T) {
	src := []byte(`import { WebClient } from "@slack/web-api";

async function notify(text) {
  const slack = new WebClient("xoxb");
  return slack.chat.postMessage({ channel: "#x", text });
}
`)
	recs := extract(t, src, "typescript", parseTS(t, src))
	if !jsSvcEdge(recs, "notify", "slack") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(notify -> slack)")
	}
	if op := jsSvcOp(recs, "notify", "slack"); op != "chat.postMessage" {
		t.Errorf("expected operation chat.postMessage, got %q", op)
	}
}

// TestSaaS_AWSS3Send: `new S3Client()` + `s3.send(new PutObjectCommand())`
// → aws-s3.
func TestSaaS_AWSS3Send_5502(t *testing.T) {
	src := []byte(`import { S3Client, PutObjectCommand } from "@aws-sdk/client-s3";

async function upload(body) {
  const s3 = new S3Client({});
  return s3.send(new PutObjectCommand({ Bucket: "b", Key: "k", Body: body }));
}
`)
	recs := extract(t, src, "typescript", parseTS(t, src))
	if !jsSvcEdge(recs, "upload", "aws-s3") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(upload -> aws-s3)")
	}
	if id, _ := jsSvcNode(recs, "aws-s3"); id == "" {
		t.Errorf("missing SCOPE.ExternalService:aws-s3 node")
	}
}

// TestSaaS_PostHogCapture: `new PostHog()` from posthog-node +
// `posthog.capture(...)` → posthog (the issue acceptance shape).
func TestSaaS_PostHogCapture_5502(t *testing.T) {
	src := []byte(`import { PostHog } from "posthog-node";

function track(event) {
  const posthog = new PostHog("phc_key");
  return posthog.capture({ distinctId: "u", event });
}
`)
	recs := extract(t, src, "typescript", parseTS(t, src))
	if !jsSvcEdge(recs, "track", "posthog") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(track -> posthog)")
	}
	if op := jsSvcOp(recs, "track", "posthog"); op != "capture" {
		t.Errorf("expected operation capture, got %q", op)
	}
}

// TestSaaS_ScopedFamilies: a scoped-family SDK (`@knocklabs/node`,
// `@linear/sdk`, `@hubspot/api-client`) resolves via the allow-list.
func TestSaaS_ScopedFamilies_5502(t *testing.T) {
	cases := []struct {
		name, pkg, ctor, service string
	}{
		{"knock", "@knocklabs/node", "Knock", "knock"},
		{"linear", "@linear/sdk", "LinearClient", "linear"},
		{"hubspot", "@hubspot/api-client", "Client", "hubspot"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := []byte("import { " + c.ctor + " } from \"" + c.pkg + "\";\n" +
				"function run() {\n" +
				"  const client = new " + c.ctor + "({});\n" +
				"  return client;\n" +
				"}\n")
			recs := extract(t, src, "typescript", parseTS(t, src))
			if !jsSvcEdge(recs, "run", c.service) {
				t.Fatalf("missing DEPENDS_ON_SERVICE(run -> %s) for %s", c.service, c.pkg)
			}
		})
	}
}

// TestSaaS_VercelAISubpath: a `@ai-sdk/openai` subpath import resolves to the
// vercel-ai service (scope-prefix family).
func TestSaaS_VercelAISubpath_5502(t *testing.T) {
	// The dictionary resolves the import; a constructor shape ties it to a fn.
	if got := extreg.ServiceForImportSource("@ai-sdk/openai"); got != "vercel-ai" {
		t.Errorf("@ai-sdk/openai: expected vercel-ai, got %q", got)
	}
	if got := extreg.ServiceForImportSource("@openfeature/server-sdk"); got != "openfeature" {
		t.Errorf("@openfeature/server-sdk: expected openfeature, got %q", got)
	}
	if got := extreg.ServiceForImportSource("ai"); got != "vercel-ai" {
		t.Errorf("ai: expected vercel-ai, got %q", got)
	}
}

// TestSaaS_NonImportedStripeLocal_NoEdge: a `stripe`-named local that was NEVER
// imported from the `stripe` package must emit NO edge (the import gate).
func TestSaaS_NonImportedStripeLocal_NoEdge_5502(t *testing.T) {
	src := []byte(`function pay(stripe) {
  // local param named stripe, no import — must NOT false-positive
  return stripe.charges.create({ amount: 10 });
}
`)
	recs := extract(t, src, "typescript", parseTS(t, src))
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindDependsOnService) {
				t.Fatalf("unexpected DEPENDS_ON_SERVICE on non-imported stripe local: %s -> %s",
					recs[i].Name, r.ToID)
			}
		}
	}
}
