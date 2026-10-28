terraform {
  required_providers {
    moq = {
      source = "moq-dev/moq"
    }
  }
}

provider "moq" {}

resource "moq_project" "app" {}

resource "moq_signing_key" "publish" {
  project_id = moq_project.app.id
  algorithm  = "HS256"
  label      = "quickstart"
}

resource "moq_recording_rule" "hello" {
  project_id = moq_project.app.id
  patterns   = ["hello.hang"]
}

output "project_id" {
  value = moq_project.app.id
}

output "key_id" {
  value = moq_signing_key.publish.id
}
