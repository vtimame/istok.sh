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
	CodeRateLimited  = "rate_limited"
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

// PullResponse returns rows after a cursor, including rows the calling device
// wrote. Next is the cursor to continue from; More asks for another page.
type PullResponse struct {
	Rows []Row `json:"rows"`
	Next int64 `json:"next"`
	More bool  `json:"more"`
}

// NumberClaim asks for the number of a task. Number is the one the device
// would like, 0 for any. The hub answers with the task's number: the one it
// already holds, the requested one when free, or the next free one.
type NumberClaim struct {
	TaskID string `json:"task_id"`
	Number int64  `json:"number"`
}

type NumbersRequest struct {
	ProjectID string        `json:"project_id"`
	Claims    []NumberClaim `json:"claims"`
}

// NumbersResponse answers the claims in request order.
type NumbersResponse struct {
	Numbers []NumberClaim `json:"numbers"`
}

// LeaseClaim acquires or renews the lease of a task for a run of the calling
// device for TTLSeconds, measured by the hub's clock.
type LeaseClaim struct {
	TaskID     string `json:"task_id"`
	RunID      string `json:"run_id"`
	TTLSeconds int64  `json:"ttl_seconds"`
}

type LeasesRequest struct {
	ProjectID string       `json:"project_id"`
	Acquire   []LeaseClaim `json:"acquire,omitempty"`

	// Release lists tasks whose lease the calling device gives up.
	Release []string `json:"release,omitempty"`
}

// LeaseResult answers one acquire. A refused lease names its holder.
type LeaseResult struct {
	TaskID  string       `json:"task_id"`
	Granted bool         `json:"granted"`
	Holder  *LeaseHolder `json:"holder,omitempty"`
}

type LeaseHolder struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	RunID      string `json:"run_id"`
	ExpiresAt  string `json:"expires_at"`
}

type LeasesResponse struct {
	Results []LeaseResult `json:"results"`
}

// Limits.
const (
	MaxNumberClaims = 1000
	MaxLeaseClaims  = 500
	MaxLeaseTTL     = 24 * 60 * 60

	MaxPushRows   = 500
	MaxPullRows   = 1000
	MaxBodyBytes  = 64 << 20
	MaxRowBytes   = 8 << 20
	MaxMetaBytes  = 64 << 10
	MaxNameLength = 64
)
