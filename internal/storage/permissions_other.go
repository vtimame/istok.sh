//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// Non-Unix platforms use their native ACL model. The Go standard library cannot
// create an owner-only ACL, so SQLite creates the database with the directory's
// inherited access controls.
func prepareDatabasePath(path string, _ bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}

	return nil
}
