terraform {
  required_providers {
    moq = {
      source = "moq-dev/moq"
    }
  }
}

# Reads the account token from MOQ_TOKEN. Set MOQ_ENDPOINT, or endpoint here,
# for anything but production.
provider "moq" {}
