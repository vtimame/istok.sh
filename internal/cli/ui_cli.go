package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	"github.com/vtimame/istok.sh/internal/uiservice"
	"github.com/vtimame/istok.sh/internal/webui"
)

func runUICommand(ctx context.Context, command UICommand, commandName string, output io.Writer) error {
	switch commandName {
	case "ui service install":
		return runUIServiceInstall(ctx, command.Service.Install, output)
	case "ui service uninstall":
		if err := uiservice.Uninstall(ctx); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "Removed %s\n", uiservice.Name())
		return err
	case "ui service start", "ui service stop", "ui service restart":
		return uiservice.Control(ctx, commandName[len("ui service "):])
	case "ui service status":
		return uiservice.Status(ctx, output)
	default:
		return runUI(ctx, command.Serve, output)
	}
}

func runUIServiceInstall(ctx context.Context, command UIServiceInstallCommand, output io.Writer) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate istok executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve istok executable: %w", err)
	}

	// The service does not inherit this shell's environment, so a database
	// chosen here is recorded in the unit as an absolute path.
	database := command.Database
	if database != "" {
		if database, err = filepath.Abs(database); err != nil {
			return fmt.Errorf("resolve database path: %w", err)
		}
	}

	config := uiservice.Config{Executable: executable, Port: command.Port, Database: database}
	changed, err := uiservice.Install(ctx, config)
	if err != nil {
		return err
	}

	state := "unchanged"
	if changed {
		state = "written"
	}
	_, err = fmt.Fprintf(output,
		"Service %s (%s)\nRunning %s\nIstok UI: http://127.0.0.1:%d/\n"+
			"`istok update` restarts it on the new version. If you move istok elsewhere, rerun `istok ui service install`.\n",
		uiservice.Path(), state, executable, command.Port)
	if err != nil {
		return err
	}

	for _, note := range uiservice.Notes() {
		if _, err = fmt.Fprintln(output, note); err != nil {
			return err
		}
	}

	return nil
}

func runUI(ctx context.Context, command UIServeCommand, output io.Writer) error {
	var services webui.Services
	app := bootstrap.UIApp(command.Database, &services)

	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start web UI application: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = app.Stop(stopCtx)
	}()

	server, err := webui.Listen(command.Port, services)
	if err != nil {
		return err
	}

	fmt.Fprintf(output, "Istok UI: %s\nPress Ctrl+C to stop.\n", server.URL())
	if !command.NoOpen {
		if err := webui.OpenBrowser(server.URL()); err != nil {
			fmt.Fprintf(output, "Could not open a browser (%v); open the URL above manually.\n", err)
		}
	}

	return server.Serve(ctx)
}
