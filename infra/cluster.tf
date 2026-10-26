resource "kupe_cluster" "todo" {
  name    = var.cluster_name
  version = "1.35"

  resources = {
    cpu     = "2"
    memory  = "4Gi"
    storage = "20Gi"
  }
}
