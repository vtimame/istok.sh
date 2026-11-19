package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/vtimame/istok.sh/internal/hub/protocol"
)

// Device is a registered device.
type Device struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))

	return hex.EncodeToString(sum[:])
}

// AddEnrollment stores a one-time secret for a device that has not joined yet.
func (s *Store) AddEnrollment(ctx context.Context, name, secret string, ttl time.Duration) error {
	now := s.now()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO enrollments (secret_hash, name, created_at, expires_at) VALUES (?, ?, ?, ?)",
		hashSecret(secret), name, now.Format(timeFormat), now.Add(ttl).Format(timeFormat))
	if err != nil {
		return fmt.Errorf("store enrollment: %w", err)
	}

	return nil
}

// Accepts decides during the TLS handshake: registered devices connect, and
// unknown certificates only while an enrollment is open.
func (s *Store) Accepts(ctx context.Context, fingerprint string) bool {
	var known int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE id = ? AND revoked_at IS NULL", fingerprint).Scan(&known)
	if err != nil {
		return false
	}
	if known > 0 {
		return true
	}

	open, err := s.openEnrollments(ctx)

	return err == nil && open > 0
}

func (s *Store) openEnrollments(ctx context.Context) (int, error) {
	var open int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM enrollments WHERE used_at IS NULL AND expires_at > ?",
		s.timestamp()).Scan(&open)

	return open, err
}

// Device returns the active device with this fingerprint and marks it seen.
func (s *Store) Device(ctx context.Context, fingerprint string) (Device, bool, error) {
	device, err := s.device(ctx, fingerprint)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && device.RevokedAt != nil) {
		return Device{}, false, nil
	}
	if err != nil {
		return Device{}, false, err
	}

	if _, err := s.db.ExecContext(ctx, "UPDATE devices SET last_seen_at = ? WHERE id = ?", s.timestamp(), fingerprint); err != nil {
		return Device{}, false, err
	}

	return device, true, nil
}

func (s *Store) device(ctx context.Context, id string) (Device, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, created_at, last_seen_at, revoked_at FROM devices WHERE id = ?", id)
	if err != nil {
		return Device{}, err
	}

	devices, err := scanDevices(rows)
	if err != nil {
		return Device{}, err
	}
	if len(devices) == 0 {
		return Device{}, sql.ErrNoRows
	}

	return devices[0], nil
}

// Enroll trades an open secret for a device registration of fingerprint.
func (s *Store) Enroll(ctx context.Context, fingerprint, secret string) (Device, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Device{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var name string
	err = tx.QueryRowContext(ctx,
		"SELECT name FROM enrollments WHERE secret_hash = ? AND used_at IS NULL AND expires_at > ?",
		hashSecret(secret), s.timestamp()).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, &protocol.Error{Code: protocol.CodeUnauthorized, Message: "the invitation is unknown, used or expired"}
	}
	if err != nil {
		return Device{}, err
	}

	var existing int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE id = ?", fingerprint).Scan(&existing); err != nil {
		return Device{}, err
	}
	if existing > 0 {
		return Device{}, &protocol.Error{Code: protocol.CodeConflict,
			Message: "this device key is already registered or was revoked; create a new device key to join again"}
	}

	now := s.timestamp()
	if _, err := tx.ExecContext(ctx, "INSERT INTO devices (id, name, created_at) VALUES (?, ?, ?)", fingerprint, name, now); err != nil {
		return Device{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE enrollments SET used_at = ?, device_id = ? WHERE secret_hash = ?",
		now, fingerprint, hashSecret(secret)); err != nil {
		return Device{}, err
	}

	if err := tx.Commit(); err != nil {
		return Device{}, err
	}

	return s.device(ctx, fingerprint)
}

// Devices lists registered devices, revoked ones included.
func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, created_at, last_seen_at, revoked_at FROM devices ORDER BY created_at")
	if err != nil {
		return nil, err
	}

	return scanDevices(rows)
}

// Revoke disconnects a device on its next request. A prefix of the ID or the
// exact name selects it when unambiguous.
func (s *Store) Revoke(ctx context.Context, selector string) (Device, error) {
	devices, err := s.Devices(ctx)
	if err != nil {
		return Device{}, err
	}

	var matches []Device
	for _, device := range devices {
		if device.RevokedAt == nil && (device.Name == selector || (len(selector) >= 4 && len(device.ID) >= len(selector) && device.ID[:len(selector)] == selector)) {
			matches = append(matches, device)
		}
	}
	switch len(matches) {
	case 0:
		return Device{}, fmt.Errorf("no active device matches %q", selector)
	case 1:
	default:
		return Device{}, fmt.Errorf("%q matches %d devices; use a longer ID prefix", selector, len(matches))
	}

	if _, err := s.db.ExecContext(ctx, "UPDATE devices SET revoked_at = ? WHERE id = ?", s.timestamp(), matches[0].ID); err != nil {
		return Device{}, err
	}

	return s.device(ctx, matches[0].ID)
}

func scanDevices(rows *sql.Rows) ([]Device, error) {
	defer rows.Close()

	var result []Device
	for rows.Next() {
		var device Device
		var created string
		var seen, revoked sql.NullString
		if err := rows.Scan(&device.ID, &device.Name, &created, &seen, &revoked); err != nil {
			return nil, err
		}

		device.CreatedAt, _ = time.Parse(timeFormat, created)
		device.LastSeenAt = parseOptional(seen)
		device.RevokedAt = parseOptional(revoked)
		result = append(result, device)
	}

	return result, rows.Err()
}

func parseOptional(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}

	parsed, err := time.Parse(timeFormat, value.String)
	if err != nil {
		return nil
	}

	return &parsed
}
