package exchangerepo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/vtimame/istok.sh/internal/exchange"
)

// Sync row kinds mirror the table kinds; a group is the child rows of one
// parent, kept and replaced together.
const (
	SyncMutable = "mutable"
	SyncAppend  = "append"
	SyncGroup   = "group"
)

// SyncRow is one unit of synchronization: a row, or the children of one
// parent for followsParent tables.
type SyncRow struct {
	Table    string
	Key      string
	Kind     string
	Revision int64
	Rows     []exchange.Row
}

// Snapshot reads one project as sync rows, in import order. Every parent of a
// followsParent table gets a group, empty or not, so removed children reach
// other devices.
func (r *Repository) Snapshot(ctx context.Context, projectID string) (int64, []SyncRow, error) {
	bundle, err := r.Export(ctx, []string{projectID})
	if err != nil {
		return 0, nil, err
	}

	result := []SyncRow{}
	for _, spec := range tables {
		switch spec.kind {
		case mutable:
			for _, row := range bundle.Tables[spec.name] {
				revision, _ := normalize(row["revision"]).(int64)
				result = append(result, SyncRow{Table: spec.name, Key: keyOf(spec, row), Kind: SyncMutable, Revision: revision, Rows: []exchange.Row{row}})
			}
		case appendOnly:
			for _, row := range bundle.Tables[spec.name] {
				result = append(result, SyncRow{Table: spec.name, Key: keyOf(spec, row), Kind: SyncAppend, Rows: []exchange.Row{row}})
			}
		case followsParent:
			result = append(result, groups(spec, bundle)...)
		}
	}

	return bundle.SchemaVersion, result, nil
}

func groups(spec table, bundle exchange.Bundle) []SyncRow {
	ownerTable := ""
	for _, link := range spec.parents {
		if link.column == spec.owner {
			ownerTable = link.table
		}
	}

	children := map[string][]exchange.Row{}
	for _, row := range bundle.Tables[spec.name] {
		owner := fmt.Sprint(normalize(row[spec.owner]))
		children[owner] = append(children[owner], row)
	}

	result := []SyncRow{}
	for _, parent := range bundle.Tables[ownerTable] {
		owner := fmt.Sprint(normalize(parent["id"]))
		revision, _ := normalize(parent["revision"]).(int64)

		rows := children[owner]
		sort.Slice(rows, func(i, j int) bool { return keyOf(spec, rows[i]) < keyOf(spec, rows[j]) })
		if rows == nil {
			rows = []exchange.Row{}
		}

		result = append(result, SyncRow{Table: spec.name, Key: owner, Kind: SyncGroup, Revision: revision, Rows: rows})
	}

	return result
}

// Canonical is the content a tag is computed from: normalized rows without
// device-local columns such as task numbers. Equal content on two devices
// gives equal bytes.
func Canonical(row SyncRow) ([]byte, error) {
	spec, ok := specOf(row.Table)
	if !ok {
		return nil, fmt.Errorf("unknown sync table %q", row.Table)
	}

	values := make([]map[string]any, 0, len(row.Rows))
	for _, source := range row.Rows {
		value := map[string]any{}
		for name, column := range source {
			if slices.Contains(spec.keepLocal, name) {
				continue
			}
			value[name] = normalize(column)
		}
		values = append(values, value)
	}

	return json.Marshal(values)
}

// Body is the full content sent to other devices, device-local columns
// included: a new task keeps its number when it is free.
func Body(row SyncRow) ([]byte, error) {
	values := make([]map[string]any, 0, len(row.Rows))
	for _, source := range row.Rows {
		value := map[string]any{}
		for name, column := range source {
			value[name] = normalize(column)
		}
		values = append(values, value)
	}

	return json.Marshal(values)
}

// ParseBody reads rows written by Body. Numbers stay exact.
func ParseBody(data []byte) ([]exchange.Row, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var values []map[string]any
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}

	rows := make([]exchange.Row, 0, len(values))
	for _, value := range values {
		row := exchange.Row{}
		for name, column := range value {
			row[name] = normalize(column)
		}
		rows = append(rows, row)
	}

	return rows, nil
}

// SyncBundle turns pulled rows of one project into a bundle for Import. Rows
// are checked column by column on import, so devices on releases with the
// same exported columns can sync even when their migration versions differ.
//
// Import replaces the children of every replaced parent. A parent pulled
// without its group kept its children on the hub, so the local children are
// carried along unchanged.
func (r *Repository) SyncBundle(ctx context.Context, projectID string, rows []SyncRow) (exchange.Bundle, error) {
	version, err := r.SchemaVersion(ctx)
	if err != nil {
		return exchange.Bundle{}, err
	}

	bundle := exchange.Bundle{
		Format:        exchange.Format,
		SchemaVersion: version,
		Projects:      []exchange.ProjectSummary{{ID: projectID}},
		Tables:        map[string][]exchange.Row{},
	}

	pulledGroups := map[string]map[string]bool{}
	for _, row := range rows {
		if _, ok := specOf(row.Table); !ok {
			return exchange.Bundle{}, exchange.Errorf(exchange.CodeInvalid, "pulled rows contain unknown table %q; update Istok", row.Table)
		}
		if row.Kind == SyncGroup {
			if pulledGroups[row.Table] == nil {
				pulledGroups[row.Table] = map[string]bool{}
			}
			pulledGroups[row.Table][row.Key] = true
		}
		bundle.Tables[row.Table] = append(bundle.Tables[row.Table], row.Rows...)
	}

	for _, spec := range tables {
		if spec.kind != followsParent {
			continue
		}
		if err := r.carryChildren(ctx, spec, &bundle, pulledGroups[spec.name]); err != nil {
			return exchange.Bundle{}, err
		}
	}

	return bundle, nil
}

func (r *Repository) carryChildren(ctx context.Context, spec table, bundle *exchange.Bundle, pulled map[string]bool) error {
	ownerTable := ""
	for _, link := range spec.parents {
		if link.column == spec.owner {
			ownerTable = link.table
		}
	}

	var owners []any
	for _, parent := range bundle.Tables[ownerTable] {
		owner := fmt.Sprint(normalize(parent["id"]))
		if !pulled[owner] {
			owners = append(owners, owner)
		}
	}
	if len(owners) == 0 {
		return nil
	}

	names, err := columns(ctx, r.db, spec)
	if err != nil {
		return err
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s IN (%s) ORDER BY %s",
		strings.Join(names, ", "), spec.name, spec.owner, placeholders(len(owners)), strings.Join(spec.key, ", "))
	result, err := r.db.QueryContext(ctx, query, owners...)
	if err != nil {
		return fmt.Errorf("read local %s: %w", spec.name, err)
	}

	children, err := scanRows(result, names)
	if err != nil {
		return err
	}
	bundle.Tables[spec.name] = append(bundle.Tables[spec.name], children...)

	return nil
}

func specOf(name string) (table, bool) {
	for _, spec := range tables {
		if spec.name == name {
			return spec, true
		}
	}

	return table{}, false
}
