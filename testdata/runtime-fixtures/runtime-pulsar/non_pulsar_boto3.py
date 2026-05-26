"""
Non-Pulsar boto3 fixture — testdata/runtime-fixtures/runtime-pulsar/non_pulsar_boto3.py

Must NOT produce any Pulsar edges. boto3 SQS uses create_queue / send_message
with a completely different call signature; the Pulsar pass must ignore it.
"""
import boto3

sqs = boto3.client('sqs', region_name='us-east-1')


def send_sqs(message: str) -> None:
    url = sqs.create_queue(QueueName='orders')['QueueUrl']
    sqs.send_message(QueueUrl=url, MessageBody=message)
