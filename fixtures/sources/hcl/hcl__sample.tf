# Sample Terraform file for archigraph HCL extractor fixture (MX-1072).
# Covers: resource, data, module, variable, output, provider, locals, depends_on.

terraform {
  required_version = ">= 1.0"
  backend "s3" {
    bucket = "archigraph-tfstate"
    key    = "prod/terraform.tfstate"
    region = "us-east-1"
  }
}

# ---- Providers ----

provider "aws" {
  region = "us-east-1"
}

provider "aws" {
  alias  = "eu"
  region = "eu-west-1"
}

# ---- Variables ----

variable "env" {
  type        = string
  description = "Deployment environment (prod, staging, dev)"
  default     = "prod"
}

variable "lambda_memory" {
  type    = number
  default = 512
}

variable "lambda_timeout" {
  type    = number
  default = 30
}

variable "org_id" {
  type = string
}

variable "enable_tracing" {
  type    = bool
  default = true
}

# ---- Locals ----

locals {
  prefix         = "archigraph"
  function_name  = "${local.prefix}-extract-${var.env}"
  ecr_image_uri  = "${aws_ecr_repository.memx_extract.repository_url}:latest"
  common_tags = {
    Project = "archigraph"
    Env     = var.env
    ManagedBy = "terraform"
  }
}

# ---- Data sources ----

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
      "s3:GetObject",
      "s3:ListBucket",
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
    ]
    resources = ["*"]
  }
}

data "aws_caller_identity" "current" {}

data "aws_region" "current" {}

# ---- IAM resources ----

resource "aws_iam_role" "lambda_role" {
  name               = "${local.prefix}-lambda-role-${var.env}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_iam_role_policy" "lambda_permissions" {
  name   = "${local.prefix}-lambda-policy-${var.env}"
  role   = aws_iam_role.lambda_role.id
  policy = data.aws_iam_policy_document.lambda_permissions.json

  depends_on = [aws_iam_role.lambda_role]
}

resource "aws_iam_role_policy_attachment" "lambda_basic_execution" {
  role       = aws_iam_role.lambda_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"

  depends_on = [aws_iam_role.lambda_role]
}

# ---- ECR ----

resource "aws_ecr_repository" "memx_extract" {
  name                 = "${local.prefix}-extract-${var.env}"
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }
}

# ---- SQS ----

resource "aws_sqs_queue" "extract_dlq" {
  name                       = "${local.prefix}-extract-dlq-${var.env}"
  message_retention_seconds  = 1209600
  visibility_timeout_seconds = 60
}

resource "aws_sqs_queue" "extract_queue" {
  name                       = "${local.prefix}-extract-queue-${var.env}"
  visibility_timeout_seconds = 60
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.extract_dlq.arn
    maxReceiveCount     = 3
  })

  depends_on = [aws_sqs_queue.extract_dlq]
}

# ---- Lambda ----

resource "aws_lambda_function" "memx_extract" {
  function_name = local.function_name
  package_type  = "Image"
  image_uri     = local.ecr_image_uri
  role          = aws_iam_role.lambda_role.arn
  memory_size   = var.lambda_memory
  timeout       = var.lambda_timeout

  environment {
    variables = {
      ENV    = var.env
      ORG_ID = var.org_id
    }
  }

  tracing_config {
    mode = var.enable_tracing ? "Active" : "PassThrough"
  }

  depends_on = [
    aws_iam_role.lambda_role,
    aws_ecr_repository.memx_extract,
  ]
}

resource "aws_lambda_event_source_mapping" "extract_sqs" {
  event_source_arn = aws_sqs_queue.extract_queue.arn
  function_name    = aws_lambda_function.memx_extract.arn
  batch_size       = 1
  enabled          = true

  depends_on = [
    aws_lambda_function.memx_extract,
    aws_sqs_queue.extract_queue,
  ]
}

# ---- S3 ----

resource "aws_s3_bucket" "repo_uploads" {
  bucket = "${local.prefix}-repo-uploads-${var.env}"
}

resource "aws_s3_bucket_versioning" "repo_uploads" {
  bucket = aws_s3_bucket.repo_uploads.id
  versioning_configuration {
    status = "Enabled"
  }

  depends_on = [aws_s3_bucket.repo_uploads]
}

# ---- CloudWatch ----

resource "aws_cloudwatch_log_group" "lambda_logs" {
  name              = "/aws/lambda/${local.function_name}"
  retention_in_days = 30
}

# ---- Modules ----

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.0.0"

  name = "${local.prefix}-vpc-${var.env}"
  cidr = "10.0.0.0/16"
}

module "alb" {
  source  = "terraform-aws-modules/alb/aws"
  version = "8.7.0"

  name    = "${local.prefix}-alb-${var.env}"
  vpc_id  = module.vpc.vpc_id

  depends_on = [module.vpc]
}

# ---- Outputs ----

output "lambda_arn" {
  description = "ARN of the archigraph Extract Lambda function"
  value       = aws_lambda_function.memx_extract.arn
}

output "lambda_function_name" {
  description = "Name of the archigraph Extract Lambda function"
  value       = aws_lambda_function.memx_extract.function_name
}

output "sqs_queue_url" {
  description = "URL of the SQS extract queue"
  value       = aws_sqs_queue.extract_queue.url
}

output "ecr_repository_url" {
  description = "ECR repository URL for archigraph extract image"
  value       = aws_ecr_repository.memx_extract.repository_url
}

output "vpc_id" {
  description = "VPC ID"
  value       = module.vpc.vpc_id
}
