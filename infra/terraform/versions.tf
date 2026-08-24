terraform {
  required_version = "= 1.15.9"
  required_providers {
    aws        = { source = "hashicorp/aws", version = "= 6.60.0" }
    azurerm    = { source = "hashicorp/azurerm", version = "= 5.1.0" }
    kubernetes = { source = "hashicorp/kubernetes", version = "= 3.2.1" }
    random     = { source = "hashicorp/random", version = "= 3.9.0" }
  }
}
