package extractor

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestServiceForImportSource(t *testing.T) {
	cases := map[string]string{
		"stripe":             ServiceStripe,
		"twilio.rest":        ServiceTwilio,
		"@sendgrid/mail":     ServiceSendGrid,
		"openai":             ServiceOpenAI,
		"@slack/web-api":     ServiceSlack,
		"slack_sdk":          ServiceSlack,
		"@sentry/node":       ServiceSentry,
		"firebase-admin":     ServiceFirebase,
		"algoliasearch":      ServiceAlgolia,
		"boto3":              awsServicePrefix, // sentinel
		"@aws-sdk/client-s3": awsServicePrefix, // sentinel
		"express":            "",               // not an external service
		"./local/util":       "",
	}
	for in, want := range cases {
		if got := ServiceForImportSource(in); got != want {
			t.Errorf("ServiceForImportSource(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAWSServiceFromArg(t *testing.T) {
	cases := map[string]string{
		`"s3"`:     "aws-s3",
		"ses":      "aws-ses",
		"sesv2":    "aws-ses",
		"sns":      "aws-sns",
		"sqs":      "aws-sqs",
		"dynamodb": "aws-dynamodb",
		"events":   "aws-eventbridge",
		// Cognito: both boto3 hyphenated strings and aws-sdk v3 ctor-derived
		// CamelCase-collapsed tokens converge on the single aws-cognito service.
		"cognito-idp":             "aws-cognito",
		`"cognito-idp"`:           "aws-cognito",
		"cognito-identity":        "aws-cognito",
		"CognitoIdentityProvider": "aws-cognito", // aws-sdk v3 ctor token
		"CognitoIdentity":         "aws-cognito",
		"frobulate":               "", // unknown service → drop
	}
	for in, want := range cases {
		if got := AWSServiceFromArg(in); got != want {
			t.Errorf("AWSServiceFromArg(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestEmitServiceDependencyEdges_Converge: two calls to the same service from
// different functions produce ONE service node and one edge each.
func TestEmitServiceDependencyEdges_Converge(t *testing.T) {
	entities := []types.EntityRecord{
		{Name: "file.py", Kind: string(types.EntityKindOperation)},
		{Name: "charge", Kind: string(types.EntityKindFunction)},
		{Name: "refund", Kind: string(types.EntityKindFunction)},
	}
	calls := []ServiceCall{
		{Service: ServiceStripe, FromName: "charge", Operation: "Charge.create"},
		{Service: ServiceStripe, FromName: "refund", Operation: "Refund.create"},
	}
	n := EmitServiceDependencyEdges(&entities, "python", calls)
	if n != 2 {
		t.Fatalf("emitted %d edges, want 2", n)
	}
	// Exactly one stripe service entity appended.
	svcCount := 0
	for i := range entities {
		if entities[i].Kind == string(types.EntityKindExternalService) {
			svcCount++
			if entities[i].QualifiedName != ExternalServiceTargetID(ServiceStripe) {
				t.Errorf("service QualifiedName = %q, want %q",
					entities[i].QualifiedName, ExternalServiceTargetID(ServiceStripe))
			}
		}
	}
	if svcCount != 1 {
		t.Errorf("expected 1 service node, got %d", svcCount)
	}
	// Edge targets converge on the same ToID.
	want := ExternalServiceTargetID(ServiceStripe)
	for _, name := range []string{"charge", "refund"} {
		found := false
		for i := range entities {
			if entities[i].Name != name {
				continue
			}
			for _, r := range entities[i].Relationships {
				if r.Kind == string(types.RelationshipKindDependsOnService) && r.ToID == want {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("missing DEPENDS_ON_SERVICE(%s -> stripe)", name)
		}
	}
}

// TestEmitServiceDependencyEdges_DropsSentinelAndEmpty: the bare "aws-"
// sentinel and empty services never produce an edge.
func TestEmitServiceDependencyEdges_DropsSentinelAndEmpty(t *testing.T) {
	entities := []types.EntityRecord{
		{Name: "file.py", Kind: string(types.EntityKindOperation)},
		{Name: "f", Kind: string(types.EntityKindFunction)},
	}
	calls := []ServiceCall{
		{Service: awsServicePrefix, FromName: "f"},
		{Service: "", FromName: "f"},
	}
	if n := EmitServiceDependencyEdges(&entities, "python", calls); n != 0 {
		t.Fatalf("emitted %d edges, want 0 (sentinel + empty dropped)", n)
	}
}
