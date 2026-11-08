# Import by project ID and rule ID. Bucket credentials are write-only: the
# next apply records them from configuration without replacing the rule.
terraform import moq_recording_rule.live p123example/r123example
