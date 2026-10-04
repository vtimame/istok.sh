// Package uiservice manages `istok ui` as a systemd user service on Linux.
package uiservice

import (
	"strconv"
	"strings"
)

// UnitName is the systemd user unit that runs the web UI.
const UnitName = "istok-ui.service"

// Config describes how the service starts the UI.
type Config struct {
	Executable string
	Port       int
	Database   string
}

// Arguments returns the command line the unit runs.
func (c Config) Arguments() []string {
	args := []string{c.Executable, "ui", "--no-open", "--port", strconv.Itoa(c.Port)}
	if c.Database != "" {
		args = append(args, "--database", c.Database)
	}

	return args
}

// RenderUnit produces the unit file. It is regenerated on every install, so
// the header tells readers not to edit it by hand.
func RenderUnit(config Config) string {
	quoted := make([]string, 0, len(config.Arguments()))
	for _, argument := range config.Arguments() {
		quoted = append(quoted, quoteArgument(argument))
	}

	return strings.Join([]string{
		"# Managed by `istok ui service install`; manual edits are overwritten.",
		"[Unit]",
		"Description=Istok web UI",
		"",
		"[Service]",
		"Type=simple",
		"ExecStart=" + strings.Join(quoted, " "),
		"Restart=on-failure",
		"RestartSec=2",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n")
}

// quoteArgument quotes one ExecStart word for systemd: backslash and double
// quote are escaped inside quotes, and % and $ are doubled so systemd does not
// expand them as specifiers or environment variables.
func quoteArgument(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$")
	return `"` + replacer.Replace(value) + `"`
}
