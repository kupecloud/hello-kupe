resource "kupe_database" "todo" {
  name    = var.database_name
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
