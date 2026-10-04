//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func prepareDatabasePath(path string, defaultPath bool) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("stat database directory: %w", err)
	}
	if !directoryInfo.IsDir() {
		return fmt.Errorf("database parent is not a directory (mode %s)", directoryInfo.Mode().Type())
	}
	if defaultPath {
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("set default database directory permissions: %w", err)
		}
	} else if directoryInfo.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("custom database directory is writable by group or others (mode %04o)", directoryInfo.Mode().Perm())
	}

	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("open database without following symlinks: %w", err)
	}

	file := os.NewFile(uintptr(fd), path)
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat opened database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("database is not a regular file (mode %s)", info.Mode().Type())
	}
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("set database permissions: %w", err)
	}

	return nil
}
