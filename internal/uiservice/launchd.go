package uiservice

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/adrg/xdg"
)

// launchdLabel names the launchd agent that runs the web UI.
const launchdLabel = "sh.istok.ui"

// launchd manages the UI as a per-user agent in ~/Library/LaunchAgents,
// loaded into the user's GUI domain (gui/<uid>), so it needs no sudo and
// starts again at every login.
type launchd struct {
	uid int
}

func (launchd) name() string {
	return launchdLabel
}

func (launchd) path() string {
	return filepath.Join(xdg.Home, "Library", "LaunchAgents", launchdLabel+".plist")
}

func launchdLogPath() string {
	return filepath.Join(xdg.Home, "Library", "Logs", "istok", "ui.log")
}

func (launchd) notes() []string {
	return []string{"Logs: " + launchdLogPath()}
}

func (launchd) render(config Config) string {
	return RenderPlist(config, launchdLogPath())
}

func (launchd) runs(content []byte, executable string) bool {
	return bytes.Contains(content, []byte("<array>\n\t\t<string>"+escapeXML(executable)+"</string>"))
}

func (l launchd) domain() string {
	return "gui/" + strconv.Itoa(l.uid)
}

func (l launchd) target() string {
	return l.domain() + "/" + launchdLabel
}

// loaded reports whether the agent is loaded in the user's domain. With
// RunAtLoad and KeepAlive a loaded agent is running or about to restart.
func (l launchd) loaded(ctx context.Context) bool {
	return exec.CommandContext(ctx, "launchctl", "print", l.target()).Run() == nil
}

func (l launchd) apply(ctx context.Context, changed bool) error {
	// launchd creates the log file but not its directory.
	if err := os.MkdirAll(filepath.Dir(launchdLogPath()), 0o755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	if l.loaded(ctx) {
		if !changed {
			return launchctl(ctx, "kickstart", "-k", l.target())
		}

		// A loaded agent keeps its old definition until it is booted out.
		if err := launchctl(ctx, "bootout", l.target()); err != nil {
			return err
		}
	}

	return l.bootstrap(ctx)
}

// bootstrap loads the agent. It first clears a "disabled" override that
// `launchctl disable` may have left, and retries briefly because bootout
// finishes asynchronously and an immediate bootstrap can fail.
func (l launchd) bootstrap(ctx context.Context) error {
	if _, err := os.Stat(l.path()); err != nil {
		return fmt.Errorf("%s is not installed; run `istok ui service install`", launchdLabel)
	}

	if err := launchctl(ctx, "enable", l.target()); err != nil {
		return err
	}

	var err error
	for attempt := 0; attempt < 10; attempt++ {
		if err = launchctl(ctx, "bootstrap", l.domain(), l.path()); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}

	return err
}

func (l launchd) uninstall(ctx context.Context) error {
	if l.loaded(ctx) {
		if err := launchctl(ctx, "bootout", l.target()); err != nil {
			return err
		}
	}

	if err := os.Remove(l.path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove agent: %w", err)
	}

	return nil
}

// control maps the systemd-style actions onto launchd. Stop boots the agent
// out, because KeepAlive would restart a process that was only killed; the
// agent loads again on start or at the next login.
func (l launchd) control(ctx context.Context, action string) error {
	switch action {
	case "start":
		if l.loaded(ctx) {
			return launchctl(ctx, "kickstart", l.target())
		}
		return l.bootstrap(ctx)
	case "restart":
		if l.loaded(ctx) {
			return launchctl(ctx, "kickstart", "-k", l.target())
		}
		return l.bootstrap(ctx)
	case "stop":
		if !l.loaded(ctx) {
			return nil
		}
		return launchctl(ctx, "bootout", l.target())
	default:
		return fmt.Errorf("unsupported service action %q", action)
	}
}

func (l launchd) status(ctx context.Context, output io.Writer) error {
	if !l.loaded(ctx) {
		state := "not installed"
		if _, err := os.Stat(l.path()); err == nil {
			state = "installed at " + l.path()
		}

		_, err := fmt.Fprintf(output, "%s is not loaded (%s)\n", launchdLabel, state)
		return err
	}

	command := exec.CommandContext(ctx, "launchctl", "print", l.target())
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		return fmt.Errorf("launchctl print: %w", err)
	}

	return nil
}

func (l launchd) restartIfActive(ctx context.Context) (bool, error) {
	if !l.loaded(ctx) {
		return false, nil
	}
	if err := launchctl(ctx, "kickstart", "-k", l.target()); err != nil {
		return false, err
	}

	return true, nil
}

// launchctl runs `launchctl ...` and includes its output in errors.
func launchctl(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "launchctl", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(output))
	}

	return nil
}

// RenderPlist produces the agent's property list. It is regenerated on every
// install, so a comment tells readers not to edit it by hand. KeepAlive with
// SuccessfulExit=false restarts the UI after a crash but not after a clean
// exit, like Restart=on-failure in the systemd unit.
func RenderPlist(config Config, logPath string) string {
	var arguments strings.Builder
	for _, argument := range config.Arguments() {
		arguments.WriteString("\t\t<string>" + escapeXML(argument) + "</string>\n")
	}

	log := escapeXML(logPath)

	return strings.Join([]string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`,
		"<!-- Managed by `istok ui service install`; manual edits are overwritten. -->",
		`<plist version="1.0">`,
		"<dict>",
		"\t<key>Label</key>",
		"\t<string>" + launchdLabel + "</string>",
		"\t<key>ProgramArguments</key>",
		"\t<array>",
		strings.TrimSuffix(arguments.String(), "\n"),
		"\t</array>",
		"\t<key>RunAtLoad</key>",
		"\t<true/>",
		"\t<key>KeepAlive</key>",
		"\t<dict>",
		"\t\t<key>SuccessfulExit</key>",
		"\t\t<false/>",
		"\t</dict>",
		"\t<key>StandardOutPath</key>",
		"\t<string>" + log + "</string>",
		"\t<key>StandardErrorPath</key>",
		"\t<string>" + log + "</string>",
		"</dict>",
		"</plist>",
		"",
	}, "\n")
}

func escapeXML(value string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(value))

	return escaped.String()
}
