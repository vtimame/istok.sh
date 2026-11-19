// Package replica syncs local projects through an Istok hub. The local
// database stays the source of truth; the hub relays encrypted rows between
// devices and never sees their content.
package replica

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

const recoveryPrefix = "istok-key-"

var recoveryEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Key is the encryption key shared by the devices of one hub. It never
// leaves the devices; the hub stores only a check value.
type Key struct {
	seal []byte
	mac  []byte
}

// NewKey creates a random key and returns it with its recovery string.
func NewKey() (Key, string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return Key{}, "", fmt.Errorf("generate encryption key: %w", err)
	}

	key, err := keyFrom(secret)
	if err != nil {
		return Key{}, "", err
	}

	return key, recoveryPrefix + strings.ToLower(recoveryEncoding.EncodeToString(secret)), nil
}

// ParseKey reads a recovery string printed by `istok sync key`.
func ParseKey(recovery string) (Key, error) {
	value := strings.TrimSpace(recovery)
	if !strings.HasPrefix(value, recoveryPrefix) {
		return Key{}, fmt.Errorf("not an Istok sync key: it must start with %q", recoveryPrefix)
	}

	secret, err := recoveryEncoding.DecodeString(strings.ToUpper(strings.TrimPrefix(value, recoveryPrefix)))
	if err != nil || len(secret) != 32 {
		return Key{}, errors.New("the sync key is damaged")
	}

	return keyFrom(secret)
}

func keyFrom(secret []byte) (Key, error) {
	seal, err := hkdf.Key(sha256.New, secret, nil, "istok sync seal v1", chacha20poly1305.KeySize)
	if err != nil {
		return Key{}, err
	}
	mac, err := hkdf.Key(sha256.New, secret, nil, "istok sync tag v1", 32)
	if err != nil {
		return Key{}, err
	}

	return Key{seal: seal, mac: mac}, nil
}

// Check proves to the hub that a device holds the same key without revealing it.
func (k Key) Check() string {
	return k.tag([]byte("istok key check"))
}

// Tag is a keyed hash of row content: equal content gives equal tags, and the
// hub learns nothing else.
func (k Key) Tag(table, key string, content []byte) string {
	return k.tag([]byte(table), []byte{0}, []byte(key), []byte{0}, content)
}

func (k Key) tag(parts ...[]byte) string {
	mac := hmac.New(sha256.New, k.mac)
	for _, part := range parts {
		mac.Write(part)
	}

	return hex.EncodeToString(mac.Sum(nil))
}

// Seal encrypts plaintext bound to its context, so the hub cannot move a body
// to another row.
func (k Key) Seal(plaintext []byte, context string) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(k.seal)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(plaintext)+aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	return aead.Seal(nonce, nonce, plaintext, []byte(context)), nil
}

// Open decrypts what Seal produced for the same context.
func (k Key) Open(ciphertext []byte, context string) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(k.seal)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("encrypted body is too short")
	}

	nonce, sealed := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, sealed, []byte(context))
	if err != nil {
		return nil, errors.New("cannot decrypt a hub row: the sync key differs or the row was altered")
	}

	return plaintext, nil
}

// rowContext binds a row body to its place and version.
func rowContext(projectID, table, key, kind string, revision int64) string {
	return fmt.Sprintf("row\x00%s\x00%s\x00%s\x00%s\x00%d", projectID, table, key, kind, revision)
}

func projectContext(projectID string) string {
	return "project\x00" + projectID
}
