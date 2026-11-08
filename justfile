# Build, vet, unit tests, and lint.
check:
	go build ./...
	go vet ./...
	go test ./...
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run

# Regenerate docs/ from the schema, templates/, and examples/.
docs:
	cd tools && go generate ./...

# Acceptance tests: create and destroy real projects on MOQ_ENDPOINT.
acc *args:
	TF_ACC=1 go test -v -timeout 15m ./internal/provider/ {{ args }}

# Delete tfacc-* projects a failed acceptance run left behind.
sweep:
	go test -v ./internal/provider/ -sweep=all -sweep-run=moq_project

# Install the provider into GOBIN for dev_overrides.
install:
	go install .
