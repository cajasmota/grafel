package engine

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

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

// cfnHasEdge reports whether an edge of `kind` exists whose FromID ends with
// `#fromLogical` and ToID ends with `#toLogical`.
func cfnHasEdge(rels []types.RelationshipRecord, kind, fromLogical, toLogical string) bool {
	for _, r := range rels {
		if r.Kind != kind {
			continue
		}
		if strings.HasSuffix(r.FromID, "#"+fromLogical) && strings.HasSuffix(r.ToID, "#"+toLogical) {
			return true
		}
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
	if !cfnHasEdge(rels, "USES", "ProcessorFn", "DataBucket") {
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
	if !cfnHasEdge(rels, "USES", "ProcessorFn", "DataBucket") {
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
	if !cfnHasEdge(rels, "DEPENDS_ON", "Events", "TableName") {
		t.Errorf("missing DEPENDS_ON Events->TableName (Ref to Parameter); rels=%+v", rels)
	}
	// Worker → DEPENDS_ON Events (DependsOn list form AND !Sub).
	if !cfnHasEdge(rels, "DEPENDS_ON", "Worker", "Events") {
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
