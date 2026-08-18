package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"

	contextapp "s26.dev/istok-cli/internal/application/context"
	indexingapp "s26.dev/istok-cli/internal/application/indexing"
	runapp "s26.dev/istok-cli/internal/application/run"
	runworkflow "s26.dev/istok-cli/internal/application/runworkflow"
	taskapp "s26.dev/istok-cli/internal/application/task"
	"s26.dev/istok-cli/internal/buildinfo"
	"s26.dev/istok-cli/internal/project"
)

type Profile string

const (
	Worker     Profile = "worker"
	Supervisor Profile = "supervisor"
	Admin      Profile = "admin"
)

type Config struct {
	Profile   Profile
	Root      string
	ActorID   string
	ActorName string
}

type HealthChecker interface {
	PingContext(context.Context) error
}

type HealthStatus struct {
	SchemaVersion string `json:"schema_version"`
	Status        string `json:"status"`
	Version       string `json:"version"`
}

type Result struct {
	SchemaVersion string           `json:"schema_version"`
	Project       *project.Project `json:"project,omitempty"`
	Error         *ToolError       `json:"error,omitempty"`
}

type InitResult struct {
	SchemaVersion string           `json:"schema_version"`
	Project       *project.Project `json:"project,omitempty"`
	Created       bool             `json:"created"`
	Error         *ToolError       `json:"error,omitempty"`
}

type ListResult struct {
	SchemaVersion string            `json:"schema_version"`
	Projects      []project.Project `json:"projects"`
	Error         *ToolError        `json:"error,omitempty"`
}

type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResult struct {
	SchemaVersion string `json:"schema_version"`
	Error         struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func Module(config Config) fx.Option {
	return fx.Module("mcp", fx.Supply(config), fx.Provide(New))
}

func New(health HealthChecker, service *project.Service, tasks *taskapp.Service, contexts *contextapp.Service, indexes *indexingapp.Service, runs *runapp.Service, workflow *runworkflow.Service, info buildinfo.Info, config Config) (*mcp.Server, error) {
	if config.Profile == "" {
		config.Profile = Worker
	}
	if config.Profile != Worker && config.Profile != Supervisor && config.Profile != Admin {
		return nil, fmt.Errorf("unsupported MCP profile %q", config.Profile)
	}

	actor, err := newActor(config.ActorID, config.ActorName)
	if err != nil {
		return nil, fmt.Errorf("create MCP actor identity: %w", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "istok", Version: info.Version}, nil)
	mcp.AddTool(server, tool("health", "Check Istok and its SQLite storage.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, HealthStatus, error) {
		if err := health.PingContext(ctx); err != nil {
			return errorTool(err), HealthStatus{}, nil
		}
		return nil, HealthStatus{SchemaVersion: "1", Status: "ok", Version: info.Version}, nil
	})
	mcp.AddTool(server, tool("project_current", "Get the project associated with the server root.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, Result, error) {
		p, err := service.Current(ctx, config.Root)
		if err != nil {
			return errorTool(err), errorResult(err), nil
		}
		return nil, result(p), nil
	})
	mcp.AddTool(server, tool("project_init", "Initialize the server root as a project.", false, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Name string `json:"name,omitempty"`
	}) (*mcp.CallToolResult, InitResult, error) {
		initialized, err := service.Init(ctx, config.Root, in.Name)
		if err != nil {
			return errorTool(err), InitResult{SchemaVersion: "1", Error: toolError(err)}, nil
		}

		return nil, InitResult{SchemaVersion: "1", Project: &initialized.Project, Created: initialized.Created}, nil
	})
	addTaskTools(server, service, tasks, config.Root, actor)
	addContextTools(server, service, contexts, config.Root, actor)
	addIndexTools(server, service, indexes, config.Root)
	addRunTools(server, service, runs, workflow, config.Root, actor, config.Profile)
	if config.Profile == Admin {
		addAdmin(server, service, tasks, contexts, config.Root, actor)
	}

	return server, nil
}

func addAdmin(server *mcp.Server, service *project.Service, tasks *taskapp.Service, contexts *contextapp.Service, root string, actor actorIdentity) {
	mcp.AddTool(server, tool("project_list", "List local projects.", true, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		IncludeDeleted bool `json:"include_deleted,omitempty"`
	}) (*mcp.CallToolResult, ListResult, error) {
		items, err := service.List(ctx, in.IncludeDeleted)
		if err != nil {
			return errorTool(err), ListResult{SchemaVersion: "1", Error: toolError(err)}, nil
		}
		return nil, ListResult{SchemaVersion: "1", Projects: items}, nil
	})
	mcp.AddTool(server, tool("project_rename", "Rename a project.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in renameInput) (*mcp.CallToolResult, Result, error) {
		if err := exactProjectID(in.ProjectID); err != nil {
			return errorTool(err), errorResult(err), nil
		}
		p, err := service.Rename(ctx, in.ProjectID, in.Name, &in.ExpectedRevision)
		if err != nil {
			return errorTool(err), errorResult(err), nil
		}
		return nil, result(p), nil
	})
	mcp.AddTool(server, tool("project_rebind", "Bind a project to the server root.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in revisionInput) (*mcp.CallToolResult, Result, error) {
		if err := exactProjectID(in.ProjectID); err != nil {
			return errorTool(err), errorResult(err), nil
		}
		p, err := service.Rebind(ctx, in.ProjectID, root, &in.ExpectedRevision)
		if err != nil {
			return errorTool(err), errorResult(err), nil
		}
		return nil, result(p), nil
	})
	mcp.AddTool(server, tool("project_delete", "Move a project to local trash.", false, true, true), func(ctx context.Context, _ *mcp.CallToolRequest, in deleteInput) (*mcp.CallToolResult, Result, error) {
		if err := exactProjectID(in.ProjectID); err != nil {
			return errorTool(err), errorResult(err), nil
		}
		if in.ProjectID != in.ConfirmProjectID {
			err := &project.Error{Code: project.CodeInvalid, Message: "confirm_project_id must equal project_id"}
			return errorTool(err), errorResult(err), nil
		}
		p, err := service.Delete(ctx, in.ProjectID, &in.ExpectedRevision)
		if err != nil {
			return errorTool(err), errorResult(err), nil
		}
		return nil, result(p), nil
	})
	mcp.AddTool(server, tool("project_restore", "Restore a project at the server root.", false, false, true), func(ctx context.Context, _ *mcp.CallToolRequest, in revisionInput) (*mcp.CallToolResult, Result, error) {
		if err := exactProjectID(in.ProjectID); err != nil {
			return errorTool(err), errorResult(err), nil
		}
		p, err := service.Restore(ctx, in.ProjectID, root, &in.ExpectedRevision)
		if err != nil {
			return errorTool(err), errorResult(err), nil
		}
		return nil, result(p), nil
	})
	addAdminTaskTools(server, service, tasks, root, actor)
	addAdminContextTools(server, service, contexts, root, actor)
}

type revisionInput struct {
	ProjectID        string `json:"project_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type renameInput struct {
	ProjectID        string `json:"project_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Name             string `json:"name"`
}

type deleteInput struct {
	ProjectID        string `json:"project_id"`
	ConfirmProjectID string `json:"confirm_project_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

func result(p project.Project) Result { return Result{SchemaVersion: "1", Project: &p} }
func errorResult(err error) Result    { return Result{SchemaVersion: "1", Error: toolError(err)} }

func toolError(err error) *ToolError {
	return &ToolError{Code: errorCode(err), Message: err.Error()}
}
func tool(name, description string, readOnly, destructive, idempotent bool) *mcp.Tool {
	open := false

	return &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    readOnly,
			DestructiveHint: &destructive,
			IdempotentHint:  idempotent,
			OpenWorldHint:   &open,
		},
	}
}

func errorTool(err error) *mcp.CallToolResult {
	return errorToolAtVersion(err, "1")
}

func runErrorTool(err error) *mcp.CallToolResult {
	return errorToolAtVersion(err, runSchemaVersion)
}

func errorToolAtVersion(err error, schemaVersion string) *mcp.CallToolResult {
	encoded, _ := json.Marshal(ErrorResult{SchemaVersion: schemaVersion, Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: errorCode(err), Message: err.Error()}})
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}}
}

func exactProjectID(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value || parsed.Version() != 7 {
		return &project.Error{Code: project.CodeInvalid, Message: "project_id must be a canonical UUIDv7"}
	}

	return nil
}
