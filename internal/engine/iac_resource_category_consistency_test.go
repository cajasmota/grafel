// Cross-tool IaC resource_category consistency — #3549 (epic #3512).
//
// Proves the ONE shared classifier (types.IaCResourceCategory) yields the SAME
// resource_category for EQUIVALENT resources across CDK, Pulumi and
// CloudFormation (HCL/Terraform + Bicep are covered in their own packages).
// These are VALUE-asserting: each checks a specific category string, not len>0.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// catOf returns the resource_category property of the entity with the given
// name from a detector result, or "" if absent.
func catOf(ents []entityResult, name string) string {
	for i := range ents {
		if ents[i].name == name {
			return ents[i].props["resource_category"]
		}
	}
	return ""
}

// TestIaCCategory_Classifier_ValueTable asserts the shared classifier directly
// for one representative type per category, in each tool dialect, proving the
// SAME category for equivalent resources regardless of tool syntax.
func TestIaCCategory_Classifier_ValueTable(t *testing.T) {
	cases := []struct {
		typeString string
		want       string
	}{
		// datastore — equivalents across all five dialects
		{"aws_db_instance", types.IaCCategoryDatastore},                 // Terraform
		{"Microsoft.Sql/servers/databases", types.IaCCategoryDatastore}, // Bicep
		{"dynamodb.Table", types.IaCCategoryDatastore},                  // CDK
		{"aws.rds.Instance", types.IaCCategoryDatastore},                // Pulumi
		{"AWS::RDS::DBInstance", types.IaCCategoryDatastore},            // CFN
		// queue
		{"aws_sqs_queue", types.IaCCategoryQueue},
		{"sqs.Queue", types.IaCCategoryQueue},
		{"aws.sqs.Queue", types.IaCCategoryQueue},
		{"AWS::SQS::Queue", types.IaCCategoryQueue},
		// topic
		{"aws_sns_topic", types.IaCCategoryTopic},
		{"sns.Topic", types.IaCCategoryTopic},
		{"aws.sns.Topic", types.IaCCategoryTopic},
		{"AWS::SNS::Topic", types.IaCCategoryTopic},
		// stream
		{"aws_kinesis_stream", types.IaCCategoryStream},
		{"AWS::Kinesis::Stream", types.IaCCategoryStream},
		// function
		{"aws_lambda_function", types.IaCCategoryFunction},
		{"lambda.Function", types.IaCCategoryFunction},
		{"aws.lambda.Function", types.IaCCategoryFunction},
		{"AWS::Lambda::Function", types.IaCCategoryFunction},
		// cache
		{"aws_elasticache_cluster", types.IaCCategoryCache},
		{"AWS::ElastiCache::CacheCluster", types.IaCCategoryCache},
		// secret
		{"aws_secretsmanager_secret", types.IaCCategorySecret},
		{"AWS::SecretsManager::Secret", types.IaCCategorySecret},
		{"Microsoft.KeyVault/vaults", types.IaCCategorySecret},
		// storage
		{"aws_s3_bucket", types.IaCCategoryStorage},
		{"s3.Bucket", types.IaCCategoryStorage},
		{"AWS::S3::Bucket", types.IaCCategoryStorage},
		{"Microsoft.Storage/storageAccounts", types.IaCCategoryStorage},
		// network
		{"aws_vpc", types.IaCCategoryNetwork},
		{"AWS::EC2::VPC", types.IaCCategoryNetwork},
		{"Microsoft.Network/virtualNetworks", types.IaCCategoryNetwork},
		// compute
		{"aws_instance", types.IaCCategoryCompute},
		{"AWS::ECS::Cluster", types.IaCCategoryCompute},
		{"AWS::ECS::Service", types.IaCCategoryCompute},
		// security / identity (#4885) — IAM/KMS/ACM/Cognito across dialects
		{"aws_iam_role", types.IaCCategorySecurity},
		{"AWS::IAM::Role", types.IaCCategorySecurity},
		{"iam.Role", types.IaCCategorySecurity},
		{"aws_kms_key", types.IaCCategorySecurity},
		{"AWS::KMS::Key", types.IaCCategorySecurity},
		{"aws_acm_certificate", types.IaCCategorySecurity},
		{"AWS::Cognito::UserPool", types.IaCCategorySecurity},
		{"google_kms_crypto_key", types.IaCCategorySecurity},
		// observability (#4885) — CloudWatch / X-Ray / GCP logging
		{"aws_cloudwatch_log_group", types.IaCCategoryObservability},
		{"AWS::Logs::LogGroup", types.IaCCategoryObservability},
		{"aws_cloudwatch_metric_alarm", types.IaCCategoryObservability},
		{"AWS::CloudWatch::Dashboard", types.IaCCategoryObservability},
		{"aws_xray_sampling_rule", types.IaCCategoryObservability},
		{"google_logging_metric", types.IaCCategoryObservability},
		// Kubernetes samples — provider-agnostic catalog coverage
		{"apps/v1/Deployment", types.IaCCategoryCompute},
		{"v1/Service", types.IaCCategoryNetwork},
		{"v1/Secret", types.IaCCategorySecret},
		// other — genuine fallback only
		{"aws_glue_crawler", types.IaCCategoryOther},
		{"some_totally_unknown_resource", types.IaCCategoryOther},
	}
	for _, c := range cases {
		if got := types.IaCResourceCategory(c.typeString); got != c.want {
			t.Errorf("IaCResourceCategory(%q) = %q, want %q", c.typeString, got, c.want)
		}
	}
}

// TestIaCCategory_CDK_StampsCategory proves a CDK dynamodb.Table → datastore,
// sqs.Queue → queue (value-asserting on the stamped property).
func TestIaCCategory_CDK_StampsCategory(t *testing.T) {
	src := `import * as cdk from 'aws-cdk-lib';
import * as dynamodb from 'aws-cdk-lib/aws-dynamodb';
import * as sqs from 'aws-cdk-lib/aws-sqs';
const t1 = new dynamodb.Table(this, 'Orders', {});
const q1 = new sqs.Queue(this, 'Jobs', {});
`
	ents, _ := runCDKDetect(t, "typescript", "lib/stack.ts", src)
	if got := catOf(ents, "Orders"); got != types.IaCCategoryDatastore {
		t.Errorf("CDK dynamodb.Table resource_category = %q, want datastore", got)
	}
	if got := catOf(ents, "Jobs"); got != types.IaCCategoryQueue {
		t.Errorf("CDK sqs.Queue resource_category = %q, want queue", got)
	}
}

// TestIaCCategory_Pulumi_StampsTopic proves a Pulumi aws.sns.Topic → topic.
func TestIaCCategory_Pulumi_StampsTopic(t *testing.T) {
	src := `import * as aws from "@pulumi/aws";
const events = new aws.sns.Topic("events", {});
`
	res := applyPulumiEdges(DetectorPassArgs{Lang: "typescript", Path: "index.ts", Content: []byte(src)})
	ents := make([]entityResult, 0, len(res.Entities))
	for _, e := range res.Entities {
		ents = append(ents, entityResult{kind: e.Kind, name: e.Name, props: e.Properties})
	}
	if got := catOf(ents, "events"); got != types.IaCCategoryTopic {
		t.Errorf("Pulumi aws.sns.Topic resource_category = %q, want topic", got)
	}
}

// TestIaCCategory_CFN_StampsCategory proves AWS::RDS::DBInstance → datastore,
// AND that the aligned entity Kind is SCOPE.Datastore (derived from the same
// classifier so Kind and property can never diverge).
func TestIaCCategory_CFN_StampsCategory(t *testing.T) {
	src := `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  Db:
    Type: AWS::RDS::DBInstance
    Properties:
      Engine: postgres
  Q:
    Type: AWS::SQS::Queue
    Properties: {}
  Fn:
    Type: AWS::Lambda::Function
    Properties: {}
`
	res := applyCloudFormationEdges(DetectorPassArgs{Lang: "yaml", Path: "template.yaml", Content: []byte(src)})

	byName := map[string]struct {
		kind string
		cat  string
	}{}
	for _, e := range res.Entities {
		// Logical ids are name-suffixed in the cfn: id; match on Properties.
		if e.Properties["logical_id"] != "" {
			byName[e.Properties["logical_id"]] = struct {
				kind string
				cat  string
			}{e.Kind, e.Properties["resource_category"]}
		}
	}

	if got := byName["Db"]; got.cat != types.IaCCategoryDatastore || got.kind != "SCOPE.Datastore" {
		t.Errorf("CFN AWS::RDS::DBInstance = kind %q cat %q, want SCOPE.Datastore/datastore", got.kind, got.cat)
	}
	if got := byName["Q"]; got.cat != types.IaCCategoryQueue || got.kind != "SCOPE.Queue" {
		t.Errorf("CFN AWS::SQS::Queue = kind %q cat %q, want SCOPE.Queue/queue", got.kind, got.cat)
	}
	if got := byName["Fn"]; got.cat != types.IaCCategoryFunction || got.kind != "SCOPE.ServerlessFunction" {
		t.Errorf("CFN AWS::Lambda::Function = kind %q cat %q, want SCOPE.ServerlessFunction/function", got.kind, got.cat)
	}
}
