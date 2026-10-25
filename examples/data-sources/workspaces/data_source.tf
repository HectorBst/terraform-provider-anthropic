data "anthropic_workspaces" "all" {}

output "workspaces_count" {
  description = "Number of workspaces in the organization."
  value       = length(data.anthropic_workspaces.all.workspaces)
}

# The Default Workspace is listed only on request. Its ID is what API keys
# bound to it report in scope.workspace_id.
data "anthropic_workspaces" "with_default" {
  include_default = true
}

output "default_workspace_id" {
  description = "ID of the organization's Default Workspace."
  value       = one([for ws in data.anthropic_workspaces.with_default.workspaces : ws.id if ws.name == "Default"])
}
