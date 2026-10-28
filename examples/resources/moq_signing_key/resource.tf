# A generated key: the API keeps it, and private_key holds the secret.
resource "moq_signing_key" "publish" {
  project_id = moq_project.example.id
  algorithm  = "HS256"
  label      = "publishers"
  publish    = ["live/**"]
}

# Your own key pair: register the public half and sign tokens yourself.
resource "moq_signing_key" "external" {
  project_id = moq_project.example.id
  public_key = file("${path.module}/public.jwk")
}
