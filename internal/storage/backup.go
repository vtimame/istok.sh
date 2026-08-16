package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mattn/go-sqlite3"
)

// Backup creates a consistent SQLite snapshot using SQLite's online backup API.
// It intentionally does not create a source database when it is absent.
func Backup(ctx context.Context, path string) (backupPath string, existed bool, err error) {
	if path == ":memory:" {
		return "", false, fmt.Errorf("in-memory database cannot be backed up for an applied update")
	}

	if _, err := os.Stat(path); errorsIsNotExist(err) {
		return "", false, nil
	} else if err != nil {
		return "", false, fmt.Errorf("stat database: %w", err)
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".istok-update-db-*.bak")
	if err != nil {
		return "", false, fmt.Errorf("create database backup: %w", err)
	}
	backupPath = file.Name()
	defer func() {
		if err == nil || backupPath == "" {
			return
		}

		if removeErr := removeDatabaseFiles(backupPath); removeErr != nil {
			err = fmt.Errorf("%w; remove partial database backup %q: %v", err, backupPath, removeErr)
		} else {
			backupPath = ""
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return backupPath, true, fmt.Errorf("set database backup permissions: %w", err)
	}
	if err := file.Close(); err != nil {
		return backupPath, true, fmt.Errorf("close database backup file: %w", err)
	}

	source, err := sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		return backupPath, true, err
	}
	defer source.Close()
	destination, err := sql.Open("sqlite3", sqliteDSN(backupPath))
	if err != nil {
		return backupPath, true, err
	}
	defer destination.Close()

	connSource, err := source.Conn(ctx)
	if err != nil {
		return backupPath, true, err
	}
	defer connSource.Close()
	connDestination, err := destination.Conn(ctx)
	if err != nil {
		return backupPath, true, err
	}
	defer connDestination.Close()

	err = connDestination.Raw(func(dest any) error {
		return connSource.Raw(func(src any) error {
			backup, err := dest.(*sqlite3.SQLiteConn).Backup("main", src.(*sqlite3.SQLiteConn), "main")
			if err != nil {
				return err
			}
			defer backup.Finish()
			for {
				done, err := backup.Step(-1)
				if err != nil {
					return err
				}
				if done {
					return nil
				}
			}
		})
	})
	if err != nil {
		return backupPath, true, fmt.Errorf("backup SQLite database: %w", err)
	}

	return backupPath, true, nil
}

func Restore(path, backupPath string, existed bool) error {
	if !existed {
		if err := removeDatabaseFiles(path); err != nil {
			return fmt.Errorf("remove newly-created database %q and sidecars: %w", path, err)
		}
		return nil
	}

	failedPath := ""
	if _, err := os.Lstat(path); err == nil {
		failedPath, err = uniqueRestorePath(path)
		if err != nil {
			return err
		}
		if err := os.Rename(path, failedPath); err != nil {
			return fmt.Errorf("stage current database %q for restore: %w", path, err)
		}
	} else if !errorsIsNotExist(err) {
		return fmt.Errorf("inspect current database %q for restore: %w", path, err)
	}

	if err := removeSidecars(path); err != nil {
		recoveryErr := restoreStagedFile(failedPath, path)
		return fmt.Errorf("remove database sidecars before restore: %w; restore original database from %q: %v", err, failedPath, recoveryErr)
	}
	if err := os.Rename(backupPath, path); err != nil {
		recoveryErr := restoreStagedFile(failedPath, path)
		return fmt.Errorf("promote database backup %q to %q: %w; restore original database from %q: %v; retained artifacts: backup %q, staged database %q", backupPath, path, err, failedPath, recoveryErr, backupPath, failedPath)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set restored database permissions %q: %w; retained previous database artifact %q", path, err, failedPath)
	}
	if failedPath != "" {
		if err := removeDatabaseFiles(failedPath); err != nil {
			return fmt.Errorf("restored database but remove previous database artifact %q: %w", failedPath, err)
		}
	}

	return nil
}

func restoreStagedFile(stagedPath, targetPath string) error {
	if stagedPath == "" {
		return nil
	}
	if err := os.Rename(stagedPath, targetPath); err != nil {
		return err
	}

	return nil
}

func uniqueRestorePath(path string) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".istok-update-db-failed-*")
	if err != nil {
		return "", fmt.Errorf("create staged database restore path: %w", err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close staged database restore path: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return "", fmt.Errorf("prepare staged database restore path: %w", err)
	}
	return name, nil
}

func removeDatabaseFiles(path string) error {
	var result error
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(name); err != nil && !errorsIsNotExist(err) {
			result = fmt.Errorf("remove %q: %w", name, err)
		}
	}
	return result
}

func removeSidecars(path string) error {
	var result error
	for _, name := range []string{path + "-wal", path + "-shm"} {
		if err := os.Remove(name); err != nil && !errorsIsNotExist(err) {
			result = fmt.Errorf("remove %q: %w", name, err)
		}
	}
	return result
}

func errorsIsNotExist(err error) bool { return os.IsNotExist(err) }
