package cli

// UICommand serves the web UI by default, so `istok ui --port N` keeps working,
// and groups the background service helpers under `istok ui service`.
type UICommand struct {
	Serve   UIServeCommand   `cmd:"" default:"withargs" help:"Serve the web UI in the foreground (default)."`
	Service UIServiceCommand `cmd:"" help:"Run the web UI as a background service (systemd on Linux, launchd on macOS)."`
}

// UIServeCommand defaults to a fixed port so the address stays stable when the
// UI runs as a long-lived service.
type UIServeCommand struct {
	Port     int    `name:"port" default:"7700" help:"Loopback port; 0 picks a free port." env:"ISTOK_UI_PORT"`
	NoOpen   bool   `name:"no-open" help:"Print the URL without opening a browser."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
}

type UIServiceCommand struct {
	Install   UIServiceInstallCommand `cmd:"" help:"Create or update the service, enable it and (re)start it."`
	Uninstall struct{}                `cmd:"" help:"Stop and disable the service and remove its file."`
	Start     struct{}                `cmd:"" help:"Start the service."`
	Stop      struct{}                `cmd:"" help:"Stop the service."`
	Restart   struct{}                `cmd:"" help:"Restart the service."`
	Status    struct{}                `cmd:"" help:"Show the service status."`
}

type UIServiceInstallCommand struct {
	Port     int    `name:"port" default:"7700" help:"Loopback port the service listens on." env:"ISTOK_UI_PORT"`
	Database string `name:"database" help:"Path to the SQLite database; recorded in the service file." env:"ISTOK_DATABASE"`
}
