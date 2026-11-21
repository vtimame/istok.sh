// Package runrepo persists the local Run kernel and its immutable evidence.
package runrepo

import (
	"database/sql"
	"time"
)

type Repository struct {
	db  *sql.DB
	now func() time.Time

	// device names this device on the hub; leases written here carry it.
	device string
}

// UseDevice sets the hub device name leases written here carry; empty for a
// device without a hub.
func (r *Repository) UseDevice(name string) { r.device = name }

func New(db *sql.DB) *Repository {
	return &Repository{db: db, now: time.Now}
}

func NewWithClock(db *sql.DB, now func() time.Time) *Repository {
	return &Repository{db: db, now: now}
}
