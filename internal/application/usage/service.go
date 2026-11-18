// Package usage records MCP tool calls and reports how agents use Istok.
// The MCP server records calls; the CLI reads the report.
package usage

import (
	"context"
	"time"

	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/storage/usagerepo"
	model "github.com/vtimame/istok.sh/internal/usage"
)

type Service struct {
	projects   *project.Service
	repository *usagerepo.Repository
	now        func() time.Time
}

func NewService(projects *project.Service, repository *usagerepo.Repository) *Service {
	return &Service{projects: projects, repository: repository, now: time.Now}
}

// Record appends one call to the log.
func (s *Service) Record(ctx context.Context, call model.Call) error {
	return s.repository.Record(ctx, call)
}

// Prune deletes calls older than the retention period.
func (s *Service) Prune(ctx context.Context) error {
	return s.repository.Prune(ctx, s.now().Add(-model.Retention))
}

// ReportInput selects the report period and, optionally, one project by ID,
// unambiguous ID prefix or name.
type ReportInput struct {
	Period  time.Duration
	Project string
}

// Report summarizes the calls of the period.
func (s *Service) Report(ctx context.Context, input ReportInput) (model.Report, error) {
	filter := model.Filter{Since: s.now().Add(-input.Period)}

	if input.Project != "" {
		selected, err := s.projects.Resolve(ctx, input.Project, false)
		if err != nil {
			return model.Report{}, err
		}
		filter.ProjectID = selected.ID
	}

	counts, err := s.repository.Counts(ctx, filter)
	if err != nil {
		return model.Report{}, err
	}

	return model.Summarize(filter.Since, counts), nil
}
