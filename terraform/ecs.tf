resource "aws_ecs_cluster" "this" {
  name = "${var.project_name}-cluster"
}

resource "aws_cloudwatch_log_group" "ecs" {
  name              = "/ecs/${var.project_name}"
  retention_in_days = 7
}

# Receiver：对外 HTTP（/orders/sync + /orders/async + 你将实现的 /shopping-carts*）
resource "aws_ecs_task_definition" "receiver" {
  family                   = "${var.project_name}-receiver"
  cpu                      = "256"
  memory                   = "512"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  # execution_role_arn: used for ECR pull and CloudWatch Logs
  execution_role_arn       = "arn:aws:iam::211125751164:role/LabRole"
  # task_role_arn: used by the container to access AWS services (DynamoDB, etc.)
  task_role_arn            = "arn:aws:iam::211125751164:role/LabRole"

  container_definitions = jsonencode([
    {
      name         = "receiver"
      image        = var.receiver_image
      essential    = true
      portMappings = [
        { containerPort = var.container_port, hostPort = var.container_port, protocol = "tcp" }
      ]

      # === 新增：MySQL/RDS 连接所需环境变量 ===
      environment = [
        # Basic configuration
        { name = "PORT",              value = tostring(var.container_port) },
        { name = "PAYMENT_PERMITS",   value = tostring(var.payment_permits) },
        { name = "AWS_REGION",        value = var.aws_region },

        # SNS/SQS configuration
        { name = "SNS_TOPIC_ARN", value = aws_sns_topic.orders.arn },
        { name = "SQS_QUEUE_URL", value = aws_sqs_queue.orders.url },
        { name = "SQS_QUEUE_ARN", value = aws_sqs_queue.orders.arn },

        # Backend selection: "mysql" or "dynamodb"
        { name = "DB_BACKEND",        value = var.db_backend },

        # MySQL/RDS configuration (used when DB_BACKEND=mysql)
        { name = "DB_HOST",           value = aws_db_instance.cart.address },
        { name = "DB_USER",           value = var.db_user },
        { name = "DB_PASS",           value = var.db_pass },
        { name = "DB_NAME",           value = var.db_name },
        { name = "DB_MAX_OPEN_CONNS", value = "40" },
        { name = "DB_MAX_IDLE_CONNS", value = "20" },

        # DynamoDB configuration (used when DB_BACKEND=dynamodb)
        { name = "DYNAMODB_TABLE_NAME", value = aws_dynamodb_table.shopping_carts.name }
      ]

      # logConfiguration removed - requires execution role with PassRole permission
      # Container logs will go to stdout (viewable via ECS console or aws ecs describe-tasks)
    }
  ])
}

resource "aws_ecs_service" "receiver" {
  name            = "${var.project_name}-receiver-svc"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.receiver.arn
  desired_count   = var.desired_count_receiver
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = local.private_subnet_ids
    security_groups  = [aws_security_group.ecs_sg.id]
    assign_public_ip = false
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.receiver_tg.arn
    container_name   = "receiver"
    container_port   = var.container_port
  }

  depends_on = [aws_lb_listener.http]
}

# Processor：后台 worker（SQS 消费）
resource "aws_ecs_task_definition" "processor" {
  family                   = "${var.project_name}-processor"
  cpu                      = "256"
  memory                   = "512"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  # execution_role_arn: used for ECR pull and CloudWatch Logs
  execution_role_arn       = "arn:aws:iam::211125751164:role/LabRole"
  # task_role_arn: used by the container to access AWS services (DynamoDB, SQS, etc.)
  task_role_arn            = "arn:aws:iam::211125751164:role/LabRole"

  container_definitions = jsonencode([
    {
      name      = "processor"
      image     = var.processor_image
      essential = true

      # Processor 不对外暴露端口，但你之前保留了 PORT；保留不影响
      environment = [
        { name = "PORT",               value = tostring(var.container_port) },
        { name = "PAYMENT_PERMITS",    value = tostring(var.payment_permits) },
        { name = "WORKER_GOROUTINES",  value = tostring(var.worker_goroutines) },
        { name = "AWS_REGION",         value = var.aws_region },

        # SQS/SNS（如仍需要）
        { name = "SNS_TOPIC_ARN", value = aws_sns_topic.orders.arn },
        { name = "SQS_QUEUE_URL", value = aws_sqs_queue.orders.url },

        # 如果 Processor 也需要访问 DB（例如写入统计/异步加购物车），保留；否则可删除以下 6 行
        { name = "DB_HOST",            value = aws_db_instance.cart.address },
        { name = "DB_USER",            value = var.db_user },
        { name = "DB_PASS",            value = var.db_pass },
        { name = "DB_NAME",            value = var.db_name },
        { name = "DB_MAX_OPEN_CONNS",  value = "10" },
        { name = "DB_MAX_IDLE_CONNS",  value = "5" }
      ]

      # logConfiguration removed - requires execution role with PassRole permission
      # Container logs will go to stdout (viewable via ECS console or aws ecs describe-tasks)
    }
  ])
}

resource "aws_ecs_service" "processor" {
  name            = "${var.project_name}-processor-svc"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.processor.arn
  desired_count   = var.desired_count_processor
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = local.private_subnet_ids
    security_groups  = [aws_security_group.ecs_sg.id]
    assign_public_ip = false
  }
}