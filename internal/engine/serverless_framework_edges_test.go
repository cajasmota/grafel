// Tests for the Serverless Framework (serverless.yml) topology pass — #3519.
//
// Value-asserting: a serverless.yml with a function `hello` (handler
// `src/handler.hello`) + an `http GET /users` event + an `sqs` event must yield
//   - the SCOPE.ServerlessFunction entity (aws-lambda:hello),
//   - the GET /users http_endpoint_definition,
//   - the sqs queue TRIGGERS edge,
//   - the HANDLES edge to the handler symbol,
//
// plus provider runtime/region metadata, schedule + sns coverage, the
// resolveServerlessYMLName join, and detection / no-op guards.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
	"github.com/cajasmota/archigraph/internal/types"
)

func runSLSFrameworkDetect(t *testing.T, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applyServerlessFrameworkEdges(DetectorPassArgs{Lang: "yaml", Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

func slsEntityByKindName(ents []types.EntityRecord, kind, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func slsHasEdge(rels []types.RelationshipRecord, kind, fromID, toID string) bool {
	for _, r := range rels {
		if r.Kind == kind && r.FromID == fromID && r.ToID == toID {
			return true
		}
	}
	return false
}

// TestServerlessFramework_FullManifest is the core value-asserting test.
func TestServerlessFramework_FullManifest(t *testing.T) {
	src := `service: orders-api
provider:
  name: aws
  runtime: nodejs18.x
  region: us-east-1
functions:
  hello:
    handler: src/handler.hello
    events:
      - http:
          method: GET
          path: /users
      - sqs:
          arn: arn:aws:sqs:us-east-1:123456789012:order-events
`
	ents, rels := runSLSFrameworkDetect(t, "serverless.yml", src)

	// 1. ServerlessFunction entity, keyed aws-lambda:hello, with provider meta.
	fn := slsEntityByKindName(ents, serverlessFunctionKind, lambdaFunctionID("hello"))
	if fn == nil {
		t.Fatalf("expected ServerlessFunction entity %q; ents=%v", lambdaFunctionID("hello"), ents)
	}
	if fn.Properties["runtime"] != "nodejs18.x" {
		t.Errorf("expected runtime nodejs18.x, got %q", fn.Properties["runtime"])
	}
	if fn.Properties["region"] != "us-east-1" {
		t.Errorf("expected region us-east-1, got %q", fn.Properties["region"])
	}
	if fn.Properties["handler"] != "src/handler.hello" {
		t.Errorf("expected handler src/handler.hello, got %q", fn.Properties["handler"])
	}
	fnRef := serverlessFunctionKind + ":" + lambdaFunctionID("hello")

	// 2. GET /users endpoint entity.
	epID := httproutes.SyntheticID("GET", "/users")
	ep := slsEntityByKindName(ents, httpEndpointDefinitionKind, epID)
	if ep == nil {
		t.Fatalf("expected http_endpoint_definition %q; ents=%v", epID, ents)
	}
	if ep.Properties["verb"] != "GET" || ep.Properties["path"] != "/users" {
		t.Errorf("endpoint verb/path wrong: %v", ep.Properties)
	}
	// SERVES: function → endpoint.
	if !slsHasEdge(rels, slsServesEdgeKind, fnRef, httpEndpointDefinitionKind+":"+epID) {
		t.Errorf("expected SERVES edge %s -> %s; rels=%v", fnRef, epID, rels)
	}

	// 3. sqs queue entity + TRIGGERS edge queue → function.
	qID := sqsQueueID("arn:aws:sqs:us-east-1:123456789012:order-events")
	if slsEntityByKindName(ents, queueEntityKind, qID) == nil {
		t.Fatalf("expected sqs queue entity %q; ents=%v", qID, ents)
	}
	if !slsHasEdge(rels, slsTriggersEdgeKind, queueEntityKind+":"+qID, fnRef) {
		t.Errorf("expected sqs TRIGGERS edge %s -> %s; rels=%v", qID, fnRef, rels)
	}

	// 4. HANDLES edge: handler symbol → function.
	if !slsHasEdge(rels, serverlessHandlesEdgeKind, "SCOPE.Function:hello", fnRef) {
		t.Errorf("expected HANDLES edge SCOPE.Function:hello -> %s; rels=%v", fnRef, rels)
	}
}

// TestServerlessFramework_HttpShortForm covers the inline `http: GET /users` form.
func TestServerlessFramework_HttpShortForm(t *testing.T) {
	src := `service: svc
provider:
  name: aws
  runtime: python3.11
functions:
  list:
    handler: app.list
    events:
      - http: GET /items
`
	ents, rels := runSLSFrameworkDetect(t, "serverless.yml", src)
	epID := httproutes.SyntheticID("GET", "/items")
	if slsEntityByKindName(ents, httpEndpointDefinitionKind, epID) == nil {
		t.Fatalf("expected endpoint %q from short form; ents=%v", epID, ents)
	}
	fnRef := serverlessFunctionKind + ":" + lambdaFunctionID("list")
	if !slsHasEdge(rels, slsServesEdgeKind, fnRef, httpEndpointDefinitionKind+":"+epID) {
		t.Errorf("expected SERVES edge for short-form http; rels=%v", rels)
	}
}

// TestServerlessFramework_ScheduleAndSNS covers schedule + sns events.
func TestServerlessFramework_ScheduleAndSNS(t *testing.T) {
	src := `service: svc
provider:
  name: aws
  runtime: go1.x
functions:
  cron:
    handler: bin/cron
    events:
      - schedule: rate(5 minutes)
  notify:
    handler: src/notify.handler
    events:
      - sns:
          arn: arn:aws:sns:us-east-1:123456789012:user-signups
`
	ents, rels := runSLSFrameworkDetect(t, "serverless.yml", src)

	// schedule → ScheduledJob + TRIGGERS → cron function.
	cronRef := serverlessFunctionKind + ":" + lambdaFunctionID("cron")
	jobID := "serverless-framework:cron:schedule"
	job := slsEntityByKindName(ents, scheduledJobKind, jobID)
	if job == nil {
		t.Fatalf("expected ScheduledJob %q; ents=%v", jobID, ents)
	}
	if job.Properties["schedule"] != "rate(5 minutes)" {
		t.Errorf("expected schedule expr, got %q", job.Properties["schedule"])
	}
	if !slsHasEdge(rels, slsTriggersEdgeKind, scheduledJobKind+":"+jobID, cronRef) {
		t.Errorf("expected schedule TRIGGERS edge; rels=%v", rels)
	}

	// sns → MessageTopic + TRIGGERS → notify function.
	notifyRef := serverlessFunctionKind + ":" + lambdaFunctionID("notify")
	tID := snsTopicID("user-signups")
	if slsEntityByKindName(ents, messageTopicKind, tID) == nil {
		t.Fatalf("expected sns topic %q; ents=%v", tID, ents)
	}
	if !slsHasEdge(rels, slsTriggersEdgeKind, messageTopicKind+":"+tID, notifyRef) {
		t.Errorf("expected sns TRIGGERS edge; rels=%v", rels)
	}
}

// TestServerlessFramework_ResolveYMLName verifies the resolveServerlessYMLName
// stub is wired: after a manifest is parsed, the handler symbol resolves to the
// logical function name.
func TestServerlessFramework_ResolveYMLName(t *testing.T) {
	// Clean slate for the package-level index.
	delete(serverlessYMLHandlerIndex, "doWork")
	src := `service: svc
provider:
  name: aws
  runtime: nodejs18.x
functions:
  processOrder:
    handler: src/orders.doWork
`
	runSLSFrameworkDetect(t, "serverless.yml", src)
	if got := resolveServerlessYMLName("src/orders.js", "doWork"); got != "processOrder" {
		t.Errorf("expected resolveServerlessYMLName to map doWork -> processOrder, got %q", got)
	}
	// Unknown symbol falls back to itself.
	if got := resolveServerlessYMLName("x.js", "unknownSym"); got != "unknownSym" {
		t.Errorf("expected fallback to symbol, got %q", got)
	}
}

// TestServerlessFramework_Detection covers the content-sniff path (no
// serverless.yml basename) and the no-op guards.
func TestServerlessFramework_Detection(t *testing.T) {
	// Content-sniff: signature keys present though the file is named oddly.
	src := `service: svc
provider:
  name: aws
  runtime: nodejs18.x
functions:
  h:
    handler: a.b
    events:
      - http: GET /ping
`
	ents, _ := runSLSFrameworkDetect(t, "config/sls.yml", src)
	if slsEntityByKindName(ents, serverlessFunctionKind, lambdaFunctionID("h")) == nil {
		t.Errorf("expected content-sniff detection to fire; ents=%v", ents)
	}

	// No-op: a non-serverless YAML (docker-compose-ish) yields nothing.
	other := "version: '3'\nservices:\n  web:\n    image: nginx\n"
	e2, r2 := runSLSFrameworkDetect(t, "docker-compose.yml", other)
	if len(e2) != 0 || len(r2) != 0 {
		t.Errorf("expected no entities/edges for non-serverless yaml, got %d ents %d rels", len(e2), len(r2))
	}

	// No-op: non-yaml language is skipped even with serverless content.
	res := applyServerlessFrameworkEdges(DetectorPassArgs{Lang: "python", Path: "serverless.yml", Content: []byte(src)})
	if len(res.Entities) != 0 {
		t.Errorf("expected non-yaml language to be skipped, got %d ents", len(res.Entities))
	}
}
