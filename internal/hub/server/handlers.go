package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vtimame/istok.sh/internal/hub/identity"
	"github.com/vtimame/istok.sh/internal/hub/protocol"
)

const metaKeyCheck = "key_check"

// handler serves the protocol. Every request arrives over mutual TLS; the
// device is looked up from the client certificate.
type handler struct {
	store  *Store
	guard  *guard
	logger *slog.Logger
}

func newHandler(store *Store, guard *guard, logger *slog.Logger) http.Handler {
	h := &handler{store: store, guard: guard, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/enroll", h.enroll)
	mux.Handle("GET /v1/space", h.device(h.getSpace))
	mux.Handle("PUT /v1/space", h.device(h.putSpace))
	mux.Handle("GET /v1/projects", h.device(h.projects))
	mux.Handle("PUT /v1/projects/{id}", h.device(h.putProject))
	mux.Handle("POST /v1/push", h.device(h.push))
	mux.Handle("GET /v1/pull", h.device(h.pull))
	mux.Handle("POST /v1/numbers", h.device(h.numbers))
	mux.Handle("POST /v1/leases", h.device(h.leases))

	return h.common(mux)
}

// common checks the protocol version and bounds the request body.
func (h *handler) common(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(protocol.HeaderVersion) != strconv.Itoa(protocol.Version) {
			writeError(w, http.StatusBadRequest, &protocol.Error{Code: protocol.CodeUnsupported,
				Message: "this hub speaks protocol " + strconv.Itoa(protocol.Version) + "; update Istok on the device or the hub"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, protocol.MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func fingerprintOf(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}

	return identity.Fingerprint(r.TLS.PeerCertificates[0])
}

// device admits registered, unrevoked devices only.
func (h *handler) device(next func(http.ResponseWriter, *http.Request, Device)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		device, ok, err := h.store.Device(r.Context(), fingerprintOf(r))
		if err != nil {
			h.internal(w, r, err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, &protocol.Error{Code: protocol.CodeUnauthorized, Message: "this device is not registered on the hub"})
			return
		}

		if !h.guard.allowRequest(device.ID) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, &protocol.Error{Code: protocol.CodeRateLimited, Message: "too many requests from this device; retry shortly"})
			return
		}

		next(w, r, device)
	})
}

func (h *handler) enroll(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !h.guard.allowEnroll(host) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, &protocol.Error{Code: protocol.CodeRateLimited, Message: "too many join attempts; retry in a minute"})
		return
	}

	var request protocol.EnrollRequest
	if !h.decode(w, r, &request) {
		return
	}

	device, err := h.store.Enroll(r.Context(), fingerprintOf(r), request.Secret)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.logger.Info("device joined", "device", device.ID, "name", device.Name)
	writeJSON(w, protocol.EnrollResponse{DeviceID: device.ID, Name: device.Name})
}

func (h *handler) getSpace(w http.ResponseWriter, r *http.Request, _ Device) {
	check, err := h.store.Meta(r.Context(), metaKeyCheck)
	if err != nil {
		h.internal(w, r, err)
		return
	}

	writeJSON(w, protocol.Space{KeyCheck: check})
}

// putSpace sets the key check once. The first device chooses the key; later
// devices must hold the same one.
func (h *handler) putSpace(w http.ResponseWriter, r *http.Request, _ Device) {
	var request protocol.Space
	if !h.decode(w, r, &request) {
		return
	}
	if request.KeyCheck == "" || len(request.KeyCheck) > 128 {
		writeError(w, http.StatusBadRequest, &protocol.Error{Code: protocol.CodeInvalid, Message: "key check is missing or too long"})
		return
	}

	current, err := h.store.SetMetaOnce(r.Context(), metaKeyCheck, request.KeyCheck)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	if current != request.KeyCheck {
		writeError(w, http.StatusConflict, &protocol.Error{Code: protocol.CodeConflict, Message: "the hub already uses another encryption key"})
		return
	}

	writeJSON(w, protocol.Space{KeyCheck: request.KeyCheck})
}

func (h *handler) projects(w http.ResponseWriter, r *http.Request, _ Device) {
	projects, err := h.store.Projects(r.Context())
	if err != nil {
		h.internal(w, r, err)
		return
	}

	writeJSON(w, protocol.ProjectsResponse{Projects: projects})
}

func (h *handler) putProject(w http.ResponseWriter, r *http.Request, _ Device) {
	var request protocol.Project
	if !h.decode(w, r, &request) {
		return
	}

	request.ID = r.PathValue("id")
	if !validKey(request.ID) || len(request.Meta) == 0 || len(request.Meta) > protocol.MaxMetaBytes {
		writeError(w, http.StatusBadRequest, &protocol.Error{Code: protocol.CodeInvalid, Message: "project ID or metadata is invalid"})
		return
	}

	if err := h.store.PutProject(r.Context(), request); err != nil {
		h.internal(w, r, err)
		return
	}

	writeJSON(w, request)
}

func (h *handler) push(w http.ResponseWriter, r *http.Request, device Device) {
	var request protocol.PushRequest
	if !h.decode(w, r, &request) {
		return
	}
	if err := validatePush(request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	results, err := h.store.Push(r.Context(), device.ID, request)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, protocol.PushResponse{Results: results})
}

func validatePush(request protocol.PushRequest) *protocol.Error {
	invalid := func(message string) *protocol.Error {
		return &protocol.Error{Code: protocol.CodeInvalid, Message: message}
	}

	if !validKey(request.ProjectID) {
		return invalid("project ID is invalid")
	}
	if len(request.Rows) == 0 || len(request.Rows) > protocol.MaxPushRows {
		return invalid("a push carries 1 to " + strconv.Itoa(protocol.MaxPushRows) + " rows")
	}

	for _, row := range request.Rows {
		switch {
		case !validKey(row.Table) || !validKey(row.Key):
			return invalid("row table or key is invalid")
		case row.Kind != protocol.KindMutable && row.Kind != protocol.KindAppend && row.Kind != protocol.KindGroup:
			return invalid("row kind is invalid")
		case row.Tag == "" || len(row.Tag) > 128:
			return invalid("row tag is invalid")
		case len(row.Body) == 0 || len(row.Body) > protocol.MaxRowBytes:
			return invalid("row body is empty or too large")
		case row.Revision < 0 || row.SchemaVersion < 0:
			return invalid("row revision or schema version is invalid")
		}
	}

	return nil
}

// validKey bounds identifiers the hub stores in plaintext.
func validKey(value string) bool {
	return value != "" && len(value) <= 512 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func (h *handler) pull(w http.ResponseWriter, r *http.Request, _ Device) {
	query := r.URL.Query()

	projectID := query.Get("project")
	after, err1 := strconv.ParseInt(query.Get("after"), 10, 64)
	limit, err2 := strconv.Atoi(query.Get("limit"))
	if !validKey(projectID) || err1 != nil || after < 0 {
		writeError(w, http.StatusBadRequest, &protocol.Error{Code: protocol.CodeInvalid, Message: "project and after are required"})
		return
	}
	if err2 != nil || limit <= 0 || limit > protocol.MaxPullRows {
		limit = protocol.MaxPullRows
	}

	response, err := h.store.Pull(r.Context(), projectID, after, limit)
	if err != nil {
		h.internal(w, r, err)
		return
	}

	writeJSON(w, response)
}

func (h *handler) numbers(w http.ResponseWriter, r *http.Request, _ Device) {
	var request protocol.NumbersRequest
	if !h.decode(w, r, &request) {
		return
	}

	valid := validKey(request.ProjectID) && len(request.Claims) > 0 && len(request.Claims) <= protocol.MaxNumberClaims
	for _, claim := range request.Claims {
		valid = valid && validKey(claim.TaskID) && claim.Number >= 0
	}
	if !valid {
		writeError(w, http.StatusBadRequest, &protocol.Error{Code: protocol.CodeInvalid,
			Message: "a number request carries a project and 1 to " + strconv.Itoa(protocol.MaxNumberClaims) + " task claims"})
		return
	}

	numbers, err := h.store.Numbers(r.Context(), request)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, protocol.NumbersResponse{Numbers: numbers})
}

func (h *handler) leases(w http.ResponseWriter, r *http.Request, device Device) {
	var request protocol.LeasesRequest
	if !h.decode(w, r, &request) {
		return
	}

	valid := validKey(request.ProjectID) && len(request.Acquire)+len(request.Release) > 0 &&
		len(request.Acquire) <= protocol.MaxLeaseClaims && len(request.Release) <= protocol.MaxLeaseClaims
	for _, claim := range request.Acquire {
		valid = valid && validKey(claim.TaskID) && validKey(claim.RunID) && claim.TTLSeconds > 0 && claim.TTLSeconds <= protocol.MaxLeaseTTL
	}
	for _, taskID := range request.Release {
		valid = valid && validKey(taskID)
	}
	if !valid {
		writeError(w, http.StatusBadRequest, &protocol.Error{Code: protocol.CodeInvalid, Message: "lease request is invalid"})
		return
	}

	results, err := h.store.Leases(r.Context(), device, request)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, protocol.LeasesResponse{Results: results})
}

func (h *handler) decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, &protocol.Error{Code: protocol.CodeInvalid, Message: "request body is not valid JSON or too large"})
		return false
	}

	return true
}

func (h *handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var protocolError *protocol.Error
	if !errors.As(err, &protocolError) {
		h.internal(w, r, err)
		return
	}

	status := http.StatusBadRequest
	switch protocolError.Code {
	case protocol.CodeUnauthorized:
		status = http.StatusUnauthorized
	case protocol.CodeNotFound:
		status = http.StatusNotFound
	case protocol.CodeConflict:
		status = http.StatusConflict
	}

	writeError(w, status, protocolError)
}

func (h *handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, &protocol.Error{Code: protocol.CodeInternal, Message: "internal hub error"})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, value *protocol.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
