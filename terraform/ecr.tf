########################################################
# ECR: repository + lifecycle policy + helpful outputs #
########################################################

# 创建 ECR 仓库（名字用 project_name，当前为 hw07）
resource "aws_ecr_repository" "this" {
  name                 = var.project_name
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = {
    Project = var.project_name
  }
}




# 便利输出：仓库名字与完整 URL
output "ecr_repository_name" {
  description = "ECR repository name"
  value       = aws_ecr_repository.this.name
}

output "ecr_repository_url" {
  description = "ECR repository URL for tagging/pushing"
  value       = aws_ecr_repository.this.repository_url
}

# 贴心输出：直接拷贝即可的 push 命令（使用当前 region）
output "ecr_push_instructions" {
  value = <<EOT
Login, build, tag and push:

aws ecr get-login-password --region ${var.aws_region} | docker login --username AWS --password-stdin ${aws_ecr_repository.this.repository_url}
docker build -t ${var.project_name} ../src
docker tag ${var.project_name}:latest ${aws_ecr_repository.this.repository_url}:latest
docker push ${aws_ecr_repository.this.repository_url}:latest
EOT
}