resource "aws_sns_topic" "orders" {
  name = "${var.project_name}-order-events"
}

resource "aws_sqs_queue" "orders" {
  name                       = "${var.project_name}-order-queue"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 345600 # 4 days
  receive_wait_time_seconds  = 20     # long polling
}

# 允许 SNS 向队列投递
data "aws_iam_policy_document" "sqs_policy" {
  statement {
    actions = ["sqs:SendMessage"]
    effect  = "Allow"

    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }

    resources = [aws_sqs_queue.orders.arn]

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.orders.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "orders" {
  queue_url = aws_sqs_queue.orders.url
  policy    = data.aws_iam_policy_document.sqs_policy.json
}

resource "aws_sns_topic_subscription" "orders_to_queue" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.orders.arn
}