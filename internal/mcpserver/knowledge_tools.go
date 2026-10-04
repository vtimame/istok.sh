package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	knowledgeapp "github.com/vtimame/istok.sh/internal/application/knowledge"
	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/knowledge"
	"github.com/vtimame/istok.sh/internal/project"
)

func addKnowledgeTools(server *mcp.Server, projects *project.Service, service *knowledgeapp.Service, root string, actor actorIdentity) {
	mcp.AddTool(server, tool("knowledge_create", "Create draft project knowledge in Istok storage outside the repository.", false, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in knowledgeCreateInput) (*mcp.CallToolResult, KnowledgeResult, error) {
		return createKnowledge(ctx, projects, service, root, actor, in, false)
	})
	mcp.AddTool(server, tool("knowledge_distill", "Create draft knowledge from explicit provenance; review and promotion remain separate.", false, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in knowledgeCreateInput) (*mcp.CallToolResult, KnowledgeResult, error) {
		return createKnowledge(ctx, projects, service, root, actor, in, true)
	})
	mcp.AddTool(server, tool("knowledge_catalog", "List bounded knowledge metadata and snippets without full bodies.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in knowledgeCatalogInput) (*mcp.CallToolResult, KnowledgeCatalogResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), knowledgeCatalogError(err), nil
		}
		options := knowledge.CatalogOptions{Kinds: in.Kinds, Statuses: in.Statuses, Visibilities: in.Visibilities, Sensitivities: in.Sensitivities, Limit: in.Limit, Offset: in.Offset}
		items, err := service.Catalog(ctx, current.ID, options)
		if err != nil {
			return errorTool(err), knowledgeCatalogError(err), nil
		}
		return nil, knowledgeCatalogResult(items, options.EffectiveLimit(), options.Offset), nil
	})
	mcp.AddTool(server, tool("knowledge_search", "Search project knowledge and return bounded metadata and snippets.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in knowledgeSearchInput) (*mcp.CallToolResult, KnowledgeCatalogResult, error) {
		current, err := projects.Current(ctx, root)
		if err != nil {
			return errorTool(err), knowledgeCatalogError(err), nil
		}
		options := knowledge.SearchOptions{Query: in.Query, Limit: in.Limit, Offset: in.Offset}
		items, err := service.Search(ctx, current.ID, options)
		if err != nil {
			return errorTool(err), knowledgeCatalogError(err), nil
		}
		return nil, knowledgeCatalogResult(items, options.EffectiveLimit(), options.Offset), nil
	})
	show := func(name, description string) {
		mcp.AddTool(server, tool(name, description, true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in knowledgeShowInput) (*mcp.CallToolResult, KnowledgeResult, error) {
			_, item, err := currentKnowledgeItem(ctx, projects, service, root, in.KnowledgeID)
			if err != nil {
				return errorTool(err), knowledgeErrorResult(err), nil
			}
			return nil, knowledgeResult(item), nil
		})
	}
	show("knowledge_show", "Show one complete knowledge item on demand.")
	show("knowledge_read", "Read one complete knowledge item on demand.")
	mcp.AddTool(server, tool("knowledge_update", "Update draft knowledge with revision checking; current knowledge requires a replacement draft.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in knowledgeUpdateInput) (*mcp.CallToolResult, KnowledgeResult, error) {
		_, _, err := currentKnowledgeItem(ctx, projects, service, root, in.KnowledgeID)
		if err != nil {
			return errorTool(err), knowledgeErrorResult(err), nil
		}
		item, err := service.Update(ctx, in.KnowledgeID, in.ExpectedRevision, knowledge.Patch{Kind: in.Kind, Title: in.Title, Summary: in.Summary, Body: in.Body, Tags: in.Tags, Visibility: in.Visibility, Sensitivity: in.Sensitivity, Provenance: in.Provenance}, actor.context())
		if err != nil {
			return errorTool(err), knowledgeErrorResult(err), nil
		}
		return nil, knowledgeResult(item), nil
	})
	mcp.AddTool(server, tool("knowledge_promote", "Promote reviewed draft knowledge to current.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in knowledgeLifecycleInput) (*mcp.CallToolResult, KnowledgeResult, error) {
		_, _, err := currentKnowledgeItem(ctx, projects, service, root, in.KnowledgeID)
		if err != nil {
			return errorTool(err), knowledgeErrorResult(err), nil
		}
		item, err := service.Promote(ctx, in.KnowledgeID, in.ExpectedRevision, actor.context())
		if err != nil {
			return errorTool(err), knowledgeErrorResult(err), nil
		}
		return nil, knowledgeResult(item), nil
	})
	mcp.AddTool(server, tool("knowledge_review", "Record explicit review evidence for a draft.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in knowledgeReviewInput) (*mcp.CallToolResult, KnowledgeResult, error) {
		_, _, err := currentKnowledgeItem(ctx, projects, service, root, in.KnowledgeID)
		if err != nil {
			return errorTool(err), knowledgeErrorResult(err), nil
		}
		item, err := service.Review(ctx, in.KnowledgeID, in.ExpectedRevision, in.Note, actor.context())
		if err != nil {
			return errorTool(err), knowledgeErrorResult(err), nil
		}
		return nil, knowledgeResult(item), nil
	})
	mcp.AddTool(server, tool("knowledge_supersede", "Supersede current knowledge with an explicit replacement.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in knowledgeSupersedeInput) (*mcp.CallToolResult, KnowledgeSupersedeResult, error) {
		_, _, err := currentKnowledgeItem(ctx, projects, service, root, in.KnowledgeID)
		if err != nil {
			return errorTool(err), knowledgeSupersedeError(err), nil
		}
		_, _, err = currentKnowledgeItem(ctx, projects, service, root, in.ReplacementID)
		if err != nil {
			return errorTool(err), knowledgeSupersedeError(err), nil
		}
		old, replacement, err := service.Supersede(ctx, in.KnowledgeID, in.ExpectedRevision, in.ReplacementID, actor.context())
		if err != nil {
			return errorTool(err), knowledgeSupersedeError(err), nil
		}
		return nil, KnowledgeSupersedeResult{SchemaVersion: knowledgeSchemaVersion, Superseded: &old, Replacement: &replacement}, nil
	})
}

func createKnowledge(ctx context.Context, projects *project.Service, service *knowledgeapp.Service, root string, actor actorIdentity, in knowledgeCreateInput, distill bool) (*mcp.CallToolResult, KnowledgeResult, error) {
	current, err := projects.Current(ctx, root)
	if err != nil {
		return errorTool(err), knowledgeErrorResult(err), nil
	}
	input := knowledge.CreateInput{ProjectID: current.ID, Kind: defaultKnowledgeKind(in.Kind), Title: in.Title, Summary: in.Summary, Body: in.Body, Tags: in.Tags, Visibility: defaultKnowledgeVisibility(in.Visibility), Sensitivity: defaultKnowledgeSensitivity(in.Sensitivity), Provenance: in.Provenance}
	var item knowledge.Item
	if distill {
		item, err = service.Distill(ctx, input, actor.context())
	} else {
		item, err = service.Create(ctx, input, actor.context())
	}
	if err != nil {
		return errorTool(err), knowledgeErrorResult(err), nil
	}
	return nil, knowledgeResult(item), nil
}

func currentKnowledgeItem(ctx context.Context, projects *project.Service, service *knowledgeapp.Service, root, id string) (project.Project, knowledge.Item, error) {
	current, err := projects.Current(ctx, root)
	if err != nil {
		return project.Project{}, knowledge.Item{}, err
	}
	item, err := service.Get(ctx, id)
	if err != nil {
		return project.Project{}, knowledge.Item{}, err
	}
	if item.ProjectID != current.ID {
		return project.Project{}, knowledge.Item{}, knowledge.NewError(knowledge.CodeNotFound, "knowledge item was not found")
	}
	return current, item, nil
}

func knowledgeResult(item knowledge.Item) KnowledgeResult {
	return KnowledgeResult{SchemaVersion: knowledgeSchemaVersion, Knowledge: &item}
}
func knowledgeErrorResult(err error) KnowledgeResult {
	return KnowledgeResult{SchemaVersion: knowledgeSchemaVersion, Error: toolError(err)}
}
func knowledgeCatalogResult(items []knowledge.CatalogItem, limit, offset int) KnowledgeCatalogResult {
	if items == nil {
		items = []knowledge.CatalogItem{}
	}
	return KnowledgeCatalogResult{SchemaVersion: knowledgeSchemaVersion, Items: items, Limit: limit, Offset: offset}
}
func knowledgeCatalogError(err error) KnowledgeCatalogResult {
	result := knowledgeCatalogResult(nil, 0, 0)
	result.Error = toolError(err)
	return result
}
func knowledgeSupersedeError(err error) KnowledgeSupersedeResult {
	return KnowledgeSupersedeResult{SchemaVersion: knowledgeSchemaVersion, Error: toolError(err)}
}
func defaultKnowledgeKind(value knowledge.Kind) knowledge.Kind {
	if value == "" {
		return knowledge.KindNote
	}
	return value
}
func defaultKnowledgeVisibility(value contextmodel.Visibility) contextmodel.Visibility {
	if value == "" {
		return contextmodel.VisibilityShared
	}
	return value
}
func defaultKnowledgeSensitivity(value contextmodel.Sensitivity) contextmodel.Sensitivity {
	if value == "" {
		return contextmodel.SensitivityNormal
	}
	return value
}
