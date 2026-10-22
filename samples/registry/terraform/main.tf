# The tenant's registry, a connection for one existing cluster so every pod
# in it can pull, and a push access token for CI.
#
# deletion_protection is left at its default, true: the platform refuses to
# delete the registry, and every image in it. To delete it, set
# deletion_protection = false on kupe_registry, apply, then destroy.

terraform {
  required_providers {
    kupe = {
      source = "kupecloud/kupe"
    }
  }
}

provider "kupe" {
  host   = "https://api.kupe.cloud"
  tenant = var.tenant
  # api_key is read from KUPE_API_KEY
}

variable "tenant" {
  description = "Your Kupe tenant name."
  type        = string
}

variable "cluster_name" {
  description = "Name of an existing cluster to connect to the registry."
  type        = string
}

resource "kupe_registry" "this" {}

# Every pod of the cluster can pull from the registry, with no
# imagePullSecrets. Pods running before the connection need a restart.
resource "kupe_registry_binding" "this" {
  registry = kupe_registry.this.name
  cluster  = var.cluster_name
}

# A push token for CI. Its secret is stored in Terraform state in plain
# text: keep state in an encrypted backend with restricted access.
resource "kupe_registry_token" "ci" {
  registry        = kupe_registry.this.name
  display_name    = "hello-kupe-ci"
  access          = "push"
  expires_in_days = 90
}

output "registry_endpoint" {
  description = "Image path prefix, e.g. registry.kupe.cloud/acme."
  value       = kupe_registry.this.endpoint
}

output "ci_username" {
  description = "Registry username of the CI token."
  value       = kupe_registry_token.ci.username
}

output "ci_secret" {
  description = "Registry secret of the CI token (kupecr_...)."
  value       = kupe_registry_token.ci.secret
  sensitive   = true
}
