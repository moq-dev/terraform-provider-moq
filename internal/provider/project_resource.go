package provider

import (
	"context"
	"net/http"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/moq-dev/terraform-provider-moq/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &projectResource{}
	_ resource.ResourceWithImportState = &projectResource{}
)

type projectResource struct {
	client *client.Client
}

type projectModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Public         types.Object `tfsdk:"public"`
	DomainAccess   types.Bool   `tfsdk:"domain_access"`
	DomainRole     types.String `tfsdk:"domain_role"`
	Domain         types.String `tfsdk:"domain"`
	SpendCapMicros types.Int64  `tfsdk:"spend_cap_micros"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

var publicTypes = map[string]attr.Type{
	"publish":   types.SetType{ElemType: types.StringType},
	"subscribe": types.SetType{ElemType: types.StringType},
}

var projectNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func newProjectResource() resource.Resource {
	return &projectResource{}
}

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func patternSet(description string) schema.SetAttribute {
	return schema.SetAttribute{
		MarkdownDescription: description,
		ElementType:         types.StringType,
		Optional:            true,
		Computed:            true,
		Default:             setdefault.StaticValue(stringSet(nil)),
		Validators: []validator.Set{
			setvalidator.SizeAtMost(32),
			setvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, 256)),
		},
	}
}

func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A MoQ Pro project: a namespace for broadcasts with its own keys, recordings, usage, and bill.\n\n" +
			"An account can own a limited number of projects. If a create is refused for that limit, [ask support](mailto:support@moq.pro) to raise it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Immutable project ID, used in API paths and relay URLs.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Optional vanity name, 4 to 32 lowercase letters, digits, and inner hyphens. A rename keeps the old name as a redirect and locks further renames for seven days.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(4, 32),
					stringvalidator.RegexMatches(projectNamePattern, "must be lowercase letters, digits, and inner hyphens"),
				},
			},
			"public": schema.SingleNestedAttribute{
				MarkdownDescription: "Anonymous access: broadcast path patterns anyone may publish or subscribe to without a token. A bare path matches exactly; a trailing `**` segment matches its subtree. Omit for no anonymous access.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"publish":   patternSet("Patterns anyone may publish to."),
					"subscribe": patternSet("Patterns anyone may subscribe to."),
				},
			},
			"domain_access": schema.BoolAttribute{
				MarkdownDescription: "Admit members with a verified email address on the token owner's domain.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"domain_role": schema.StringAttribute{
				MarkdownDescription: "Role granted to domain members: `read` or `write`.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.OneOf("read", "write")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "The verified email domain admitted when `domain_access` is on.",
				Computed:            true,
			},
			"spend_cap_micros": schema.Int64Attribute{
				MarkdownDescription: "Monthly spending cap in micro-USD (1000000 is USD 1), from USD 1 to USD 1,000,000. Omit for no cap.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.Between(1_000_000, 1_000_000_000_000)},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation time, RFC 3339.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := map[string]any{}
	if !plan.Name.IsNull() {
		body["name"] = plan.Name.ValueString()
	}
	var project client.Project
	err := r.client.Do(ctx, client.Request{
		Method: http.MethodPost, Path: "/projects", Body: body,
		IdempotencyKey: client.NewIdempotencyKey(), Out: &project,
	})
	if client.HasCode(err, "limit_reached") {
		resp.Diagnostics.AddError("Project limit reached",
			"This account owns as many projects as it may. Delete one, or ask support@moq.pro to raise the limit.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Creating project", err.Error())
		return
	}

	// Record the project before configuring it, so a later failure leaves it
	// tracked (and tainted) rather than orphaned.
	plan.ID = types.StringValue(project.ID)
	created := projectModel{ID: plan.ID, Name: plan.Name, Public: types.ObjectNull(publicTypes),
		DomainAccess: types.BoolValue(false), DomainRole: types.StringValue(project.DomainRole),
		Domain: types.StringNull(), SpendCapMicros: types.Int64Null(), CreatedAt: types.StringValue(project.CreatedAt)}
	resp.Diagnostics.Append(resp.State.Set(ctx, &created)...)

	r.update(ctx, &plan, &created, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.read(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	diags := r.read(ctx, &state)
	if state.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.update(ctx, &plan, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.read(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Do(ctx, client.Request{Method: http.MethodDelete, Path: projectPath(state.ID.ValueString())})
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting project", err.Error())
	}
}

func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// update sends the planned changes from prior: one merge patch for the
// project's own fields, and the spend cap through its own endpoint.
func (r *projectResource) update(ctx context.Context, plan, prior *projectModel, diags *diag.Diagnostics) {
	pid := plan.ID.ValueString()
	patch := map[string]any{}
	if !plan.Name.Equal(prior.Name) {
		if plan.Name.IsNull() {
			patch["name"] = nil
		} else {
			patch["name"] = plan.Name.ValueString()
		}
	}
	if !plan.Public.Equal(prior.Public) {
		patch["public"] = publicBody(ctx, plan.Public, diags)
	}
	if !plan.DomainAccess.Equal(prior.DomainAccess) {
		patch["domainEnabled"] = plan.DomainAccess.ValueBool()
	}
	if !plan.DomainRole.IsUnknown() && !plan.DomainRole.Equal(prior.DomainRole) {
		patch["domainRole"] = plan.DomainRole.ValueString()
	}
	if diags.HasError() {
		return
	}
	if len(patch) > 0 {
		if err := r.client.Do(ctx, client.Request{Method: http.MethodPatch, Path: projectPath(pid), Body: patch}); err != nil {
			diags.AddError("Updating project", err.Error())
			return
		}
	}
	if !plan.SpendCapMicros.Equal(prior.SpendCapMicros) {
		body := map[string]any{"spendCapMicros": nil}
		if !plan.SpendCapMicros.IsNull() {
			body["spendCapMicros"] = plan.SpendCapMicros.ValueInt64()
		}
		if err := r.client.Do(ctx, client.Request{Method: http.MethodPatch, Path: projectPath(pid, "spend-cap"), Body: body}); err != nil {
			diags.AddError("Setting spend cap", err.Error())
		}
	}
}

func publicBody(ctx context.Context, public types.Object, diags *diag.Diagnostics) any {
	if public.IsNull() {
		return nil
	}
	var m struct {
		Publish   types.Set `tfsdk:"publish"`
		Subscribe types.Set `tfsdk:"subscribe"`
	}
	diags.Append(public.As(ctx, &m, objectAsOptions)...)
	return client.Public{V: 1, Publish: setStrings(ctx, m.Publish, diags), Subscribe: setStrings(ctx, m.Subscribe, diags)}
}

// read refreshes m from the API, clearing m.ID when the project is gone.
func (r *projectResource) read(ctx context.Context, m *projectModel) diag.Diagnostics {
	var diags diag.Diagnostics
	pid := m.ID.ValueString()
	var project client.Project
	err := r.client.Do(ctx, client.Request{Method: http.MethodGet, Path: projectPath(pid), Out: &project})
	if client.IsNotFound(err) {
		m.ID = types.StringNull()
		return diags
	}
	if err != nil {
		diags.AddError("Reading project", err.Error())
		return diags
	}
	var spend client.SpendCap
	if err := r.client.Do(ctx, client.Request{Method: http.MethodGet, Path: projectPath(pid, "spend-cap"), Out: &spend}); err != nil {
		diags.AddError("Reading spend cap", err.Error())
		return diags
	}
	m.Name = optionalString(project.Name)
	m.Public = publicValue(project.Public)
	m.DomainAccess = types.BoolValue(project.Domain != nil)
	m.DomainRole = types.StringValue(project.DomainRole)
	m.Domain = optionalString(project.Domain)
	m.CreatedAt = types.StringValue(project.CreatedAt)
	m.SpendCapMicros = types.Int64Null()
	if spend.SpendCapMicros != nil {
		m.SpendCapMicros = types.Int64Value(*spend.SpendCapMicros)
	}
	return diags
}

func publicValue(p *client.Public) types.Object {
	p = p.V1()
	if p == nil {
		return types.ObjectNull(publicTypes)
	}
	return types.ObjectValueMust(publicTypes, map[string]attr.Value{
		"publish":   stringSet(p.Publish),
		"subscribe": stringSet(p.Subscribe),
	})
}
