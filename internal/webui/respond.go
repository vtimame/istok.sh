package webui

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/vtimame/istok.sh/internal/knowledge"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

const schemaVersion = "1"

type envelope struct {
	SchemaVersion string     `json:"schema_version"`
	Result        any        `json:"result,omitempty"`
	Error         *errorBody `json:"error,omitempty"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeResult(w http.ResponseWriter, result any) {
	writeJSON(w, http.StatusOK, envelope{SchemaVersion: schemaVersion, Result: result})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, envelope{SchemaVersion: schemaVersion, Error: &errorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}

// writeDomainError maps typed domain errors to HTTP statuses and hides
// messages of unexpected internal errors.
func writeDomainError(w http.ResponseWriter, err error) {
	codes := []string{
		string(task.ErrorCode(err)),
		string(run.ErrorCode(err)),
		string(project.ErrorCode(err)),
		string(knowledge.ErrorCode(err)),
	}

	for _, code := range codes {
		if code == "" || code == "internal_error" {
			continue
		}

		writeError(w, statusForCode(code), code, err.Error())
		return
	}

	writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
}

func statusForCode(code string) int {
	switch {
	case strings.HasSuffix(code, "not_found"):
		return http.StatusNotFound
	case code == "invalid_argument" || code == "invalid":
		return http.StatusBadRequest
	default:
		return http.StatusConflict
	}
}
