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
  label      = "Quickstart"
}

output "project_id" {
  value = moq_project.app.id
}

output "key_id" {
  value = moq_signing_key.publish.id
}
