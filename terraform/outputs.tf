output "alb_dns" { value = aws_lb.this.dns_name }
output "sns_topic_arn" { value = aws_sns_topic.orders.arn }
output "sqs_queue_url" { value = aws_sqs_queue.orders.url }
output "sqs_queue_arn" { value = aws_sqs_queue.orders.arn }

output "rds_endpoint" {
  value = aws_db_instance.cart.address
}