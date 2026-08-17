package runapp

import (
	"context"
	"errors"
	"testing"
	"time"

	projectcontext "s26.dev/istok-cli/internal/context"
	contextpack "s26.dev/istok-cli/internal/contextpack"
	run "s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/task"
)

func TestClaimCallsResolverBuilderRepositoryOnce(t *testing.T) {
	taskValue := task.Task{
		ID:        mustTaskID(t),
		ProjectID: mustProjectID(t),
		Status:    task.StatusOpen,
	}
	projectID := taskValue.ProjectID

	resolver := &fakeTaskResolver{
		taskValue: taskValue,
	}
	builder := &fakeContextBuilder{
		packageValue: contextpack.Package{
			SchemaVersion: contextpack.SchemaVersion,
			ProjectID:     projectID,
			GeneratedAt:   nowTime(),
			Records: []projectcontext.ContextPackageItem{
				{
					RecordID:       mustRunID(t),
					RecordRevision: 1,
					ContentHash:    "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
					Kind:           projectcontext.KindNote,
					Source:         projectcontext.SourceUser,
					Visibility:     projectcontext.VisibilityShared,
					Sensitivity:    projectcontext.SensitivityNormal,
					Title:          "title",
					Tags:           []string{"t"},
				},
			},
		},
	}

	repository := &fakeRepository{
		claim: func(_ context.Context, value run.ClaimRecord) (run.Run, error) {
			if value.RunID == "" || value.TaskID == "" {
				t.Fatalf("invalid claim record %#v", value)
			}
			if value.EventID == "" || value.Snapshot.ID == "" {
				t.Fatalf("claim record missing identifiers %#v", value)
			}
			return run.Run{}, nil
		},
	}

	service := NewService(repository, resolver, builder)
	_, err := service.Claim(context.Background(), task.Selector{ProjectID: projectID, ID: taskValue.ID}, run.ClaimInput{
		ContextLimit: 1,
	}, run.ActorSnapshot{ID: "agent", Kind: "user", Name: "Agent"})
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}

	if resolver.resolveCalls != 1 || builder.buildCalls != 1 || repository.claimCalls != 1 {
		t.Fatalf("call counts mismatch resolver=%d builder=%d claim=%d", resolver.resolveCalls, builder.buildCalls, repository.claimCalls)
	}
}

func TestClaimRejectsBlockedTaskBeforeBuildingContext(t *testing.T) {
	taskValue := task.Task{
		ID:        mustTaskID(t),
		ProjectID: mustProjectID(t),
		Status:    task.StatusBlocked,
	}
	builder := &fakeContextBuilder{}
	repository := &fakeRepository{
		claim: func(context.Context, run.ClaimRecord) (run.Run, error) {
			t.Fatal("repository should not be called")

			return run.Run{}, nil
		},
	}
	service := NewService(repository, &fakeTaskResolver{taskValue: taskValue}, builder)

	_, err := service.Claim(
		context.Background(),
		task.Selector{ProjectID: taskValue.ProjectID, ID: taskValue.ID},
		run.ClaimInput{},
		mustActor(t),
	)
	if run.ErrorCode(err) != run.CodeInvalidTransition {
		t.Fatalf("Claim() error = %v, want %s", err, run.CodeInvalidTransition)
	}
	if builder.buildCalls != 0 || repository.claimCalls != 0 {
		t.Fatalf("blocked claim calls builder=%d repository=%d", builder.buildCalls, repository.claimCalls)
	}
}

func TestCompleteTaskGeneratesIDsAndUsesExactTaskAndRevision(t *testing.T) {
	taskID := mustTaskID(t)
	projectID := mustProjectID(t)
	repository := &fakeRepository{
		complete: func(_ context.Context, value run.CompletionRecord) (task.Task, run.Completion, error) {
			if value.ID == "" {
				t.Fatal("completion id was not generated")
			}
			if value.EventID == "" {
				t.Fatal("event id was not generated")
			}
			if value.TaskID != taskID {
				t.Fatalf("completion task id = %q, want %q", value.TaskID, taskID)
			}
			if value.ExpectedTaskRevision != 10 {
				t.Fatalf("expected task revision %d, got %d", 10, value.ExpectedTaskRevision)
			}
			return task.Task{}, run.Completion{}, nil
		},
	}

	service := NewService(repository, &fakeTaskResolver{
		taskValue: task.Task{ID: taskID, ProjectID: projectID, Status: task.StatusOpen, Revision: 10},
	}, &fakeContextBuilder{})

	_, _, err := service.CompleteTask(context.Background(), task.Selector{ProjectID: projectID, ID: taskID}, run.CompleteTaskInput{
		ExpectedTaskRevision: 10,
		Note:                 "done",
		RunID:                mustRunID(t),
		ValidationID:         mustRunID(t),
	}, mustActor(t))
	if err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
}

func TestCompleteTaskDoesNotAcceptInvalidEvidenceIDEvenWithOverride(t *testing.T) {
	repository := &fakeRepository{
		complete: func(context.Context, run.CompletionRecord) (task.Task, run.Completion, error) {
			t.Fatalf("repository should not be called")
			return task.Task{}, run.Completion{}, nil
		},
	}
	service := NewService(repository, &fakeTaskResolver{
		taskValue: task.Task{ID: mustTaskID(t), ProjectID: mustProjectID(t), Status: task.StatusOpen},
	}, &fakeContextBuilder{})

	_, _, err := service.CompleteTask(context.Background(), task.Selector{ProjectID: mustProjectID(t), ID: mustTaskID(t)}, run.CompleteTaskInput{
		ExpectedTaskRevision: 1,
		Note:                 "done",
		OverrideReason:       "override",
		RunID:                "bad-uuid",
	}, mustActor(t))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestFinishRunClearsOverrideReasonWhenValidationEnabled(t *testing.T) {
	repository := &fakeRepository{
		finishRun: func(_ context.Context, input run.FinishRunInput, _ run.ActorSnapshot) (run.Run, error) {
			if !run.IsUUIDv7(input.EventID) {
				t.Fatalf("event id was not generated: %q", input.EventID)
			}
			if input.OverrideReason != "" {
				t.Fatalf("override reason must be ignored when validation is required: %q", input.OverrideReason)
			}
			return run.Run{}, nil
		},
	}

	service := NewService(repository, &fakeTaskResolver{}, &fakeContextBuilder{})
	_, err := service.FinishRun(context.Background(), run.FinishRunInput{
		RunID:            mustRunID(t),
		LeaseID:          mustRunID(t),
		ExpectedRevision: 1,
		Status:           run.StatusSucceeded,
		ResultSummary:    "done",
		AllowUnvalidated: false,
		OverrideReason:   "should drop",
	}, mustActor(t))
	if err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}
}

func TestStartExecutionPrevalidationSkipsRepository(t *testing.T) {
	repository := &fakeRepository{
		start: func(context.Context, run.StartExecutionInput, run.ActorSnapshot) (run.Execution, error) {
			t.Fatalf("repository should not be called")
			return run.Execution{}, nil
		},
	}
	service := NewService(repository, &fakeTaskResolver{}, &fakeContextBuilder{})
	_, err := service.StartExecution(context.Background(), run.StartExecutionInput{RunID: mustRunID(t)}, mustActor(t))
	if err == nil {
		t.Fatal("expected validation error")
	}
	if repository.startCalls != 0 {
		t.Fatalf("expected no repository calls, got %d", repository.startCalls)
	}
}

func TestFinishExecutionPrevalidationSkipsRepository(t *testing.T) {
	negative := int64(-1)
	repository := &fakeRepository{
		finishExecution: func(context.Context, run.FinishExecutionInput, run.ActorSnapshot) (run.Execution, error) {
			t.Fatalf("repository should not be called")
			return run.Execution{}, nil
		},
	}
	service := NewService(repository, &fakeTaskResolver{}, &fakeContextBuilder{})
	_, err := service.FinishExecution(context.Background(), run.FinishExecutionInput{
		ExecutionID:      mustRunID(t),
		ExpectedRevision: 1,
		Status:           run.ExecutionSucceeded,
		DurationMS:       &negative,
	}, mustActor(t))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestClaimPrevalidationSkipsRepositoryCalls(t *testing.T) {
	repository := &fakeRepository{
		claim: func(context.Context, run.ClaimRecord) (run.Run, error) {
			t.Fatalf("repository claim should not be called")
			return run.Run{}, nil
		},
	}

	service := NewService(repository, &fakeTaskResolver{}, &fakeContextBuilder{})
	_, err := service.Claim(context.Background(), task.Selector{ProjectID: "bad", ID: mustTaskID(t)}, run.ClaimInput{}, run.ActorSnapshot{})
	if err == nil {
		t.Fatal("expected error")
	}
	if repository.claimCalls != 0 {
		t.Fatalf("repository was called despite prevalidation error")
	}
}

type fakeRepository struct {
	claimCalls           int
	startCalls           int
	completeCalls        int
	finishExecutionCalls int

	claim            func(context.Context, run.ClaimRecord) (run.Run, error)
	get              func(context.Context, string) (run.Run, error)
	list             func(context.Context, run.ListOptions) ([]run.Run, error)
	show             func(context.Context, string) (run.Show, error)
	hasActive        func(context.Context, string) (bool, error)
	start            func(context.Context, run.StartExecutionInput, run.ActorSnapshot) (run.Execution, error)
	finishExecution  func(context.Context, run.FinishExecutionInput, run.ActorSnapshot) (run.Execution, error)
	recordValidation func(context.Context, run.RecordValidationInput, run.ActorSnapshot) (run.Validation, error)
	finishRun        func(context.Context, run.FinishRunInput, run.ActorSnapshot) (run.Run, error)
	complete         func(context.Context, run.CompletionRecord) (task.Task, run.Completion, error)
}

func (f *fakeRepository) FinishManagedExecution(context.Context, run.FinishManagedExecutionInput, run.ActorSnapshot) (run.FinishManagedExecutionResult, error) {
	return run.FinishManagedExecutionResult{}, errors.New("finish managed execution not mocked")
}
func (f *fakeRepository) Heartbeat(context.Context, run.HeartbeatInput, run.ActorSnapshot) (run.Run, error) {
	return run.Run{}, errors.New("heartbeat not mocked")
}
func (f *fakeRepository) Recover(context.Context, run.RecoverInput, run.ActorSnapshot) (run.Run, error) {
	return run.Run{}, errors.New("recover not mocked")
}
func (f *fakeRepository) GetArtifact(context.Context, string) (run.Artifact, error) {
	return run.Artifact{}, errors.New("get artifact not mocked")
}
func (f *fakeRepository) ListArtifacts(context.Context, string) ([]run.Artifact, error) {
	return []run.Artifact{}, errors.New("list artifacts not mocked")
}

func (f *fakeRepository) Claim(ctx context.Context, input run.ClaimRecord) (run.Run, error) {
	f.claimCalls++
	if f.claim == nil {
		return run.Run{}, errors.New("claim not mocked")
	}
	return f.claim(ctx, input)
}
func (f *fakeRepository) GetRun(ctx context.Context, id string) (run.Run, error) {
	if f.get == nil {
		return run.Run{}, errors.New("get run not mocked")
	}
	return f.get(ctx, id)
}
func (f *fakeRepository) ListRuns(ctx context.Context, options run.ListOptions) ([]run.Run, error) {
	if f.list == nil {
		return nil, errors.New("list runs not mocked")
	}
	return f.list(ctx, options)
}
func (f *fakeRepository) ShowRun(ctx context.Context, id string) (run.Show, error) {
	if f.show == nil {
		return run.Show{}, errors.New("show run not mocked")
	}
	return f.show(ctx, id)
}
func (f *fakeRepository) HasActiveRun(ctx context.Context, id string) (bool, error) {
	if f.hasActive == nil {
		return false, errors.New("has active run not mocked")
	}
	return f.hasActive(ctx, id)
}
func (f *fakeRepository) StartExecution(ctx context.Context, input run.StartExecutionInput, actor run.ActorSnapshot) (run.Execution, error) {
	f.startCalls++
	if f.start == nil {
		return run.Execution{}, errors.New("start execution not mocked")
	}
	return f.start(ctx, input, actor)
}
func (f *fakeRepository) FinishExecution(ctx context.Context, input run.FinishExecutionInput, actor run.ActorSnapshot) (run.Execution, error) {
	f.finishExecutionCalls++
	if f.finishExecution == nil {
		return run.Execution{}, errors.New("finish execution not mocked")
	}
	return f.finishExecution(ctx, input, actor)
}
func (f *fakeRepository) RecordValidation(ctx context.Context, input run.RecordValidationInput, actor run.ActorSnapshot) (run.Validation, error) {
	if f.recordValidation == nil {
		return run.Validation{}, errors.New("record validation not mocked")
	}
	return f.recordValidation(ctx, input, actor)
}
func (f *fakeRepository) FinishRun(ctx context.Context, input run.FinishRunInput, actor run.ActorSnapshot) (run.Run, error) {
	if f.finishRun == nil {
		return run.Run{}, errors.New("finish run not mocked")
	}
	return f.finishRun(ctx, input, actor)
}
func (f *fakeRepository) CompleteTask(ctx context.Context, input run.CompletionRecord) (task.Task, run.Completion, error) {
	f.completeCalls++
	if f.complete == nil {
		return task.Task{}, run.Completion{}, errors.New("complete task not mocked")
	}
	return f.complete(ctx, input)
}

type fakeTaskResolver struct {
	taskValue    task.Task
	err          error
	resolveCalls int
}

func (f *fakeTaskResolver) Resolve(_ context.Context, _ task.Selector, _ bool) (task.Task, error) {
	f.resolveCalls++
	if f.err != nil {
		return task.Task{}, f.err
	}
	return f.taskValue, nil
}

type fakeContextBuilder struct {
	packageValue contextpack.Package
	err          error
	buildCalls   int
}

func (f *fakeContextBuilder) BuildForTask(_ context.Context, _ task.Task, _ contextpack.BuildOptions) (contextpack.Package, error) {
	f.buildCalls++
	if f.err != nil {
		return contextpack.Package{}, f.err
	}
	return f.packageValue, nil
}

func mustActor(t *testing.T) run.ActorSnapshot {
	t.Helper()
	return run.ActorSnapshot{ID: "agent", Kind: "user", Name: "Agent"}
}

func mustProjectID(t *testing.T) string {
	t.Helper()
	id, err := task.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustTaskID(t *testing.T) string {
	t.Helper()
	id, err := task.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRunID(t *testing.T) string {
	return mustTaskID(t)
}

func nowTime() time.Time {
	return time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
}
