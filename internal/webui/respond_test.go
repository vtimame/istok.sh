package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vtimame/istok.sh/internal/knowledge"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

func TestWriteDomainErrorMapsCodesToStatuses(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"task not found", task.NewError(task.CodeNotFound, "task missing"), http.StatusNotFound, "task_not_found"},
		{"run not found", run.NewError(run.CodeNotFound, "run missing"), http.StatusNotFound, "not_found"},
		{"project not found", &project.Error{Code: project.CodeNotFound, Message: "project missing"}, http.StatusNotFound, "project_not_found"},
		{"knowledge not found", &knowledge.Error{Code: knowledge.CodeNotFound, Message: "knowledge missing"}, http.StatusNotFound, "knowledge_not_found"},
		{"task invalid argument", task.NewError(task.CodeInvalid, "bad task"), http.StatusBadRequest, "invalid_argument"},
		{"run invalid", run.NewError(run.CodeInvalid, "bad run"), http.StatusBadRequest, "invalid"},
		{"revision conflict", run.NewError(run.CodeRevisionConflict, "stale revision"), http.StatusConflict, "revision_conflict"},
		{"invalid transition", run.NewError(run.CodeInvalidTransition, "not active"), http.StatusConflict, "invalid_transition"},
		{"active run", task.NewError(task.CodeHasActiveRun, "busy"), http.StatusConflict, "task_has_active_run"},
		{"project conflict", &project.Error{Code: project.CodeConflict, Message: "taken"}, http.StatusConflict, "project_conflict"},
		{"wrapped domain error", fmt.Errorf("load: %w", run.NewError(run.CodeNotFound, "run missing")), http.StatusNotFound, "not_found"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()

			writeDomainError(recorder, tc.err)

			status, body := decodeErrorEnvelope(t, recorder)
			if status != tc.wantStatus || body.Code != tc.wantCode || body.Message != tc.err.Error() {
				t.Fatalf("response = %d %+v, want %d %q with message %q", status, body, tc.wantStatus, tc.wantCode, tc.err.Error())
			}
		})
	}
}

func TestWriteDomainErrorHidesUnexpectedErrors(t *testing.T) {
	for _, err := range []error{
		errors.New("open /secret/path/istok.db: permission denied"),
		run.NewError(run.CodeInternal, "sql: /secret/path"),
	} {
		recorder := httptest.NewRecorder()

		writeDomainError(recorder, err)

		status, body := decodeErrorEnvelope(t, recorder)
		if status != http.StatusInternalServerError || body.Code != "internal_error" || body.Message != "internal error" {
			t.Fatalf("response = %d %+v, want 500 internal_error", status, body)
		}
		if strings.Contains(recorder.Body.String(), "secret") {
			t.Fatalf("response leaks the internal message: %s", recorder.Body.String())
		}
	}
}

func decodeErrorEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) (int, errorBody) {
	t.Helper()

	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}

	var value envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	if value.SchemaVersion != schemaVersion || value.Error == nil || value.Result != nil {
		t.Fatalf("envelope = %s", recorder.Body.String())
	}

	return recorder.Code, *value.Error
}
