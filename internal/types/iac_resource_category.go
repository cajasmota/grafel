package types

import "strings"

// IaC resource categorization — the ONE shared classifier used by every IaC
// extractor/detector in archigraph (Terraform/HCL, AWS CDK TS+JS+Python,
// Pulumi TS+JS+Python, CloudFormation/SAM, Serverless Framework, Kubernetes,
// and Azure Bicep). #3549, #4885, epic #3512.
//
// # The consistency model (decided, applied everywhere)
//
// Historically each IaC tool classified resources its own way, so a cross-tool
// query like "show me all the datastores" was impossible:
//
//   - CloudFormation mapped AWS::* types to coarse SCOPE.Datastore /
//     SCOPE.Queue / SCOPE.ServerlessFunction entity Kinds.
//   - CDK + Pulumi emitted SCOPE.InfraResource with a private, three-valued
//     `resource_scope` property (service / datastore / queue) computed by a
//     per-tool helper.
//   - Terraform emitted SCOPE.Component/resource with only a `resource_type`,
//     no semantic typing at all.
//   - Bicep emitted SCOPE.InfraResource with its own `resource_scope` helper.
//
// The unifying decision: a single fine-grained `resource_category` PROPERTY is
// the cross-tool join key. Every IaC resource entity, regardless of tool or
// source language, now carries `resource_category` computed by the one
// IaCResourceCategory function below. A query that wants "all datastores"
// filters on `resource_category == datastore` and gets Terraform aws_db_instance,
// CDK dynamodb.Table, Pulumi aws.rds.Instance, CFN AWS::RDS::DBInstance and Bicep
// Microsoft.Sql/servers alike.
//
// We deliberately do NOT force a single entity Kind on every tool:
//
//   - The semantic Kinds CloudFormation already emits (SCOPE.Datastore /
//     SCOPE.Queue / SCOPE.ServerlessFunction) are preserved — they are derived
//     from the same category via IaCKindForCategory so they can never diverge
//     from the property.
//   - CDK / Pulumi / Bicep keep SCOPE.InfraResource and Terraform keeps
//     SCOPE.Component/resource, because their QualifiedNames and the
//     DEPENDS_ON/USES edges already in the graph are keyed off those Kinds;
//     changing the Kind would break edge resolution. The `resource_category`
//     property gives uniform cross-tool semantics WITHOUT that risk.
//
// So: ONE classifier, ONE property name, applied to ALL tools; the CFN entity
// Kind stays aligned because it is derived from the same classifier.
//
// # The catalog (#4885)
//
// The classifier is a DECLARATIVE, ORDERED CATALOG (iacCategoryCatalog below) —
// a data table of {category, substrings} rules, not a giant switch. Each rule
// lists provider-agnostic substrings (Terraform/Bicep/CDK/Pulumi/CFN/Serverless/
// Kubernetes shapes) that map a resource-type string to a category. The first
// rule whose substrings match (case-insensitive) wins, so ordering encodes
// specificity. Adding a provider or a new resource type is a one-line table
// edit; adding a category means a new constant + a catalog entry (+ its
// frontend style/legend). "other" is the genuine fallback only.

// IaC resource category constants — the closed set returned by
// IaCResourceCategory. "other" is the fallback for anything unrecognised.
const (
	IaCCategoryDatastore     = "datastore"
	IaCCategoryQueue         = "queue"
	IaCCategoryTopic         = "topic"
	IaCCategoryStream        = "stream"
	IaCCategoryFunction      = "function"
	IaCCategoryCache         = "cache"
	IaCCategorySecret        = "secret"
	IaCCategorySecurity      = "security"      // IAM / KMS / ACM / Cognito / security groups (#4885)
	IaCCategoryObservability = "observability" // CloudWatch / X-Ray / GCP logging+monitoring (#4885)
	IaCCategoryNetwork       = "network"
	IaCCategoryCompute       = "compute"
	IaCCategoryStorage       = "storage"
	IaCCategoryOther         = "other"
)

// iacCategoryRule is one declarative entry of the catalog: a category and the
// provider-agnostic substrings that classify a resource-type string into it.
type iacCategoryRule struct {
	category string
	// any returns the category when the (lower-cased) type contains ANY of these.
	any []string
}

// iacCategoryCatalog is the ORDERED declarative catalog. Order encodes
// specificity: more specific categories (cache/secret/security/observability/
// stream/topic/queue/function) are listed BEFORE the broad datastore/storage/
// compute/network buckets so e.g. an ElastiCache resource is `cache` not
// `datastore`, an IAM role is `security` not `network`, and a CloudWatch alarm
// is `observability` not `compute`.
//
// Each rule's substrings span the IaC dialects archigraph extracts:
//
//   - Terraform / OpenTofu:  aws_db_instance, azurerm_storage_account, google_sql_database_instance
//   - Azure Bicep:           Microsoft.Sql/servers, Microsoft.ServiceBus/namespaces/queues
//   - AWS CDK construct type: s3.Bucket, dynamodb.Table, sqs.Queue, lambda.Function, CfnDBInstance
//   - Pulumi resource type:   aws.s3.Bucket, aws.rds.Instance, aws.sns.Topic, azure.storage.Account
//   - CloudFormation/SAM:     AWS::RDS::DBInstance, AWS::SQS::Queue, AWS::Lambda::Function
//   - Serverless Framework:   AWS::* (CFN shapes) — covered by the CFN substrings
//   - Kubernetes:             apps/v1/Deployment, v1/Service, v1/Secret, networking.k8s.io/Ingress
var iacCategoryCatalog = []iacCategoryRule{
	// --- cache (before datastore: elasticache/redis/memcached) ---------------
	{IaCCategoryCache, []string{
		"elasticache", "elasti_cache", "memorydb", "dax",
		"aws_dax_", "::elasticache::", "::dax::", "::memorydb::",
		"elasticache.", ".dax.", "memorydb.",
		"microsoft.cache/", "/rediscache", "rediscache",
		"google_redis_", "redis.", "memcache",
	}},

	// --- secret (vaults / managed secrets) -----------------------------------
	{IaCCategorySecret, []string{
		"secretsmanager", "secrets_manager", "::secretsmanager::",
		"secretsmanager.", "aws_ssm_parameter",
		"microsoft.keyvault/", "keyvault", "key_vault",
		"google_secret_manager", "secretmanager.", ".secret",
		// Kubernetes Secret object.
		"v1/secret", "/secret/", "core/v1/secret",
	}},

	// --- security / identity (#4885: IAM / KMS / ACM / Cognito / SGs) ---------
	{IaCCategorySecurity, []string{
		// IAM (roles / policies / users / groups / attachments / instance profiles).
		"aws_iam_", "::iam::", "iam.role", "iam.policy", "iam.user", "iam.group",
		".iam.", "iamrole", "iampolicy", "iaminstanceprofile",
		"managedpolicy", "policystatement", "rolepolicy",
		// KMS (encryption keys).
		"aws_kms_", "::kms::", "kms.key", "kms.alias", ".kms.", "kmskey",
		// ACM (certificates).
		"aws_acm_", "::certificatemanager::", "acm.certificate", "certificatemanager.",
		"::acmpca::", "acm_certificate",
		// Cognito (identity / user pools).
		"aws_cognito_", "::cognito::", "cognito.", "userpool", "identitypool",
		// WAF / Shield / GuardDuty / Inspector (perimeter security).
		"aws_wafv2_", "aws_waf_", "::wafv2::", "::waf::", "wafv2.", "::guardduty::",
		"guardduty.", "::inspector", "::shield::",
		// Azure / GCP identity + key services.
		"microsoft.authorization/", "microsoft.managedidentity/",
		"google_kms_", "google_service_account", "google_project_iam",
		"google_iam_", "serviceaccount.", "cryptokey",
		// Kubernetes RBAC / ServiceAccount.
		"rbac.authorization.k8s.io", "/role", "/rolebinding",
		"/clusterrole", "/serviceaccount", "/networkpolicy",
	}},

	// --- observability (#4885: CloudWatch / X-Ray / logging / monitoring) -----
	{IaCCategoryObservability, []string{
		// CloudWatch log group / alarm / dashboard / metric / event rule.
		"aws_cloudwatch", "::cloudwatch::", "cloudwatch.", ".cloudwatch.",
		"loggroup", "log_group", "metricalarm", "::logs::", "logs.loggroup",
		// X-Ray distributed tracing.
		"aws_xray_", "::xray::", "xray.", "_xray", "samplingrule",
		// Azure / GCP logging + monitoring.
		"microsoft.insights/", "applicationinsights", "loganalytics",
		"microsoft.operationalinsights/",
		"google_logging_", "google_monitoring_", "logging.", "monitoring.",
		"stackdriver",
	}},

	// --- stream (kinesis / event hubs / pubsub streams) ----------------------
	{IaCCategoryStream, []string{
		"kinesis", "::kinesis::", "kinesis.",
		"aws_kinesis", "msk", "managedstreaming", "::msk::",
		"microsoft.eventhub/", "eventhub",
		"google_pubsub_lite", "kafka",
	}},

	// --- topic (pub/sub fan-out) ---------------------------------------------
	{IaCCategoryTopic, []string{
		"aws_sns_topic", "::sns::", "sns.topic", "sns_topic",
		".sns.", "snstopic",
		"microsoft.eventgrid/", "eventgrid",
		"/topics", "servicebustopic",
		"google_pubsub_topic", "pubsub.topic",
	}},

	// --- queue ---------------------------------------------------------------
	{IaCCategoryQueue, []string{
		"aws_sqs_queue", "::sqs::", "sqs.queue", "sqs_queue", ".sqs.", "sqsqueue",
		"aws_mq_", "::mq::", "amazonmq",
		"microsoft.servicebus/", "servicebus", "/queues", "storagequeue",
		"google_cloud_tasks", "cloudtasks",
		"::events::eventbus", "eventbus", "google_pubsub_subscription",
	}},

	// --- function (serverless) -----------------------------------------------
	{IaCCategoryFunction, []string{
		"aws_lambda_function", "::lambda::function", "::serverless::function",
		"lambda.function", "lambda_function", ".lambda.", "lambdafunction",
		"cfnfunction",
		"microsoft.web/sites/functions", "functionapp", "azurerm_function_app",
		"google_cloudfunctions", "cloudfunction",
	}},

	// --- datastore (databases + tables; before generic storage) --------------
	{IaCCategoryDatastore, []string{
		"aws_db_instance", "aws_rds", "aws_db_cluster", "::rds::",
		"aws_dynamodb", "::dynamodb::", "dynamodb.", "dynamodbtable",
		"aws_redshift", "::redshift::", "redshift.",
		"aws_docdb", "::docdb::", "docdb.",
		"aws_neptune", "::neptune::", "neptune.",
		"aws_timestream", "::timestream::", "timestream.",
		"aws_qldb", "::qldb::", "aws_keyspaces", "::cassandra::",
		"rds.", ".rds.", "dbinstance", "dbcluster", "database",
		"microsoft.sql/", "microsoft.documentdb/", "cosmosdb", "cosmos_db",
		"microsoft.dbforpostgresql/", "microsoft.dbformysql/",
		"microsoft.dbformariadb/",
		"google_sql_", "google_spanner", "google_bigtable", "google_firestore",
		"sql.", "spanner.", "bigtable.", "firestore.",
		"rds.databaseinstance", "rds.databasecluster",
		"sql_database", "cosmosdb_account",
	}},

	// --- storage (object/file/block; after datastore so DB tables win) -------
	{IaCCategoryStorage, []string{
		"aws_s3_bucket", "::s3::bucket", "s3.bucket", "s3_bucket", ".s3.", "s3bucket",
		"aws_efs", "::efs::", "efs.", "aws_fsx", "::fsx::",
		"aws_ebs_volume", "::ec2::volume",
		"microsoft.storage/", "storageaccount", "storage_account",
		"google_storage_bucket", "storage.bucket",
		"azurerm_storage", "blobcontainer", "/blobservices",
		// Kubernetes persistent storage.
		"persistentvolume", "/persistentvolumeclaim",
	}},

	// --- network -------------------------------------------------------------
	{IaCCategoryNetwork, []string{
		"aws_vpc", "::ec2::vpc", "aws_subnet", "::ec2::subnet",
		"aws_security_group", "::ec2::securitygroup",
		"aws_route", "aws_internet_gateway", "aws_nat_gateway",
		"aws_lb", "aws_alb", "aws_elb", "::elasticloadbalancing",
		"aws_route53", "::route53::", "aws_api_gateway", "::apigateway",
		"aws_cloudfront", "::cloudfront::", "cloudfront.",
		"ec2.vpc", "ec2.subnet", "ec2.securitygroup", ".vpc.", "vpc.",
		"microsoft.network/", "google_compute_network",
		"google_compute_subnetwork", "google_compute_firewall",
		"loadbalancer", "load_balancer",
		// Kubernetes networking objects.
		"networking.k8s.io", "/ingress", "/service", "v1/service",
	}},

	// --- compute (catch-all for VMs/containers/clusters; last) ---------------
	{IaCCategoryCompute, []string{
		"aws_instance", "::ec2::instance", "ec2.instance",
		"aws_ecs", "::ecs::", "ecs.", "aws_eks", "::eks::", "eks.",
		"aws_autoscaling", "aws_batch", "::batch::",
		"microsoft.compute/", "microsoft.containerservice/", "microsoft.app/",
		"microsoft.web/sites", "appservice",
		"google_compute_instance", "google_container_cluster",
		"google_cloud_run", "cloudrun", "run.",
		"compute.instance", "fargate", "virtualmachine",
		// Kubernetes workloads.
		"apps/v1", "/deployment", "/statefulset", "/daemonset",
		"/replicaset", "/pod", "batch/v1", "/job", "/cronjob",
	}},
}

// IaCResourceCategory maps an IaC resource type string — in ANY of the tool
// dialects the catalog covers — to one of the IaCCategory* constants. It walks
// the declarative iacCategoryCatalog in order and returns the first matching
// rule's category; "other" if none match. Matching is case-insensitive on
// substrings, tolerating the differing shapes the tools use for the "same"
// resource.
func IaCResourceCategory(typeString string) string {
	t := strings.ToLower(typeString)
	for i := range iacCategoryCatalog {
		if containsAny(t, iacCategoryCatalog[i].any...) {
			return iacCategoryCatalog[i].category
		}
	}
	return IaCCategoryOther
}

// IaCKindForCategory maps a resource_category to the semantic SCOPE.* entity
// Kind that CloudFormation uses, so the CFN Kind and the property are derived
// from one source and can never drift. Categories without a dedicated CFN Kind
// fall back to SCOPE.InfraResource (the shared IaC-resource class). This keeps
// the historical CFN Kinds (Datastore / Queue / ServerlessFunction) intact while
// guaranteeing they agree with IaCResourceCategory.
func IaCKindForCategory(category string) string {
	switch category {
	case IaCCategoryDatastore, IaCCategoryStorage, IaCCategoryCache:
		return "SCOPE.Datastore"
	case IaCCategoryQueue, IaCCategoryTopic, IaCCategoryStream:
		return "SCOPE.Queue"
	case IaCCategoryFunction:
		return "SCOPE.ServerlessFunction"
	default:
		return "SCOPE.InfraResource"
	}
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
