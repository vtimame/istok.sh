// Package exchange exports projects to a bundle and imports bundles from
// other devices. Transports (CLI and web UI) call this service; the storage
// work happens in exchangerepo.
package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/vtimame/istok.sh/internal/buildinfo"
	"github.com/vtimame/istok.sh/internal/exchange"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/storage/exchangerepo"
)

// MaxBundleBytes bounds a bundle read from a file or an upload.
const MaxBundleBytes = 256 << 20

// Actor attributes history events written by an import.
type Actor = exchangerepo.Actor

type Service struct {
	projects   *project.Service
	repository *exchangerepo.Repository
	build      buildinfo.Info
}

func NewService(projects *project.Service, repository *exchangerepo.Repository, build buildinfo.Info) *Service {
	return &Service{projects: projects, repository: repository, build: build}
}

// Selection chooses the projects to export: every active project, the listed
// selectors, or the project at the working directory.
type Selection struct {
	All       bool
	Selectors []string
	Cwd       string
}

// Export builds a bundle of the selected projects.
func (s *Service) Export(ctx context.Context, selection Selection) (exchange.Bundle, error) {
	ids, err := s.resolve(ctx, selection)
	if err != nil {
		return exchange.Bundle{}, err
	}

	bundle, err := s.repository.Export(ctx, ids)
	if err != nil {
		return exchange.Bundle{}, err
	}
	bundle.IstokVersion = s.build.Version

	return bundle, nil
}

func (s *Service) resolve(ctx context.Context, selection Selection) ([]string, error) {
	switch {
	case selection.All:
		projects, err := s.projects.List(ctx, false)
		if err != nil {
			return nil, err
		}
		if len(projects) == 0 {
			return nil, exchange.Errorf(exchange.CodeNotFound, "there are no projects to export")
		}

		ids := make([]string, 0, len(projects))
		for _, value := range projects {
			ids = append(ids, value.ID)
		}
		return ids, nil
	case len(selection.Selectors) > 0:
		ids := make([]string, 0, len(selection.Selectors))
		seen := map[string]bool{}
		for _, selector := range selection.Selectors {
			value, err := s.projects.Resolve(ctx, selector, false)
			if err != nil {
				return nil, err
			}
			if !seen[value.ID] {
				seen[value.ID] = true
				ids = append(ids, value.ID)
			}
		}
		return ids, nil
	default:
		value, err := s.projects.Current(ctx, selection.Cwd)
		if err != nil {
			return nil, err
		}
		return []string{value.ID}, nil
	}
}

// Import merges a bundle. A dry run reports the outcome without writing.
func (s *Service) Import(ctx context.Context, bundle exchange.Bundle, actor Actor, dryRun bool) (exchange.Report, error) {
	return s.repository.Import(ctx, bundle, actor, dryRun)
}

// Decode reads a bundle. Numbers stay exact so revisions and sizes survive.
func Decode(input io.Reader) (exchange.Bundle, error) {
	decoder := json.NewDecoder(io.LimitReader(input, MaxBundleBytes+1))
	decoder.UseNumber()

	var bundle exchange.Bundle
	if err := decoder.Decode(&bundle); err != nil {
		return exchange.Bundle{}, exchange.Errorf(exchange.CodeInvalid, "read bundle: %v", err)
	}
	if decoder.More() {
		return exchange.Bundle{}, exchange.Errorf(exchange.CodeInvalid, "read bundle: unexpected data after the bundle")
	}

	return bundle, nil
}

// Encode writes a bundle as indented JSON.
func Encode(output io.Writer, bundle exchange.Bundle) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(bundle); err != nil {
		return fmt.Errorf("write bundle: %w", err)
	}

	return nil
}
