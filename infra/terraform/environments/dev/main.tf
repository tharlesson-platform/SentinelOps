terraform {
  required_version = "= 1.15.4"
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
variable "tags" {
  type    = map(string)
  default = { Environment = "dev", Owner = "platform", Project = "sentinelops" }
}

# Recursos reais são deliberadamente compostos pelo operador após preencher
# backend, VPC, KMS e naming. Nenhum apply é executado pelo repositório.

