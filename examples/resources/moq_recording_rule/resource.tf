# Managed storage, kept for 30 days.
resource "moq_recording_rule" "live" {
  project_id     = moq_project.example.id
  patterns       = ["live/*.hang"]
  retention_days = 30
}

# Your own S3-compatible bucket, which owns retention.
resource "moq_recording_rule" "archive" {
  project_id = moq_project.example.id
  patterns   = ["archive/**/*.hang"]

  bucket = {
    endpoint          = "https://s3.us-east-1.amazonaws.com"
    region            = "us-east-1"
    bucket            = "example-recordings"
    access_key_id     = var.recording_access_key_id
    secret_access_key = var.recording_secret_access_key
    delivery          = true
  }
}
