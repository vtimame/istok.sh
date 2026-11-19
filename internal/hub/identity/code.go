package identity

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const codePrefix = "istok-hub:"

// Invitation is what a device needs to join a hub: where it is, which key it
// holds, and a one-time secret that lets the device register its own key.
type Invitation struct {
	Address     string `json:"a"`
	Fingerprint string `json:"f"`
	Secret      string `json:"s"`
	Name        string `json:"n"`
}

// NewSecret returns 32 random bytes, base64url encoded.
func NewSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(value), nil
}

// Encode turns an invitation into one copyable line.
func (i Invitation) Encode() string {
	data, _ := json.Marshal(i)

	return codePrefix + base64.RawURLEncoding.EncodeToString(data)
}

// ParseInvitation reads a code printed by `istok hub device add`.
func ParseInvitation(code string) (Invitation, error) {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, codePrefix) {
		return Invitation{}, fmt.Errorf("not an Istok hub invitation: it must start with %q", codePrefix)
	}

	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, codePrefix))
	if err != nil {
		return Invitation{}, fmt.Errorf("invitation is damaged: %w", err)
	}

	var invitation Invitation
	if err := json.Unmarshal(data, &invitation); err != nil {
		return Invitation{}, fmt.Errorf("invitation is damaged: %w", err)
	}
	if invitation.Address == "" || invitation.Fingerprint == "" || invitation.Secret == "" {
		return Invitation{}, fmt.Errorf("invitation is incomplete")
	}

	return invitation, nil
}
