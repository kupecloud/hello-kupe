# A cluster, a PostgreSQL database and two bindings: the owner role into the
# `todo-migrate` namespace for the migration Job, the app role into `api` for
# the API. Each binding creates its namespace. The `web` namespace gets no
# binding, so it has no Secret and no network path to the database.

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
  description = "Name of the cluster the sample runs in."
  type        = string
  default     = "todo"
}

resource "kupe_cluster" "todo" {
  name    = var.cluster_name
  version = "1.35"

  resources = {
    cpu     = "2"
    memory  = "4Gi"
    storage = "20Gi"
  }
}

resource "kupe_database" "todo" {
  name    = "todo-db"
  version = "18"

  # The smallest size Kupe accepts. Billing uses these requested values for
  # every hour the database exists.
  resources = {
    cpu     = "250m"
    memory  = "512Mi"
    storage = "5Gi"
  }

  # The platform refuses to delete the database while this is true. It is the
  # default; it is written out so nobody has to look it up.
  deletion_protection = true

  # And Terraform refuses to plan a destroy at all. To delete the database,
  # remove this block and set deletion_protection = false, apply, then destroy.
  lifecycle {
    prevent_destroy = true
  }
}

# Schema changes. Mounted only by the migration Job, in a namespace of its own
# so no pod in api can mount the owner Secret.
resource "kupe_database_binding" "owner" {
  database  = kupe_database.todo.name
  cluster   = kupe_cluster.todo.name
  namespace = "todo-migrate"
  role      = "owner"
  # secret_name defaults to "todo-db-owner"
}

# Reads and writes rows. Mounted by the API.
resource "kupe_database_binding" "app" {
  database  = kupe_database.todo.name
  cluster   = kupe_cluster.todo.name
  namespace = "api"
  role      = "app"
  # secret_name defaults to "todo-db-app"
}

output "database_host" {
  description = "In-cluster DNS name the api namespace connects to."
  value       = kupe_database_binding.app.host
}

output "owner_secret_name" {
  description = "Secret in the todo-migrate namespace mounted by the migration Job."
  value       = kupe_database_binding.owner.secret_name
}

output "app_secret_name" {
  description = "Secret in the api namespace mounted by the API."
  value       = kupe_database_binding.app.secret_name
}
