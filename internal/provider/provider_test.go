package provider

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/moq-dev/terraform-provider-moq/internal/client"
)

// Acceptance tests create and destroy real projects. Run them with TF_ACC=1,
// MOQ_TOKEN set to a write token for an account that exists for testing, and
// MOQ_ENDPOINT for anything but production, such as https://api.moq.wtf/v1.

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"moq": providerserver.NewProtocol6WithError(New("test")()),
}

// Every project an acceptance test creates is named with this prefix, so the
// sweeper can find what a failed run left behind.
const testPrefix = "tfacc-"

func preCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("MOQ_TOKEN") == "" {
		t.Fatal("MOQ_TOKEN must be a write token for a test account")
	}
}

func testProjectName() string {
	return testPrefix + acctest.RandStringFromCharSet(10, "abcdefghijklmnopqrstuvwxyz0123456789")
}

func TestMain(m *testing.M) {
	resource.TestMain(m)
}

func init() {
	resource.AddTestSweepers("moq_project", &resource.Sweeper{
		Name: "moq_project",
		F: func(_ string) error {
			endpoint := os.Getenv("MOQ_ENDPOINT")
			if endpoint == "" {
				endpoint = client.DefaultEndpoint
			}
			c, err := client.New(endpoint, os.Getenv("MOQ_TOKEN"), "terraform-provider-moq/sweeper")
			if err != nil {
				return err
			}
			ctx := context.Background()
			projects, err := client.List[client.Project](ctx, c, "/projects", nil)
			if err != nil {
				return err
			}
			for _, p := range projects {
				if p.Name == nil || !strings.HasPrefix(*p.Name, testPrefix) {
					continue
				}
				if err := c.Do(ctx, client.Request{Method: http.MethodDelete, Path: projectPath(p.ID)}); err != nil && !client.IsNotFound(err) {
					return fmt.Errorf("sweeping %s: %w", p.ID, err)
				}
			}
			return nil
		},
	})
}
