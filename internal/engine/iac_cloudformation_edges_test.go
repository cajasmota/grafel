package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// cfnToDoc converts the (entities, relationships) a CFN pass produced into a
// graph.Document whose entity IDs equal the `<Kind>:<Name>` structural-ref
// form the pass uses for edge endpoints. This mirrors the post-resolution
// state (the resolver rewrites structural-ref edges to entity IDs) closely
// enough to exercise ApplyAsyncTriggerEdges, which matches SUBSCRIBES_TO
// targets against topic/queue entity IDs.
func cfnToDoc(ents []types.EntityRecord, rels []types.RelationshipRecord) *graph.Document {
	doc := &graph.Document{Repo: "fixture-cfn"}
	for _, e := range ents {
		doc.Entities = append(doc.Entities, graph.Entity{
			ID:         string(e.Kind) + ":" + e.Name,
			Name:       e.Name,
			Kind:       string(e.Kind),
			SourceFile: e.SourceFile,
			Language:   e.Language,
		}.WithProperties(e.Properties))
	}
	for _, r := range rels {
		doc.Relationships = append(doc.Relationships, graph.Relationship{
			ID:     graph.RelationshipID(r.FromID, r.ToID, r.Kind),
			FromID: r.FromID,
			ToID:   r.ToID,
			Kind:   r.Kind,
		}.WithProperties(r.Properties))
	}
	return doc
}

// cfnFindEntity returns the entity whose Name (canonical ID) ends with the
// given logical-id suffix, or nil.
func cfnFindEntityByLogical(ents []types.EntityRecord, logical string) *types.EntityRecord {
	suffix := "#" + logical
	for i := range ents {
		if strings.HasSuffix(ents[i].Name, suffix) {
			return &ents[i]
		}
	}
	return nil
}

// cfnEdgeEndpointResolves reports whether an edge endpoint string of the form
// `<Kind>:<entityName>` names a REAL emitted entity — i.e. some entity in
// `ents` has Kind==<Kind> AND Name==<entityName>. This catches dangling /
// mis-kinded edges: an endpoint whose Kind-prefix points at a Kind no entity
// was ever emitted with (e.g. "SCOPE.Config:cfn:x#P" when the Parameter was
// emitted as "SCOPE.Schema"). Endpoints emitted by cfnResourceKindFromID use
// exactly one `<Kind>:` prefix, but an entity Name itself can contain colons
// (e.g. "cfn:path#Logical"), so we match against the concrete entity strings
// rather than splitting on the first colon.
func cfnEdgeEndpointResolves(ents []types.EntityRecord, endpoint string) bool {
	for i := range ents {
		if endpoint == string(ents[i].Kind)+":"+ents[i].Name {
			return true
		}
	}
	return false
}

// cfnHasEdge reports whether an edge of `kind` exists whose FromID ends with
// `#fromLogical` and ToID ends with `#toLogical` AND both endpoints resolve to
// a real emitted entity (correct Kind-prefix, not a dangling reference). The
// endpoint resolution is the strengthened check: matching the ID suffix alone
// let a mis-kinded/dangling edge pass silently.
func cfnHasEdge(ents []types.EntityRecord, rels []types.RelationshipRecord, kind, fromLogical, toLogical string) bool {
	for _, r := range rels {
		if r.Kind != kind {
			continue
		}
		if !strings.HasSuffix(r.FromID, "#"+fromLogical) || !strings.HasSuffix(r.ToID, "#"+toLogical) {
			continue
		}
		if !cfnEdgeEndpointResolves(ents, r.FromID) {
			continue
		}
		if !cfnEdgeEndpointResolves(ents, r.ToID) {
			continue
		}
		return true
	}
	return false
}

func cfnRun(lang, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	res := applyCloudFormationEdges(DetectorPassArgs{Lang: lang, Path: path, Content: []byte(src)})
	return res.Entities, res.Relationships
}

// TestCFN_ShortForms asserts resource entities AND dependency edges using the
// `!Ref` / `!GetAtt` short forms plus DependsOn.
func TestCFN_ShortForms(t *testing.T) {
	src := `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  DataBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: my-data-bucket
  ProcessorFn:
    Type: AWS::Lambda::Function
    DependsOn: DataBucket
    Properties:
      FunctionName: processor
      Environment:
        Variables:
          BUCKET: !Ref DataBucket
          BUCKET_ARN: !GetAtt DataBucket.Arn
`
	ents, rels := cfnRun("yaml", "infra/template.yaml", src)

	bucket := cfnFindEntityByLogical(ents, "DataBucket")
	if bucket == nil {
		t.Fatalf("missing DataBucket entity; got %+v", ents)
	}
	if bucket.Kind != "SCOPE.Datastore" {
		t.Errorf("DataBucket: want Kind SCOPE.Datastore, got %s", bucket.Kind)
	}
	if bucket.Properties["resource_type"] != "AWS::S3::Bucket" {
		t.Errorf("DataBucket: want resource_type AWS::S3::Bucket, got %q", bucket.Properties["resource_type"])
	}

	fn := cfnFindEntityByLogical(ents, "ProcessorFn")
	if fn == nil {
		t.Fatalf("missing ProcessorFn entity")
	}
	if fn.Properties["resource_type"] != "AWS::Lambda::Function" {
		t.Errorf("ProcessorFn: wrong resource_type %q", fn.Properties["resource_type"])
	}

	// !GetAtt → USES wins over the Ref/DependsOn DEPENDS_ON to the same target.
	if !cfnHasEdge(ents, rels, "USES", "ProcessorFn", "DataBucket") {
		t.Errorf("missing USES (GetAtt) edge ProcessorFn->DataBucket; rels=%+v", rels)
	}
}

// TestCFN_LongForms asserts the same structure via JSON `{ "Ref": ... }` and
// `{ "Fn::GetAtt": [...] }` long forms.
func TestCFN_LongForms(t *testing.T) {
	src := `{
  "AWSTemplateFormatVersion": "2010-09-09",
  "Resources": {
    "DataBucket": {
      "Type": "AWS::S3::Bucket",
      "Properties": { "BucketName": "my-data-bucket" }
    },
    "ProcessorFn": {
      "Type": "AWS::Lambda::Function",
      "DependsOn": ["DataBucket"],
      "Properties": {
        "FunctionName": "processor",
        "Environment": {
          "Variables": {
            "BUCKET": { "Ref": "DataBucket" },
            "BUCKET_ARN": { "Fn::GetAtt": ["DataBucket", "Arn"] }
          }
        }
      }
    }
  }
}`
	ents, rels := cfnRun("json", "infra/template.json", src)

	if cfnFindEntityByLogical(ents, "DataBucket") == nil {
		t.Fatalf("JSON: missing DataBucket entity; got %d ents", len(ents))
	}
	if cfnFindEntityByLogical(ents, "ProcessorFn") == nil {
		t.Fatalf("JSON: missing ProcessorFn entity")
	}
	if !cfnHasEdge(ents, rels, "USES", "ProcessorFn", "DataBucket") {
		t.Errorf("JSON: missing USES (Fn::GetAtt) edge ProcessorFn->DataBucket; rels=%+v", rels)
	}
}

// TestCFN_RefAndDependsOnOnly asserts DEPENDS_ON is emitted when only Ref /
// DependsOn (no GetAtt) point at a target — and that a Ref to a Parameter
// resolves.
func TestCFN_RefAndDependsOnAndParam(t *testing.T) {
	src := `AWSTemplateFormatVersion: "2010-09-09"
Parameters:
  TableName:
    Type: String
Resources:
  Events:
    Type: AWS::DynamoDB::Table
    Properties:
      TableName: !Ref TableName
  Worker:
    Type: AWS::ECS::Service
    DependsOn:
      - Events
    Properties:
      Cluster: !Sub "${Events}-cluster"
`
	ents, rels := cfnRun("yaml", "infra/app.yaml", src)

	// Parameter entity exists.
	if p := cfnFindEntityByLogical(ents, "TableName"); p == nil {
		t.Fatalf("missing TableName parameter entity")
	} else if p.Subtype != "cfn_parameter" {
		t.Errorf("TableName: want subtype cfn_parameter, got %s", p.Subtype)
	}

	// Events Table → DEPENDS_ON Parameter TableName (Ref to a Parameter).
	if !cfnHasEdge(ents, rels, "DEPENDS_ON", "Events", "TableName") {
		t.Errorf("missing DEPENDS_ON Events->TableName (Ref to Parameter); rels=%+v", rels)
	}
	// Worker → DEPENDS_ON Events (DependsOn list form AND !Sub).
	if !cfnHasEdge(ents, rels, "DEPENDS_ON", "Worker", "Events") {
		t.Errorf("missing DEPENDS_ON Worker->Events; rels=%+v", rels)
	}
}

// TestCFN_CrossStack asserts Fn::ImportValue and Outputs.Export collapse onto a
// shared cfn-export node.
func TestCFN_CrossStack(t *testing.T) {
	producer := `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  Vpc:
    Type: AWS::EC2::VPC
Outputs:
  VpcId:
    Value: !Ref Vpc
    Export:
      Name: shared-vpc-id
`
	consumer := `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  Subnet:
    Type: AWS::EC2::Subnet
    Properties:
      VpcId: !ImportValue shared-vpc-id
`
	pents, _ := cfnRun("yaml", "stacks/network.yaml", producer)
	cents, crels := cfnRun("yaml", "stacks/app.yaml", consumer)

	exportID := "cfn-export:shared-vpc-id"
	foundProducer := false
	for _, e := range pents {
		if e.Name == exportID {
			foundProducer = true
		}
	}
	if !foundProducer {
		t.Errorf("producer: missing export node %s", exportID)
	}
	foundConsumer := false
	for _, e := range cents {
		if e.Name == exportID {
			foundConsumer = true
		}
	}
	if !foundConsumer {
		t.Errorf("consumer: missing export node %s", exportID)
	}
	// Consumer Subnet DEPENDS_ON the export node (cross-stack edge).
	found := false
	for _, r := range crels {
		if r.Kind == "DEPENDS_ON" && strings.HasSuffix(r.FromID, "#Subnet") &&
			strings.HasSuffix(r.ToID, exportID) {
			found = true
		}
	}
	if !found {
		t.Errorf("consumer: missing cross-stack DEPENDS_ON Subnet->%s; rels=%+v", exportID, crels)
	}
}

// TestCFN_SAM asserts SAM function modeling: Api event → endpoint + ROUTES_TO,
// SQS event → SUBSCRIBES_TO, Schedule → TRIGGERS, plus the aws-lambda join.
func TestCFN_SAM(t *testing.T) {
	src := `AWSTemplateFormatVersion: "2010-09-09"
Transform: AWS::Serverless-2016-10-31
Resources:
  JobQueue:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: jobs
  ApiFn:
    Type: AWS::Serverless::Function
    Properties:
      FunctionName: api-handler
      Events:
        GetItem:
          Type: Api
          Properties:
            Path: /items
            Method: get
        Worker:
          Type: SQS
          Properties:
            Queue: !GetAtt JobQueue.Arn
        Nightly:
          Type: Schedule
          Properties:
            Schedule: rate(1 day)
`
	ents, rels := cfnRun("yaml", "sam/template.yaml", src)

	// Function joins aws-lambda synthetic.
	foundLambda := false
	for _, e := range ents {
		if e.Name == "aws-lambda:api-handler" && e.Kind == "SCOPE.ServerlessFunction" {
			foundLambda = true
		}
	}
	if !foundLambda {
		t.Errorf("SAM: missing aws-lambda:api-handler join entity")
	}

	// Api event → endpoint + ROUTES_TO.
	foundEndpoint := false
	for _, e := range ents {
		if e.Subtype == "sam_api" && strings.Contains(e.Name, "/items") {
			foundEndpoint = true
		}
	}
	if !foundEndpoint {
		t.Errorf("SAM: missing sam_api endpoint for GET /items; ents=%+v", ents)
	}

	// SQS event → SUBSCRIBES_TO JobQueue.
	foundSub := false
	for _, r := range rels {
		if r.Kind == "SUBSCRIBES_TO" && strings.HasSuffix(r.FromID, "#ApiFn") &&
			strings.HasSuffix(r.ToID, "#JobQueue") {
			foundSub = true
		}
	}
	if !foundSub {
		t.Errorf("SAM: missing SUBSCRIBES_TO ApiFn->JobQueue; rels=%+v", rels)
	}

	// Schedule → TRIGGERS.
	foundTrigger := false
	for _, r := range rels {
		if r.Kind == "TRIGGERS" && strings.HasSuffix(r.ToID, "#ApiFn") {
			foundTrigger = true
		}
	}
	if !foundTrigger {
		t.Errorf("SAM: missing TRIGGERS schedule->ApiFn; rels=%+v", rels)
	}
}

// TestCFN_NotATemplate asserts non-CFN YAML/JSON is untouched (no entities).
func TestCFN_NotATemplate(t *testing.T) {
	src := `name: my-app
version: 1.0.0
services:
  web:
    image: nginx
`
	ents, rels := cfnRun("yaml", "docker-compose.yaml", src)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("non-CFN YAML should yield nothing, got %d ents %d rels", len(ents), len(rels))
	}
}

// TestCFN_NestedStack asserts AWS::CloudFormation::Stack → IMPORTS.
func TestCFN_NestedStack(t *testing.T) {
	src := `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  ChildStack:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: https://s3.amazonaws.com/bucket/child.yaml
`
	_, rels := cfnRun("yaml", "infra/parent.yaml", src)
	found := false
	for _, r := range rels {
		if r.Kind == "IMPORTS" && strings.HasSuffix(r.FromID, "#ChildStack") &&
			strings.Contains(r.ToID, "child.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing IMPORTS nested-stack edge; rels=%+v", rels)
	}
}

// TestCFN_NestedStack_StackAppTopology is the value-asserting test backing the
// iac_stack_app_topology (#4200) capability credit (partial) for CloudFormation.
// It asserts BOTH halves of the nested-stack composition topology:
//   - the topology ENTITY: the `AWS::CloudFormation::Stack` resource surfaces as
//     an entity (Name cfn:<path>#ChildStack), and
//   - the parent→child CONTAINMENT relationship: an IMPORTS edge from the parent
//     stack resource to the child template carrying nested_stack=true (which is
//     what distinguishes a nested-stack containment from a generic dependency).
func TestCFN_NestedStack_StackAppTopology(t *testing.T) {
	src := `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  ChildStack:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: https://s3.amazonaws.com/bucket/child.yaml
`
	ents, rels := cfnRun("yaml", "infra/parent.yaml", src)

	// (1) Nested-stack topology entity.
	if cfnFindEntityByLogical(ents, "ChildStack") == nil {
		t.Fatalf("expected AWS::CloudFormation::Stack topology entity ChildStack, got %+v", ents)
	}

	// (2) Parent→child containment edge tagged nested_stack=true.
	var foundContainment bool
	for _, r := range rels {
		if r.Kind == "IMPORTS" &&
			strings.HasSuffix(r.FromID, "#ChildStack") &&
			strings.Contains(r.ToID, "child.yaml") &&
			r.Properties["nested_stack"] == "true" {
			foundContainment = true
		}
	}
	if !foundContainment {
		t.Errorf("expected nested-stack IMPORTS containment edge (nested_stack=true) parent→child, got %+v", rels)
	}
}

// TestCFN_StandaloneEventSourceMapping — #5801 Path B. SAM's inline
// `Events:` block on AWS::Serverless::Function already produces a
// handler-->queue SUBSCRIBES_TO trigger edge (TestCFN_SAM above). A
// standalone AWS::Lambda::EventSourceMapping resource — decoupled from any
// AWS::Serverless::Function, wiring a plain AWS::Lambda::Function to its
// event source via EventSourceArn/FunctionName intrinsics — previously
// produced only generic USES/DEPENDS_ON edges into the mapping resource
// itself, with NO source-->handler trigger edge at all. This asserts the fix
// resolves the same-template `!GetAtt OrdersStream.Arn` / `!Ref
// StreamHandlerFn` intrinsics and emits the handler-->source SUBSCRIBES_TO
// edge in the SAME shape SAM's inline path uses, so ApplyAsyncTriggerEdges
// (#5686) inverts BOTH paths into an identical source-->handler DELIVERS_TO.
func TestCFN_StandaloneEventSourceMapping(t *testing.T) {
	src := `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  StreamHandlerFn:
    Type: AWS::Lambda::Function
    Properties: { Handler: h.handler, Runtime: nodejs20.x }
  OrdersStream:
    Type: AWS::Kinesis::Stream
    Properties: { ShardCount: 1 }
  Esm:
    Type: AWS::Lambda::EventSourceMapping
    Properties:
      EventSourceArn: !GetAtt OrdersStream.Arn
      FunctionName: !Ref StreamHandlerFn
      StartingPosition: LATEST
`
	ents, rels := cfnRun("yaml", "esm/template.yaml", src)

	if cfnFindEntityByLogical(ents, "OrdersStream") == nil {
		t.Fatalf("expected OrdersStream entity, got %+v", ents)
	}
	if cfnFindEntityByLogical(ents, "StreamHandlerFn") == nil {
		t.Fatalf("expected StreamHandlerFn entity, got %+v", ents)
	}

	if !cfnHasEdge(ents, rels, "SUBSCRIBES_TO", "StreamHandlerFn", "OrdersStream") {
		t.Errorf("ESM: missing SUBSCRIBES_TO StreamHandlerFn->OrdersStream (source-->handler trigger); rels=%+v", rels)
	}
}

// TestCFN_StandaloneEventSourceMapping_UnresolvedRefsSkipped asserts the
// conservative same-template-only contract: when EventSourceArn or
// FunctionName reference a logical id NOT declared in this template (e.g. a
// cross-stack import), no SUBSCRIBES_TO edge is fabricated.
func TestCFN_StandaloneEventSourceMapping_UnresolvedRefsSkipped(t *testing.T) {
	src := `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  StreamHandlerFn:
    Type: AWS::Lambda::Function
    Properties: { Handler: h.handler, Runtime: nodejs20.x }
  Esm:
    Type: AWS::Lambda::EventSourceMapping
    Properties:
      EventSourceArn: !GetAtt ExternalStream.Arn
      FunctionName: !Ref StreamHandlerFn
      StartingPosition: LATEST
`
	_, rels := cfnRun("yaml", "esm/unresolved.yaml", src)
	for _, r := range rels {
		if r.Kind == "SUBSCRIBES_TO" && strings.HasSuffix(r.FromID, "#StreamHandlerFn") {
			t.Errorf("expected no SUBSCRIBES_TO edge for unresolved cross-template EventSourceArn, got %+v", r)
		}
	}
}

// TestCFN_StandaloneEventSourceMapping_DynamoDBStreamDeliversTo — #5801 Bug 2.
// A DynamoDB table (the commit's own use case) classifies as category
// `datastore` → SCOPE.Datastore. The whole point of emitting the
// handler-->source SUBSCRIBES_TO edge is that ApplyAsyncTriggerEdges (#5686)
// inverts it into source-->handler DELIVERS_TO so both the standalone-ESM
// path and SAM's inline path "render identically". This asserts the full
// round-trip: a DynamoDB-stream ESM source DOES get a DELIVERS_TO edge after
// inversion (previously it did not, because SCOPE.Datastore was absent from
// the inversion pass's recognised topic kinds).
func TestCFN_StandaloneEventSourceMapping_DynamoDBStreamDeliversTo(t *testing.T) {
	src := `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  TableHandlerFn:
    Type: AWS::Lambda::Function
    Properties: { Handler: h.handler, Runtime: nodejs20.x }
  OrdersTable:
    Type: AWS::DynamoDB::Table
    Properties:
      TableName: orders
      StreamSpecification: { StreamViewType: NEW_AND_OLD_IMAGES }
  Esm:
    Type: AWS::Lambda::EventSourceMapping
    Properties:
      EventSourceArn: !GetAtt OrdersTable.StreamArn
      FunctionName: !Ref TableHandlerFn
      StartingPosition: LATEST
`
	ents, rels := cfnRun("yaml", "esm/ddb.yaml", src)

	// Source classifies as SCOPE.Datastore.
	src2 := cfnFindEntityByLogical(ents, "OrdersTable")
	if src2 == nil {
		t.Fatalf("expected OrdersTable entity, got %+v", ents)
	}
	if src2.Kind != "SCOPE.Datastore" {
		t.Fatalf("precondition: OrdersTable should classify as SCOPE.Datastore, got %s", src2.Kind)
	}

	// The handler-->source SUBSCRIBES_TO edge must be present and well-kinded.
	if !cfnHasEdge(ents, rels, "SUBSCRIBES_TO", "TableHandlerFn", "OrdersTable") {
		t.Fatalf("ESM(ddb): missing SUBSCRIBES_TO TableHandlerFn->OrdersTable; rels=%+v", rels)
	}

	// After inversion, the source must gain an inbound DELIVERS_TO to the
	// handler — the render-identical goal.
	doc := cfnToDoc(ents, rels)
	stats := ApplyAsyncTriggerEdges(doc)
	if stats.DeliversEdges != 1 {
		t.Fatalf("ESM(ddb): expected 1 DELIVERS_TO after inversion, got %d (SCOPE.Datastore not inverted?)", stats.DeliversEdges)
	}
	sourceID := "SCOPE.Datastore:" + src2.Name
	found := false
	for _, r := range doc.Relationships {
		if r.Kind == "DELIVERS_TO" && r.FromID == sourceID && strings.HasSuffix(r.ToID, "#TableHandlerFn") {
			found = true
		}
	}
	if !found {
		t.Errorf("ESM(ddb): missing DELIVERS_TO OrdersTable->TableHandlerFn after inversion; rels=%+v", doc.Relationships)
	}
}

// TestCFN_StandaloneEventSourceMapping_ParameterTarget — #5801 Bug 1. When
// FunctionName resolves to a CFN Parameter (parameterized handler name — a
// `knownIDs` hit that is NOT a Resource), the edge endpoint must carry the
// Parameter entity's actual Kind (SCOPE.Schema, per the Parameters emission),
// NOT a fabricated SCOPE.Config prefix that resolves to no entity (a dangling
// edge). The strengthened cfnHasEdge enforces endpoint resolution, so a
// dangling/mis-kinded edge fails this test.
func TestCFN_StandaloneEventSourceMapping_ParameterTarget(t *testing.T) {
	src := `AWSTemplateFormatVersion: '2010-09-09'
Parameters:
  HandlerFnName:
    Type: String
Resources:
  OrdersQueue:
    Type: AWS::SQS::Queue
    Properties: { QueueName: orders }
  Esm:
    Type: AWS::Lambda::EventSourceMapping
    Properties:
      EventSourceArn: !GetAtt OrdersQueue.Arn
      FunctionName: !Ref HandlerFnName
      BatchSize: 10
`
	ents, rels := cfnRun("yaml", "esm/param.yaml", src)

	// Parameter entity is emitted as SCOPE.Schema.
	p := cfnFindEntityByLogical(ents, "HandlerFnName")
	if p == nil {
		t.Fatalf("expected HandlerFnName parameter entity, got %+v", ents)
	}
	if p.Kind != "SCOPE.Schema" {
		t.Fatalf("precondition: Parameter should be SCOPE.Schema, got %s", p.Kind)
	}

	// The SUBSCRIBES_TO edge must exist AND both endpoints must resolve to real
	// entities (no dangling SCOPE.Config prefix on the handler endpoint).
	if !cfnHasEdge(ents, rels, "SUBSCRIBES_TO", "HandlerFnName", "OrdersQueue") {
		t.Errorf("ESM(param): missing/dangling SUBSCRIBES_TO HandlerFnName->OrdersQueue; rels=%+v", rels)
	}

	// Explicit Kind-prefix assertion on the handler endpoint.
	wantFrom := "SCOPE.Schema:" + p.Name
	found := false
	for _, r := range rels {
		if r.Kind == "SUBSCRIBES_TO" && strings.HasSuffix(r.ToID, "#OrdersQueue") &&
			strings.HasSuffix(r.FromID, "#HandlerFnName") {
			found = true
			if r.FromID != wantFrom {
				t.Errorf("ESM(param): handler endpoint = %q, want %q (dangling/mis-kinded)", r.FromID, wantFrom)
			}
		}
	}
	if !found {
		t.Errorf("ESM(param): no SUBSCRIBES_TO edge from HandlerFnName found; rels=%+v", rels)
	}
}
