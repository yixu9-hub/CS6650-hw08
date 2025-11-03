########################################
# RDS MySQL for Shopping Cart (private)
########################################

# 放到你已有的私有子网
resource "aws_db_subnet_group" "cart" {
  name       = "${var.project_name}-rds-subnets"
  subnet_ids = local.private_subnet_ids
  tags = { Project = var.project_name }
}

# 只允许 ECS Service 的 SG 访问 3306（SG 对 SG）
resource "aws_security_group" "cart_rds_sg" {
  name        = "${var.project_name}-rds-sg"
  description = "Allow MySQL 3306 from ECS service SG only"
  vpc_id      = local.vpc_id

  ingress {
    description     = "MySQL from ECS service"
    protocol        = "tcp"
    from_port       = 3306
    to_port         = 3306
    security_groups = [aws_security_group.ecs_sg.id]
  }

  egress {
    protocol    = "-1"
    from_port   = 0
    to_port     = 0
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Project = var.project_name }
}

# RDS MySQL 实例（作业要求）
resource "aws_db_instance" "cart" {
  identifier             = "${var.project_name}-mysql"
  engine                 = "mysql"
  engine_version         = "8.0"
  instance_class         = "db.t3.micro"
  allocated_storage      = 20

  db_subnet_group_name   = aws_db_subnet_group.cart.name
  vpc_security_group_ids = [aws_security_group.cart_rds_sg.id]
  publicly_accessible    = false

  username               = var.db_user
  password               = var.db_pass
  db_name                = var.db_name

  deletion_protection    = false
  skip_final_snapshot    = true
  apply_immediately      = true

  tags = { Project = var.project_name }
}