# A cluster, a PostgreSQL database and two bindings: the owner role into the
# `todo-migrate` namespace for the migration Job, the readwrite role into `api`
# for the API. Each binding creates its namespace. The `web` namespace gets no
# binding, so it has no Secret and no network path to the database. Set
# enable_reports to add a third, the readonly role into `todo-reports`.

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

variable "enable_reports" {
  description = "Bind the readonly role into todo-reports for a reporting job."
  type        = bool
  default     = false
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
  database   = kupe_database.todo.name
  cluster    = kupe_cluster.todo.name
  namespaces = ["todo-migrate"]
  role       = "owner"
  # secret_name defaults to "todo-db-owner"
}

# Reads and writes rows. Mounted by the API.
resource "kupe_database_binding" "readwrite" {
  database   = kupe_database.todo.name
  cluster    = kupe_cluster.todo.name
  namespaces = ["api"]
  role       = "readwrite"
  # secret_name defaults to "todo-db-readwrite"
}

# Reads rows only. Off by default: nothing in the chart uses it.
resource "kupe_database_binding" "reports" {
  count      = var.enable_reports ? 1 : 0
  database   = kupe_database.todo.name
  cluster    = kupe_cluster.todo.name
  namespaces = ["todo-reports"]
  role       = "readonly"
  # secret_name defaults to "todo-db-readonly"
}

output "database_host" {
  description = "In-cluster DNS name the api namespace connects to."
  value       = one([for ns in kupe_database_binding.readwrite.namespace_status : ns.host if ns.name == "api"])
}

output "owner_secret_name" {
  description = "Secret in the todo-migrate namespace mounted by the migration Job."
  value       = kupe_database_binding.owner.secret_name
}

output "readwrite_secret_name" {
  description = "Secret in the api namespace mounted by the API."
  value       = kupe_database_binding.readwrite.secret_name
}
