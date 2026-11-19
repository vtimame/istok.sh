package replica

import (
	"context"
	"errors"
	"fmt"

	"github.com/vtimame/istok.sh/internal/hub/client"
	"github.com/vtimame/istok.sh/internal/hub/identity"
)

// JoinResult reports a join. Recovery is set when this device created the
// key: it has to be stored somewhere safe and entered on other devices.
type JoinResult struct {
	HubAddress     string `json:"hub_address"`
	HubFingerprint string `json:"hub_fingerprint"`
	DeviceID       string `json:"device_id"`
	DeviceName     string `json:"device_name"`
	CreatedKey     bool   `json:"created_key"`
	Recovery       string `json:"recovery,omitempty"`

	// SwitchedHub is true when the device left another hub; its sync state
	// was reset.
	SwitchedHub bool `json:"switched_hub"`
}

// Join registers this device on the hub of the invitation and sets up the
// encryption key: the first device creates it, later ones pass recovery.
// A join interrupted after registration can be repeated with the same code.
func Join(ctx context.Context, dir, code, recovery string, reset func(context.Context) error) (JoinResult, error) {
	invitation, err := identity.ParseInvitation(code)
	if err != nil {
		return JoinResult{}, err
	}

	device, err := DeviceIdentity(dir)
	if err != nil {
		return JoinResult{}, err
	}

	previous, err := LoadConfig(dir)
	if err != nil && !errors.Is(err, ErrNotJoined) {
		return JoinResult{}, err
	}
	sameHub := err == nil && previous.HubFingerprint == invitation.Fingerprint && previous.DeviceID == device.Fingerprint

	hub := client.New(invitation.Address, invitation.Fingerprint, device)
	defer hub.Close()

	config := Config{
		HubAddress:     invitation.Address,
		HubFingerprint: invitation.Fingerprint,
		DeviceID:       device.Fingerprint,
		DeviceName:     invitation.Name,
	}
	result := JoinResult{HubAddress: invitation.Address, HubFingerprint: invitation.Fingerprint, DeviceID: device.Fingerprint}

	if sameHub {
		config.DeviceName = previous.DeviceName
		config.Key = previous.Key
	} else {
		enrolled, err := hub.Enroll(ctx, invitation.Secret)
		if err != nil {
			return JoinResult{}, fmt.Errorf("join the hub: %w", err)
		}
		config.DeviceName = enrolled.Name

		if err := reset(ctx); err != nil {
			return JoinResult{}, err
		}
		result.SwitchedHub = previous.HubFingerprint != ""

		// Save the membership now: a missing key can be added by repeating
		// the join without a new invitation.
		if err := SaveConfig(dir, config); err != nil {
			return JoinResult{}, err
		}
	}
	result.DeviceName = config.DeviceName

	space, err := hub.Space(ctx)
	if err != nil {
		return JoinResult{}, err
	}

	switch {
	case space.KeyCheck == "":
		// The hub has no key yet: publish the one given, the one this
		// device already holds, or a new one.
		value := recovery
		if value == "" {
			value = config.Key
		}

		var key Key
		if value == "" {
			key, value, err = NewKey()
			result.CreatedKey, result.Recovery = true, value
		} else {
			key, err = ParseKey(value)
		}
		if err != nil {
			return JoinResult{}, err
		}

		if err := hub.SetSpace(ctx, key.Check()); err != nil {
			return JoinResult{}, err
		}
		config.Key = value
	case recovery != "":
		key, err := ParseKey(recovery)
		if err != nil {
			return JoinResult{}, err
		}
		if key.Check() != space.KeyCheck {
			return JoinResult{}, errors.New("this sync key does not belong to the hub: copy it again with `istok sync key` on a device that syncs")
		}
		config.Key = recovery
	case config.Key != "":
		key, err := ParseKey(config.Key)
		if err != nil || key.Check() != space.KeyCheck {
			return JoinResult{}, errors.New("the saved sync key does not belong to the hub: pass --key")
		}
	default:
		return JoinResult{}, ErrNoKey
	}

	if err := SaveConfig(dir, config); err != nil {
		return JoinResult{}, err
	}

	return result, nil
}

// Connect opens the hub client and key of a joined device.
func Connect(dir string) (*client.Client, Key, Config, error) {
	config, err := LoadConfig(dir)
	if err != nil {
		return nil, Key{}, Config{}, err
	}
	if config.Key == "" {
		return nil, Key{}, Config{}, ErrNoKey
	}

	key, err := ParseKey(config.Key)
	if err != nil {
		return nil, Key{}, Config{}, err
	}

	device, err := DeviceIdentity(dir)
	if err != nil {
		return nil, Key{}, Config{}, err
	}

	return client.New(config.HubAddress, config.HubFingerprint, device), key, config, nil
}
