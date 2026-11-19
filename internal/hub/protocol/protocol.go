// Package protocol defines the messages between a hub and its devices. The
// hub stores rows it cannot read: bodies and project metadata are encrypted
// on the device, and the hub merges by table, key, revision and content tag.
package protocol

// Version is sent with every request; a hub rejects versions it does not speak.
const Version = 1

// HeaderVersion carries Version.
const HeaderVersion = "Istok-Protocol"

// Row kinds decide how the hub merges a pushed row with the stored one.
const (
	// KindMutable replaces the stored row when the revision is higher.
	KindMutable = "mutable"

	// KindAppend rows never change once stored.
	KindAppend = "append"

	// KindGroup is the set of child rows of one parent; a different set
	// replaces the stored one.
	KindGroup = "group"
)

// Push results per row.
const (
	StatusAccepted  = "accepted"
	StatusUnchanged = "unchanged"
	StatusStale     = "stale"
	StatusConflict  = "conflict"
)

// Error codes.
const (
	CodeInvalid      = "invalid"
	CodeUnauthorized = "unauthorized"
	CodeNotFound     = "not_found"
	CodeConflict     = "conflict"
	CodeUnsupported  = "unsupported_protocol"
	CodeInternal     = "internal"
)

// Error is the body of every failed response.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

// EnrollRequest registers the certificate of the calling connection.
type EnrollRequest struct {
	Secret string `json:"secret"`
}

type EnrollResponse struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
}

// Space holds the check value of the encryption key shared by devices. The
// hub never sees the key; a device joining later proves it has the same key.
type Space struct {
	KeyCheck string `json:"key_check"`
}

// Project is a synced project. Meta is encrypted on the device.
type Project struct {
	ID   string `json:"id"`
	Meta []byte `json:"meta"`
}

type ProjectsResponse struct {
	Projects []Project `json:"projects"`
}

// Row is one stored row. Body is ciphertext; Tag is a keyed hash of the
// content, so the hub can tell equal from different without reading it.
type Row struct {
	Table         string `json:"table"`
	Key           string `json:"key"`
	Kind          string `json:"kind"`
	Revision      int64  `json:"revision"`
	Tag           string `json:"tag"`
	SchemaVersion int64  `json:"schema_version"`
	Body          []byte `json:"body"`
	Seq           int64  `json:"seq,omitempty"`
}

type PushRequest struct {
	ProjectID string `json:"project_id"`
	Rows      []Row  `json:"rows"`
}

// PushResult answers one pushed row, in request order.
type PushResult struct {
	Status   string `json:"status"`
	Revision int64  `json:"revision,omitempty"`
	Tag      string `json:"tag,omitempty"`
}

type PushResponse struct {
	Results []PushResult `json:"results"`
}

// PullResponse returns rows after a cursor. Next is the cursor to continue
// from; it moves past rows the calling device wrote itself, which are left out.
type PullResponse struct {
	Rows []Row `json:"rows"`
	Next int64 `json:"next"`
	More bool  `json:"more"`
}

// Limits.
const (
	MaxPushRows   = 500
	MaxPullRows   = 1000
	MaxBodyBytes  = 64 << 20
	MaxRowBytes   = 8 << 20
	MaxMetaBytes  = 64 << 10
	MaxNameLength = 64
)
