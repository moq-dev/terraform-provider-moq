resource "moq_project" "example" {
  name             = "example"
  spend_cap_micros = 100000000 # USD 100 per month

  public = {
    subscribe = ["demo/**"]
  }
}
