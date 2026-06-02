package extractor

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// external_service.go — shared, cross-language helpers for the third-party
// integration topology (epic #3628). It mirrors the config-key model in
// config_key.go and the exception-type model in exception_flow.go.
//
// The capability answers one question a rewrite (or an architecture review)
// needs: "what well-known third-party services does this codebase integrate
// with via their official SDKs, and where?" — e.g. Stripe, Twilio, SendGrid,
// AWS S3/SES/SNS/SQS/DynamoDB, OpenAI, Slack, Sentry, Firebase, Algolia.
//
// To make every call site of a service converge on ONE graph node, each
// language extractor emits:
//
//   - one SCOPE.ExternalService / subtype="external_service" entity per
//     distinct normalized service name, with a SYNTHETIC constant SourceFile
//     (ExternalServiceSourceFile) so EntityRecord.ComputeID (SourceFile+Kind+
//     Name) collapses the same service across files AND languages into a
//     single node. A Stripe charge created in billing.py and another in
//     webhooks.js therefore share one "service:stripe" node, and that node's
//     inbound-DEPENDS_ON_SERVICE set is the codebase's full Stripe footprint.
//
//   - one DEPENDS_ON_SERVICE edge (calling function → service node) carried
//     as a structural-ref ToID (ExternalServiceTargetID) that the resolver
//     binds via the byQualifiedName exact-match tier (the entity's
//     QualifiedName is set equal to that ToID).
//
// Precision-first / honest-partial: this is SDK-level (NAMED services), NOT
// raw HTTP-client path extraction (CONSUMES_API). An edge is emitted only when
// the SDK import/symbol context is recognised — never on a bare `.create()` /
// `.send()` on an unknown object. A dynamic boto3 service string
// (`boto3.client(svc_var)`) maps to the generic "aws-generic" service; an
// unrecognised SDK is skipped. The detection of these shapes lives in each
// language extractor; this file owns the SERVICE DICTIONARY plus the node/edge
// construction so the convergence invariant is identical everywhere.

// ExternalServiceSourceFile is the synthetic, constant SourceFile assigned to
// every external-service entity so identical service names converge to a
// single graph node under EntityRecord.ComputeID (SourceFile+Kind+Name).
const ExternalServiceSourceFile = "<external-service>"

// Canonical service names. Centralised so language extractors and tests refer
// to one source of truth; the AWS family keeps the `aws-<svc>` shape and
// AWSGeneric is the honest-partial fallback for a dynamic boto3 service arg.
const (
	ServiceStripe       = "stripe"
	ServiceTwilio       = "twilio"
	ServiceSendGrid     = "sendgrid"
	ServiceOpenAI       = "openai"
	ServiceSlack        = "slack"
	ServiceSentry       = "sentry"
	ServiceFirebase     = "firebase"
	ServiceAlgolia      = "algolia"
	ServiceAWSGeneric   = "aws-generic"
	ServiceAWSCognito   = "aws-cognito"
	awsServicePrefix    = "aws-"
	externalServiceName = "service:"
)

// ExternalServiceName returns the canonical entity Name for a service. The
// "service:" prefix namespaces the node so it never collides with a same-named
// code symbol, e.g. "service:stripe", "service:aws-s3".
func ExternalServiceName(service string) string {
	return externalServiceName + service
}

// ExternalServiceTargetID returns the structural-ref ToID for a
// DEPENDS_ON_SERVICE edge pointing at a service entity. Shape:
//
//	scope:externalservice:<service>
//
// This value is ALSO stored as the service entity's QualifiedName, so the
// resolver's byQualifiedName exact-match tier (internal/resolve/refs.go) binds
// the edge to that entity without any new linker code. Constant across
// languages so a Python `stripe.Charge.create()` and a JS `stripe.charges.
// create()` resolve to the same node.
func ExternalServiceTargetID(service string) string {
	return "scope:externalservice:" + service
}

// AWSServiceFromArg maps a literal AWS service string (the first arg to
// boto3.client/resource or an aws-sdk client name) to the canonical
// "aws-<svc>" service name, or "" if the token is not a recognised AWS
// service. Only the curated, common set is recognised — precision over recall.
//
//	"s3"        -> "aws-s3"
//	"ses"       -> "aws-ses"
//	"sns"       -> "aws-sns"
//	"sqs"       -> "aws-sqs"
//	"dynamodb"  -> "aws-dynamodb"
//
// Some services are addressed by more than one token across SDKs: boto3 uses
// hyphenated service strings ("cognito-idp", "cognito-identity") while the
// aws-sdk v3 client class derives a CamelCase-collapsed token
// ("cognitoidentityprovider", "cognitoidentity"). awsServiceAliases maps every
// such spelling to one canonical service so both call sites converge on a
// single node (e.g. all four cognito tokens -> "aws-cognito").
func AWSServiceFromArg(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	// Strip surrounding quotes left by a literal-string token.
	s = strings.Trim(s, "'\"`")
	if alias, ok := awsServiceAliases[s]; ok {
		return awsServicePrefix + alias
	}
	switch s {
	case "s3", "ses", "sns", "sqs", "dynamodb", "lambda", "kinesis",
		"secretsmanager", "ssm", "cloudwatch", "kms", "eventbridge":
		return awsServicePrefix + s
	}
	return ""
}

// awsServiceAliases maps non-canonical AWS service tokens — SDK-version and
// language variations — to their canonical "aws-<svc>" suffix. Centralised so a
// boto3 string ("cognito-idp") and an aws-sdk v3 client class
// ("CognitoIdentityProviderClient" -> "cognitoidentityprovider") resolve to the
// same node. Keep entries lowercase, unquoted; AWSServiceFromArg normalises the
// input the same way before lookup.
var awsServiceAliases = map[string]string{
	// SES v2 SDK is the same service as SES.
	"sesv2": "ses",
	// EventBridge's legacy SDK token is "events".
	"events": "eventbridge",
	// Cognito user pools — boto3 "cognito-idp" / aws-sdk v3
	// CognitoIdentityProviderClient.
	"cognito-idp":             "cognito",
	"cognitoidentityprovider": "cognito",
	// Cognito identity pools (federated identities) — boto3 "cognito-identity"
	// / aws-sdk v3 CognitoIdentityClient. Folded into the same aws-cognito node
	// since both are the AWS Cognito service from an integration-topology view.
	"cognito-identity": "cognito",
	"cognitoidentity":  "cognito",
}

// ServiceForImportSource maps an SDK import source module / package to its
// canonical service name, or "" if the import is not a recognised
// external-service SDK. This is the IMPORT side of the dictionary: it gates
// symbol-based detection so a bare local `stripe` variable that was never
// imported from the `stripe` package never fabricates an edge.
//
// Matching is by the leading dotted/slashed segment so submodules resolve too
// (`twilio.rest` -> twilio, `@slack/web-api` -> slack, `firebase-admin/
// firestore` -> firebase).
func ServiceForImportSource(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	s = strings.Trim(s, "'\"`")
	if s == "" {
		return ""
	}
	// Normalise separators and take the first meaningful segment(s).
	s = strings.ReplaceAll(s, "\\", "/")
	// Scoped npm package: keep "@scope/pkg" as the lookup key, else first seg.
	var head string
	switch {
	case strings.HasPrefix(s, "@"):
		parts := strings.SplitN(s, "/", 3)
		if len(parts) >= 2 {
			head = parts[0] + "/" + parts[1]
		} else {
			head = s
		}
	default:
		// take first path/dotted segment
		seg := s
		if i := strings.IndexAny(seg, "/."); i >= 0 {
			seg = seg[:i]
		}
		head = seg
	}
	// AWS SDK v3 ships one package per service under the @aws-sdk scope
	// (@aws-sdk/client-s3, @aws-sdk/client-ses, …). Recognise the scope and let
	// the caller derive the concrete service from the client constructor name.
	if strings.HasPrefix(head, "@aws-sdk/") || head == "@aws-sdk" {
		return awsServicePrefix
	}
	switch head {
	case "stripe":
		return ServiceStripe
	case "twilio":
		return ServiceTwilio
	case "sendgrid", "@sendgrid/mail", "@sendgrid/client":
		return ServiceSendGrid
	case "openai":
		return ServiceOpenAI
	case "slack_sdk", "slackclient", "@slack/web-api", "@slack/bolt":
		return ServiceSlack
	case "sentry_sdk", "@sentry/node", "@sentry/browser", "@sentry/react", "sentry":
		return ServiceSentry
	case "firebase_admin", "firebase-admin", "firebase", "@firebase/app":
		return ServiceFirebase
	case "algoliasearch":
		return ServiceAlgolia
	case "boto3", "botocore", "@aws-sdk", "aws-sdk":
		// AWS is recognised at import time but the concrete service requires
		// the client/service-name argument; callers use AWSServiceFromArg.
		return awsServicePrefix // sentinel "aws-" meaning "AWS, service TBD"
	}
	return ""
}

// IsAWSImportSentinel reports whether ServiceForImportSource returned the bare
// "aws-" sentinel — meaning the import is an AWS SDK but the concrete service
// must still be resolved from the client/service-name argument.
func IsAWSImportSentinel(service string) bool {
	return service == awsServicePrefix
}

// ExternalServiceEntity builds the SCOPE.ExternalService / external_service
// entity for a single normalized service name. The entity is deliberately
// file-agnostic (synthetic SourceFile) so it is the shared integration
// convergence node, and its QualifiedName equals the edge ToID so
// DEPENDS_ON_SERVICE edges resolve via byQualifiedName.
func ExternalServiceEntity(service, lang string) types.EntityRecord {
	e := types.EntityRecord{
		Name:          ExternalServiceName(service),
		QualifiedName: ExternalServiceTargetID(service),
		Kind:          string(types.EntityKindExternalService),
		Subtype:       "external_service",
		Language:      lang,
		SourceFile:    ExternalServiceSourceFile,
		StartLine:     1,
		EndLine:       1,
		Signature:     ExternalServiceName(service),
		Properties: map[string]string{
			"service": service,
		},
	}
	if vendor := serviceVendor(service); vendor != "" {
		e.Properties["vendor"] = vendor
	}
	e.ID = e.ComputeID()
	return e
}

// serviceVendor returns the umbrella vendor for a service ("aws" for the
// aws-* family) or "" when the service IS the vendor.
func serviceVendor(service string) string {
	if strings.HasPrefix(service, awsServicePrefix) {
		return "aws"
	}
	return ""
}

// ServiceCall is one resolved external-service SDK call detected by a language
// extractor: the canonical service name, the Name of the enclosing function/
// method, and the optional operation token (e.g. "charges.create",
// "put_object") captured for the edge property.
type ServiceCall struct {
	Service   string // canonical service name (stripe, aws-s3, ...)
	FromName  string // enclosing function/method Name; "" => file entity
	Operation string // optional SDK operation, e.g. "charges.create"
}

// EmitServiceDependencyEdges appends, to *entities, the external-service
// entities and DEPENDS_ON_SERVICE edges for the given detections.
//
// entities[0] MUST be the file entity (every language extractor appends it
// first). Edges whose FromName is "" — or whose FromName has no matching host
// entity — attach to the file entity (index 0) as a conservative fallback so
// the edge is never silently dropped. Identical service names converge to one
// service entity (deduped by name) and one edge per (FromName, service) tuple
// (the first operation seen for that tuple wins).
//
// Returns the number of DEPENDS_ON_SERVICE edges emitted. Safe with nil/empty
// input. Detections whose Service is empty are skipped — precision over recall.
func EmitServiceDependencyEdges(entities *[]types.EntityRecord, lang string, calls []ServiceCall) int {
	if entities == nil || len(*entities) == 0 || len(calls) == 0 {
		return 0
	}

	hostByName := map[string]int{}
	for i := range *entities {
		hostByName[(*entities)[i].Name] = i
	}

	seenEdge := map[string]bool{}
	seenSvc := map[string]bool{}
	var newEntities []types.EntityRecord
	emitted := 0

	for _, c := range calls {
		service := strings.TrimSpace(c.Service)
		if service == "" || service == awsServicePrefix {
			continue // unresolved / sentinel — drop
		}

		hostIdx := 0 // file entity by default
		if c.FromName != "" {
			if idx, ok := hostByName[c.FromName]; ok {
				hostIdx = idx
			}
		}

		edgeKey := c.FromName + "\x00" + service
		if !seenEdge[edgeKey] {
			seenEdge[edgeKey] = true
			props := map[string]string{"service": service}
			if c.Operation != "" {
				props["operation"] = c.Operation
			}
			(*entities)[hostIdx].Relationships = append((*entities)[hostIdx].Relationships,
				types.RelationshipRecord{
					ToID:       ExternalServiceTargetID(service),
					Kind:       string(types.RelationshipKindDependsOnService),
					Properties: props,
				})
			emitted++
		}

		if !seenSvc[service] {
			seenSvc[service] = true
			newEntities = append(newEntities, ExternalServiceEntity(service, lang))
		}
	}

	*entities = append(*entities, newEntities...)
	return emitted
}
