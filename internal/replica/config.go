package replica

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"

	"github.com/vtimame/istok.sh/internal/hub/identity"
)

// ErrNotJoined means this device has not joined a hub yet.
var ErrNotJoined = errors.New("this device has not joined a hub: run `istok sync join CODE` with a code from `istok hub device add`")

// Config is this device's hub membership. It holds the encryption key, so the
// file is private to the user.
type Config struct {
	HubAddress     string `json:"hub_address"`
	HubFingerprint string `json:"hub_fingerprint"`
	DeviceID       string `json:"device_id"`
	DeviceName     string `json:"device_name"`
	Key            string `json:"key"`
}

// DefaultDir holds the device key and the hub membership.
func DefaultDir() string {
	return filepath.Join(xdg.DataHome, "istok", "sync")
}

func configPath(dir string) string { return filepath.Join(dir, "hub.json") }

// LoadConfig reads the membership; ErrNotJoined when there is none.
func LoadConfig(dir string) (Config, error) {
	data, err := os.ReadFile(configPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, ErrNotJoined
	}
	if err != nil {
		return Config{}, fmt.Errorf("read sync config: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("read sync config: %w", err)
	}

	return config, nil
}

// SaveConfig writes the membership through a temporary file.
func SaveConfig(dir string, config Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create sync directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	temporary := configPath(dir) + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("write sync config: %w", err)
	}

	return os.Rename(temporary, configPath(dir))
}

// DeviceIdentity loads or creates the key this device presents to hubs.
func DeviceIdentity(dir string) (identity.Identity, error) {
	return identity.LoadOrCreate(dir, "device")
}
