variable "identifier" { type = string }
variable "subnet_ids" {
  type = list(string)
  validation {
    condition     = length(var.subnet_ids) >= 2
    error_message = "Use at least two private subnets in distinct availability zones."
  }
}
variable "security_group_ids" {
  type = list(string)
  validation {
    condition     = length(var.security_group_ids) > 0
    error_message = "At least one restrictive database security group is required."
  }
}
variable "kms_key_id" { type = string }
variable "instance_class" {
  type    = string
  default = "db.m7g.large"
}
variable "allocated_storage" {
  type    = number
  default = 100
}
variable "max_allocated_storage" {
  type    = number
  default = 1000
}
variable "backup_retention_period" {
  type    = number
  default = 35
}
variable "multi_az" {
  type    = bool
  default = true
}
variable "deletion_protection" {
  type    = bool
  default = true
}
variable "performance_insights_retention_period" {
  type    = number
  default = 31
  validation {
    condition     = contains([7, 31, 93, 186, 372, 731], var.performance_insights_retention_period)
    error_message = "Use an RDS-supported Performance Insights retention period."
  }
}
variable "tags" { type = map(string) }

data "aws_partition" "current" {}

data "aws_subnet" "selected" {
  for_each = toset(var.subnet_ids)
  id       = each.value
}

data "aws_security_group" "selected" {
  for_each = toset(var.security_group_ids)
  id       = each.value
}

locals {
  database_ingress_rules = flatten([
    for group in values(data.aws_security_group.selected) : group.ingress
  ])
}

resource "aws_iam_role" "enhanced_monitoring" {
  name = "${var.identifier}-rds-monitoring"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "monitoring.rds.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
  tags = merge(var.tags, { Service = "SentinelOps", ManagedBy = "Terraform" })
}

resource "aws_iam_role_policy_attachment" "enhanced_monitoring" {
  role       = aws_iam_role.enhanced_monitoring.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/service-role/AmazonRDSEnhancedMonitoringRole"
}

resource "aws_db_subnet_group" "this" {
  name       = var.identifier
  subnet_ids = var.subnet_ids
  tags       = merge(var.tags, { Name = var.identifier })
  lifecycle {
    precondition {
      condition     = length(toset([for subnet in values(data.aws_subnet.selected) : subnet.availability_zone])) >= 2
      error_message = "Database subnets must span at least two availability zones."
    }
    precondition {
      condition     = alltrue([for subnet in values(data.aws_subnet.selected) : !subnet.map_public_ip_on_launch])
      error_message = "Database subnets must not auto-assign public IP addresses."
    }
  }
}

resource "aws_db_parameter_group" "this" {
  name   = "${var.identifier}-postgres18"
  family = "postgres18"
  parameter {
    name  = "rds.force_ssl"
    value = "1"
  }
  parameter {
    name  = "log_connections"
    value = "1"
  }
  parameter {
    name  = "log_disconnections"
    value = "1"
  }
  parameter {
    name  = "log_lock_waits"
    value = "1"
  }
  parameter {
    name  = "log_min_duration_statement"
    value = "1000"
  }
  tags = merge(var.tags, { Service = "SentinelOps", ManagedBy = "Terraform" })
}

resource "aws_cloudwatch_log_group" "postgresql" {
  name              = "/aws/rds/instance/${var.identifier}/postgresql"
  retention_in_days = 90
  kms_key_id        = var.kms_key_id
  tags              = merge(var.tags, { Service = "SentinelOps", ManagedBy = "Terraform" })
  lifecycle { prevent_destroy = true }
}

resource "aws_cloudwatch_log_group" "upgrade" {
  name              = "/aws/rds/instance/${var.identifier}/upgrade"
  retention_in_days = 90
  kms_key_id        = var.kms_key_id
  tags              = merge(var.tags, { Service = "SentinelOps", ManagedBy = "Terraform" })
  lifecycle { prevent_destroy = true }
}

resource "aws_db_instance" "this" {
  identifier                            = var.identifier
  engine                                = "postgres"
  engine_version                        = "18.3"
  instance_class                        = var.instance_class
  allocated_storage                     = var.allocated_storage
  max_allocated_storage                 = var.max_allocated_storage
  storage_type                          = "gp3"
  storage_encrypted                     = true
  kms_key_id                            = var.kms_key_id
  db_name                               = "sentinel"
  username                              = "sentinel_migration"
  manage_master_user_password           = true
  master_user_secret_kms_key_id         = var.kms_key_id
  db_subnet_group_name                  = aws_db_subnet_group.this.name
  parameter_group_name                  = aws_db_parameter_group.this.name
  vpc_security_group_ids                = var.security_group_ids
  publicly_accessible                   = false
  multi_az                              = var.multi_az
  backup_retention_period               = var.backup_retention_period
  backup_window                         = "02:00-03:00"
  maintenance_window                    = "sun:04:00-sun:05:00"
  copy_tags_to_snapshot                 = true
  delete_automated_backups              = false
  deletion_protection                   = var.deletion_protection
  skip_final_snapshot                   = false
  final_snapshot_identifier             = "${var.identifier}-final"
  auto_minor_version_upgrade            = true
  allow_major_version_upgrade           = false
  apply_immediately                     = false
  performance_insights_enabled          = true
  performance_insights_kms_key_id       = var.kms_key_id
  performance_insights_retention_period = var.performance_insights_retention_period
  monitoring_interval                   = 60
  monitoring_role_arn                   = aws_iam_role.enhanced_monitoring.arn
  enabled_cloudwatch_logs_exports       = ["postgresql", "upgrade"]
  tags                                  = merge(var.tags, { Service = "SentinelOps", ManagedBy = "Terraform" })
  depends_on = [
    aws_cloudwatch_log_group.postgresql,
    aws_cloudwatch_log_group.upgrade,
    aws_iam_role_policy_attachment.enhanced_monitoring,
  ]
  lifecycle {
    prevent_destroy = true
    precondition {
      condition     = var.max_allocated_storage >= var.allocated_storage
      error_message = "max_allocated_storage must be greater than or equal to allocated_storage."
    }
    precondition {
      condition = length(local.database_ingress_rules) > 0 && alltrue([
        for rule in local.database_ingress_rules :
        rule.protocol == "tcp" && rule.from_port == 5432 && rule.to_port == 5432 &&
        !contains(rule.cidr_blocks, "0.0.0.0/0") &&
        !contains(rule.ipv6_cidr_blocks, "::/0")
      ])
      error_message = "Database security groups must expose only TCP/5432 and must not allow global IPv4/IPv6 CIDRs."
    }
  }
}

output "endpoint" { value = aws_db_instance.this.endpoint }
output "port" { value = aws_db_instance.this.port }
output "database_name" { value = aws_db_instance.this.db_name }
output "master_secret_arn" {
  value     = one(aws_db_instance.this.master_user_secret).secret_arn
  sensitive = true
}
