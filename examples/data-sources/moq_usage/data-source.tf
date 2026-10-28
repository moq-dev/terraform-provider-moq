data "moq_usage" "monthly" {
  project_id = moq_project.example.id
  period     = "month"
  from       = "2026-01-01T00:00:00Z"
}

output "egress_bytes" {
  value = sum([for m in data.moq_usage.monthly.items : m.egress_external_bytes])
}
