# Optional: a tenant has one registry, shared by its applications.
resource "kupe_registry" "app" {
  count = var.enable_registry ? 1 : 0

  name                = "registry"
  deletion_protection = true

  lifecycle {
    prevent_destroy = true
  }
}

# Pull access only. Build and push images using a separate CLI/CI credential.
resource "kupe_registry_binding" "app" {
  count = var.enable_registry ? 1 : 0

  registry = kupe_registry.app[0].name
  cluster  = kupe_cluster.todo.name
}
