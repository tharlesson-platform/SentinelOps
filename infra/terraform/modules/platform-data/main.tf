variable "name_prefix" { type = string }
variable "artifact_bucket_name" { type = string }
variable "kms_key_arn" { type = string }
variable "database_subnet_ids" { type = list(string) }
variable "database_security_group_ids" { type = list(string) }
variable "database_instance_class" { type = string }
variable "database_allocated_storage" { type = number }
variable "database_max_allocated_storage" { type = number }
variable "database_backup_retention_days" { type = number }
variable "database_multi_az" { type = bool }
variable "artifact_retention_days" { type = number }
variable "tags" {
  type = map(string)
  validation {
    condition = alltrue([
      for key in ["Environment", "Owner", "Project"] :
      length(trimspace(lookup(var.tags, key, ""))) > 0
    ])
    error_message = "tags must contain non-empty Environment, Owner, and Project values."
  }
}

module "postgresql" {
  source                                = "../postgresql"
  identifier                            = "${var.name_prefix}-postgres"
  subnet_ids                            = var.database_subnet_ids
  security_group_ids                    = var.database_security_group_ids
  kms_key_id                            = var.kms_key_arn
  instance_class                        = var.database_instance_class
  allocated_storage                     = var.database_allocated_storage
  max_allocated_storage                 = var.database_max_allocated_storage
  backup_retention_period               = var.database_backup_retention_days
  multi_az                              = var.database_multi_az
  deletion_protection                   = true
  performance_insights_retention_period = 31
  tags                                  = var.tags
}

module "artifact_storage" {
  source                    = "../object-storage"
  bucket_name               = var.artifact_bucket_name
  kms_key_arn               = var.kms_key_arn
  retention_days            = var.artifact_retention_days
  noncurrent_retention_days = 30
  tags                      = var.tags
}

output "database_endpoint" { value = module.postgresql.endpoint }
output "database_port" { value = module.postgresql.port }
output "database_name" { value = module.postgresql.database_name }
output "database_master_secret_arn" {
  value     = module.postgresql.master_secret_arn
  sensitive = true
}
output "artifact_bucket_name" { value = module.artifact_storage.bucket_name }
output "artifact_bucket_arn" { value = module.artifact_storage.bucket_arn }
