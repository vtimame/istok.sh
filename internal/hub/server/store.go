// Package server is the Istok hub: it stores encrypted rows for devices and
// serves them over mutual TLS.
package server

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// timeFormat has a fixed width, so stored timestamps compare as strings.
const timeFormat = "2006-01-02T15:04:05.000000000Z"

// Store is the hub database. Writes go through one connection; handshake
// checks and reads use a separate pool, so a long push does not stall
// connections (SQLite WAL lets readers run beside the writer).
type Store struct {
	db   *sql.DB
	read *sql.DB
	now  func() time.Time

	seenMu sync.Mutex
	seen   map[string]time.Time
}

// OpenStore opens or creates the hub database and applies its migrations.
func OpenStore(ctx context.Context, path string) (*Store, error) {
	query := url.Values{
		"_busy_timeout": {"5000"},
		"_foreign_keys": {"on"},
		"_journal_mode": {"WAL"},
		"_txlock":       {"immediate"},
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open hub database: %w", err)
	}
	db.SetMaxOpenConns(1)

	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrationFS)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create hub migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply hub migrations: %w", err)
	}

	readQuery := url.Values{"_busy_timeout": {"5000"}, "_foreign_keys": {"on"}, "mode": {"ro"}}
	read, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: path, RawQuery: readQuery.Encode()}).String())
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open hub database for reading: %w", err)
	}
	read.SetMaxOpenConns(8)

	return &Store{db: db, read: read, now: func() time.Time { return time.Now().UTC() }, seen: map[string]time.Time{}}, nil
}

func (s *Store) Close() error { return errors.Join(s.read.Close(), s.db.Close()) }

func (s *Store) timestamp() string { return s.now().Format(timeFormat) }

// Meta reads a hub setting; a missing one is empty.
func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM hub_meta WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	return value, err
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO hub_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)

	return err
}

// SetMetaOnce stores a setting unless one exists and returns the stored value.
func (s *Store) SetMetaOnce(ctx context.Context, key, value string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "INSERT INTO hub_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING", key, value); err != nil {
		return "", err
	}

	var stored string
	if err := tx.QueryRowContext(ctx, "SELECT value FROM hub_meta WHERE key = ?", key).Scan(&stored); err != nil {
		return "", err
	}

	return stored, tx.Commit()
}

// Backup writes a consistent copy of the database to path.
func (s *Store) Backup(ctx context.Context, path string) error {
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return fmt.Errorf("back up hub database: %w", err)
	}

	return nil
}
