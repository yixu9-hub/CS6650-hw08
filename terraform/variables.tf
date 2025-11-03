variable "project_name" { default = "ecommerce-go" }
variable "aws_region" { default = "us-west-2" }

# 你的企业账户里已经存在的角色（不创建/不修改 IAM）
variable "ecs_task_execution_role_arn" { type = string }
variable "ecs_task_role_arn" { type = string }

# 镜像（两个服务可用同一镜像）
variable "receiver_image" { type = string }
variable "processor_image" { type = string }

# 任务与应用参数
variable "container_port" { default = 8080 }
variable "desired_count_receiver" { default = 1 }
variable "desired_count_processor" { default = 1 }
variable "worker_goroutines" { default = 1 }
variable "payment_permits" { default = 15 }

variable "db_user" {
  description = "MySQL username for the shopping cart DB"
  type        = string
  default     = "cartuser"
}

variable "db_pass" {
  description = "MySQL password for the shopping cart DB"
  type        = string
  sensitive   = true
  default     = "cartpass123!"
}

variable "db_name" {
  description = "MySQL database name"
  type        = string
  default     = "cartdb"
}

variable "db_backend" {
  description = "Database backend to use: 'mysql' or 'dynamodb'"
  type        = string
  default     = "mysql"
}

variable "environment" {
  description = "Environment name (e.g., dev, staging, prod)"
  type        = string
  default     = "dev"
}
