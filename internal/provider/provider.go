package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/moq-dev/terraform-provider-moq/internal/client"
)

var _ provider.Provider = &moqProvider{}

type moqProvider struct {
	version string
}

type providerModel struct {
	Token    types.String `tfsdk:"token"`
	Endpoint types.String `tfsdk:"endpoint"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &moqProvider{version: version}
	}
}

func (p *moqProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "moq"
	resp.Version = p.version
}

func (p *moqProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage [MoQ Pro](https://moq.pro) CDN projects, signing keys, recording rules, and sync targets through the `/v1` management API.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				MarkdownDescription: "Account API token (`moqpro_...`) from [account settings](https://moq.pro/settings/tokens). Managing resources needs a `write` token; creating projects needs one with access to all projects. Defaults to the `MOQ_TOKEN` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "API base URL including `/v1`. Defaults to the `MOQ_ENDPOINT` environment variable, then `" + client.DefaultEndpoint + "`.",
				Optional:            true,
			},
		},
	}
}

func (p *moqProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.Token.IsUnknown() || config.Endpoint.IsUnknown() {
		resp.Diagnostics.AddError("Unknown provider configuration", "The moq provider's token and endpoint must be known before planning.")
		return
	}

	token := os.Getenv("MOQ_TOKEN")
	if !config.Token.IsNull() {
		token = config.Token.ValueString()
	}
	endpoint := client.DefaultEndpoint
	if env := os.Getenv("MOQ_ENDPOINT"); env != "" {
		endpoint = env
	}
	if !config.Endpoint.IsNull() {
		endpoint = config.Endpoint.ValueString()
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing API token",
			"Set the provider's token or the MOQ_TOKEN environment variable to an account API token from https://moq.pro/settings/tokens.")
		return
	}

	c, err := client.New(endpoint, token, "terraform-provider-moq/"+p.version)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Invalid provider configuration", err.Error())
		return
	}
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *moqProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newProjectResource,
		newSigningKeyResource,
		newRecordingRuleResource,
		newSyncTargetResource,
	}
}

func (p *moqProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newProjectDataSource,
		newUsageDataSource,
		newRecordingsDataSource,
	}
}
