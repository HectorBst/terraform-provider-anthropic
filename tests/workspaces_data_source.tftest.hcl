test {
  parallel = true
}

run "workspaces_data_source_lists_workspaces" {
  parallel = true
  module { source = "../examples/data-sources/workspaces" }

  assert {
    condition     = output.workspaces_count > 0
    error_message = "Expected at least one workspace to be returned."
  }

  assert {
    condition     = length(data.anthropic_workspaces.with_default.workspaces) == output.workspaces_count + 1
    error_message = "Expected include_default to add exactly the Default Workspace to the list."
  }
}
