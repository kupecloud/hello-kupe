# Optional build credential. Database credentials never enter Terraform state.
resource "kupe_registry_token" "build" {
  count = var.enable_build_token ? 1 : 0

  registry        = one(kupe_registry.app[*].name)
  display_name    = "hello-kupe-build"
  access          = "push"
  expires_in_days = 7

  lifecycle {
    precondition {
      condition     = var.enable_registry
      error_message = "enable_build_token requires enable_registry = true."
    }
  }
}

# Preserve credentials created by the previous optional push-token.tf example.
moved {
  from = kupe_registry_token.build
  to   = kupe_registry_token.build[0]
}

output "registry_build_username" {
  value = one(kupe_registry_token.build[*].username)
}

output "registry_build_secret" {
  value     = one(kupe_registry_token.build[*].secret)
  sensitive = true
}
