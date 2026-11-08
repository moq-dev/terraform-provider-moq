package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/moq-dev/terraform-provider-moq/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &signingKeyResource{}
	_ resource.ResourceWithImportState = &signingKeyResource{}
)

var keyAlgorithms = []string{"HS256", "HS384", "HS512", "ES256", "ES384", "RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "EdDSA"}

type signingKeyResource struct {
	client *client.Client
}

type signingKeyModel struct {
	ProjectID  types.String `tfsdk:"project_id"`
	ID         types.String `tfsdk:"id"`
	Algorithm  types.String `tfsdk:"algorithm"`
	PublicKey  types.String `tfsdk:"public_key"`
	Label      types.String `tfsdk:"label"`
	Publish    types.Set    `tfsdk:"publish"`
	Subscribe  types.Set    `tfsdk:"subscribe"`
	KeyType    types.String `tfsdk:"key_type"`
	Enabled    types.Bool   `tfsdk:"enabled"`
	PrivateKey types.String `tfsdk:"private_key"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func newSigningKeyResource() resource.Resource {
	return &signingKeyResource{}
}

func (r *signingKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_signing_key"
}

func (r *signingKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	scope := func(description string) schema.SetAttribute {
		return schema.SetAttribute{
			MarkdownDescription: description,
			ElementType:         types.StringType,
			Optional:            true,
			PlanModifiers:       []planmodifier.Set{setplanmodifier.RequiresReplace()},
			Validators:          []validator.Set{setvalidator.ConflictsWith(path.MatchRoot("public_key"))},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A key that signs the media tokens publishers and viewers present to the relay. " +
			"Either generate one with `algorithm`, or register your own with `public_key` and sign tokens yourself.\n\n" +
			"A key is immutable: changing anything but `enabled` replaces it, which invalidates every token it signed.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{
				MarkdownDescription: "The project's immutable ID.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Key ID, the `kid` in tokens it signs.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"algorithm": schema.StringAttribute{
				MarkdownDescription: "Generate a key for this algorithm. `HS*` keys are symmetric secrets; the others are asymmetric key pairs. Exactly one of `algorithm` and `public_key` is required.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.OneOf(keyAlgorithms...),
					stringvalidator.ExactlyOneOf(path.MatchRoot("public_key")),
				},
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "Register this public key, a serialized public JWK, instead of generating one. Private parameters are refused.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{replaceIfKnown()},
			},
			"label": schema.StringAttribute{
				MarkdownDescription: "Human-readable label. The API generates one when omitted.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()},
			},
			"publish":   scope("Upper bound on the publish grants of tokens this key signs, as patterns relative to the project. Omit both `publish` and `subscribe` for no bound. Not available with `public_key`."),
			"subscribe": scope("Upper bound on the subscribe grants of tokens this key signs, as patterns relative to the project. Not available with `public_key`."),
			"key_type": schema.StringAttribute{
				MarkdownDescription: "`symmetric` or `asymmetric`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the relay accepts tokens signed by this key. A key is disabled before it is deleted.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"private_key": schema.StringAttribute{
				MarkdownDescription: "The generated private key as a serialized JWK: the shared secret of an `HS*` key, or the private half of a key pair. " +
					"A symmetric key's secret is always readable, including after an import. An asymmetric private key is returned only at creation, so an imported one leaves this null. Null for `public_key` keys.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation time, RFC 3339.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *signingKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *signingKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan signingKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid := plan.ProjectID.ValueString()

	body := map[string]any{}
	keysPath := projectPath(pid, "keys")
	if !plan.PublicKey.IsNull() {
		keysPath += "/import"
		body["publicKey"] = plan.PublicKey.ValueString()
	} else {
		body["algorithm"] = plan.Algorithm.ValueString()
		if publish := setStrings(ctx, plan.Publish, &resp.Diagnostics); publish != nil {
			body["publish"] = publish
		}
		if subscribe := setStrings(ctx, plan.Subscribe, &resp.Diagnostics); subscribe != nil {
			body["subscribe"] = subscribe
		}
	}
	if !plan.Label.IsNull() {
		body["label"] = plan.Label.ValueString()
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var key client.Key
	err := r.client.Do(ctx, client.Request{
		Method: http.MethodPost, Path: keysPath, Body: body,
		IdempotencyKey: client.NewIdempotencyKey(), Out: &key,
	})
	if err != nil {
		resp.Diagnostics.AddError("Creating signing key", err.Error())
		return
	}
	plan.ID = types.StringValue(key.ID)
	plan.PrivateKey = types.StringNull()
	if key.PrivateKey != "" {
		plan.PrivateKey = types.StringValue(key.PrivateKey)
	}
	keyState(&plan, &key)
	if key.KeyType == "symmetric" {
		// A symmetric secret stays downloadable, so the create omits it.
		secret, err := r.secret(ctx, pid, key.ID)
		if err != nil {
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			resp.Diagnostics.AddError("Reading signing key secret", err.Error())
			return
		}
		plan.PrivateKey = types.StringValue(secret)
	}
	if !plan.Enabled.ValueBool() {
		// Keep the created key in state even if disabling it fails.
		enabled := plan.Enabled
		plan.Enabled = types.BoolValue(true)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		plan.Enabled = enabled
		resp.Diagnostics.Append(r.setEnabled(ctx, &plan)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *signingKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state signingKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid, kid := state.ProjectID.ValueString(), state.ID.ValueString()
	key, err := client.Find(ctx, r.client, projectPath(pid, "keys"), func(k client.Key) bool { return k.ID == kid })
	if client.IsNotFound(err) || (err == nil && key == nil) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading signing key", err.Error())
		return
	}
	keyState(&state, key)

	// After an import, read back a symmetric key's secret. An asymmetric
	// private key is only ever returned by the create.
	if state.PrivateKey.IsNull() && key.KeyType == "symmetric" {
		secret, err := r.secret(ctx, pid, kid)
		if err != nil {
			resp.Diagnostics.AddError("Reading signing key secret", err.Error())
			return
		}
		state.PrivateKey = types.StringValue(secret)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *signingKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Only enabled changes through the API. Everything else requires
	// replacement, except a public_key recorded after an import.
	var plan signingKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.setEnabled(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *signingKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state signingKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid, kid := state.ProjectID.ValueString(), state.ID.ValueString()
	// The API deletes only a disabled key, so a live key cannot vanish by accident.
	var err error
	if state.Enabled.ValueBool() {
		err = r.client.Do(ctx, client.Request{Method: http.MethodPost, Path: projectPath(pid, "keys", kid, "disable")})
		if client.HasCode(err, "invalid_state") {
			err = nil // Disabled elsewhere since the last refresh.
		}
	}
	if err == nil {
		err = r.client.Do(ctx, client.Request{Method: http.MethodDelete, Path: projectPath(pid, "keys", kid)})
	}
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting signing key", err.Error())
	}
}

func (r *signingKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := importParts(req.ID, "project_id", "id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("private_key"), types.StringNull())...)
}

func (r *signingKeyResource) secret(ctx context.Context, pid, kid string) (string, error) {
	var private client.PrivateKey
	err := r.client.Do(ctx, client.Request{Method: http.MethodGet, Path: projectPath(pid, "keys", kid, "private"), Out: &private})
	return private.PrivateKey, err
}

func (r *signingKeyResource) setEnabled(ctx context.Context, m *signingKeyModel) diag.Diagnostics {
	var diags diag.Diagnostics
	action := "disable"
	if m.Enabled.ValueBool() {
		action = "enable"
	}
	var key client.Key
	err := r.client.Do(ctx, client.Request{
		Method: http.MethodPost, Path: projectPath(m.ProjectID.ValueString(), "keys", m.ID.ValueString(), action), Out: &key,
	})
	if err != nil {
		diags.AddError("Changing signing key admission", err.Error())
		return diags
	}
	keyState(m, &key)
	return diags
}

// keyState copies the API's view into m. Immutable inputs the API echoes in
// another form (an empty scope list, a key's public half) keep their configured
// value; after an import they are filled from the API.
func keyState(m *signingKeyModel, key *client.Key) {
	m.ID = types.StringValue(key.ID)
	m.Algorithm = types.StringValue(key.Algorithm)
	m.KeyType = types.StringValue(key.KeyType)
	m.Enabled = types.BoolValue(key.DisabledAt == nil)
	m.CreatedAt = types.StringValue(key.CreatedAt)
	m.Label = optionalString(key.Label)
	var publish, subscribe []string
	if key.Scope != nil {
		publish, subscribe = key.Scope.Publish, key.Scope.Subscribe
	}
	m.Publish = immutableSet(m.Publish, publish)
	m.Subscribe = immutableSet(m.Subscribe, subscribe)
	if m.PublicKey.IsUnknown() {
		m.PublicKey = types.StringNull()
	}
}

// immutableSet reads an optional list the API returns as empty when unset.
func immutableSet(prior types.Set, api []string) types.Set {
	if len(api) == 0 && (prior.IsNull() || prior.IsUnknown()) {
		return types.SetNull(types.StringType)
	}
	return stringSet(api)
}
