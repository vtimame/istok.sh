package contextapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	contextmodel "s26.dev/istok-cli/internal/context"
)

const schemaVersion = "1"

func (s *Service) BuildPackage(ctx context.Context, projectID string, options contextmodel.BuildOptions) (contextmodel.ContextPackage, error) {
	if !contextmodel.IsUUIDv7(projectID) {
		return contextmodel.ContextPackage{}, contextmodel.NewError(contextmodel.CodeInvalid, "project id must be a canonical UUIDv7")
	}
	if err := options.Validate(); err != nil {
		return contextmodel.ContextPackage{}, err
	}

	values, err := s.List(ctx, projectID, contextmodel.ListOptions{
		IncludeDeleted: options.IncludeDeleted,
		Limit:          options.Limit,
	})
	if err != nil {
		return contextmodel.ContextPackage{}, err
	}

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
	}{
		Kind:        value.Kind,
		Title:       value.Title,
		Body:        value.Body,
		Tags:        tags,
		Source:      value.Source,
		Visibility:  value.Visibility,
		Sensitivity: value.Sensitivity,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
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
