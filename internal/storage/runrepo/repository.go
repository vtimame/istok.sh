// Package runrepo persists the local Run kernel and its immutable evidence.
package runrepo

import (
	"database/sql"
	"time"
)

type Repository struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db, now: time.Now}
}

func NewWithClock(db *sql.DB, now func() time.Time) *Repository {
	return &Repository{db: db, now: now}
}
