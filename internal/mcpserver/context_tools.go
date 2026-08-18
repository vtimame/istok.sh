package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	contextapp "github.com/vtimame/istok.sh/internal/application/context"
	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/project"
)

func addContextTools(server *mcp.Server, projects *project.Service, contexts *contextapp.Service, root string, actor actorIdentity) {
	addContextAddTool(server, projects, contexts, root, actor)
	addContextUpdateTool(server, projects, contexts, root, actor)
	addContextEnabledTools(server, projects, contexts, root, actor)
	addContextListTool(server, projects, contexts, root)
	addContextShowTool(server, projects, contexts, root)
	addContextSearchTool(server, projects, contexts, root)
	addContextPackageTool(server, projects, contexts, root)
}

func addContextEnabledTools(server *mcp.Server, projects *project.Service, contexts *contextapp.Service, root string, actor actorIdentity) {
	add := func(name string, enabled bool) {
		mcp.AddTool(server, tool(name, "Change enabled state of an instruction in the current project.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			ContextID        string `json:"context_id"`
			ExpectedRevision int64  `json:"expected_revision"`
		}) (*mcp.CallToolResult, ContextResult, error) {
			_, value, err := currentContextRecord(ctx, projects, contexts, root, in.ContextID, false)
			if err != nil {
				return errorTool(err), contextErrorResult(err), nil
			}
			value, err = contexts.Update(ctx, value.ID, in.ExpectedRevision, contextmodel.Patch{Enabled: &enabled}, actor.context())
			if err != nil {
				return errorTool(err), contextErrorResult(err), nil
			}
			return nil, contextResult(value), nil
		})
	}
	add("context_enable", true)
	add("context_disable", false)
}

func addContextAddTool(server *mcp.Server, projects *project.Service, contexts *contextapp.Service, root string, actor actorIdentity) {
	mcp.AddTool(server, tool("context_add", "Add saved context to the current project.", false, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in contextAddInput) (*mcp.CallToolResult, ContextResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), contextErrorResult(err), nil
		}

		input := contextmodel.CreateInput{
			ProjectID:   current.ID,
			Kind:        defaultContextKind(in.Kind),
			Title:       in.Title,
			Body:        in.Body,
			Tags:        in.Tags,
			Source:      defaultContextSource(in.Source),
			Visibility:  defaultContextVisibility(in.Visibility),
			Sensitivity: defaultContextSensitivity(in.Sensitivity),
			Priority:    in.Priority,
			Scope:       in.Scope,
		}
		value, err := contexts.Create(ctx, input, actor.context())
		if err != nil {
			return errorTool(err), contextErrorResult(err), nil
		}

		return nil, contextResult(value), nil
	})
}

func addContextUpdateTool(server *mcp.Server, projects *project.Service, contexts *contextapp.Service, root string, actor actorIdentity) {
	mcp.AddTool(server, tool("context_update", "Update saved context in the current project.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in contextUpdateInput) (*mcp.CallToolResult, ContextResult, error) {
		current, value, err := currentContextRecord(ctx, projects, contexts, root, in.ContextID, false)
		if err != nil {
			return errorTool(err), contextErrorResult(err), nil
		}
		_ = current

		patch := contextmodel.Patch{
			Kind:        in.Kind,
			Title:       in.Title,
			Body:        in.Body,
			Tags:        in.Tags,
			Source:      in.Source,
			Visibility:  in.Visibility,
			Sensitivity: in.Sensitivity,
			Priority:    in.Priority,
			Scope:       in.Scope,
		}
		value, err = contexts.Update(ctx, value.ID, in.ExpectedRevision, patch, actor.context())
		if err != nil {
			return errorTool(err), contextErrorResult(err), nil
		}

		return nil, contextResult(value), nil
	})
}

func addContextListTool(server *mcp.Server, projects *project.Service, contexts *contextapp.Service, root string) {
	mcp.AddTool(server, tool("context_list", "List saved context in the current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in contextListInput) (*mcp.CallToolResult, ContextListResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), contextListErrorResult(err), nil
		}
		values, err := contexts.List(ctx, current.ID, contextmodel.ListOptions{
			Kinds:           in.Kinds,
			Sources:         in.Sources,
			Visibilities:    in.Visibilities,
			Sensitivities:   in.Sensitivities,
			IncludeDeleted:  in.IncludeDeleted,
			IncludeDisabled: in.IncludeDisabled,
			Limit:           in.Limit,
		})
		if err != nil {
			return errorTool(err), contextListErrorResult(err), nil
		}

		return nil, contextListResult(values, nil), nil
	})
}

func addContextShowTool(server *mcp.Server, projects *project.Service, contexts *contextapp.Service, root string) {
	mcp.AddTool(server, tool("context_show", "Show saved context in the current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in contextShowInput) (*mcp.CallToolResult, ContextResult, error) {
		_, value, err := currentContextRecord(ctx, projects, contexts, root, in.ContextID, in.IncludeDeleted)
		if err != nil {
			return errorTool(err), contextErrorResult(err), nil
		}

		return nil, contextResult(value), nil
	})
}

func addContextSearchTool(server *mcp.Server, projects *project.Service, contexts *contextapp.Service, root string) {
	mcp.AddTool(server, tool("context_search", "Search saved context in the current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in contextSearchInput) (*mcp.CallToolResult, ContextListResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), contextListErrorResult(err), nil
		}
		values, err := contexts.Search(ctx, current.ID, contextmodel.SearchOptions{
			Query:           in.Query,
			IncludeDeleted:  in.IncludeDeleted,
			IncludeDisabled: in.IncludeDisabled,
			Limit:           in.Limit,
		})
		if err != nil {
			return errorTool(err), contextListErrorResult(err), nil
		}

		return nil, contextListResult(values, nil), nil
	})
}

func addContextPackageTool(server *mcp.Server, projects *project.Service, contexts *contextapp.Service, root string) {
	mcp.AddTool(server, tool("context_package", "Build a versioned context package for the current project.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in contextPackageInput) (*mcp.CallToolResult, ContextPackageResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), contextPackageErrorResult(err), nil
		}
		value, err := contexts.BuildPackage(ctx, current.ID, contextmodel.BuildOptions{IncludeDeleted: in.IncludeDeleted, Limit: in.Limit})
		if err != nil {
			return errorTool(err), contextPackageErrorResult(err), nil
		}

		return nil, ContextPackageResult{SchemaVersion: contextSchemaVersion, Package: &value}, nil
	})
}

func addAdminContextTools(server *mcp.Server, projects *project.Service, contexts *contextapp.Service, root string, actor actorIdentity) {
	mcp.AddTool(server, tool("context_archive", "Archive saved context in the current project.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in contextArchiveInput) (*mcp.CallToolResult, ContextResult, error) {
		_, value, err := currentContextRecord(ctx, projects, contexts, root, in.ContextID, false)
		if err != nil {
			return errorTool(err), contextErrorResult(err), nil
		}
		value, err = contexts.Archive(ctx, value.ID, in.ExpectedRevision, actor.context())
		if err != nil {
			return errorTool(err), contextErrorResult(err), nil
		}

		return nil, contextResult(value), nil
	})
}

func currentContextRecord(ctx context.Context, projects *project.Service, contexts *contextapp.Service, root, id string, includeDeleted bool) (project.Project, contextmodel.ProjectContextRecord, error) {
	current, err := projects.Current(ctx, root)
	if err != nil {
		return project.Project{}, contextmodel.ProjectContextRecord{}, err
	}
	value, err := contexts.Get(ctx, id, includeDeleted)
	if err != nil {
		return project.Project{}, contextmodel.ProjectContextRecord{}, err
	}
	if value.ProjectID != current.ID {
		return project.Project{}, contextmodel.ProjectContextRecord{}, contextmodel.NewError(contextmodel.CodeNotFound, "context record was not found")
	}

	return current, value, nil
}

func contextResult(value contextmodel.ProjectContextRecord) ContextResult {
	return ContextResult{SchemaVersion: contextSchemaVersion, Context: &value}
}

func contextErrorResult(err error) ContextResult {
	return ContextResult{SchemaVersion: contextSchemaVersion, Error: toolError(err)}
}

func contextListErrorResult(err error) ContextListResult {
	return contextListResult(nil, toolError(err))
}

func contextListResult(values []contextmodel.ProjectContextRecord, toolErr *ToolError) ContextListResult {
	if values == nil {
		values = []contextmodel.ProjectContextRecord{}
	}

	return ContextListResult{SchemaVersion: contextSchemaVersion, Contexts: values, Error: toolErr}
}

func contextPackageErrorResult(err error) ContextPackageResult {
	return ContextPackageResult{SchemaVersion: contextSchemaVersion, Error: toolError(err)}
}

func defaultContextKind(value contextmodel.Kind) contextmodel.Kind {
	if value == "" {
		return contextmodel.KindNote
	}

	return value
}

func defaultContextSource(value contextmodel.Source) contextmodel.Source {
	if value == "" {
		return contextmodel.SourceAgent
	}

	return value
}

func defaultContextVisibility(value contextmodel.Visibility) contextmodel.Visibility {
	if value == "" {
		return contextmodel.VisibilityShared
	}

	return value
}

func defaultContextSensitivity(value contextmodel.Sensitivity) contextmodel.Sensitivity {
	if value == "" {
		return contextmodel.SensitivityNormal
	}

	return value
}
