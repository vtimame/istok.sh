package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vtimame/istok.sh/internal/cli/presentation"
	"github.com/vtimame/istok.sh/internal/hub/server"
)

// HubCommand runs and administers a sync hub.
type HubCommand struct {
	Serve  HubServeCommand  `cmd:"" help:"Run the hub. Devices connect over mutual TLS; no domain or proxy is needed."`
	Device HubDeviceCommand `cmd:"" help:"Invite, list and revoke devices."`
	Backup HubBackupCommand `cmd:"" help:"Write a consistent copy of the hub database."`
}

type HubServeCommand struct {
	Listen    string `name:"listen" default:":7443" env:"ISTOK_HUB_LISTEN" help:"Address to listen on."`
	Advertise string `name:"advertise" env:"ISTOK_HUB_ADVERTISE" help:"HOST:PORT devices use to reach the hub; put into invitations."`
	Data      string `name:"data" env:"ISTOK_HUB_DATA" help:"Hub data directory. Defaults to ~/.local/share/istok-hub."`
}

type HubDeviceCommand struct {
	Add    HubDeviceAddCommand    `cmd:"" help:"Print a one-time invitation for a new device."`
	List   HubDeviceListCommand   `cmd:"" help:"List devices."`
	Revoke HubDeviceRevokeCommand `cmd:"" help:"Disconnect a device for good."`
}

type HubDeviceAddCommand struct {
	Name    string        `arg:"" help:"Device name, for example macbook."`
	Address string        `name:"address" help:"HOST:PORT for the invitation; defaults to the --advertise address of hub serve."`
	Expires time.Duration `name:"expires" default:"15m" help:"How long the invitation stays valid."`
	Data    string        `name:"data" env:"ISTOK_HUB_DATA" help:"Hub data directory."`
	JSON    bool          `name:"json" help:"Write a versioned JSON response."`
}

type HubDeviceListCommand struct {
	Data string `name:"data" env:"ISTOK_HUB_DATA" help:"Hub data directory."`
	JSON bool   `name:"json" help:"Write a versioned JSON response."`
}

type HubDeviceRevokeCommand struct {
	Device string `arg:"" help:"Device name or ID prefix."`
	Data   string `name:"data" env:"ISTOK_HUB_DATA" help:"Hub data directory."`
	JSON   bool   `name:"json" help:"Write a versioned JSON response."`
}

type HubBackupCommand struct {
	File string `arg:"" help:"File to write; must not exist."`
	Data string `name:"data" env:"ISTOK_HUB_DATA" help:"Hub data directory."`
}

func runHub(ctx context.Context, command HubCommand, commandName, cwd string, output, errorOutput io.Writer) error {
	switch {
	case strings.HasPrefix(commandName, "hub serve"):
		return runHubServe(ctx, command.Serve, errorOutput)
	case strings.HasPrefix(commandName, "hub device add"):
		return withHub(ctx, command.Device.Add.Data, command.Device.Add.JSON, output, func(hub *server.Hub) (any, error) {
			invitation, err := hub.Invite(ctx, command.Device.Add.Name, command.Device.Add.Address, command.Device.Add.Expires)
			if err != nil {
				return nil, err
			}

			return hubInvitation{Name: invitation.Name, Address: invitation.Address, Code: invitation.Encode(),
				Expires: time.Now().Add(command.Device.Add.Expires).UTC()}, nil
		})
	case strings.HasPrefix(commandName, "hub device list"):
		return withHub(ctx, command.Device.List.Data, command.Device.List.JSON, output, func(hub *server.Hub) (any, error) {
			devices, err := hub.Store.Devices(ctx)
			return hubDevices(devices), err
		})
	case strings.HasPrefix(commandName, "hub device revoke"):
		return withHub(ctx, command.Device.Revoke.Data, command.Device.Revoke.JSON, output, func(hub *server.Hub) (any, error) {
			device, err := hub.Store.Revoke(ctx, command.Device.Revoke.Device)
			return hubDevices{device}, err
		})
	case strings.HasPrefix(commandName, "hub backup"):
		return withHub(ctx, command.Backup.Data, false, output, func(hub *server.Hub) (any, error) {
			path := resolvePath(cwd, command.Backup.File)
			if err := hub.Store.Backup(ctx, path); err != nil {
				return nil, err
			}
			return "Hub database written to " + path, nil
		})
	default:
		return fmt.Errorf("unsupported command %q", commandName)
	}
}

func runHubServe(ctx context.Context, command HubServeCommand, errorOutput io.Writer) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub, err := server.Open(ctx, command.Data)
	if err != nil {
		return err
	}
	defer hub.Close()

	logger := slog.New(slog.NewTextHandler(errorOutput, nil))

	return hub.Serve(ctx, server.ServeConfig{Listen: command.Listen, Advertise: command.Advertise, Logger: logger})
}

func withHub(ctx context.Context, data string, jsonOutput bool, output io.Writer, action func(*server.Hub) (any, error)) error {
	hub, err := server.Open(ctx, data)
	if err != nil {
		return err
	}
	defer hub.Close()

	value, err := action(hub)

	return printResult(jsonOutput, output, value, err)
}

// printResult writes a command result as text or as a versioned JSON document.
func printResult(jsonOutput bool, output io.Writer, value any, err error) error {
	if err != nil {
		if jsonOutput {
			return jsonError(err)
		}
		return err
	}

	if jsonOutput {
		return json.NewEncoder(output).Encode(struct {
			SchemaVersion string `json:"schema_version"`
			Result        any    `json:"result"`
		}{"1", value})
	}

	_, err = fmt.Fprintln(output, value)

	return err
}

type hubInvitation struct {
	Name    string    `json:"name"`
	Address string    `json:"address"`
	Code    string    `json:"code"`
	Expires time.Time `json:"expires_at"`
}

func (i hubInvitation) String() string {
	var output strings.Builder
	output.WriteString(presentation.SectionTitle("Invitation for " + i.Name))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(presentation.Key("Hub") + " " + i.Address))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(presentation.Key("Valid until") + " " + i.Expires.Local().Format("15:04")))
	output.WriteString("\n\n")
	output.WriteString(i.Code)
	output.WriteString("\n\n")
	output.WriteString(presentation.Metadata("On the device run: istok sync join " + "<code>" + ". The code works once."))

	return output.String()
}

type hubDevices []server.Device

func (d hubDevices) String() string {
	if len(d) == 0 {
		return presentation.Metadata("No devices yet. Invite one: istok hub device add NAME")
	}

	rows := make([][]string, 0, len(d))
	for _, device := range d {
		state, seen := "active", "never"
		if device.RevokedAt != nil {
			state = "revoked"
		}
		if device.LastSeenAt != nil {
			seen = device.LastSeenAt.Local().Format("2006-01-02 15:04")
		}
		rows = append(rows, []string{device.Name, device.ID[:16], state, seen})
	}

	return presentation.RenderTable([]string{"NAME", "ID", "STATE", "LAST SEEN"}, rows)
}
