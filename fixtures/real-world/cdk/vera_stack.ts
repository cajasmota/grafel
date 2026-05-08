// Source: https://github.com/aws-samples/aws-cdk-examples (synthetic based on real AWS CDK patterns) | License: Apache-2.0

import * as cdk from 'aws-cdk-lib';
import { Construct } from 'constructs';
import * as lambda from 'aws-cdk-lib/aws-lambda';
import * as sqs from 'aws-cdk-lib/aws-sqs';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as logs from 'aws-cdk-lib/aws-logs';
import * as secretsmanager from 'aws-cdk-lib/aws-secretsmanager';
import * as lambdaEventSources from 'aws-cdk-lib/aws-lambda-event-sources';
import * as ecr from 'aws-cdk-lib/aws-ecr';

export interface VeraIndexerStackProps extends cdk.StackProps {
  environment: 'dev' | 'staging' | 'prod';
  ecrRepository: ecr.IRepository;
  imageTag: string;
}

export class VeraIndexerStack extends cdk.Stack {
  public readonly indexerFunction: lambda.Function;
  public readonly indexQueue: sqs.Queue;
  public readonly dlq: sqs.Queue;

  constructor(scope: Construct, id: string, props: VeraIndexerStackProps) {
    super(scope, id, props);

    const { environment, ecrRepository, imageTag } = props;
    const isProd = environment === 'prod';

    // ============================================================
    // Secrets
    // ============================================================
    const appSecrets = secretsmanager.Secret.fromSecretNameV2(
      this,
      'AppSecrets',
      `${environment}/archigraph-indexer`
    );

    // ============================================================
    // Dead Letter Queue
    // ============================================================
    this.dlq = new sqs.Queue(this, 'IndexerDLQ', {
      queueName: `archigraph-indexer-dlq-${environment}`,
      retentionPeriod: cdk.Duration.days(14),
      encryption: sqs.QueueEncryption.SQS_MANAGED,
    });

    // ============================================================
    // Main Queue
    // ============================================================
    this.indexQueue = new sqs.Queue(this, 'IndexerQueue', {
      queueName: `archigraph-indexer-${environment}`,
      visibilityTimeout: cdk.Duration.seconds(300),
      retentionPeriod: cdk.Duration.days(4),
      encryption: sqs.QueueEncryption.SQS_MANAGED,
      deadLetterQueue: {
        maxReceiveCount: 3,
        queue: this.dlq,
      },
    });

    // ============================================================
    // Lambda Execution Role
    // ============================================================
    const executionRole = new iam.Role(this, 'IndexerRole', {
      assumedBy: new iam.ServicePrincipal('lambda.amazonaws.com'),
      managedPolicies: [
        iam.ManagedPolicy.fromAwsManagedPolicyName(
          'service-role/AWSLambdaBasicExecutionRole'
        ),
      ],
    });

    appSecrets.grantRead(executionRole);
    this.indexQueue.grantConsumeMessages(executionRole);
    ecrRepository.grantPull(executionRole);

    // ============================================================
    // Lambda Function
    // ============================================================
    this.indexerFunction = new lambda.DockerImageFunction(this, 'IndexerFunction', {
      functionName: `archigraph-indexer-${environment}`,
      code: lambda.DockerImageCode.fromEcr(ecrRepository, {
        tagOrDigest: imageTag,
      }),
      role: executionRole,
      memorySize: isProd ? 1024 : 512,
      timeout: cdk.Duration.seconds(300),
      environment: {
        DEPLOY_ENV: environment,
        LOG_LEVEL: isProd ? 'warn' : 'debug',
        SECRET_ARN: appSecrets.secretArn,
        QUEUE_URL: this.indexQueue.queueUrl,
        POWERTOOLS_SERVICE_NAME: 'archigraph-indexer',
        POWERTOOLS_LOG_LEVEL: isProd ? 'WARN' : 'DEBUG',
      },
      logRetention: isProd
        ? logs.RetentionDays.THREE_MONTHS
        : logs.RetentionDays.ONE_WEEK,
      tracing: lambda.Tracing.ACTIVE,
      reservedConcurrentExecutions: isProd ? 50 : 10,
    });

    // ============================================================
    // SQS Event Source
    // ============================================================
    this.indexerFunction.addEventSource(
      new lambdaEventSources.SqsEventSource(this.indexQueue, {
        batchSize: 10,
        maxBatchingWindow: cdk.Duration.seconds(30),
        reportBatchItemFailures: true,
      })
    );

    // ============================================================
    // CloudWatch Alarms (prod only)
    // ============================================================
    if (isProd) {
      const dlqAlarm = this.dlq
        .metricNumberOfMessagesSent()
        .createAlarm(this, 'DLQAlarm', {
          alarmName: `archigraph-indexer-dlq-messages-${environment}`,
          threshold: 1,
          evaluationPeriods: 1,
          alarmDescription: 'Messages landing in archigraph-indexer DLQ',
        });
    }

    // ============================================================
    // Stack Outputs
    // ============================================================
    new cdk.CfnOutput(this, 'IndexQueueUrl', {
      value: this.indexQueue.queueUrl,
      exportName: `archigraph-indexer-queue-url-${environment}`,
    });

    new cdk.CfnOutput(this, 'IndexerFunctionArn', {
      value: this.indexerFunction.functionArn,
      exportName: `archigraph-indexer-function-arn-${environment}`,
    });
  }
}
