package contextapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
)

const schemaVersion = "2"

func (s *Service) BuildPackage(ctx context.Context, projectID string, options contextmodel.BuildOptions) (contextmodel.ContextPackage, error) {
	if !contextmodel.IsUUIDv7(projectID) {
		return contextmodel.ContextPackage{}, contextmodel.NewError(contextmodel.CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if err := options.Validate(); err != nil {
		return contextmodel.ContextPackage{}, err
	}

	values, err := s.List(ctx, projectID, contextmodel.ListOptions{IncludeDeleted: options.IncludeDeleted})
	if err != nil {
		return contextmodel.ContextPackage{}, err
	}

	instructions := make([]contextmodel.ProjectContextRecord, 0, len(values))
	ordinary := make([]contextmodel.ProjectContextRecord, 0, len(values))
	for _, value := range values {
		if value.Kind == contextmodel.KindInstruction {
			if value.Enabled == nil || value.Priority == nil || value.Scope == nil || !value.Priority.Valid() || !value.Scope.Valid() {
				return contextmodel.ContextPackage{}, contextmodel.NewError(contextmodel.CodeInvalid, "instruction context policy is invalid")
			}
			if !*value.Enabled {
				continue
			}

			instructions = append(instructions, value)
		} else {
			if value.Enabled != nil || value.Priority != nil || value.Scope != nil {
				return contextmodel.ContextPackage{}, contextmodel.NewError(contextmodel.CodeInvalid, "instruction policy is only valid for instruction context")
			}

			ordinary = append(ordinary, value)
		}
	}
	sort.SliceStable(instructions, func(i, j int) bool {
		return priorityRank(*instructions[i].Priority) < priorityRank(*instructions[j].Priority)
	})
	if options.Limit > 0 && len(ordinary) > options.Limit {
		ordinary = ordinary[:options.Limit]
	}
	values = append(instructions, ordinary...)

	items := make([]contextmodel.ContextPackageItem, 0, len(values))
	for _, value := range values {
		hash, err := contextRecordHash(value)
		if err != nil {
			return contextmodel.ContextPackage{}, err
		}

		items = append(items, contextmodel.ContextPackageItem{
			RecordID:       value.ID,
			RecordRevision: value.Revision,
			ContentHash:    hash,
			Kind:           value.Kind,
			Source:         value.Source,
			Visibility:     value.Visibility,
			Sensitivity:    value.Sensitivity,
			Enabled:        value.Enabled,
			Priority:       value.Priority,
			Scope:          value.Scope,
			Title:          value.Title,
			Body:           value.Body,
			Snippet:        contextSnippet(value.Body),
			Tags:           value.Tags,
		})
	}

	return contextmodel.ContextPackage{
		SchemaVersion: schemaVersion,
		ProjectID:     projectID,
		GeneratedAt:   s.now().UTC(),
		Records:       items,
	}, nil
}

func contextRecordHash(value contextmodel.ProjectContextRecord) (string, error) {
	tags := append([]string(nil), value.Tags...)
	sort.Strings(tags)
	payload := struct {
		Kind        contextmodel.Kind        `json:"kind"`
		Title       string                   `json:"title"`
		Body        string                   `json:"body"`
		Tags        []string                 `json:"tags"`
		Source      contextmodel.Source      `json:"source"`
		Visibility  contextmodel.Visibility  `json:"visibility"`
		Sensitivity contextmodel.Sensitivity `json:"sensitivity"`
		Enabled     *bool                    `json:"enabled,omitempty"`
		Priority    *contextmodel.Priority   `json:"priority,omitempty"`
		Scope       *contextmodel.Scope      `json:"scope,omitempty"`
	}{
		Kind:        value.Kind,
		Title:       value.Title,
		Body:        value.Body,
		Tags:        tags,
		Source:      value.Source,
		Visibility:  value.Visibility,
		Sensitivity: value.Sensitivity,
		Enabled:     value.Enabled,
		Priority:    value.Priority,
		Scope:       value.Scope,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func priorityRank(value contextmodel.Priority) int {
	switch value {
	case contextmodel.PriorityCritical:
		return 0
	case contextmodel.PriorityHigh:
		return 1
	case contextmodel.PriorityNormal:
		return 2
	default:
		return 3
	}
}

func contextSnippet(body string) string {
	snippet := strings.TrimSpace(body)
	runes := []rune(snippet)
	const maxLen = 120
	if len(runes) <= maxLen {
		return snippet
	}

	return string(runes[:maxLen])
}
