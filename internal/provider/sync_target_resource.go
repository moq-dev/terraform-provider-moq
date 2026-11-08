package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/moq-dev/terraform-provider-moq/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &syncTargetResource{}
	_ resource.ResourceWithImportState = &syncTargetResource{}
)

type syncTargetResource struct {
	client *client.Client
}

type syncTargetModel struct {
	ProjectID         types.String `tfsdk:"project_id"`
	ID                types.String `tfsdk:"id"`
	Kind              types.String `tfsdk:"kind"`
	Label             types.String `tfsdk:"label"`
	PublishPrefixes   types.Set    `tfsdk:"publish_prefixes"`
	PublishURL        types.String `tfsdk:"publish_url"`
	PublishConfigured types.Bool   `tfsdk:"publish_configured"`
	PublishOrigin     types.String `tfsdk:"publish_origin"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

func newSyncTargetResource() resource.Resource {
	return &syncTargetResource{}
}

func (r *syncTargetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sync_target"
}

func (r *syncTargetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Publishes a project's broadcasts to another MoQ relay, such as Cloudflare's.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{
				MarkdownDescription: "The project's immutable ID.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Sync target ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"kind": schema.StringAttribute{
				MarkdownDescription: "`cloudflare` or `custom`. Changing it replaces the target.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{stringvalidator.OneOf("cloudflare", "custom")},
			},
			"label": schema.StringAttribute{
				MarkdownDescription: "Human-readable label, at most 80 characters.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				Validators:          []validator.String{stringvalidator.LengthAtMost(80)},
			},
			"publish_prefixes": schema.SetAttribute{
				MarkdownDescription: "Broadcast path prefixes, relative to the project, to publish to the target.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Default:             setdefault.StaticValue(stringSet(nil)),
				Validators: []validator.Set{
					setvalidator.SizeAtMost(20),
					setvalidator.ValueStringsAre(stringvalidator.LengthAtMost(200)),
				},
			},
			"publish_url": schema.StringAttribute{
				MarkdownDescription: "Publish URL including its credential. Write-only: the API returns only its origin, as `publish_origin`.",
				Optional:            true,
				Sensitive:           true,
				Validators:          []validator.String{stringvalidator.LengthAtMost(2000)},
			},
			"publish_configured": schema.BoolAttribute{
				MarkdownDescription: "Whether a publish credential is configured.",
				Computed:            true,
			},
			"publish_origin": schema.StringAttribute{
				MarkdownDescription: "The credential-free origin of `publish_url`.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Last change, RFC 3339. Updates refuse to overwrite a change made elsewhere since the last refresh.",
				Computed:            true,
			},
		},
	}
}

func (r *syncTargetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *syncTargetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan syncTargetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"kind":            plan.Kind.ValueString(),
		"label":           plan.Label.ValueString(),
		"publishPrefixes": setStrings(ctx, plan.PublishPrefixes, &resp.Diagnostics),
	}
	if !plan.PublishURL.IsNull() {
		body["publishUrl"] = plan.PublishURL.ValueString()
	}
	if resp.Diagnostics.HasError() {
		return
	}
	var target client.SyncTarget
	err := r.client.Do(ctx, client.Request{
		Method: http.MethodPost, Path: projectPath(plan.ProjectID.ValueString(), "sync", "targets"), Body: body,
		IdempotencyKey: client.NewIdempotencyKey(), Out: &target,
	})
	if err != nil {
		resp.Diagnostics.AddError("Creating sync target", err.Error())
		return
	}
	syncTargetState(&plan, &target)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *syncTargetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state syncTargetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	target, err := client.Find(ctx, r.client, projectPath(state.ProjectID.ValueString(), "sync", "targets"),
		func(t client.SyncTarget) bool { return t.ID == id })
	if client.IsNotFound(err) || (err == nil && target == nil) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading sync target", err.Error())
		return
	}
	// A credential cleared elsewhere shows as drift to restore.
	if !target.PublishConfigured {
		state.PublishURL = types.StringNull()
	}
	syncTargetState(&state, target)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *syncTargetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state syncTargetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	patch := map[string]any{"expectedUpdatedAt": state.UpdatedAt.ValueString()}
	if !plan.Label.Equal(state.Label) {
		patch["label"] = plan.Label.ValueString()
	}
	if !plan.PublishPrefixes.Equal(state.PublishPrefixes) {
		patch["publishPrefixes"] = setStrings(ctx, plan.PublishPrefixes, &resp.Diagnostics)
	}
	if !plan.PublishURL.Equal(state.PublishURL) {
		if plan.PublishURL.IsNull() {
			patch["publishUrl"] = nil
		} else {
			patch["publishUrl"] = plan.PublishURL.ValueString()
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	var target client.SyncTarget
	err := r.client.Do(ctx, client.Request{
		Method: http.MethodPatch, Path: projectPath(plan.ProjectID.ValueString(), "sync", "targets", plan.ID.ValueString()),
		Body: patch, Out: &target,
	})
	if err != nil {
		resp.Diagnostics.AddError("Updating sync target", err.Error())
		return
	}
	syncTargetState(&plan, &target)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *syncTargetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state syncTargetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Do(ctx, client.Request{
		Method: http.MethodDelete, Path: projectPath(state.ProjectID.ValueString(), "sync", "targets", state.ID.ValueString()),
	})
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting sync target", err.Error())
	}
}

func (r *syncTargetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := importParts(req.ID, "project_id", "id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

func syncTargetState(m *syncTargetModel, t *client.SyncTarget) {
	m.ID = types.StringValue(t.ID)
	m.Kind = types.StringValue(t.Kind)
	m.Label = types.StringValue(t.Label)
	m.PublishPrefixes = stringSet(t.PublishPrefixes)
	m.PublishConfigured = types.BoolValue(t.PublishConfigured)
	m.PublishOrigin = optionalString(t.PublishOrigin)
	m.UpdatedAt = types.StringValue(t.UpdatedAt)
}
