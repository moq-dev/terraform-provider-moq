resource "moq_sync_target" "cloudflare" {
  project_id       = moq_project.example.id
  kind             = "cloudflare"
  label            = "Cloudflare relay"
  publish_prefixes = ["live"]
  publish_url      = var.cloudflare_publish_url
}
