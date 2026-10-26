output "database_host" {
  description = "In-cluster DNS name the application namespace connects to."
  value       = one([for ns in kupe_database_binding.readwrite.namespace_status : ns.host if ns.name == var.app_namespace])
}

output "owner_secret_name" {
  description = "Secret in the migration namespace mounted by the migration Job."
  value       = kupe_database_binding.owner.secret_name
}

output "readwrite_secret_name" {
  description = "Secret in the application namespace mounted by the API."
  value       = kupe_database_binding.readwrite.secret_name
}

output "cluster_name" {
  description = "Cluster name to pass to the app Helm chart."
  value       = kupe_cluster.todo.name
}

output "registry_endpoint" {
  description = "Image path prefix when the managed registry is enabled."
  value       = var.enable_registry ? kupe_registry.app[0].endpoint : null
}

output "deployment" {
  description = "Non-secret configuration used by make cloud-kubeconfig and make cloud-deploy."
  value = {
    tenant                = var.tenant
    cluster               = kupe_cluster.todo.name
    app_namespace         = var.app_namespace
    migration_namespace   = var.migration_namespace
    owner_secret_name     = kupe_database_binding.owner.secret_name
    readwrite_secret_name = kupe_database_binding.readwrite.secret_name
    registry_endpoint     = var.enable_registry ? kupe_registry.app[0].endpoint : null
    url                   = "https://hello-kupe.${kupe_cluster.todo.name}.${var.tenant}.clusters.kupe.cloud"
  }
}
