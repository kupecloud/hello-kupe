# Schema changes. Mounted only by the migration Job, in a namespace of its own
# so no pod in the application namespace can mount the owner Secret.
resource "kupe_database_binding" "owner" {
  database   = kupe_database.todo.name
  cluster    = kupe_cluster.todo.name
  namespaces = [var.migration_namespace]
  role       = "owner"
  # secret_name defaults to "<database_name>-owner".
  lifecycle {
    precondition {
      condition     = var.app_namespace != var.migration_namespace
      error_message = "Keep the owner binding in a separate migration namespace."
    }
  }
}

# Reads and writes rows. Mounted by the API.
resource "kupe_database_binding" "readwrite" {
  database   = kupe_database.todo.name
  cluster    = kupe_cluster.todo.name
  namespaces = [var.app_namespace]
  role       = "readwrite"
  # secret_name defaults to "<database_name>-readwrite".
}

# Reads rows only. Off by default: nothing in the chart uses it.
resource "kupe_database_binding" "reports" {
  count      = var.enable_reports ? 1 : 0
  database   = kupe_database.todo.name
  cluster    = kupe_cluster.todo.name
  namespaces = ["todo-reports"]
  role       = "readonly"
  # secret_name defaults to "<database_name>-readonly".
}
