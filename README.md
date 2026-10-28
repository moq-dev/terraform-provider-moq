# Terraform provider for MoQ Pro

Manage [MoQ Pro](https://moq.pro) CDN resources with Terraform or OpenTofu: projects, signing keys, recording rules, and sync targets, with usage and recordings as data sources. The provider is hand-written against the `/v1` [management API](https://moq.pro/doc/api).

```hcl
terraform {
  required_providers {
    moq = {
      source = "moq-dev/moq"
    }
  }
}

provider "moq" {} # MOQ_TOKEN, and MOQ_ENDPOINT for anything but production

resource "moq_project" "app" {}

resource "moq_signing_key" "publish" {
  project_id = moq_project.app.id
  algorithm  = "HS256"
}
```

Not yet published to the registries: the first release waits for the `/v1` contract to settle. [docs/](docs/index.md) holds the generated reference.

## Development

Requires Go and [just](https://just.systems).

```sh
just check   # build, vet, unit tests, lint
just docs    # regenerate docs/ from the schema, templates/, and examples/
just acc     # acceptance tests: real projects on MOQ_ENDPOINT
```

Acceptance tests create and destroy real projects. Run them with `MOQ_TOKEN` set to a write token for an account that exists only for testing, and `MOQ_ENDPOINT` set to a non-production `/v1` base such as `https://api.moq.wtf/v1` or a local `just dev` API. Test projects are named `tfacc-*`; `just sweep` deletes any a failed run left behind. CI runs them against staging with Terraform and OpenTofu.

To try a local build, point a CLI config's `dev_overrides` at it:

```hcl
provider_installation {
  dev_overrides {
    "moq-dev/moq" = "/path/to/go/bin"
  }
  direct {}
}
```

## Releasing

Pushing a `v*` tag runs goreleaser, which builds every platform, signs the checksums with the repository's GPG key, and publishes a GitHub release. The Terraform registry picks it up by webhook; the OpenTofu registry reads releases once the provider and its signing key are registered there.

## License

Licensed under either of [Apache License 2.0](LICENSE-APACHE) or [MIT](LICENSE-MIT) at your option.
