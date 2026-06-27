output "dev_server_public_ip" {
  description = "Public IP address of dev server"
  value       = aws_eip.dev_server.public_ip
}

output "dev_server_instance_id" {
  description = "Instance ID of dev server"
  value       = aws_instance.dev_server.id
}

output "dev_server_private_ip" {
  description = "Private IP address of dev server"
  value       = aws_instance.dev_server.private_ip
}

output "rds_endpoint" {
  description = "RDS instance endpoint"
  value       = aws_db_instance.dev.endpoint
}

output "rds_address" {
  description = "RDS instance address (without port)"
  value       = aws_db_instance.dev.address
}

output "database_url" {
  description = "PostgreSQL connection string for applications"
  value       = "postgresql://${var.db_username}:${var.db_password}@${aws_db_instance.dev.address}:5432/${var.db_name}?sslmode=require"
  sensitive   = true
}

output "s3_bucket_binaries" {
  description = "S3 bucket name for binaries"
  value       = aws_s3_bucket.dev_binaries.id
}

output "s3_bucket_artifacts" {
  description = "S3 bucket name for artifacts"
  value       = aws_s3_bucket.dev_artifacts.id
}

output "dev_domain" {
  description = "Dev environment domain name"
  value       = aws_route53_record.dev.fqdn
}

output "ssh_private_key" {
  description = "SSH private key for dev server access"
  value       = tls_private_key.dev_key.private_key_pem
  sensitive   = true
}

output "ssh_command" {
  description = "SSH command to connect to dev server"
  value       = "ssh -i ~/.ssh/${var.key_name} ubuntu@${aws_eip.dev_server.public_ip}"
}

# Instructions for next steps
output "next_steps" {
  description = "Next steps after infrastructure is created"
  sensitive   = true
  value = <<-EOT

    === OCPCTL Dev Environment Created Successfully ===

    1. Save SSH private key:
       terraform output -raw ssh_private_key > ~/.ssh/${var.key_name}
       chmod 600 ~/.ssh/${var.key_name}

    2. SSH to dev server:
       ssh -i ~/.ssh/${var.key_name} ubuntu@${aws_eip.dev_server.public_ip}

    3. Update deploy-env.sh with dev server IP:
       API_HOST="${aws_eip.dev_server.public_ip}"

    4. Create config files from templates:
       cp config/api.env.dev.template config/api.env.dev
       cp config/worker.env.dev.template config/worker.env.dev

    5. Update config files with:
       DATABASE_URL: postgresql://${var.db_username}:${var.db_password}@${aws_db_instance.dev.address}:5432/${var.db_name}?sslmode=require
       S3_BUCKET_NAME: ${aws_s3_bucket.dev_binaries.id}

    6. Bootstrap dev server:
       ./scripts/bootstrap-dev-server.sh ${aws_eip.dev_server.public_ip}

    7. Initialize database:
       ./scripts/init-dev-database.sh

    8. Deploy to dev:
       ./scripts/deploy-env.sh dev

    9. Access dev environment:
       https://${aws_route53_record.dev.fqdn}

    === End ===
  EOT
}
