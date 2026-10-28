package provider

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func importID(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("%s not in state", resourceName)
		}
		return rs.Primary.Attributes["project_id"] + "/" + rs.Primary.ID, nil
	}
}

func TestAccProject(t *testing.T) {
	name, renamed := testProjectName(), testProjectName()
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "moq_project" "test" {
  name             = %q
  spend_cap_micros = 25000000
  public = {
    subscribe = ["demo/**"]
  }
}

data "moq_project" "test" {
  id = moq_project.test.id
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("moq_project.test", "name", name),
					resource.TestCheckResourceAttr("moq_project.test", "spend_cap_micros", "25000000"),
					resource.TestCheckTypeSetElemAttr("moq_project.test", "public.subscribe.*", "demo/**"),
					resource.TestCheckResourceAttr("moq_project.test", "public.publish.#", "0"),
					resource.TestCheckResourceAttr("moq_project.test", "domain_access", "false"),
					resource.TestCheckResourceAttrSet("moq_project.test", "created_at"),
					resource.TestCheckResourceAttrPair("data.moq_project.test", "name", "moq_project.test", "name"),
					resource.TestCheckResourceAttr("data.moq_project.test", "spend_cap_micros", "25000000"),
					resource.TestCheckResourceAttr("data.moq_project.test", "role", "owner"),
				),
			},
			{
				ResourceName:      "moq_project.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: fmt.Sprintf(`
resource "moq_project" "test" {
  name = %q
}
`, renamed),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("moq_project.test", "name", renamed),
					resource.TestCheckNoResourceAttr("moq_project.test", "spend_cap_micros"),
					resource.TestCheckNoResourceAttr("moq_project.test", "public"),
				),
			},
		},
	})
}

func publicJWK(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdh, err := key.PublicKey.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	point := ecdh.Bytes() // 0x04 || X || Y
	jwk, err := json.Marshal(map[string]string{
		"kty": "EC", "crv": "P-256", "alg": "ES256",
		"x": base64.RawURLEncoding.EncodeToString(point[1:33]),
		"y": base64.RawURLEncoding.EncodeToString(point[33:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(jwk)
}

func TestAccSigningKey(t *testing.T) {
	project, jwk := testProjectName(), publicJWK(t)
	config := func(enabled bool) string {
		return fmt.Sprintf(`
resource "moq_project" "test" {
  name = %q
}

resource "moq_signing_key" "hmac" {
  project_id = moq_project.test.id
  algorithm  = "HS256"
  label      = "acceptance"
  publish    = ["live/**"]
  subscribe  = ["**"]
  enabled    = %t
}

resource "moq_signing_key" "pair" {
  project_id = moq_project.test.id
  algorithm  = "ES256"
}

resource "moq_signing_key" "imported" {
  project_id = moq_project.test.id
  public_key = %q
}
`, project, enabled, jwk)
	}
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("moq_signing_key.hmac", "key_type", "symmetric"),
					resource.TestCheckResourceAttr("moq_signing_key.hmac", "enabled", "true"),
					resource.TestCheckTypeSetElemAttr("moq_signing_key.hmac", "publish.*", "live/**"),
					resource.TestCheckResourceAttrSet("moq_signing_key.hmac", "private_key"),
					resource.TestCheckResourceAttr("moq_signing_key.pair", "key_type", "asymmetric"),
					resource.TestCheckResourceAttrSet("moq_signing_key.pair", "private_key"),
					resource.TestCheckNoResourceAttr("moq_signing_key.pair", "publish"),
					resource.TestCheckResourceAttr("moq_signing_key.imported", "algorithm", "ES256"),
					resource.TestCheckNoResourceAttr("moq_signing_key.imported", "private_key"),
				),
			},
			{
				// A symmetric key's secret reads back on import.
				ResourceName:      "moq_signing_key.hmac",
				ImportState:       true,
				ImportStateIdFunc: importID("moq_signing_key.hmac"),
				ImportStateVerify: true,
			},
			{
				// An asymmetric private key is returned only at creation.
				ResourceName:            "moq_signing_key.pair",
				ImportState:             true,
				ImportStateIdFunc:       importID("moq_signing_key.pair"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"private_key"},
			},
			{
				ResourceName:            "moq_signing_key.imported",
				ImportState:             true,
				ImportStateIdFunc:       importID("moq_signing_key.imported"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"public_key"},
			},
			{
				Config: config(false),
				Check:  resource.TestCheckResourceAttr("moq_signing_key.hmac", "enabled", "false"),
			},
		},
	})
}

func TestAccRecordingRule(t *testing.T) {
	project := testProjectName()
	config := func(vod bool, retention int) string {
		return fmt.Sprintf(`
resource "moq_project" "test" {
  name = %q
}

resource "moq_recording_rule" "test" {
  project_id     = moq_project.test.id
  label          = "acceptance"
  patterns       = ["live/**/*.hang", "hello.hang"]
  vod_enabled    = %t
  retention_days = %d
}

data "moq_recordings" "test" {
  project_id = moq_recording_rule.test.project_id
}

data "moq_usage" "test" {
  project_id = moq_project.test.id
  period     = "month"
}
`, project, vod, retention)
	}
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config(true, 7),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckTypeSetElemAttr("moq_recording_rule.test", "patterns.*", "live/**/*.hang"),
					resource.TestCheckResourceAttr("moq_recording_rule.test", "retention_days", "7"),
					resource.TestCheckNoResourceAttr("moq_recording_rule.test", "bucket"),
					resource.TestCheckNoResourceAttr("moq_recording_rule.test", "bucket_id"),
					resource.TestCheckResourceAttr("data.moq_recordings.test", "recordings.#", "0"),
					resource.TestCheckResourceAttrSet("data.moq_usage.test", "items.#"),
				),
			},
			{
				ResourceName:      "moq_recording_rule.test",
				ImportState:       true,
				ImportStateIdFunc: importID("moq_recording_rule.test"),
				ImportStateVerify: true,
			},
			{
				Config: config(false, 1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("moq_recording_rule.test", "vod_enabled", "false"),
					resource.TestCheckResourceAttr("moq_recording_rule.test", "retention_days", "1"),
				),
			},
		},
	})
}

func TestAccSyncTarget(t *testing.T) {
	project := testProjectName()
	config := func(label, prefix string) string {
		return fmt.Sprintf(`
resource "moq_project" "test" {
  name = %q
}

resource "moq_sync_target" "test" {
  project_id       = moq_project.test.id
  kind             = "custom"
  label            = %q
  publish_prefixes = [%q]
  publish_url      = "https://relay.example.com/acceptance?jwt=secret"
}
`, project, label, prefix)
	}
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config("first", "live"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("moq_sync_target.test", "publish_configured", "true"),
					resource.TestCheckResourceAttr("moq_sync_target.test", "publish_origin", "https://relay.example.com"),
					resource.TestCheckTypeSetElemAttr("moq_sync_target.test", "publish_prefixes.*", "live"),
				),
			},
			{
				ResourceName:            "moq_sync_target.test",
				ImportState:             true,
				ImportStateIdFunc:       importID("moq_sync_target.test"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"publish_url"},
			},
			{
				Config: config("second", "events"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("moq_sync_target.test", "label", "second"),
					resource.TestCheckTypeSetElemAttr("moq_sync_target.test", "publish_prefixes.*", "events"),
				),
			},
		},
	})
}
