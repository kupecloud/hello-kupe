variable "tenant" {
  description = "Your Kupe tenant name."
  type        = string
}

variable "cluster_name" {
  description = "Name of the cluster the app runs in."
  type        = string
  default     = "todo"
}

variable "database_name" {
  description = "Managed PostgreSQL database name. Keep the existing value when upgrading an applied setup."
  type        = string
  default     = "todo-db"
}

variable "enable_build_token" {
  description = "Create a seven-day registry push token for the demo build. Its secret is stored in Terraform state."
  type        = bool
  default     = false
}

variable "enable_reports" {
  description = "Bind the readonly role into todo-reports for a reporting job."
  type        = bool
  default     = false
}

variable "enable_registry" {
  description = "Create the tenant registry and allow the app cluster to pull images."
  type        = bool
  default     = false
}

variable "app_namespace" {
  description = "Namespace receiving the application's readwrite database binding."
  type        = string
  default     = "hello-kupe"
}

variable "migration_namespace" {
  description = "Separate namespace receiving the schema owner's database binding."
  type        = string
  default     = "hello-kupe-migrate"
}
