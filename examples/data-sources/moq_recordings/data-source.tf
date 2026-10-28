data "moq_recordings" "example" {
  project_id = moq_project.example.id
}

output "playlists" {
  value = [for r in data.moq_recordings.example.recordings : r.playlist_url if r.status == "ENDED"]
}
