package replica

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/vtimame/istok.sh/internal/exchange"
	"github.com/vtimame/istok.sh/internal/hub/protocol"
	"github.com/vtimame/istok.sh/internal/storage/exchangerepo"
	"github.com/vtimame/istok.sh/internal/storage/syncrepo"
)

// Hub is the part of the hub client the engine uses.
type Hub interface {
	Projects(ctx context.Context) ([]protocol.Project, error)
	PutProject(ctx context.Context, value protocol.Project) error
	Push(ctx context.Context, request protocol.PushRequest) ([]protocol.PushResult, error)
	Pull(ctx context.Context, projectID string, after int64) (protocol.PullResponse, error)
}

// Actor attributes history the sync import writes, such as renumbering.
var Actor = exchangerepo.Actor{ID: "istok-sync", Kind: "sync", Name: "Istok sync"}

// pushBatchBytes keeps one push well under the hub's body limit.
const pushBatchBytes = 32 << 20

// Engine syncs projects of the local database with one hub.
type Engine struct {
	exchange *exchangerepo.Repository
	state    *syncrepo.Repository
	hub      Hub
	key      Key
}

func NewEngine(exchange *exchangerepo.Repository, state *syncrepo.Repository, hub Hub, key Key) *Engine {
	return &Engine{exchange: exchange, state: state, hub: hub, key: key}
}

// RemoteProject is a project stored on the hub.
type RemoteProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type projectMeta struct {
	Name string `json:"name"`
}

// Result reports one project sync.
type Result struct {
	ProjectID string           `json:"project_id"`
	Pulled    int              `json:"pulled"`
	Pushed    int              `json:"pushed"`
	Stale     int              `json:"stale"`
	Conflicts int              `json:"conflicts"`
	Import    *exchange.Report `json:"import,omitempty"`
}

// RemoteProjects lists the hub's projects with their names decrypted.
func (e *Engine) RemoteProjects(ctx context.Context) ([]RemoteProject, error) {
	projects, err := e.hub.Projects(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]RemoteProject, 0, len(projects))
	for _, value := range projects {
		plaintext, err := e.key.Open(value.Meta, projectContext(value.ID))
		if err != nil {
			return nil, err
		}

		var meta projectMeta
		if err := json.Unmarshal(plaintext, &meta); err != nil {
			return nil, fmt.Errorf("read project metadata: %w", err)
		}
		result = append(result, RemoteProject{ID: value.ID, Name: meta.Name})
	}

	return result, nil
}

// Register publishes a local project on the hub and starts syncing it.
func (e *Engine) Register(ctx context.Context, projectID, name string) error {
	plaintext, err := json.Marshal(projectMeta{Name: name})
	if err != nil {
		return err
	}
	sealed, err := e.key.Seal(plaintext, projectContext(projectID))
	if err != nil {
		return err
	}

	if err := e.hub.PutProject(ctx, protocol.Project{ID: projectID, Meta: sealed}); err != nil {
		return err
	}

	return e.state.Enable(ctx, projectID)
}

// Sync pulls the project's changes from the hub, merges them, and pushes
// local changes.
func (e *Engine) Sync(ctx context.Context, projectID string) (Result, error) {
	value, ok, err := e.state.Project(ctx, projectID)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, fmt.Errorf("project %s is not synced on this device: run `istok sync enable`", projectID)
	}

	return e.sync(ctx, projectID, value.Cursor, false)
}

// Clone fetches a hub project this device does not sync yet, creating it
// locally when needed, and starts syncing it.
func (e *Engine) Clone(ctx context.Context, projectID string) (Result, error) {
	if _, ok, err := e.state.Project(ctx, projectID); err != nil || ok {
		if err != nil {
			return Result{}, err
		}
		return e.Sync(ctx, projectID)
	}

	return e.sync(ctx, projectID, 0, true)
}

func (e *Engine) sync(ctx context.Context, projectID string, cursor int64, clone bool) (Result, error) {
	result := Result{ProjectID: projectID}

	pulled, next, err := e.pull(ctx, projectID, cursor)
	if err != nil {
		return result, err
	}
	result.Pulled = len(pulled)

	if len(pulled) > 0 {
		rows := make([]exchangerepo.SyncRow, len(pulled))
		for index, row := range pulled {
			rows[index] = row.row
		}

		bundle, err := e.exchange.SyncBundle(ctx, projectID, rows)
		if err != nil {
			return result, err
		}
		report, err := e.exchange.Import(ctx, bundle, Actor, false)
		if err != nil {
			return result, explainImport(err)
		}
		result.Import = &report
	} else if clone {
		return result, fmt.Errorf("the hub has no rows for project %s", projectID)
	}

	if clone {
		if err := e.state.Enable(ctx, projectID); err != nil {
			return result, err
		}
	}

	schema, snapshot, err := e.exchange.Snapshot(ctx, projectID)
	if err != nil {
		return result, err
	}

	local := map[syncrepo.RowKey]localRow{}
	for _, row := range snapshot {
		tag, err := e.tagOf(row)
		if err != nil {
			return result, err
		}
		local[syncrepo.RowKey{Table: row.Table, Key: row.Key}] = localRow{row: row, tag: tag}
	}

	updates := make([]syncrepo.Update, 0, len(pulled))
	for _, row := range pulled {
		key := syncrepo.RowKey{Table: row.row.Table, Key: row.row.Key}
		updates = append(updates, syncrepo.Update{RowKey: key, RowState: syncrepo.RowState{
			Revision: row.row.Revision, HubTag: row.tag, LocalTag: local[key].tag,
		}})
	}
	if err := e.state.Record(ctx, projectID, updates, &next); err != nil {
		return result, err
	}

	if err := e.push(ctx, projectID, schema, snapshot, local, &result); err != nil {
		return result, err
	}

	return result, e.state.Touch(ctx, projectID)
}

type pulledRow struct {
	row exchangerepo.SyncRow
	tag string
}

type localRow struct {
	row exchangerepo.SyncRow
	tag string
}

func (e *Engine) pull(ctx context.Context, projectID string, cursor int64) ([]pulledRow, int64, error) {
	var result []pulledRow
	for {
		response, err := e.hub.Pull(ctx, projectID, cursor)
		if err != nil {
			return nil, 0, err
		}

		for _, row := range response.Rows {
			value, err := e.decode(projectID, row)
			if err != nil {
				return nil, 0, err
			}
			result = append(result, pulledRow{row: value, tag: row.Tag})
		}

		cursor = response.Next
		if !response.More {
			return result, cursor, nil
		}
	}
}

// decode decrypts a pulled row and checks that its tag matches its content,
// so a hub cannot alter rows unnoticed.
func (e *Engine) decode(projectID string, row protocol.Row) (exchangerepo.SyncRow, error) {
	plaintext, err := e.key.Open(row.Body, rowContext(projectID, row.Table, row.Key, row.Kind, row.Revision))
	if err != nil {
		return exchangerepo.SyncRow{}, err
	}

	rows, err := exchangerepo.ParseBody(plaintext)
	if err != nil {
		return exchangerepo.SyncRow{}, fmt.Errorf("read hub row %s/%s: %w", row.Table, row.Key, err)
	}

	value := exchangerepo.SyncRow{Table: row.Table, Key: row.Key, Kind: row.Kind, Revision: row.Revision, Rows: rows}
	tag, err := e.tagOf(value)
	if err != nil {
		return exchangerepo.SyncRow{}, err
	}
	if tag != row.Tag {
		return exchangerepo.SyncRow{}, fmt.Errorf("hub row %s/%s does not match its tag", row.Table, row.Key)
	}

	return value, nil
}

func (e *Engine) tagOf(row exchangerepo.SyncRow) (string, error) {
	canonical, err := exchangerepo.Canonical(row)
	if err != nil {
		return "", err
	}

	return e.key.Tag(row.Table, row.Key, canonical), nil
}

// shouldPush decides whether a local row goes to the hub: it is new to the
// hub, it changed here since the last sync, or it is a newer revision than
// the hub holds.
func shouldPush(row localRow, state syncrepo.RowState, known bool) bool {
	if !known {
		return true
	}
	if row.tag == state.HubTag {
		return false
	}
	if row.tag != state.LocalTag {
		return true
	}

	return row.row.Kind == exchangerepo.SyncMutable && row.row.Revision > state.Revision
}

func (e *Engine) push(ctx context.Context, projectID string, schema int64, snapshot []exchangerepo.SyncRow,
	local map[syncrepo.RowKey]localRow, result *Result) error {
	states, err := e.state.Rows(ctx, projectID)
	if err != nil {
		return err
	}

	var batch []protocol.Row
	var keys []syncrepo.RowKey
	size := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		results, err := e.hub.Push(ctx, protocol.PushRequest{ProjectID: projectID, Rows: batch})
		if err != nil {
			return err
		}

		updates := make([]syncrepo.Update, 0, len(results))
		for index, answer := range results {
			sent := batch[index]
			state := syncrepo.RowState{Revision: sent.Revision, HubTag: sent.Tag, LocalTag: sent.Tag}

			switch answer.Status {
			case protocol.StatusAccepted:
				result.Pushed++
			case protocol.StatusUnchanged:
			case protocol.StatusStale, protocol.StatusConflict:
				// The hub keeps its version; remember it so the row is not
				// sent again until it changes here or arrives from the hub.
				state.Revision, state.HubTag = answer.Revision, answer.Tag
				if answer.Status == protocol.StatusStale {
					result.Stale++
				} else {
					result.Conflicts++
				}
			default:
				return fmt.Errorf("hub answered unknown push status %q", answer.Status)
			}
			updates = append(updates, syncrepo.Update{RowKey: keys[index], RowState: state})
		}

		batch, keys, size = nil, nil, 0

		return e.state.Record(ctx, projectID, updates, nil)
	}

	for _, row := range snapshot {
		key := syncrepo.RowKey{Table: row.Table, Key: row.Key}
		entry := local[key]

		state, known := states[key]
		if !shouldPush(entry, state, known) {
			continue
		}

		value, err := e.encode(projectID, schema, entry)
		if err != nil {
			return err
		}

		if len(batch) == protocol.MaxPushRows || (len(batch) > 0 && size+len(value.Body) > pushBatchBytes) {
			if err := flush(); err != nil {
				return err
			}
		}
		batch = append(batch, value)
		keys = append(keys, key)
		size += len(value.Body)
	}

	return flush()
}

func (e *Engine) encode(projectID string, schema int64, entry localRow) (protocol.Row, error) {
	plaintext, err := exchangerepo.Body(entry.row)
	if err != nil {
		return protocol.Row{}, err
	}
	if len(plaintext) > protocol.MaxRowBytes-1024 {
		return protocol.Row{}, fmt.Errorf("row %s/%s is too large to sync (%d bytes)", entry.row.Table, entry.row.Key, len(plaintext))
	}

	row := entry.row
	sealed, err := e.key.Seal(plaintext, rowContext(projectID, row.Table, row.Key, row.Kind, row.Revision))
	if err != nil {
		return protocol.Row{}, err
	}

	return protocol.Row{
		Table:         row.Table,
		Key:           row.Key,
		Kind:          row.Kind,
		Revision:      row.Revision,
		Tag:           entry.tag,
		SchemaVersion: schema,
		Body:          sealed,
	}, nil
}

// explainImport turns a column mismatch into an update hint: another device
// runs a release with different exported columns.
func explainImport(err error) error {
	code, ok := exchange.ErrorCode(err)
	if ok && code == exchange.CodeInvalid {
		return fmt.Errorf("cannot apply rows from the hub: %w; run the same Istok release on every device", err)
	}

	return err
}

// ErrNoKey means the device joined but does not hold the encryption key yet.
var ErrNoKey = errors.New("this device has no sync key yet: run `istok sync join CODE --key istok-key-...` with the key from `istok sync key` on another device")
