# DynamoDB table for shopping carts with embedded items (single-table design)
resource "aws_dynamodb_table" "shopping_carts" {
  name           = "${var.project_name}-shopping-carts"
  billing_mode   = "PAY_PER_REQUEST"  # On-demand pricing for unpredictable workloads
  hash_key       = "cart_id"

  attribute {
    name = "cart_id"
    type = "S"  # String type for cart_id
  }

  # Enable point-in-time recovery for production use
  point_in_time_recovery {
    enabled = false  # Disabled for cost savings in lab environment
  }

  # Server-side encryption using AWS managed keys
  server_side_encryption {
    enabled = true
  }

  tags = {
    Name        = "${var.project_name}-shopping-carts"
    Environment = var.environment
    ManagedBy   = "terraform"
  }
}

# Output the DynamoDB table name for ECS task configuration
output "dynamodb_table_name" {
  description = "Name of the DynamoDB shopping carts table"
  value       = aws_dynamodb_table.shopping_carts.name
}

output "dynamodb_table_arn" {
  description = "ARN of the DynamoDB shopping carts table"
  value       = aws_dynamodb_table.shopping_carts.arn
}
