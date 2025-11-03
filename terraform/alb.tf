resource "aws_lb" "this" {
  name               = "${var.project_name}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb_sg.id]
  subnets            = local.public_subnet_ids
}

# 改这里：用 name_prefix + Fargate 需要 ip 类型 + 允许先创建后删除
resource "aws_lb_target_group" "receiver_tg" {
  name_prefix = "tg-"   # 改：用前缀，允许并存旧/新 TG
  port        = var.container_port
  protocol    = "HTTP"
  vpc_id      = local.vpc_id
  target_type = "ip"                         # 关键：Fargate/awsvpc 必须是 ip

  health_check {
    path                = "/health"
    healthy_threshold   = 2
    unhealthy_threshold = 2
    interval            = 10
    timeout             = 5
    matcher             = "200"
  }

  lifecycle {
    create_before_destroy = true             # 先建新 TG，再删旧 TG
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.receiver_tg.arn
  }
}