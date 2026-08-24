terraform {
  required_version = "= 1.15.9"
  required_providers {
    aws = { source = "hashicorp/aws", version = "= 6.60.0" }
  }
  backend "s3" {}
}

provider "aws" {
  region = var.aws_region
  default_tags { tags = var.tags }
}

variable "aws_region" {
  type    = string
  default = "sa-east-1"
}
variable "name_prefix" {
  type    = string
  default = "sentinelops-prod"
}
variable "artifact_bucket_name" { type = string }
variable "kms_key_arn" { type = string }
variable "database_subnet_ids" { type = list(string) }
variable "database_security_group_ids" { type = list(string) }
variable "tags" {
  type    = map(string)
  default = { Environment = "prod", Owner = "platform", Project = "sentinelops" }
}

module "platform_data" {
  source                         = "../../modules/platform-data"
  name_prefix                    = var.name_prefix
  artifact_bucket_name           = var.artifact_bucket_name
  kms_key_arn                    = var.kms_key_arn
  database_subnet_ids            = var.database_subnet_ids
  database_security_group_ids    = var.database_security_group_ids
  database_instance_class        = "db.m7g.xlarge"
  database_allocated_storage     = 200
  database_max_allocated_storage = 2000
  database_backup_retention_days = 35
  database_multi_az              = true
  artifact_retention_days        = 365
  tags                           = var.tags
}

output "database_endpoint" { value = module.platform_data.database_endpoint }
output "database_master_secret_arn" {
  value     = module.platform_data.database_master_secret_arn
  sensitive = true
}
output "artifact_bucket_name" { value = module.platform_data.artifact_bucket_name }
