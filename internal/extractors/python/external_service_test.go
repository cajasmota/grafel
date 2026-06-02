package python_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// svcEdge reports whether the entity Named fromName has a DEPENDS_ON_SERVICE
// relationship whose ToID targets the given service.
func svcEdge(recs []types.EntityRecord, fromName, service string) bool {
	want := extractor.ExternalServiceTargetID(service)
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

// svcNodeID returns the synthetic service node ID, or "" if absent.
func svcNodeID(recs []types.EntityRecord, service string) string {
	want := extractor.ExternalServiceName(service)
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExternalService) && recs[i].Name == want {
			return recs[i].ID
		}
	}
	return ""
}

// svcOperation returns the `operation` property on the DEPENDS_ON_SERVICE edge
// from fromName to service, or "" if absent.
func svcOperation(recs []types.EntityRecord, fromName, service string) string {
	want := extractor.ExternalServiceTargetID(service)
	for i := range recs {
		if recs[i].Name != fromName {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindDependsOnService) && r.ToID == want {
				return r.Properties["operation"]
			}
		}
	}
	return ""
}

// TestPyService_Stripe asserts an explicit edge + node id, not just len>0.
func TestPyService_Stripe(t *testing.T) {
	src := `import stripe

def pay(amount):
    return stripe.Charge.create(amount=amount, currency="usd")
`
	recs := extractPy(t, src, "billing.py")

	if !svcEdge(recs, "pay", "stripe") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(pay -> stripe)")
	}
	if svcNodeID(recs, "stripe") == "" {
		t.Errorf("missing SCOPE.ExternalService:stripe node")
	}
	if op := svcOperation(recs, "pay", "stripe"); op != "Charge.create" {
		t.Errorf("operation = %q, want Charge.create", op)
	}
}

// TestPyService_AWSS3FromLiteral: the AWS service is resolved from the literal
// arg to boto3.client(...).
func TestPyService_AWSS3FromLiteral(t *testing.T) {
	src := `import boto3

def upload(key, body):
    s3 = boto3.client("s3")
    return s3.put_object(Key=key, Body=body)
`
	recs := extractPy(t, src, "store.py")

	if !svcEdge(recs, "upload", "aws-s3") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(upload -> aws-s3)")
	}
	if svcNodeID(recs, "aws-s3") == "" {
		t.Errorf("missing SCOPE.ExternalService:aws-s3 node")
	}
	// Must NOT mis-resolve to aws-generic when a literal is present.
	if svcEdge(recs, "upload", "aws-generic") {
		t.Errorf("unexpected aws-generic edge when literal 's3' present")
	}
}

// TestPyService_AWSDynamoResource: boto3.resource("dynamodb") → aws-dynamodb.
func TestPyService_AWSDynamoResource(t *testing.T) {
	src := `import boto3

def get_table():
    return boto3.resource("dynamodb")
`
	recs := extractPy(t, src, "db.py")
	if !svcEdge(recs, "get_table", "aws-dynamodb") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(get_table -> aws-dynamodb)")
	}
}

// TestPyService_AWSCognito: boto3.client("cognito-idp") → aws-cognito (#3868).
func TestPyService_AWSCognito(t *testing.T) {
	src := `import boto3

def sign_up(username, password):
    idp = boto3.client("cognito-idp")
    return idp.sign_up(Username=username, Password=password)
`
	recs := extractPy(t, src, "auth.py")

	if !svcEdge(recs, "sign_up", "aws-cognito") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(sign_up -> aws-cognito)")
	}
	if svcNodeID(recs, "aws-cognito") == "" {
		t.Errorf("missing SCOPE.ExternalService:aws-cognito node")
	}
	// Literal present → must NOT fall back to the generic node.
	if svcEdge(recs, "sign_up", "aws-generic") {
		t.Errorf("unexpected aws-generic edge when literal 'cognito-idp' present")
	}
}

// TestPyService_AWSCognitoIdentityConverges: the identity-pool token folds into
// the SAME aws-cognito node as the user-pool token — one convergence node.
func TestPyService_AWSCognitoIdentityConverges(t *testing.T) {
	src := `import boto3

def user_pool():
    return boto3.client("cognito-idp")

def identity_pool():
    return boto3.client("cognito-identity")
`
	recs := extractPy(t, src, "auth.py")

	if !svcEdge(recs, "user_pool", "aws-cognito") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(user_pool -> aws-cognito)")
	}
	if !svcEdge(recs, "identity_pool", "aws-cognito") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(identity_pool -> aws-cognito)")
	}
	// Exactly one aws-cognito node despite two distinct boto3 tokens.
	n := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExternalService) &&
			recs[i].QualifiedName == extractor.ExternalServiceTargetID("aws-cognito") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected exactly 1 aws-cognito node, got %d", n)
	}
}

// TestPyService_AWSCognitoDynamicNoFabrication: a dynamic service arg must NOT
// fabricate an aws-cognito node (honest-partial).
func TestPyService_AWSCognitoDynamicNoFabrication(t *testing.T) {
	src := `import boto3

def make(svc):
    return boto3.client(svc)
`
	recs := extractPy(t, src, "auth.py")
	if svcEdge(recs, "make", "aws-cognito") {
		t.Errorf("dynamic service arg must not fabricate an aws-cognito edge")
	}
}

// TestPyService_Convergence: two functions using Stripe converge on ONE node.
func TestPyService_Convergence(t *testing.T) {
	src := `import stripe

def charge(a):
    return stripe.Charge.create(amount=a)

def refund(c):
    return stripe.Refund.create(charge=c)
`
	recs := extractPy(t, src, "pay.py")

	if !svcEdge(recs, "charge", "stripe") || !svcEdge(recs, "refund", "stripe") {
		t.Fatalf("both functions must depend on stripe")
	}
	// Exactly one stripe node.
	n := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindExternalService) &&
			recs[i].Name == extractor.ExternalServiceName("stripe") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected exactly 1 stripe service node, got %d", n)
	}
}

// TestPyService_TwilioConstructor: `from twilio.rest import Client; Client(...)`
// resolves via the import source module.
func TestPyService_TwilioConstructor(t *testing.T) {
	src := `from twilio.rest import Client

def notify(sid, token):
    client = Client(sid, token)
    return client
`
	recs := extractPy(t, src, "sms.py")
	if !svcEdge(recs, "notify", "twilio") {
		t.Fatalf("missing DEPENDS_ON_SERVICE(notify -> twilio)")
	}
}

// TestPyService_DynamicAWSArg_Generic: a dynamic boto3 service string maps to
// aws-generic (honest-partial), never a concrete service.
func TestPyService_DynamicAWSArg_Generic(t *testing.T) {
	src := `import boto3

def make(svc):
    return boto3.client(svc)
`
	recs := extractPy(t, src, "dyn.py")
	if !svcEdge(recs, "make", "aws-generic") {
		t.Fatalf("dynamic boto3.client(svc) should map to aws-generic")
	}
	if svcEdge(recs, "make", "aws-s3") {
		t.Errorf("must not fabricate a concrete aws service from a variable arg")
	}
}

// TestPyService_NonSDKReceiver_NoEdge: a local `.create()` on a non-SDK object
// produces NO edge — the negative invariant.
func TestPyService_NonSDKReceiver_NoEdge(t *testing.T) {
	src := `def handler(repo):
    return repo.charges.create(amount=10)

class Charge:
    @staticmethod
    def create(**kw):
        return kw
`
	recs := extractPy(t, src, "local.py")
	for i := range recs {
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindDependsOnService) {
				t.Fatalf("unexpected DEPENDS_ON_SERVICE edge on non-SDK receiver: %s -> %s",
					recs[i].Name, r.ToID)
			}
		}
	}
}

// TestPyService_NoImport_NoEdge: a bare `stripe.Charge.create` with NO stripe
// import must not fabricate an edge (import-gated).
func TestPyService_NoImport_NoEdge(t *testing.T) {
	src := `def pay():
    return stripe.Charge.create(amount=10)
`
	recs := extractPy(t, src, "noimp.py")
	if svcEdge(recs, "pay", "stripe") {
		t.Fatalf("emitted stripe edge without a stripe import")
	}
}
