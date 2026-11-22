package runapp

import (
	"context"

	"github.com/vtimame/istok.sh/internal/project"
	run "github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
	"github.com/vtimame/istok.sh/internal/worktree"
)

// ProjectResolver finds the project of a task, whose root holds the working
// tree a validation checked.
type ProjectResolver interface {
	Resolve(ctx context.Context, selector string, deleted bool) (project.Project, error)
}

// UseProjects lets task completion check that the code has not changed since
// the validation it relies on. Without it, completion accepts any passed
// validation.
func (s *Service) UseProjects(projects ProjectResolver) {
	s.projects = projects
}

// requireFreshValidation refuses a validation whose working tree has changed
// since the check ran: the passed check no longer describes the code being
// completed. Validations not tied to a tree pass: attested ones, ones from a
// project without Git, and ones recorded before Istok fingerprinted trees.
func (s *Service) requireFreshValidation(ctx context.Context, taskValue task.Task, runID, validationID string) error {
	if s.projects == nil || runID == "" || validationID == "" {
		return nil
	}

	validation, found, err := s.findValidation(ctx, runID, validationID)
	if err != nil || !found {
		return err
	}
	if validation.Source != run.ValidationSourceExecuted || validation.TreeState != run.TreeStateBound {
		return nil
	}

	projectValue, err := s.projects.Resolve(ctx, taskValue.ProjectID, false)
	if err != nil {
		return err
	}
	if projectValue.Root == nil {
		return run.NewError(run.CodeEvidenceStale,
			"cannot confirm that the code is unchanged since validation %s: the project has no root on this device; "+
				"complete with not_verified_reason", validation.ID)
	}

	digest, err := worktree.Digest(ctx, projectValue.Root.CanonicalPath, validation.TreeCommit)
	if err != nil {
		return run.NewError(run.CodeEvidenceStale,
			"cannot confirm that the code is unchanged since validation %s (%s): %v; "+
				"run the check again with run_validate, or complete with not_verified_reason",
			validation.ID, validation.Command, err)
	}
	if digest != validation.TreeDigest {
		return run.NewError(run.CodeEvidenceStale,
			"the code changed after validation %s (%s) passed; "+
				"run the check again with run_validate, or complete with not_verified_reason",
			validation.ID, validation.Command)
	}

	return nil
}

func (s *Service) findValidation(ctx context.Context, runID, validationID string) (run.Validation, bool, error) {
	show, err := s.repository.ShowRun(ctx, runID)
	if err != nil {
		return run.Validation{}, false, err
	}

	for _, validation := range show.Validations {
		if validation.ID == validationID {
			return validation, true, nil
		}
	}

	return run.Validation{}, false, nil
}
