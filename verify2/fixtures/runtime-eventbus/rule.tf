# EventBridge rule fixture — #927 acceptance criteria.
#
# Acceptance: Terraform aws_cloudwatch_event_rule with event_pattern matching
# 'orders' + 'OrderPlaced' and target aws_lambda_function.process_order →
# SUBSCRIBES_TO same synthetic event:eventbridge:orders:OrderPlaced;
# EVENTBRIDGE_TRIGGERS edge into the lambda handler.

resource "aws_cloudwatch_event_rule" "process_order" {
  name        = "process-order-rule"
  description = "Route OrderPlaced events to the process_order Lambda"

  event_pattern = "{\"source\":[\"orders\"],\"detail-type\":[\"OrderPlaced\"]}"
}

resource "aws_cloudwatch_event_target" "order_lambda" {
  rule = aws_cloudwatch_event_rule.process_order.name
  arn  = aws_lambda_function.process_order.arn
}
