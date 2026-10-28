package provider

import (
	"context"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/moq-dev/terraform-provider-moq/internal/client"
)

// moq_project

type projectDataSource struct {
	client *client.Client
}

type projectDataModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Public         types.Object `tfsdk:"public"`
	DomainAccess   types.Bool   `tfsdk:"domain_access"`
	DomainRole     types.String `tfsdk:"domain_role"`
	Domain         types.String `tfsdk:"domain"`
	SpendCapMicros types.Int64  `tfsdk:"spend_cap_micros"`
	CreatedAt      types.String `tfsdk:"created_at"`
	Role           types.String `tfsdk:"role"`
}

func newProjectDataSource() datasource.DataSource {
	return &projectDataSource{}
}

func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	patterns := func(description string) schema.SetAttribute {
		return schema.SetAttribute{MarkdownDescription: description, ElementType: types.StringType, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a project the token can reach.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{MarkdownDescription: "The project's immutable ID.", Required: true},
			"name": schema.StringAttribute{MarkdownDescription: "Vanity name, or null.", Computed: true},
			"public": schema.SingleNestedAttribute{
				MarkdownDescription: "Anonymous access patterns, or null for none.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"publish":   patterns("Patterns anyone may publish to."),
					"subscribe": patterns("Patterns anyone may subscribe to."),
				},
			},
			"domain_access":    schema.BoolAttribute{MarkdownDescription: "Whether a verified email domain is admitted.", Computed: true},
			"domain_role":      schema.StringAttribute{MarkdownDescription: "Role granted to domain members.", Computed: true},
			"domain":           schema.StringAttribute{MarkdownDescription: "The admitted email domain, or null.", Computed: true},
			"spend_cap_micros": schema.Int64Attribute{MarkdownDescription: "Monthly spending cap in micro-USD, or null for none.", Computed: true},
			"created_at":       schema.StringAttribute{MarkdownDescription: "Creation time, RFC 3339.", Computed: true},
			"role":             schema.StringAttribute{MarkdownDescription: "The token owner's role on the project.", Computed: true},
		},
	}
}

func (d *projectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m projectDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid := m.ID.ValueString()
	var project client.Project
	if err := d.client.Do(ctx, client.Request{Method: http.MethodGet, Path: projectPath(pid), Out: &project}); err != nil {
		resp.Diagnostics.AddError("Reading project", err.Error())
		return
	}
	var spend client.SpendCap
	if err := d.client.Do(ctx, client.Request{Method: http.MethodGet, Path: projectPath(pid, "spend-cap"), Out: &spend}); err != nil {
		resp.Diagnostics.AddError("Reading spend cap", err.Error())
		return
	}
	m.Name = optionalString(project.Name)
	m.Public = publicValue(project.Public)
	m.DomainAccess = types.BoolValue(project.Domain != nil)
	m.DomainRole = types.StringValue(project.DomainRole)
	m.Domain = optionalString(project.Domain)
	m.CreatedAt = types.StringValue(project.CreatedAt)
	m.Role = types.StringValue(project.Role)
	m.SpendCapMicros = types.Int64Null()
	if spend.SpendCapMicros != nil {
		m.SpendCapMicros = types.Int64Value(*spend.SpendCapMicros)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// moq_usage

type usageDataSource struct {
	client *client.Client
}

type usageDataModel struct {
	ProjectID types.String `tfsdk:"project_id"`
	Period    types.String `tfsdk:"period"`
	From      types.String `tfsdk:"from"`
	To        types.String `tfsdk:"to"`
	Items     []usageItem  `tfsdk:"items"`
}

type usageItem struct {
	PeriodStart          types.String  `tfsdk:"period_start"`
	PeriodEnd            types.String  `tfsdk:"period_end"`
	Partial              types.Bool    `tfsdk:"partial"`
	EgressExternalBytes  types.Float64 `tfsdk:"egress_external_bytes"`
	IngressBytes         types.Float64 `tfsdk:"ingress_bytes"`
	StoredGbSeconds      types.Float64 `tfsdk:"stored_gb_seconds"`
	VodRecordingBytes    types.Float64 `tfsdk:"vod_recording_bytes"`
	VodEgressBytes       types.Float64 `tfsdk:"vod_egress_bytes"`
	Transcode480Seconds  types.Float64 `tfsdk:"transcode_480_seconds"`
	Transcode720Seconds  types.Float64 `tfsdk:"transcode_720_seconds"`
	Transcode1080Seconds types.Float64 `tfsdk:"transcode_1080_seconds"`
	AgentSeconds         types.Float64 `tfsdk:"agent_seconds"`
	CostMicros           types.Float64 `tfsdk:"cost_micros"`
}

func newUsageDataSource() datasource.DataSource {
	return &usageDataSource{}
}

func (d *usageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_usage"
}

func (d *usageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	quantity := func(description string) schema.Float64Attribute {
		return schema.Float64Attribute{MarkdownDescription: description, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a project's metered usage, aggregated by UTC calendar period. The [API reference](https://moq.pro/doc/api) defines each quantity.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{MarkdownDescription: "The project's immutable ID.", Required: true},
			"period": schema.StringAttribute{
				MarkdownDescription: "Aggregation grain: `minute`, `hour`, `day` (the default), `month`, `year`, or `forever`.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.OneOf("minute", "hour", "day", "month", "year", "forever")},
			},
			"from": schema.StringAttribute{MarkdownDescription: "Inclusive RFC 3339 bound on period starts. Defaults to 30 days before `to`.", Optional: true},
			"to":   schema.StringAttribute{MarkdownDescription: "Exclusive RFC 3339 bound on period starts. Defaults to now.", Optional: true},
			"items": schema.ListNestedAttribute{
				MarkdownDescription: "One entry per period, oldest first.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"period_start":           schema.StringAttribute{MarkdownDescription: "Period start, RFC 3339.", Computed: true},
					"period_end":             schema.StringAttribute{MarkdownDescription: "Period end, RFC 3339.", Computed: true},
					"partial":                schema.BoolAttribute{MarkdownDescription: "Whether the period is still incomplete.", Computed: true},
					"egress_external_bytes":  quantity("Live payload bytes delivered, billable."),
					"ingress_bytes":          quantity("Publisher payload bytes received, never billed."),
					"stored_gb_seconds":      quantity("Managed storage in GB·second, billable."),
					"vod_recording_bytes":    quantity("Managed recording bytes written, billable."),
					"vod_egress_bytes":       quantity("Recording delivery bytes, billable."),
					"transcode_480_seconds":  quantity("Encoded 480p rendition-seconds, billable."),
					"transcode_720_seconds":  quantity("Encoded 720p rendition-seconds, billable."),
					"transcode_1080_seconds": quantity("Encoded 1080p rendition-seconds, billable."),
					"agent_seconds":          quantity("Live voice agent-seconds, billable."),
					"cost_micros":            quantity("Gross cost in micro-USD, before allowances and credit."),
				}},
			},
		},
	}
}

func (d *usageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *usageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m usageDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	query := url.Values{}
	for name, v := range map[string]types.String{"period": m.Period, "from": m.From, "to": m.To} {
		if !v.IsNull() {
			query.Set(name, v.ValueString())
		}
	}
	usage, err := client.List[client.Usage](ctx, d.client, projectPath(m.ProjectID.ValueString(), "usage"), query)
	if err != nil {
		resp.Diagnostics.AddError("Reading usage", err.Error())
		return
	}
	m.Items = make([]usageItem, len(usage))
	for i, u := range usage {
		m.Items[i] = usageItem{
			PeriodStart:          types.StringValue(u.PeriodStart),
			PeriodEnd:            types.StringValue(u.PeriodEnd),
			Partial:              types.BoolValue(u.Partial),
			EgressExternalBytes:  types.Float64Value(u.EgressExternalBytes),
			IngressBytes:         types.Float64Value(u.IngressBytes),
			StoredGbSeconds:      types.Float64Value(u.StoredGbSeconds),
			VodRecordingBytes:    types.Float64Value(u.VodRecordingBytes),
			VodEgressBytes:       types.Float64Value(u.VodEgressBytes),
			Transcode480Seconds:  types.Float64Value(u.Transcode480Seconds),
			Transcode720Seconds:  types.Float64Value(u.Transcode720Seconds),
			Transcode1080Seconds: types.Float64Value(u.Transcode1080Seconds),
			AgentSeconds:         types.Float64Value(u.AgentSeconds),
			CostMicros:           types.Float64Value(u.CostMicros),
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// moq_recordings

type recordingsDataSource struct {
	client *client.Client
}

type recordingsDataModel struct {
	ProjectID  types.String    `tfsdk:"project_id"`
	Recordings []recordingItem `tfsdk:"recordings"`
}

type recordingItem struct {
	ID            types.String  `tfsdk:"id"`
	BroadcastPath types.String  `tfsdk:"broadcast_path"`
	Status        types.String  `tfsdk:"status"`
	StartedAt     types.String  `tfsdk:"started_at"`
	EndedAt       types.String  `tfsdk:"ended_at"`
	PlaylistURL   types.String  `tfsdk:"playlist_url"`
	Bytes         types.Float64 `tfsdk:"bytes"`
	DurationMs    types.Float64 `tfsdk:"duration_ms"`
}

func newRecordingsDataSource() datasource.DataSource {
	return &recordingsDataSource{}
}

func (d *recordingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_recordings"
}

func (d *recordingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists a project's recordings, excluding those being deleted.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{MarkdownDescription: "The project's immutable ID.", Required: true},
			"recordings": schema.ListNestedAttribute{
				MarkdownDescription: "The project's recordings, in the API's order.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":             schema.StringAttribute{MarkdownDescription: "Recording ID.", Computed: true},
					"broadcast_path": schema.StringAttribute{MarkdownDescription: "Broadcast path relative to the project.", Computed: true},
					"status":         schema.StringAttribute{MarkdownDescription: "`LIVE` while ingesting, then `ENDED`.", Computed: true},
					"started_at":     schema.StringAttribute{MarkdownDescription: "Start time, RFC 3339.", Computed: true},
					"ended_at":       schema.StringAttribute{MarkdownDescription: "End time, RFC 3339, or null while live.", Computed: true},
					"playlist_url":   schema.StringAttribute{MarkdownDescription: "Playback playlist URL, or null until media is ready.", Computed: true},
					"bytes":          schema.Float64Attribute{MarkdownDescription: "Recorded media bytes.", Computed: true},
					"duration_ms":    schema.Float64Attribute{MarkdownDescription: "Recorded media duration in milliseconds.", Computed: true},
				}},
			},
		},
	}
}

func (d *recordingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *recordingsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m recordingsDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	recordings, err := client.List[client.Recording](ctx, d.client, projectPath(m.ProjectID.ValueString(), "recording", "sessions"), nil)
	if err != nil {
		resp.Diagnostics.AddError("Reading recordings", err.Error())
		return
	}
	m.Recordings = []recordingItem{}
	for _, r := range recordings {
		if r.Deleting || r.Status == "DELETED" {
			continue
		}
		m.Recordings = append(m.Recordings, recordingItem{
			ID:            types.StringValue(r.ID),
			BroadcastPath: types.StringValue(r.BroadcastPath),
			Status:        types.StringValue(r.Status),
			StartedAt:     types.StringValue(r.StartedAt),
			EndedAt:       optionalString(r.EndedAt),
			PlaylistURL:   optionalString(r.PlaylistURL),
			Bytes:         types.Float64Value(r.Bytes),
			DurationMs:    types.Float64Value(r.DurationMs),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
