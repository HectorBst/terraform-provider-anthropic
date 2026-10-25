// Copyright (c) Ippon
// SPDX-License-Identifier: MPL-2.0

package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// TestMapAgentResponseToState_customToolInputSchema is a regression test for the
// anthropic-sdk-go upgrade (v1.37 -> v1.51) that changed how the
// BetaManagedAgentsCustomToolInputSchema struct marshals: field reordering plus a
// spurious "ExtraFields":null key. Re-marshaling the parsed schema with
// encoding/json produced a state value that no longer matched the user's planned
// input_schema string, triggering "Provider produced inconsistent result after
// apply". The mapping now preserves the raw API JSON via RawJSON() and stores it
// as a jsontypes.Normalized value, so semantic (key-order-insensitive) equality
// holds against the user's jsonencode() output.
func TestMapAgentResponseToState_customToolInputSchema(t *testing.T) {
	t.Parallel()

	// API response schema: "type" first, no ExtraFields key.
	const apiInputSchema = `{"type":"object","properties":{"email":{"description":"The user's email address","type":"string"}},"required":["email"]}`

	agentJSON := `{
		"id": "agent_123",
		"name": "test",
		"model": {"id": "claude-sonnet-4-6"},
		"version": 1,
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-01T00:00:00Z",
		"tools": [
			{
				"type": "custom",
				"name": "lookup_user",
				"description": "Look up a user",
				"input_schema": ` + apiInputSchema + `
			}
		]
	}`

	var agent anthropic.BetaManagedAgentsAgent
	if err := json.Unmarshal([]byte(agentJSON), &agent); err != nil {
		t.Fatalf("failed to unmarshal agent fixture: %s", err)
	}

	var data AgentResourceModel
	if diags := mapAgentResponseToState(context.Background(), &agent, &data); diags.HasError() {
		t.Fatalf("mapAgentResponseToState returned errors: %+v", diags)
	}

	var tools []agentCustomToolModel
	if diags := data.CustomTools.ElementsAs(context.Background(), &tools, false); diags.HasError() {
		t.Fatalf("failed to read custom tools: %+v", diags)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 custom tool, got %d", len(tools))
	}

	got := tools[0].InputSchema
	if got.IsNull() || got.IsUnknown() {
		t.Fatalf("input_schema should be set, got null/unknown")
	}

	// The state value must not contain the encoding/json artifact that broke
	// round-tripping under the SDK upgrade.
	if strings.Contains(got.ValueString(), "ExtraFields") {
		t.Errorf("input_schema must not contain ExtraFields; got %q", got.ValueString())
	}

	// Semantic equality must hold against the user's jsonencode() output, which
	// orders keys alphabetically (properties, required, type) — a different order
	// than the API response. Without jsontypes.Normalized this mismatch is what
	// surfaced as "inconsistent result after apply".
	const userInputSchema = `{"properties":{"email":{"description":"The user's email address","type":"string"}},"required":["email"],"type":"object"}`
	planned := jsontypes.NewNormalizedValue(userInputSchema)
	equal, diags := planned.StringSemanticEquals(context.Background(), got)
	if diags.HasError() {
		t.Fatalf("semantic equality check returned errors: %+v", diags)
	}
	if !equal {
		t.Errorf("planned and applied input_schema should be semantically equal\nplanned: %s\napplied: %s", userInputSchema, got.ValueString())
	}
}

// TestBuildAgentToolConfigParams is a regression test for the anthropic-sdk-go
// upgrade (v1.58 -> v1.67) that replaced the flat
// BetaManagedAgentsAgentToolConfigParams (a `name` enum plus shared `enabled` /
// `permission_policy` fields) with a discriminated union carrying one struct per
// built-in tool. The Terraform `name` attribute now selects the union branch, so
// this asserts every schema-accepted name lands on its own branch with the
// enabled flag and permission policy carried across.
func TestBuildAgentToolConfigParams(t *testing.T) {
	t.Parallel()

	// Each case checks the branch the name must select, and reads back the
	// enabled/permission-policy fields from that branch.
	tests := []struct {
		name    string
		extract func(anthropic.BetaManagedAgentsAgentToolConfigParamsUnion) (set bool, enabled param.Opt[bool], allow bool, ask bool)
	}{
		{"bash", func(u anthropic.BetaManagedAgentsAgentToolConfigParamsUnion) (bool, param.Opt[bool], bool, bool) {
			c := u.OfBash
			if c == nil {
				return false, param.Opt[bool]{}, false, false
			}
			return true, c.Enabled, c.PermissionPolicy.OfAlwaysAllow != nil, c.PermissionPolicy.OfAlwaysAsk != nil
		}},
		{"edit", func(u anthropic.BetaManagedAgentsAgentToolConfigParamsUnion) (bool, param.Opt[bool], bool, bool) {
			c := u.OfEdit
			if c == nil {
				return false, param.Opt[bool]{}, false, false
			}
			return true, c.Enabled, c.PermissionPolicy.OfAlwaysAllow != nil, c.PermissionPolicy.OfAlwaysAsk != nil
		}},
		{"read", func(u anthropic.BetaManagedAgentsAgentToolConfigParamsUnion) (bool, param.Opt[bool], bool, bool) {
			c := u.OfRead
			if c == nil {
				return false, param.Opt[bool]{}, false, false
			}
			return true, c.Enabled, c.PermissionPolicy.OfAlwaysAllow != nil, c.PermissionPolicy.OfAlwaysAsk != nil
		}},
		{"write", func(u anthropic.BetaManagedAgentsAgentToolConfigParamsUnion) (bool, param.Opt[bool], bool, bool) {
			c := u.OfWrite
			if c == nil {
				return false, param.Opt[bool]{}, false, false
			}
			return true, c.Enabled, c.PermissionPolicy.OfAlwaysAllow != nil, c.PermissionPolicy.OfAlwaysAsk != nil
		}},
		{"glob", func(u anthropic.BetaManagedAgentsAgentToolConfigParamsUnion) (bool, param.Opt[bool], bool, bool) {
			c := u.OfGlob
			if c == nil {
				return false, param.Opt[bool]{}, false, false
			}
			return true, c.Enabled, c.PermissionPolicy.OfAlwaysAllow != nil, c.PermissionPolicy.OfAlwaysAsk != nil
		}},
		{"grep", func(u anthropic.BetaManagedAgentsAgentToolConfigParamsUnion) (bool, param.Opt[bool], bool, bool) {
			c := u.OfGrep
			if c == nil {
				return false, param.Opt[bool]{}, false, false
			}
			return true, c.Enabled, c.PermissionPolicy.OfAlwaysAllow != nil, c.PermissionPolicy.OfAlwaysAsk != nil
		}},
		{"web_fetch", func(u anthropic.BetaManagedAgentsAgentToolConfigParamsUnion) (bool, param.Opt[bool], bool, bool) {
			c := u.OfWebFetch
			if c == nil {
				return false, param.Opt[bool]{}, false, false
			}
			return true, c.Enabled, c.PermissionPolicy.OfAlwaysAllow != nil, c.PermissionPolicy.OfAlwaysAsk != nil
		}},
		{"web_search", func(u anthropic.BetaManagedAgentsAgentToolConfigParamsUnion) (bool, param.Opt[bool], bool, bool) {
			c := u.OfWebSearch
			if c == nil {
				return false, param.Opt[bool]{}, false, false
			}
			return true, c.Enabled, c.PermissionPolicy.OfAlwaysAllow != nil, c.PermissionPolicy.OfAlwaysAsk != nil
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name+"/always_allow", func(t *testing.T) {
			t.Parallel()

			u, diags := buildAgentToolConfigParams(tc.name, param.NewOpt(true), "always_allow")
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			set, enabled, allow, ask := tc.extract(u)
			if !set {
				t.Fatalf("name %q did not select its own union branch", tc.name)
			}
			if !enabled.Valid() || !enabled.Value {
				t.Errorf("enabled = %+v, want set to true", enabled)
			}
			if !allow || ask {
				t.Errorf("permission policy = (allow=%t, ask=%t), want (true, false)", allow, ask)
			}
		})

		t.Run(tc.name+"/always_ask", func(t *testing.T) {
			t.Parallel()

			u, diags := buildAgentToolConfigParams(tc.name, param.NewOpt(false), "always_ask")
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			set, enabled, allow, ask := tc.extract(u)
			if !set {
				t.Fatalf("name %q did not select its own union branch", tc.name)
			}
			if !enabled.Valid() || enabled.Value {
				t.Errorf("enabled = %+v, want set to false", enabled)
			}
			if allow || !ask {
				t.Errorf("permission policy = (allow=%t, ask=%t), want (false, true)", allow, ask)
			}
		})

		// An omitted permission_policy must leave both variants unset so the
		// field is elided from the request and the toolset default applies.
		t.Run(tc.name+"/no_policy", func(t *testing.T) {
			t.Parallel()

			u, diags := buildAgentToolConfigParams(tc.name, param.Opt[bool]{}, "")
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			set, enabled, allow, ask := tc.extract(u)
			if !set {
				t.Fatalf("name %q did not select its own union branch", tc.name)
			}
			if enabled.Valid() {
				t.Errorf("enabled = %+v, want unset", enabled)
			}
			if allow || ask {
				t.Errorf("permission policy = (allow=%t, ask=%t), want both unset", allow, ask)
			}
		})
	}
}

// TestBuildAgentToolConfigParams_unknownName guards the union's default branch:
// a name outside the eight built-ins must surface a diagnostic rather than
// silently produce an empty union that marshals without a discriminator.
func TestBuildAgentToolConfigParams_unknownName(t *testing.T) {
	t.Parallel()

	_, diags := buildAgentToolConfigParams("not_a_tool", param.NewOpt(true), "always_allow")
	if !diags.HasError() {
		t.Fatal("expected an error diagnostic for an unsupported tool name, got none")
	}
	if got := diags.Errors()[0].Detail(); !strings.Contains(got, "not_a_tool") {
		t.Errorf("diagnostic detail = %q, want it to name the offending value", got)
	}
}

// toolConfigsList builds a configs list in state shape from typed entries.
func toolConfigsList(t *testing.T, attrTypes map[string]attr.Type, entries ...agentToolConfigModel) types.List {
	t.Helper()
	objs := make([]attr.Value, 0, len(entries))
	for _, e := range entries {
		obj, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
			"name":              e.Name,
			"enabled":           e.Enabled,
			"permission_policy": e.PermissionPolicy,
		})
		if diags.HasError() {
			t.Fatalf("build config object: %+v", diags)
		}
		objs = append(objs, obj)
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: attrTypes}, objs)
	if diags.HasError() {
		t.Fatalf("build configs list: %+v", diags)
	}
	return list
}

func mcpToolsetsList(t *testing.T, toolsets ...agentMCPToolsetModel) types.List {
	t.Helper()
	objs := make([]attr.Value, 0, len(toolsets))
	for _, ts := range toolsets {
		obj, diags := types.ObjectValue(agentMCPToolsetAttrTypes, map[string]attr.Value{
			"mcp_server_name":           ts.MCPServerName,
			"default_enabled":           ts.DefaultEnabled,
			"default_permission_policy": ts.DefaultPermissionPolicy,
			"configs":                   ts.Configs,
		})
		if diags.HasError() {
			t.Fatalf("build toolset object: %+v", diags)
		}
		objs = append(objs, obj)
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: agentMCPToolsetAttrTypes}, objs)
	if diags.HasError() {
		t.Fatalf("build toolsets list: %+v", diags)
	}
	return list
}

func parseAgentWithTools(t *testing.T, tools string) *anthropic.BetaManagedAgentsAgent {
	t.Helper()
	var agent anthropic.BetaManagedAgentsAgent
	raw := `{"id":"agent_1","name":"n","model":{"id":"claude-sonnet-4-6"},"version":1,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","tools":` + tools + `}`
	if err := json.Unmarshal([]byte(raw), &agent); err != nil {
		t.Fatalf("unmarshal: %s", err)
	}
	return &agent
}

func mcpToolsetsFromState(t *testing.T, data AgentResourceModel) []agentMCPToolsetModel {
	t.Helper()
	var toolsets []agentMCPToolsetModel
	if diags := data.MCPToolsets.ElementsAs(context.Background(), &toolsets, false); diags.HasError() {
		t.Fatalf("read mcp_toolsets: %+v", diags)
	}
	return toolsets
}

func toolConfigsFromList(t *testing.T, list types.List) []agentToolConfigModel {
	t.Helper()
	if list.IsNull() {
		return nil
	}
	var configs []agentToolConfigModel
	if diags := list.ElementsAs(context.Background(), &configs, false); diags.HasError() {
		t.Fatalf("read configs: %+v", diags)
	}
	return configs
}

// An MCP toolset declared without configs, where the live agent gained an
// always_allow per-tool config out of band, must show that config in state so
// the plan reverts it (security finding: Read used to drop it).
func TestMapMCPToolsetsToState_outOfBandConfigOnNullConfigs(t *testing.T) {
	t.Parallel()

	data := AgentResourceModel{MCPToolsets: mcpToolsetsList(t, agentMCPToolsetModel{
		MCPServerName:           types.StringValue("crm"),
		DefaultEnabled:          types.BoolValue(true),
		DefaultPermissionPolicy: types.StringValue("always_ask"),
		Configs:                 types.ListNull(types.ObjectType{AttrTypes: agentMCPToolConfigAttrTypes}),
	})}
	agent := parseAgentWithTools(t, `[{"type":"mcp_toolset","mcp_server_name":"crm",
		"default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}},
		"configs":[{"name":"delete_record","enabled":true,"permission_policy":{"type":"always_allow"}}]}]`)

	if diags := mapAgentResponseToState(context.Background(), agent, &data); diags.HasError() {
		t.Fatalf("diags: %+v", diags)
	}
	toolsets := mcpToolsetsFromState(t, data)
	if len(toolsets) != 1 {
		t.Fatalf("got %d toolsets, want 1", len(toolsets))
	}
	configs := toolConfigsFromList(t, toolsets[0].Configs)
	if len(configs) != 1 {
		t.Fatalf("got %d configs, want the out-of-band one", len(configs))
	}
	if configs[0].Name.ValueString() != "delete_record" || configs[0].PermissionPolicy.ValueString() != "always_allow" {
		t.Errorf("got config %+v, want delete_record/always_allow", configs[0])
	}
}

// A toolset the API returns for a server the configuration does not declare a
// toolset for must be appended after the tracked ones, not dropped.
func TestMapMCPToolsetsToState_outOfBandToolset(t *testing.T) {
	t.Parallel()

	data := AgentResourceModel{MCPToolsets: mcpToolsetsList(t, agentMCPToolsetModel{
		MCPServerName:           types.StringValue("crm"),
		DefaultEnabled:          types.BoolValue(true),
		DefaultPermissionPolicy: types.StringValue("always_ask"),
		Configs:                 types.ListNull(types.ObjectType{AttrTypes: agentMCPToolConfigAttrTypes}),
	})}
	agent := parseAgentWithTools(t, `[
		{"type":"mcp_toolset","mcp_server_name":"billing","default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}},
		{"type":"mcp_toolset","mcp_server_name":"crm","default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}}}]`)

	if diags := mapAgentResponseToState(context.Background(), agent, &data); diags.HasError() {
		t.Fatalf("diags: %+v", diags)
	}
	toolsets := mcpToolsetsFromState(t, data)
	if len(toolsets) != 2 {
		t.Fatalf("got %d toolsets, want 2", len(toolsets))
	}
	if toolsets[0].MCPServerName.ValueString() != "crm" || toolsets[1].MCPServerName.ValueString() != "billing" {
		t.Errorf("got order %s, %s; want the tracked toolset first, then the out-of-band one",
			toolsets[0].MCPServerName.ValueString(), toolsets[1].MCPServerName.ValueString())
	}
	if toolsets[1].DefaultPermissionPolicy.ValueString() != "always_allow" {
		t.Errorf("out-of-band toolset policy = %s, want always_allow", toolsets[1].DefaultPermissionPolicy.ValueString())
	}
}

// A field left unset in configuration stays null while the API echoes the
// toolset default it inherits, and surfaces once it diverges from it.
func TestMapToolConfigsToState_unsetFields(t *testing.T) {
	t.Parallel()

	current := toolConfigsList(t, agentMCPToolConfigAttrTypes,
		agentToolConfigModel{Name: types.StringValue("search"), Enabled: types.BoolNull(), PermissionPolicy: types.StringValue("always_allow")},
		agentToolConfigModel{Name: types.StringValue("delete_record"), Enabled: types.BoolValue(false), PermissionPolicy: types.StringNull()},
	)

	tests := []struct {
		name       string
		api        []apiToolConfig
		wantSearch agentToolConfigModel
		wantDelete agentToolConfigModel
	}{
		{
			name: "echoed defaults stay null",
			api: []apiToolConfig{
				{name: "search", enabled: true, permissionPolicy: "always_allow"},
				{name: "delete_record", enabled: false, permissionPolicy: "always_ask"},
			},
			wantSearch: agentToolConfigModel{Name: types.StringValue("search"), Enabled: types.BoolNull(), PermissionPolicy: types.StringValue("always_allow")},
			wantDelete: agentToolConfigModel{Name: types.StringValue("delete_record"), Enabled: types.BoolValue(false), PermissionPolicy: types.StringNull()},
		},
		{
			name: "diverging values surface",
			api: []apiToolConfig{
				{name: "search", enabled: false, permissionPolicy: "always_allow"},
				{name: "delete_record", enabled: false, permissionPolicy: "always_allow"},
			},
			wantSearch: agentToolConfigModel{Name: types.StringValue("search"), Enabled: types.BoolValue(false), PermissionPolicy: types.StringValue("always_allow")},
			wantDelete: agentToolConfigModel{Name: types.StringValue("delete_record"), Enabled: types.BoolValue(false), PermissionPolicy: types.StringValue("always_allow")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			list, diags := mapToolConfigsToState(context.Background(), current, tt.api, true, "always_ask", agentMCPToolConfigAttrTypes)
			if diags.HasError() {
				t.Fatalf("diags: %+v", diags)
			}
			got := toolConfigsFromList(t, list)
			if len(got) != 2 {
				t.Fatalf("got %d configs, want 2", len(got))
			}
			for i, want := range []agentToolConfigModel{tt.wantSearch, tt.wantDelete} {
				if !got[i].Name.Equal(want.Name) || !got[i].Enabled.Equal(want.Enabled) || !got[i].PermissionPolicy.Equal(want.PermissionPolicy) {
					t.Errorf("config %d = %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}

// A tracked config the API no longer returns is omitted (re-created on apply),
// and a config for an untracked tool is appended after the tracked ones.
func TestMapToolConfigsToState_trackedAndExtra(t *testing.T) {
	t.Parallel()

	current := toolConfigsList(t, agentToolConfigAttrTypes,
		agentToolConfigModel{Name: types.StringValue("bash"), Enabled: types.BoolNull(), PermissionPolicy: types.StringValue("always_ask")},
		agentToolConfigModel{Name: types.StringValue("write"), Enabled: types.BoolValue(false), PermissionPolicy: types.StringNull()},
	)
	api := []apiToolConfig{
		{name: "web_fetch", enabled: true, permissionPolicy: "always_allow"},
		{name: "bash", enabled: true, permissionPolicy: "always_ask"},
	}

	list, diags := mapToolConfigsToState(context.Background(), current, api, true, "always_allow", agentToolConfigAttrTypes)
	if diags.HasError() {
		t.Fatalf("diags: %+v", diags)
	}
	got := toolConfigsFromList(t, list)
	if len(got) != 2 {
		t.Fatalf("got %d configs, want bash then web_fetch", len(got))
	}
	if got[0].Name.ValueString() != "bash" || got[1].Name.ValueString() != "web_fetch" {
		t.Errorf("got %s, %s; want bash, web_fetch", got[0].Name.ValueString(), got[1].Name.ValueString())
	}
	if !got[1].Enabled.Equal(types.BoolValue(true)) || got[1].PermissionPolicy.ValueString() != "always_allow" {
		t.Errorf("extra config = %+v, want every field set from the API", got[1])
	}
}

// The built-in agent toolset gets the same treatment as MCP toolsets: an
// override for an untracked tool is surfaced rather than filtered out.
func TestMapAgentToolsetToState_outOfBandConfig(t *testing.T) {
	t.Parallel()

	current, diags := types.ObjectValue(agentToolsetAttrTypes, map[string]attr.Value{
		"default_enabled":           types.BoolValue(true),
		"default_permission_policy": types.StringValue("always_ask"),
		"configs":                   types.ListNull(types.ObjectType{AttrTypes: agentToolConfigAttrTypes}),
	})
	if diags.HasError() {
		t.Fatalf("build toolset: %+v", diags)
	}
	data := AgentResourceModel{AgentToolset: current}
	agent := parseAgentWithTools(t, `[{"type":"agent_toolset_20260401",
		"default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}},
		"configs":[{"type":"bash","name":"bash","enabled":true,"permission_policy":{"type":"always_allow"}}]}]`)

	if diags := mapAgentResponseToState(context.Background(), agent, &data); diags.HasError() {
		t.Fatalf("diags: %+v", diags)
	}
	var toolset agentToolsetModel
	if diags := data.AgentToolset.As(context.Background(), &toolset, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("read agent_toolset: %+v", diags)
	}
	configs := toolConfigsFromList(t, toolset.Configs)
	if len(configs) != 1 || configs[0].Name.ValueString() != "bash" || configs[0].PermissionPolicy.ValueString() != "always_allow" {
		t.Errorf("got configs %+v, want the out-of-band bash/always_allow override", configs)
	}
}

// Import (no prior state) still maps every toolset and config verbatim.
func TestMapMCPToolsetsToState_import(t *testing.T) {
	t.Parallel()

	data := AgentResourceModel{
		AgentToolset: types.ObjectNull(agentToolsetAttrTypes),
		MCPToolsets:  types.ListNull(types.ObjectType{AttrTypes: agentMCPToolsetAttrTypes}),
	}
	agent := parseAgentWithTools(t, `[{"type":"mcp_toolset","mcp_server_name":"crm",
		"default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}},
		"configs":[{"name":"search","enabled":true,"permission_policy":{"type":"always_ask"}}]}]`)

	if diags := mapAgentResponseToState(context.Background(), agent, &data); diags.HasError() {
		t.Fatalf("diags: %+v", diags)
	}
	toolsets := mcpToolsetsFromState(t, data)
	if len(toolsets) != 1 {
		t.Fatalf("got %d toolsets, want 1", len(toolsets))
	}
	configs := toolConfigsFromList(t, toolsets[0].Configs)
	if len(configs) != 1 || !configs[0].Enabled.Equal(types.BoolValue(true)) || configs[0].PermissionPolicy.ValueString() != "always_ask" {
		t.Errorf("got configs %+v, want the API config verbatim", configs)
	}
}

func modelParamsJSON(t *testing.T, m anthropic.BetaManagedAgentsModelConfigParams) map[string]any {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal model params: %s", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal model params: %s", err)
	}
	return out
}

func TestBuildModelConfigParams(t *testing.T) {
	t.Parallel()

	base := func(effort, geo types.String) AgentResourceModel {
		return AgentResourceModel{
			Model:             types.StringValue("claude-sonnet-4-6"),
			ModelSpeed:        types.StringNull(),
			ModelEffort:       effort,
			ModelInferenceGeo: geo,
		}
	}

	tests := []struct {
		name string
		plan AgentResourceModel
		want map[string]any
	}{
		{
			name: "create with effort and geo",
			plan: base(types.StringValue("high"), types.StringValue("us")),
			want: map[string]any{"id": "claude-sonnet-4-6", "effort": "high", "inference_geo": "us"},
		},
		{
			name: "create with unknown effort omits it",
			plan: base(types.StringUnknown(), types.StringNull()),
			want: map[string]any{"id": "claude-sonnet-4-6"},
		},
		{
			name: "update sends planned effort",
			plan: base(types.StringValue("low"), types.StringNull()),
			want: map[string]any{"id": "claude-sonnet-4-6", "effort": "low"},
		},
		{
			name: "update with null geo omits it so the pin is cleared",
			plan: base(types.StringValue("high"), types.StringNull()),
			want: map[string]any{"id": "claude-sonnet-4-6", "effort": "high"},
		},
		{
			name: "update keeps unchanged geo",
			plan: base(types.StringValue("high"), types.StringValue("us")),
			want: map[string]any{"id": "claude-sonnet-4-6", "effort": "high", "inference_geo": "us"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := modelParamsJSON(t, buildModelConfigParams(tt.plan))
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("%s: got %v, want %v (full: %v)", k, got[k], v, got)
				}
			}
		})
	}
}

func TestMapAgentResponseToState_modelEffortAndGeo(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		data := AgentResourceModel{ModelEffort: types.StringUnknown()}
		agent := parseAgentFixture(t, `{"id":"claude-sonnet-4-6","effort":{"type":"xhigh"},"inference_geo":"us"}`)
		if diags := mapAgentResponseToState(context.Background(), agent, &data); diags.HasError() {
			t.Fatalf("diags: %+v", diags)
		}
		if data.ModelEffort.ValueString() != "xhigh" || data.ModelInferenceGeo.ValueString() != "us" {
			t.Errorf("got effort=%v geo=%v", data.ModelEffort, data.ModelInferenceGeo)
		}
	})

	t.Run("absent resolves unknown to null", func(t *testing.T) {
		t.Parallel()
		data := AgentResourceModel{ModelEffort: types.StringUnknown()}
		agent := parseAgentFixture(t, `{"id":"claude-sonnet-4-6"}`)
		if diags := mapAgentResponseToState(context.Background(), agent, &data); diags.HasError() {
			t.Fatalf("diags: %+v", diags)
		}
		if !data.ModelEffort.IsNull() || !data.ModelInferenceGeo.IsNull() {
			t.Errorf("got effort=%v geo=%v, want null", data.ModelEffort, data.ModelInferenceGeo)
		}
	})

	t.Run("empty and null map to null", func(t *testing.T) {
		t.Parallel()
		for _, model := range []string{
			`{"id":"claude-sonnet-4-6","effort":null,"inference_geo":""}`,
			`{"id":"claude-sonnet-4-6","inference_geo":null}`,
		} {
			data := AgentResourceModel{}
			if diags := mapAgentResponseToState(context.Background(), parseAgentFixture(t, model), &data); diags.HasError() {
				t.Fatalf("diags: %+v", diags)
			}
			if !data.ModelEffort.IsNull() || !data.ModelInferenceGeo.IsNull() {
				t.Errorf("%s: got effort=%v geo=%v, want null", model, data.ModelEffort, data.ModelInferenceGeo)
			}
		}
	})
}
