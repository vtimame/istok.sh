package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	knowledgeapp "github.com/vtimame/istok.sh/internal/application/knowledge"
	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/knowledge"
	"github.com/vtimame/istok.sh/internal/project"
)

var cliKnowledgeActor = contextmodel.ActorSnapshot{ID: "cli", Kind: "cli", Name: "CLI"}

type knowledgeView struct {
	Project project.Project   `json:"project"`
	Item    knowledge.Item    `json:"item"`
	Events  []knowledge.Event `json:"events,omitempty"`
}

type knowledgeCatalogView struct {
	Project project.Project         `json:"project"`
	Items   []knowledge.CatalogItem `json:"items"`
	Limit   int                     `json:"limit"`
	Offset  int                     `json:"offset"`
}

type knowledgeSupersedeView struct {
	Project     project.Project `json:"project"`
	Superseded  knowledge.Item  `json:"superseded"`
	Replacement knowledge.Item  `json:"replacement"`
}

type knowledgeExportView struct {
	Project     project.Project `json:"project"`
	ItemID      string          `json:"item_id"`
	Output      string          `json:"output"`
	Format      string          `json:"format"`
	ContentHash string          `json:"content_hash"`
}

func runKnowledge(ctx context.Context, command KnowledgeCommand, commandName, cwd string, output io.Writer) error {
	switch {
	case strings.HasPrefix(commandName, "knowledge add"), strings.HasPrefix(commandName, "knowledge distill"):
		input := command.Add
		distill := strings.HasPrefix(commandName, "knowledge distill")
		if distill {
			input = command.Distill
		}
		body, err := knowledgeBody(input.Body, input.BodyFile)
		if err != nil {
			return knowledgeCommandError(input.JSON, err)
		}
		provenance, err := parseKnowledgeProvenance(input.Source)
		if err != nil {
			return knowledgeCommandError(input.JSON, err)
		}
		return runKnowledgeApp(ctx, input.Database, input.JSON, output, func(projects *project.Service, service *knowledgeapp.Service) (any, error) {
			current, err := projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}
			create := knowledge.CreateInput{ProjectID: current.ID, Kind: input.Kind, Title: input.Title, Summary: input.Summary, Body: body, Tags: input.Tag, Visibility: input.Visibility, Sensitivity: input.Sensitivity, Provenance: provenance}
			var item knowledge.Item
			if distill {
				item, err = service.Distill(ctx, create, cliKnowledgeActor)
			} else {
				item, err = service.Create(ctx, create, cliKnowledgeActor)
			}
			return knowledgeView{Project: current, Item: item}, err
		})
	case commandName == "knowledge list", commandName == "knowledge catalog":
		return runKnowledgeApp(ctx, command.List.Database, command.List.JSON, output, func(projects *project.Service, service *knowledgeapp.Service) (any, error) {
			current, err := projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}
			options := knowledge.CatalogOptions{Kinds: command.List.Kind, Statuses: command.List.Status, Visibilities: command.List.Visibility, Sensitivities: command.List.Sensitivity, Limit: command.List.Limit, Offset: command.List.Offset}
			items, err := service.Catalog(ctx, current.ID, options)
			return knowledgeCatalogView{Project: current, Items: items, Limit: options.EffectiveLimit(), Offset: options.Offset}, err
		})
	case strings.HasPrefix(commandName, "knowledge search"):
		return runKnowledgeApp(ctx, command.Search.Database, command.Search.JSON, output, func(projects *project.Service, service *knowledgeapp.Service) (any, error) {
			current, err := projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}
			options := knowledge.SearchOptions{Query: command.Search.Query, Limit: command.Search.Limit, Offset: command.Search.Offset}
			items, err := service.Search(ctx, current.ID, options)
			return knowledgeCatalogView{Project: current, Items: items, Limit: options.EffectiveLimit(), Offset: options.Offset}, err
		})
	case strings.HasPrefix(commandName, "knowledge show"), strings.HasPrefix(commandName, "knowledge read"):
		input := command.Show
		if strings.HasPrefix(commandName, "knowledge read") {
			input = command.Read
		}
		return runKnowledgeApp(ctx, input.Database, input.JSON, output, func(projects *project.Service, service *knowledgeapp.Service) (any, error) {
			current, item, err := currentKnowledge(ctx, projects, service, cwd, input.ID)
			if err != nil {
				return nil, err
			}
			events, err := service.Events(ctx, item.ID)
			return knowledgeView{Project: current, Item: item, Events: events}, err
		})
	case strings.HasPrefix(commandName, "knowledge update"):
		body := command.Update.Body
		if command.Update.BodyFile != "" {
			if body != nil {
				return knowledgeCommandError(command.Update.JSON, knowledge.NewError(knowledge.CodeInvalid, "body and body-file are mutually exclusive"))
			}
			value, err := os.ReadFile(command.Update.BodyFile)
			if err != nil {
				return knowledgeCommandError(command.Update.JSON, fmt.Errorf("read knowledge body file: %w", err))
			}
			text := string(value)
			body = &text
		}
		provenance, err := parseKnowledgeProvenance(command.Update.Source)
		if err != nil {
			return knowledgeCommandError(command.Update.JSON, err)
		}
		return runKnowledgeApp(ctx, command.Update.Database, command.Update.JSON, output, func(projects *project.Service, service *knowledgeapp.Service) (any, error) {
			current, _, err := currentKnowledge(ctx, projects, service, cwd, command.Update.ID)
			if err != nil {
				return nil, err
			}
			patch := knowledge.Patch{Kind: command.Update.Kind, Title: command.Update.Title, Summary: command.Update.Summary, Body: body, Visibility: command.Update.Visibility, Sensitivity: command.Update.Sensitivity}
			if command.Update.Tag != nil {
				patch.Tags = &command.Update.Tag
			}
			if command.Update.Source != nil {
				patch.Provenance = &provenance
			}
			item, err := service.Update(ctx, command.Update.ID, command.Update.ExpectedRevision, patch, cliKnowledgeActor)
			return knowledgeView{Project: current, Item: item}, err
		})
	case strings.HasPrefix(commandName, "knowledge promote"):
		return runKnowledgeApp(ctx, command.Promote.Database, command.Promote.JSON, output, func(projects *project.Service, service *knowledgeapp.Service) (any, error) {
			current, _, err := currentKnowledge(ctx, projects, service, cwd, command.Promote.ID)
			if err != nil {
				return nil, err
			}
			item, err := service.Promote(ctx, command.Promote.ID, command.Promote.ExpectedRevision, cliKnowledgeActor)
			return knowledgeView{Project: current, Item: item}, err
		})
	case strings.HasPrefix(commandName, "knowledge review"):
		return runKnowledgeApp(ctx, command.Review.Database, command.Review.JSON, output, func(projects *project.Service, service *knowledgeapp.Service) (any, error) {
			current, _, err := currentKnowledge(ctx, projects, service, cwd, command.Review.ID)
			if err != nil {
				return nil, err
			}
			item, err := service.Review(ctx, command.Review.ID, command.Review.ExpectedRevision, command.Review.Note, cliKnowledgeActor)
			return knowledgeView{Project: current, Item: item}, err
		})
	case strings.HasPrefix(commandName, "knowledge supersede"):
		return runKnowledgeApp(ctx, command.Supersede.Database, command.Supersede.JSON, output, func(projects *project.Service, service *knowledgeapp.Service) (any, error) {
			current, _, err := currentKnowledge(ctx, projects, service, cwd, command.Supersede.ID)
			if err != nil {
				return nil, err
			}
			_, _, err = currentKnowledge(ctx, projects, service, cwd, command.Supersede.ReplacementID)
			if err != nil {
				return nil, err
			}
			old, replacement, err := service.Supersede(ctx, command.Supersede.ID, command.Supersede.ExpectedRevision, command.Supersede.ReplacementID, cliKnowledgeActor)
			return knowledgeSupersedeView{Project: current, Superseded: old, Replacement: replacement}, err
		})
	case strings.HasPrefix(commandName, "knowledge export"):
		return runKnowledgeApp(ctx, command.Export.Database, command.Export.JSON, output, func(projects *project.Service, service *knowledgeapp.Service) (any, error) {
			current, item, err := currentKnowledge(ctx, projects, service, cwd, command.Export.ID)
			if err != nil {
				return nil, err
			}
			content, err := encodeKnowledgeExport(item, command.Export.Format)
			if err != nil {
				return nil, err
			}
			absolute, err := filepath.Abs(command.Export.Output)
			if err != nil {
				return nil, fmt.Errorf("resolve export destination: %w", err)
			}
			if err := writeExclusiveAtomic(absolute, content); err != nil {
				return nil, err
			}
			return knowledgeExportView{Project: current, ItemID: item.ID, Output: absolute, Format: command.Export.Format, ContentHash: item.ContentHash}, nil
		})
	default:
		return fmt.Errorf("unsupported command %q", commandName)
	}
}

func currentKnowledge(ctx context.Context, projects *project.Service, service *knowledgeapp.Service, cwd, id string) (project.Project, knowledge.Item, error) {
	current, err := projects.Current(ctx, cwd)
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

func knowledgeBody(body, file string) (string, error) {
	if body != "" && file != "" {
		return "", knowledge.NewError(knowledge.CodeInvalid, "body and body-file are mutually exclusive")
	}
	if file == "" {
		return body, nil
	}
	value, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read knowledge body file: %w", err)
	}
	return string(value), nil
}

func parseKnowledgeProvenance(values []string) ([]knowledge.Provenance, error) {
	result := make([]knowledge.Provenance, 0, len(values))
	for _, raw := range values {
		kindValue, identity, ok := strings.Cut(raw, ":")
		if !ok {
			return nil, knowledge.NewError(knowledge.CodeInvalid, "knowledge source must use TYPE:ID[@REVISION][#DETAIL]")
		}
		value := knowledge.Provenance{Type: knowledge.ProvenanceType(kindValue)}
		identity, value.Detail, _ = strings.Cut(identity, "#")
		if value.Type != knowledge.ProvenanceRepository {
			if id, revision, found := strings.Cut(identity, "@"); found {
				parsed, err := strconv.ParseInt(revision, 10, 64)
				if err != nil || parsed < 1 {
					return nil, knowledge.NewError(knowledge.CodeInvalid, "knowledge source revision must be positive")
				}
				value.ID, value.Revision = id, parsed
			} else {
				value.ID = identity
			}
		} else {
			value.ID = identity
		}
		result = append(result, value)
	}
	return knowledge.NormalizeProvenance(result)
}

func encodeKnowledgeExport(item knowledge.Item, format string) ([]byte, error) {
	if format == "json" {
		return json.MarshalIndent(struct {
			SchemaVersion string         `json:"schema_version"`
			Item          knowledge.Item `json:"item"`
		}{"istok.knowledge-export.v1", item}, "", "  ")
	}
	var out strings.Builder
	tags, _ := json.Marshal(item.Tags)
	provenance, _ := json.Marshal(item.Provenance)
	reviewedAt := ""
	if item.ReviewedAt != nil {
		reviewedAt = item.ReviewedAt.UTC().Format(time.RFC3339Nano)
	}
	out.WriteString("---\nschema_version: istok.knowledge-export.v1\n")
	out.WriteString(fmt.Sprintf("id: %s\nrevision: %d\nkind: %s\nstatus: %s\nvisibility: %s\nsensitivity: %s\nreviewed_at: %s\ncontent_hash: %s\ntags_json: %s\nprovenance_json: %s\n---\n\n", item.ID, item.Revision, item.Kind, item.Status, item.Visibility, item.Sensitivity, reviewedAt, item.ContentHash, tags, provenance))
	out.WriteString("# " + strings.TrimSpace(item.Title) + "\n\n")
	out.WriteString(strings.TrimSpace(item.Summary) + "\n\n")
	if strings.TrimSpace(item.Body) != "" {
		out.WriteString(strings.TrimSpace(item.Body) + "\n")
	}
	return []byte(out.String()), nil
}

func writeExclusiveAtomic(destination string, content []byte) error {
	if strings.TrimSpace(destination) == "" {
		return knowledge.NewError(knowledge.CodeInvalid, "explicit export destination is required")
	}
	if _, err := os.Stat(destination); err == nil {
		return knowledge.NewError(knowledge.CodeConflict, "export destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect export destination: %w", err)
	}
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create export directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".istok-knowledge-export-*")
	if err != nil {
		return fmt.Errorf("create temporary export: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary export: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary export: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary export: %w", err)
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return knowledge.NewError(knowledge.CodeConflict, "export destination already exists")
		}
		return fmt.Errorf("publish knowledge export: %w", err)
	}
	return nil
}

func runKnowledgeApp(ctx context.Context, database string, jsonOutput bool, output io.Writer, action func(*project.Service, *knowledgeapp.Service) (any, error)) error {
	var projects *project.Service
	var service *knowledgeapp.Service
	app := fx.New(fx.NopLogger, bootstrap.KnowledgeOptions(database), fx.Invoke(func(p *project.Service, s *knowledgeapp.Service) { projects, service = p, s }))
	if err := app.Start(ctx); err != nil {
		return knowledgeCommandError(jsonOutput, err)
	}
	value, actionErr := action(projects, service)
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopErr := app.Stop(stopCtx)
	err := errors.Join(actionErr, stopErr)
	if err != nil {
		return knowledgeCommandError(jsonOutput, err)
	}
	if jsonOutput {
		return json.NewEncoder(output).Encode(struct {
			SchemaVersion string `json:"schema_version"`
			Result        any    `json:"result"`
		}{"1", value})
	}
	_, err = fmt.Fprint(output, renderKnowledgeValue(value))
	return err
}

func knowledgeCommandError(jsonOutput bool, err error) error {
	if !jsonOutput {
		return err
	}
	encoded, _ := json.Marshal(struct {
		SchemaVersion string `json:"schema_version"`
		Error         struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{SchemaVersion: "1", Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: knowledgeErrorCode(err), Message: err.Error()}})
	return jsonCommandError{value: string(encoded), cause: err}
}

func knowledgeErrorCode(err error) string {
	var value *knowledge.Error
	if errors.As(err, &value) {
		return string(value.Code)
	}
	var projectError *project.Error
	if errors.As(err, &projectError) {
		return string(projectError.Code)
	}
	return string(knowledge.CodeInternal)
}
