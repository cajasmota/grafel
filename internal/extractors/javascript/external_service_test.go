package javascript_test

import (
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func jsSvcEdge(recs []types.EntityRecord, fromName, service string) bool {
	want := extreg.ExternalServiceTargetID(service)
	for i := range recs {
		if recs[i].Name != fromName {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindDependsOnService) && r.ToID == want {
				return true
			}
		}
	}
	return false
}

func jsSvcNode(recs []types.EntityRecord, service string) (string, int) {
	want := extreg.ExternalServiceName(service)
	id, count := "", 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExternalService) && recs[i].Name == want {
			id = recs[i].ID
			count++
		}
	}
	return id, count
}

// TestJSService_StripeConstructThenCall: `new Stripe(key)` + `stripe.charges.
// create()` resolves the local var back to the stripe service.
func TestJSService_StripeConstructThenCall(t *testing.T) {
	src := []byte(`import Stripe from "stripe";

function pay(amount) {
  const stripe = new Stripe("sk_test");
  return stripe.charges.create({ amount });
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))

	if !jsSvcEdge(recs, "pay", "stripe") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(pay -> stripe)")
	}
	if id, n := jsSvcNode(recs, "stripe"); id == "" || n != 1 {
		t.Errorf("expected exactly 1 stripe node, got id=%q n=%d", id, n)
	}
}

// TestJSService_SendGrid: a default-import receiver call `sgMail.send()`.
func TestJSService_SendGrid(t *testing.T) {
	src := []byte(`import sgMail from "@sendgrid/mail";

function email(to) {
  return sgMail.send({ to, subject: "hi" });
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsSvcEdge(recs, "email", "sendgrid") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(email -> sendgrid)")
	}
}

// TestJSService_AWSS3Client: `new S3Client()` from @aws-sdk → aws-s3.
func TestJSService_AWSS3Client(t *testing.T) {
	src := []byte(`import { S3Client } from "@aws-sdk/client-s3";

function upload() {
  const client = new S3Client({});
  return client;
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsSvcEdge(recs, "upload", "aws-s3") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(upload -> aws-s3)")
	}
	if id, _ := jsSvcNode(recs, "aws-s3"); id == "" {
		t.Errorf("missing SCOPE.ExternalService:aws-s3 node")
	}
}

// TestJSService_AWSCognitoClient: `new CognitoIdentityProviderClient()` from
// @aws-sdk/client-cognito-identity-provider → aws-cognito (#3868).
func TestJSService_AWSCognitoClient(t *testing.T) {
	src := []byte(`import { CognitoIdentityProviderClient } from "@aws-sdk/client-cognito-identity-provider";

function signUp() {
  const client = new CognitoIdentityProviderClient({});
  return client;
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsSvcEdge(recs, "signUp", "aws-cognito") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(signUp -> aws-cognito)")
	}
	if id, _ := jsSvcNode(recs, "aws-cognito"); id == "" {
		t.Errorf("missing SCOPE.ExternalService:aws-cognito node")
	}
}

// TestJSService_AWSCognitoConverges: user-pool and identity-pool client
// constructors fold into ONE aws-cognito node.
func TestJSService_AWSCognitoConverges(t *testing.T) {
	src := []byte(`import { CognitoIdentityProviderClient } from "@aws-sdk/client-cognito-identity-provider";
import { CognitoIdentityClient } from "@aws-sdk/client-cognito-identity";

function userPool() {
  return new CognitoIdentityProviderClient({});
}
function identityPool() {
  return new CognitoIdentityClient({});
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsSvcEdge(recs, "userPool", "aws-cognito") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(userPool -> aws-cognito)")
	}
	if !jsSvcEdge(recs, "identityPool", "aws-cognito") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(identityPool -> aws-cognito)")
	}
	if _, count := jsSvcNode(recs, "aws-cognito"); count != 1 {
		t.Errorf("expected exactly 1 aws-cognito node, got %d", count)
	}
}

// TestJSService_SentryNamespaceInit: `import * as Sentry` + `Sentry.init()`.
func TestJSService_SentryNamespaceInit(t *testing.T) {
	src := []byte(`import * as Sentry from "@sentry/node";

function boot() {
  Sentry.init({ dsn: "x" });
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsSvcEdge(recs, "boot", "sentry") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(boot -> sentry)")
	}
}

// TestJSService_Convergence: two functions, one Stripe node.
func TestJSService_Convergence(t *testing.T) {
	src := []byte(`import Stripe from "stripe";

function chargeIt() {
  const s = new Stripe("k");
  return s.charges.create({});
}

function refundIt() {
  const s = new Stripe("k");
  return s.refunds.create({});
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsSvcEdge(recs, "chargeIt", "stripe") || !jsSvcEdge(recs, "refundIt", "stripe") {
		t.Fatalf("both functions must depend on stripe")
	}
	if _, n := jsSvcNode(recs, "stripe"); n != 1 {
		t.Errorf("expected exactly 1 stripe node, got %d", n)
	}
}

// TestJSService_NonSDKReceiver_NoEdge: `.create()` on a non-SDK local object
// must NOT emit an edge.
func TestJSService_NonSDKReceiver_NoEdge(t *testing.T) {
	src := []byte(`function handler(repo) {
  return repo.charges.create({ amount: 10 });
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindDependsOnService) {
				t.Fatalf("unexpected DEPENDS_ON_SERVICE on non-SDK receiver: %s -> %s",
					recs[i].Name, r.ToID)
			}
		}
	}
}

// TestTSService_Stripe: same flow under the TypeScript grammar.
func TestTSService_Stripe(t *testing.T) {
	src := []byte(`import Stripe from "stripe";

function pay(amount: number) {
  const stripe = new Stripe("sk");
  return stripe.charges.create({ amount });
}
`)
	recs := extract(t, src, "typescript", parseTS(t, src))
	if !jsSvcEdge(recs, "pay", "stripe") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(pay -> stripe) [typescript]")
	}
}
