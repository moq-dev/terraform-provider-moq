package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/moq-dev/terraform-provider-moq/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &recordingRuleResource{}
	_ resource.ResourceWithImportState = &recordingRuleResource{}
)

type recordingRuleResource struct {
	client *client.Client
}

type recordingRuleModel struct {
	ProjectID     types.String `tfsdk:"project_id"`
	ID            types.String `tfsdk:"id"`
	Label         types.String `tfsdk:"label"`
	Patterns      types.Set    `tfsdk:"patterns"`
	VodEnabled    types.Bool   `tfsdk:"vod_enabled"`
	RetentionDays types.Int64  `tfsdk:"retention_days"`
	Bucket        types.Object `tfsdk:"bucket"`
	BucketID      types.String `tfsdk:"bucket_id"`
	UploadRefusal types.String `tfsdk:"upload_refusal"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

type bucketModel struct {
	Endpoint        types.String `tfsdk:"endpoint"`
	Region          types.String `tfsdk:"region"`
	Bucket          types.String `tfsdk:"bucket"`
	AccessKeyID     types.String `tfsdk:"access_key_id"`
	SecretAccessKey types.String `tfsdk:"secret_access_key"`
	Destination     types.String `tfsdk:"destination"`
	Delivery        types.Bool   `tfsdk:"delivery"`
}

var bucketTypes = map[string]attr.Type{
	"endpoint":          types.StringType,
	"region":            types.StringType,
	"bucket":            types.StringType,
	"access_key_id":     types.StringType,
	"secret_access_key": types.StringType,
	"destination":       types.StringType,
	"delivery":          types.BoolType,
}

func newRecordingRuleResource() resource.Resource {
	return &recordingRuleResource{}
}

func (r *recordingRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_recording_rule"
}

func (r *recordingRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Records broadcasts whose paths match its patterns, into managed storage or your own S3-compatible bucket.\n\n" +
			"~> Destroying or replacing a rule deletes its recordings. Changing `patterns`, `label`, or the bucket's endpoint, credentials, or destination replaces the rule.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{
				MarkdownDescription: "The project's immutable ID.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Rule ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"label": schema.StringAttribute{
				MarkdownDescription: "Display label, at most 80 characters.",
				Optional:            true,
				PlanModifiers:       replace,
				Validators:          []validator.String{stringvalidator.LengthAtMost(80)},
			},
			"patterns": schema.SetAttribute{
				MarkdownDescription: "Broadcast path patterns relative to the project, 1 to 32. Each must match only Hang broadcasts, whose paths end in `.hang`, such as `live/*.hang` or `live/**/*.hang`.",
				ElementType:         types.StringType,
				Required:            true,
				PlanModifiers:       []planmodifier.Set{setplanmodifier.RequiresReplace()},
				Validators: []validator.Set{
					setvalidator.SizeBetween(1, 32),
					setvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, 200)),
				},
			},
			"vod_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether matching broadcasts are recorded. Re-enabling resumes a rule stopped by a bucket refusal.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"retention_days": schema.Int64Attribute{
				MarkdownDescription: "Managed storage retention in days: 1, 7, 30, 90, 180, or 365. More than 7 needs a payment method. Your own bucket's lifecycle rules govern its retention.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(7),
				Validators:          []validator.Int64{int64validator.OneOf(1, 7, 30, 90, 180, 365)},
			},
			"bucket": schema.SingleNestedAttribute{
				MarkdownDescription: "Record into your own S3-compatible bucket instead of managed storage. Adding or removing it replaces the rule.",
				Optional:            true,
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplaceIf(
					func(_ context.Context, req planmodifier.ObjectRequest, resp *objectplanmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = req.StateValue.IsNull() != req.PlanValue.IsNull()
					},
					"Switching between managed storage and a bucket replaces the rule.",
					"Switching between managed storage and a bucket replaces the rule.",
				)},
				Attributes: map[string]schema.Attribute{
					"endpoint": schema.StringAttribute{
						MarkdownDescription: "S3-compatible endpoint, an https URL on a public host.",
						Required:            true,
						PlanModifiers:       replace,
						Validators:          []validator.String{stringvalidator.LengthBetween(1, 400)},
					},
					"region": schema.StringAttribute{
						MarkdownDescription: "Region used for signing.",
						Required:            true,
						Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
					},
					"bucket": schema.StringAttribute{
						MarkdownDescription: "Bucket name.",
						Required:            true,
						Validators:          []validator.String{stringvalidator.LengthBetween(1, 200)},
					},
					"access_key_id": schema.StringAttribute{
						MarkdownDescription: "Access key ID with write access, and read access for `delivery`. Write-only: the API never returns it.",
						Required:            true,
						Sensitive:           true,
						PlanModifiers:       []planmodifier.String{replaceIfKnown()},
						Validators:          []validator.String{stringvalidator.LengthBetween(1, 200)},
					},
					"secret_access_key": schema.StringAttribute{
						MarkdownDescription: "Secret access key. Write-only: the API encrypts it at rest and never returns it.",
						Required:            true,
						Sensitive:           true,
						PlanModifiers:       []planmodifier.String{replaceIfKnown()},
						Validators:          []validator.String{stringvalidator.LengthBetween(1, 400)},
					},
					"destination": schema.StringAttribute{
						MarkdownDescription: "Object prefix inside the bucket. Defaults to the bucket root.",
						Optional:            true,
						Computed:            true,
						Default:             stringdefault.StaticString(""),
						PlanModifiers:       replace,
						Validators:          []validator.String{stringvalidator.LengthAtMost(400)},
					},
					"delivery": schema.BoolAttribute{
						MarkdownDescription: "Serve playback from the bucket, which needs read access too.",
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
					},
				},
			},
			"bucket_id": schema.StringAttribute{
				MarkdownDescription: "The customer bucket's ID, or null for managed storage.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"upload_refusal": schema.StringAttribute{
				MarkdownDescription: "Why the bucket refused an upload, which stopped recording until the rule is re-enabled; null while recording.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation time, RFC 3339.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *recordingRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *recordingRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan recordingRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"patterns":      setStrings(ctx, plan.Patterns, &resp.Diagnostics),
		"vodEnabled":    plan.VodEnabled.ValueBool(),
		"retentionDays": plan.RetentionDays.ValueInt64(),
	}
	if !plan.Label.IsNull() {
		body["label"] = plan.Label.ValueString()
	}
	if b := bucketFrom(ctx, plan.Bucket, &resp.Diagnostics); b != nil {
		bucket := map[string]any{
			"endpoint":        b.Endpoint.ValueString(),
			"region":          b.Region.ValueString(),
			"bucket":          b.Bucket.ValueString(),
			"accessKeyId":     b.AccessKeyID.ValueString(),
			"secretAccessKey": b.SecretAccessKey.ValueString(),
			"delivery":        b.Delivery.ValueBool(),
		}
		if d := b.Destination.ValueString(); d != "" {
			bucket["destination"] = d
		}
		body["bucket"] = bucket
		// The API owns retention for a customer bucket.
		delete(body, "retentionDays")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var rule client.RecordingRule
	err := r.client.Do(ctx, client.Request{
		Method: http.MethodPost, Path: projectPath(plan.ProjectID.ValueString(), "recording", "rules"), Body: body,
		IdempotencyKey: client.NewIdempotencyKey(), Out: &rule,
	})
	if err != nil {
		resp.Diagnostics.AddError("Creating recording rule", err.Error())
		return
	}
	resp.Diagnostics.Append(ruleState(ctx, &plan, &rule)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *recordingRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state recordingRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	rule, err := client.Find(ctx, r.client, projectPath(state.ProjectID.ValueString(), "recording", "rules"),
		func(rule client.RecordingRule) bool { return rule.ID == id })
	// A rule whose recordings are draining is already deleted.
	if client.IsNotFound(err) || (err == nil && (rule == nil || rule.Deleting)) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading recording rule", err.Error())
		return
	}
	resp.Diagnostics.Append(ruleState(ctx, &state, rule)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *recordingRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state recordingRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	patch := map[string]any{}
	if !plan.VodEnabled.Equal(state.VodEnabled) {
		patch["vodEnabled"] = plan.VodEnabled.ValueBool()
	}
	planBucket := bucketFrom(ctx, plan.Bucket, &resp.Diagnostics)
	stateBucket := bucketFrom(ctx, state.Bucket, &resp.Diagnostics)
	if planBucket == nil && !plan.RetentionDays.Equal(state.RetentionDays) {
		patch["retentionDays"] = plan.RetentionDays.ValueInt64()
	}
	if planBucket != nil && stateBucket != nil {
		if !planBucket.Bucket.Equal(stateBucket.Bucket) {
			patch["bucket"] = planBucket.Bucket.ValueString()
		}
		if !planBucket.Region.Equal(stateBucket.Region) {
			patch["region"] = planBucket.Region.ValueString()
		}
		if !planBucket.Delivery.Equal(stateBucket.Delivery) {
			patch["delivery"] = planBucket.Delivery.ValueBool()
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	pid, id := plan.ProjectID.ValueString(), plan.ID.ValueString()
	if len(patch) == 0 {
		// Only write-only values recorded after an import changed.
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	var rule client.RecordingRule
	err := r.client.Do(ctx, client.Request{Method: http.MethodPatch, Path: projectPath(pid, "recording", "rules", id), Body: patch, Out: &rule})
	if err != nil {
		resp.Diagnostics.AddError("Updating recording rule", err.Error())
		return
	}
	resp.Diagnostics.Append(ruleState(ctx, &plan, &rule)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *recordingRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state recordingRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rulePath := projectPath(state.ProjectID.ValueString(), "recording", "rules", state.ID.ValueString())
	// The API deletes only a rule that has stopped recording.
	var err error
	if state.VodEnabled.ValueBool() {
		err = r.client.Do(ctx, client.Request{Method: http.MethodPatch, Path: rulePath, Body: map[string]any{"vodEnabled": false}})
	}
	// Deletion is accepted at once; the rule's recordings drain afterwards.
	if err == nil {
		err = r.client.Do(ctx, client.Request{Method: http.MethodDelete, Path: rulePath})
	}
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting recording rule", err.Error())
	}
}

func (r *recordingRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := importParts(req.ID, "project_id", "id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

func bucketFrom(ctx context.Context, obj types.Object, diags *diag.Diagnostics) *bucketModel {
	if obj.IsNull() || obj.IsUnknown() {
		return nil
	}
	var b bucketModel
	diags.Append(obj.As(ctx, &b, objectAsOptions)...)
	return &b
}

// ruleState copies the API's view into m, keeping the write-only bucket
// credentials m already holds.
func ruleState(ctx context.Context, m *recordingRuleModel, rule *client.RecordingRule) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(rule.ID)
	m.Label = optionalString(rule.Label)
	// The API stores a canonical, sorted union, and patterns never change in
	// place, so keep the configured form once known.
	if m.Patterns.IsNull() || m.Patterns.IsUnknown() {
		m.Patterns = stringSet(rule.Patterns)
	}
	m.VodEnabled = types.BoolValue(rule.VodEnabled)
	m.RetentionDays = types.Int64Value(rule.RetentionDays)
	m.BucketID = optionalString(rule.BucketID)
	m.UploadRefusal = optionalString(rule.UploadRefusal)
	m.CreatedAt = types.StringValue(rule.CreatedAt)
	if rule.Bucket == nil {
		m.Bucket = types.ObjectNull(bucketTypes)
		return diags
	}
	accessKeyID, secret := types.StringNull(), types.StringNull()
	if prior := bucketFrom(ctx, m.Bucket, &diags); prior != nil {
		accessKeyID, secret = prior.AccessKeyID, prior.SecretAccessKey
	}
	b := rule.Bucket
	obj, d := types.ObjectValue(bucketTypes, map[string]attr.Value{
		"endpoint":          types.StringValue(b.Endpoint),
		"region":            types.StringValue(b.Region),
		"bucket":            types.StringValue(b.Bucket),
		"access_key_id":     accessKeyID,
		"secret_access_key": secret,
		"destination":       types.StringValue(b.Destination),
		"delivery":          types.BoolValue(b.Delivery),
	})
	diags.Append(d...)
	m.Bucket = obj
	return diags
}
