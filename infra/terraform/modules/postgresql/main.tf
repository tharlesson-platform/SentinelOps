variable "identifier" { type = string }
variable "subnet_ids" { type = list(string) }
variable "security_group_ids" { type = list(string) }
variable "kms_key_id" { type = string }
variable "instance_class" {
  type    = string
  default = "db.m7g.large"
}
variable "tags" { type = map(string) }

resource "aws_db_subnet_group" "this" {
  name       = var.identifier
  subnet_ids = var.subnet_ids
  tags       = var.tags
}

resource "aws_db_instance" "this" {
  identifier                      = var.identifier
  engine                          = "postgres"
  engine_version                  = "18.3"
  instance_class                  = var.instance_class
  allocated_storage               = 100
  max_allocated_storage           = 1000
  storage_type                    = "gp3"
  storage_encrypted               = true
  kms_key_id                      = var.kms_key_id
  db_name                         = "sentinel"
  username                        = "sentinel_admin"
  manage_master_user_password     = true
  db_subnet_group_name            = aws_db_subnet_group.this.name
  vpc_security_group_ids          = var.security_group_ids
  publicly_accessible             = false
  multi_az                        = true
  backup_retention_period         = 14
  copy_tags_to_snapshot           = true
  deletion_protection             = true
  skip_final_snapshot             = false
  final_snapshot_identifier       = "${var.identifier}-final"
  auto_minor_version_upgrade      = true
  apply_immediately               = false
  performance_insights_enabled    = true
  monitoring_interval             = 0
  enabled_cloudwatch_logs_exports = ["postgresql", "upgrade"]
  tags                            = merge(var.tags, { Service = "SentinelOps", ManagedBy = "Terraform" })
  lifecycle { prevent_destroy = true }
}

output "endpoint" { value = aws_db_instance.this.endpoint }
output "master_secret_arn" {
  value     = one(aws_db_instance.this.master_user_secret).secret_arn
  sensitive = true
}

