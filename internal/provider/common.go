package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/moq-dev/terraform-provider-moq/internal/client"
)

// clientFrom unwraps the provider's configured client. ProviderData is nil
// while Terraform validates configuration before the provider is configured.
func clientFrom(data any, diags *diag.Diagnostics) *client.Client {
	if data == nil {
		return nil
	}
	c, ok := data.(*client.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", data))
		return nil
	}
	return c
}

var objectAsOptions = basetypes.ObjectAsOptions{}

func stringSet(values []string) types.Set {
	elems := make([]attr.Value, len(values))
	for i, v := range values {
		elems[i] = types.StringValue(v)
	}
	return types.SetValueMust(types.StringType, elems)
}

// setStrings returns nil for a null set, so the field is omitted.
func setStrings(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	out := []string{}
	diags.Append(set.ElementsAs(ctx, &out, false)...)
	return out
}

func optionalString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// importParts splits an import ID of the form "project_id/id".
func importParts(id string, names ...string) ([]string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != len(names) {
		return nil, fmt.Errorf("expected an import ID of the form %s, got %q", strings.Join(names, "/"), id)
	}
	for i, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("the %s part of import ID %q is empty", names[i], id)
		}
	}
	return parts, nil
}

func projectPath(pid string, rest ...string) string {
	p := "/projects/" + client.Escape(pid)
	for _, r := range rest {
		p += "/" + client.Escape(r)
	}
	return p
}

// replaceIfKnown replaces the resource when a write-only input changes, but not
// when its prior value is null because the API never returns it, as after an
// import. The new value is then recorded without an API call.
func replaceIfKnown() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !req.StateValue.IsNull()
		},
		"Changing this value replaces the resource, unless it was imported without it.",
		"Changing this value replaces the resource, unless it was imported without it.",
	)
}
