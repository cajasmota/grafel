# Synthetic fixture: EKS + Lambda + RDS infrastructure module.
# Used by extractor_test.go TestTerraformFixture* tests.
# Covers: resource, data, module, variable, output, provider, locals, depends_on.

terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.0"
    }
  }
  backend "s3" {
    bucket = "infra-tfstate"
    key    = "eks-lambda-rds/terraform.tfstate"
    region = "us-east-1"
  }
}

# ---- Providers ----

provider "aws" {
  region = var.region
}

provider "kubernetes" {
  host                   = module.eks.cluster_endpoint
  cluster_ca_certificate = base64decode(module.eks.cluster_ca)
  token                  = data.aws_eks_cluster_auth.cluster.token
}

# ---- Variables ----

variable "region" {
  type        = string
  description = "AWS region for all resources"
  default     = "us-east-1"
}

variable "cluster_name" {
  type        = string
  description = "EKS cluster name"
  default     = "infra-eks"
}

variable "db_password" {
  type        = string
  description = "RDS master password"
  sensitive   = true
}

variable "db_name" {
  type    = string
  default = "appdb"
}

variable "lambda_memory" {
  type    = number
  default = 512
}

variable "lambda_timeout" {
  type    = number
  default = 30
}

variable "environment" {
  type    = string
  default = "prod"
}

# ---- Locals ----

locals {
  name_prefix   = "infra-${var.environment}"
  common_tags = {
    Project     = "infra"
    Environment = var.environment
    ManagedBy   = "terraform"
  }
  lambda_function_name = "${local.name_prefix}-processor"
  db_identifier        = "${local.name_prefix}-postgres"
  eks_node_group_name  = "${local.name_prefix}-nodes"
}

# ---- Data sources ----

data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_caller_identity" "current" {}

data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "lambda_permissions" {
  statement {
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
      "rds:DescribeDBInstances",
      "secretsmanager:GetSecretValue",
    ]
    resources = ["*"]
  }
}

data "aws_eks_cluster_auth" "cluster" {
  name = module.eks.cluster_name

  depends_on = [module.eks]
}

data "aws_secretsmanager_secret_version" "db_password" {
  secret_id = aws_secretsmanager_secret.db_password.id

  depends_on = [aws_secretsmanager_secret_version.db_password_val]
}

# ---- Networking module ----

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.1.0"

  name = "${local.name_prefix}-vpc"
  cidr = "10.0.0.0/16"

  azs             = data.aws_availability_zones.available.names
  private_subnets = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]
  public_subnets  = ["10.0.101.0/24", "10.0.102.0/24", "10.0.103.0/24"]

  enable_nat_gateway = true
  single_nat_gateway = true

  tags = local.common_tags
}

# ---- EKS module ----

module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "20.0.0"

  cluster_name    = var.cluster_name
  cluster_version = "1.28"

  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.private_subnets

  cluster_endpoint_public_access = true

  eks_managed_node_groups = {
    default = {
      min_size     = 1
      max_size     = 5
      desired_size = 2
      instance_types = ["t3.medium"]
      labels = local.common_tags
    }
  }

  tags = local.common_tags

  depends_on = [module.vpc]
}

# ---- RDS (PostgreSQL) ----

resource "aws_db_subnet_group" "postgres" {
  name       = "${local.name_prefix}-db-subnet"
  subnet_ids = module.vpc.private_subnets
  tags       = local.common_tags
}

resource "aws_security_group" "rds" {
  name        = "${local.name_prefix}-rds-sg"
  description = "RDS security group"
  vpc_id      = module.vpc.vpc_id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.lambda.id]
  }

  tags = local.common_tags
}

resource "aws_db_instance" "postgres" {
  identifier           = local.db_identifier
  engine               = "postgres"
  engine_version       = "15.4"
  instance_class       = "db.t3.medium"
  allocated_storage    = 20
  max_allocated_storage = 100

  db_name  = var.db_name
  username = "appuser"
  password = data.aws_secretsmanager_secret_version.db_password.secret_string

  db_subnet_group_name   = aws_db_subnet_group.postgres.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  backup_retention_period = 7
  skip_final_snapshot     = false
  final_snapshot_identifier = "${local.db_identifier}-final"

  tags = local.common_tags

  depends_on = [
    aws_db_subnet_group.postgres,
    aws_security_group.rds,
  ]
}

# ---- Secrets Manager ----

resource "aws_secretsmanager_secret" "db_password" {
  name        = "${local.name_prefix}/db/password"
  description = "RDS master password for ${local.db_identifier}"
  tags        = local.common_tags
}

resource "aws_secretsmanager_secret_version" "db_password_val" {
  secret_id     = aws_secretsmanager_secret.db_password.id
  secret_string = var.db_password

  depends_on = [aws_secretsmanager_secret.db_password]
}

# ---- Lambda function ----

resource "aws_iam_role" "lambda_role" {
  name               = "${local.name_prefix}-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
  tags               = local.common_tags
}

resource "aws_iam_role_policy" "lambda_policy" {
  name   = "${local.name_prefix}-lambda-policy"
  role   = aws_iam_role.lambda_role.id
  policy = data.aws_iam_policy_document.lambda_permissions.json

  depends_on = [aws_iam_role.lambda_role]
}

resource "aws_security_group" "lambda" {
  name        = "${local.name_prefix}-lambda-sg"
  description = "Lambda security group"
  vpc_id      = module.vpc.vpc_id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = local.common_tags
}

resource "aws_lambda_function" "processor" {
  function_name = local.lambda_function_name
  role          = aws_iam_role.lambda_role.arn
  runtime       = "provided.al2"
  handler       = "bootstrap"
  filename      = "processor.zip"

  memory_size = var.lambda_memory
  timeout     = var.lambda_timeout

  vpc_config {
    subnet_ids         = module.vpc.private_subnets
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = {
      DB_HOST   = aws_db_instance.postgres.address
      DB_NAME   = var.db_name
      DB_SECRET = aws_secretsmanager_secret.db_password.name
    }
  }

  tags = local.common_tags

  depends_on = [
    aws_iam_role_policy.lambda_policy,
    aws_db_instance.postgres,
  ]
}

resource "aws_cloudwatch_log_group" "lambda_logs" {
  name              = "/aws/lambda/${local.lambda_function_name}"
  retention_in_days = 14
  tags              = local.common_tags
}

resource "aws_lambda_event_source_mapping" "sqs_trigger" {
  event_source_arn = aws_sqs_queue.processor_input.arn
  function_name    = aws_lambda_function.processor.arn
  batch_size       = 10
  enabled          = true

  depends_on = [aws_lambda_function.processor]
}

# ---- SQS ----

resource "aws_sqs_queue" "processor_input" {
  name                       = "${local.name_prefix}-processor-input"
  visibility_timeout_seconds = 60
  message_retention_seconds  = 86400
  tags                       = local.common_tags
}

resource "aws_sqs_queue" "processor_dlq" {
  name                      = "${local.name_prefix}-processor-dlq"
  message_retention_seconds = 1209600
  tags                      = local.common_tags
}

# ---- S3 (Lambda artifacts + config) ----

resource "aws_s3_bucket" "artifacts" {
  bucket = "${local.name_prefix}-artifacts-${data.aws_caller_identity.current.account_id}"
  tags   = local.common_tags
}

resource "aws_s3_bucket_versioning" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# ---- Outputs ----

output "eks_cluster_name" {
  description = "EKS cluster name"
  value       = module.eks.cluster_name
}

output "eks_cluster_endpoint" {
  description = "EKS cluster API endpoint"
  value       = module.eks.cluster_endpoint
}

output "rds_endpoint" {
  description = "RDS instance connection endpoint"
  value       = aws_db_instance.postgres.address
}

output "lambda_arn" {
  description = "Lambda function ARN"
  value       = aws_lambda_function.processor.arn
}

output "lambda_function_name" {
  description = "Lambda function name"
  value       = aws_lambda_function.processor.function_name
}

output "sqs_queue_url" {
  description = "SQS input queue URL"
  value       = aws_sqs_queue.processor_input.url
}

output "artifacts_bucket" {
  description = "S3 bucket for Lambda artifacts"
  value       = aws_s3_bucket.artifacts.bucket
}

output "vpc_id" {
  description = "VPC ID"
  value       = module.vpc.vpc_id
}
