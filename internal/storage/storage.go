package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/adrg/xdg"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
	"go.uber.org/fx"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Config struct {
	Path string
}

func DefaultPath() string {
	return filepath.Join(xdg.DataHome, "istok", "istok.db")
}

func Module(config Config) fx.Option {
	return fx.Module("storage", fx.Provide(func(lc fx.Lifecycle) (*sql.DB, error) {
		return New(config, lc)
	}))
}

func sqliteDSN(path string) string {
	query := url.Values{
		"_busy_timeout": {"5000"},
		"_foreign_keys": {"on"},
	}

	if path == ":memory:" {
		return "file::memory:?" + query.Encode()
	}

	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

func New(config Config, lc fx.Lifecycle) (*sql.DB, error) {
	path := config.Path
	if path == "" {
		path = DefaultPath()
	}

	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	var closeOnce sync.Once
	var closeErr error
	closeDB := func() error {
		closeOnce.Do(func() { closeErr = db.Close() })
		return closeErr
	}

	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		_ = closeDB()
		return nil, fmt.Errorf("prepare embedded migrations: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrationFS)
	if err != nil {
		_ = closeDB()
		return nil, fmt.Errorf("create migration provider: %w", err)
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := db.PingContext(ctx); err != nil {
				return closeAfterStartFailure(closeDB, "ping SQLite database", err)
			}

			if _, err := provider.Up(ctx); err != nil {
				return closeAfterStartFailure(closeDB, "apply embedded migrations", err)
			}

			return nil
		},
		OnStop: func(context.Context) error {
			if err := closeDB(); err != nil {
				return fmt.Errorf("close SQLite database: %w", err)
			}

			return nil
		},
	})

	return db, nil
}

func closeAfterStartFailure(closeDB func() error, operation string, err error) error {
	startErr := fmt.Errorf("%s: %w", operation, err)
	if closeErr := closeDB(); closeErr != nil {
		return errors.Join(startErr, fmt.Errorf("close SQLite database after start failure: %w", closeErr))
	}

	return startErr
}
