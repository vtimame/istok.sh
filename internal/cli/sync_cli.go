package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/vtimame/istok.sh/internal/application/bootstrap"
	"github.com/vtimame/istok.sh/internal/cli/presentation"
	"github.com/vtimame/istok.sh/internal/gitremote"
	"github.com/vtimame/istok.sh/internal/project"
	"github.com/vtimame/istok.sh/internal/replica"
	"github.com/vtimame/istok.sh/internal/storage/exchangerepo"
	"github.com/vtimame/istok.sh/internal/storage/syncrepo"
)

// SyncCommand syncs projects with a hub.
type SyncCommand struct {
	Run      SyncRunCommand      `cmd:"" default:"withargs" help:"Pull changes from the hub and push local ones (the default)."`
	Join     SyncJoinCommand     `cmd:"" help:"Join a hub with an invitation from istok hub device add."`
	Key      SyncKeyCommand      `cmd:"" help:"Print the sync key other devices join with."`
	Enable   SyncEnableCommand   `cmd:"" help:"Sync the current project, or fetch a hub project into the current folder."`
	Disable  SyncDisableCommand  `cmd:"" help:"Stop syncing the current project on this device."`
	Projects SyncProjectsCommand `cmd:"" help:"List the hub's projects."`
	Status   SyncStatusCommand   `cmd:"" help:"Show the hub and the projects this device syncs."`
}

// syncFlags are shared by the sync commands.
type syncFlags struct {
	Dir      string `name:"sync-dir" env:"ISTOK_SYNC_DIR" help:"Directory with this device's key and hub membership. Defaults to ~/.local/share/istok/sync."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
	JSON     bool   `name:"json" help:"Write a versioned JSON response."`
}

func (f syncFlags) dir() string {
	if f.Dir == "" {
		return replica.DefaultDir()
	}

	return f.Dir
}

type SyncRunCommand struct {
	syncFlags
	Project string `name:"project" short:"p" help:"Sync only this project: ID, unambiguous ID prefix or name."`
}

type SyncJoinCommand struct {
	syncFlags
	Code string `arg:"" help:"Invitation code."`
	Key  string `name:"key" help:"Sync key from istok sync key on a device that already syncs; omit on the first device."`
}

type SyncKeyCommand struct {
	syncFlags
}

type SyncEnableCommand struct {
	syncFlags
	Project string `name:"project" short:"p" help:"Hub project ID to fetch into the current folder (see istok sync projects). Without it, the hub project of this folder's git repository is fetched."`
	New     bool   `name:"new" help:"Publish the current project even though the hub has another project of the same repository."`
}

type SyncDisableCommand struct {
	syncFlags
}

type SyncProjectsCommand struct {
	syncFlags
}

type SyncStatusCommand struct {
	syncFlags
}

// syncServices are the parts of the application sync commands use.
type syncServices struct {
	projects *project.Service
	exchange *exchangerepo.Repository
	state    *syncrepo.Repository
}

func syncApplication(database string) (*syncServices, *fx.App) {
	services := &syncServices{}
	app := fx.New(
		fx.NopLogger,
		bootstrap.SyncOptions(database),
		fx.Invoke(func(projects *project.Service, exchange *exchangerepo.Repository, state *syncrepo.Repository) {
			services.projects, services.exchange, services.state = projects, exchange, state
		}),
	)

	return services, app
}

// withEngine connects to the joined hub and runs action against it.
func withEngine(ctx context.Context, flags syncFlags, output io.Writer, action func(*syncServices, *replica.Engine) (any, error)) error {
	services, app := syncApplication(flags.Database)

	return runProjectApplication(ctx, flags.JSON, output, app, func() (any, error) {
		// The same lock as the UI service, MCP servers and hooks: one sync
		// of this device's database at a time.
		var value any
		syncer := replica.NewSyncer(flags.dir(), services.exchange, services.state, nil)
		err := syncer.WithEngine(ctx, func(engine *replica.Engine) error {
			var err error
			value, err = action(services, engine)
			return err
		})

		return value, err
	})
}

func runSync(ctx context.Context, command SyncCommand, commandName, cwd string, output io.Writer) error {
	switch {
	case strings.HasPrefix(commandName, "sync join"):
		return runSyncJoin(ctx, command.Join, output)
	case strings.HasPrefix(commandName, "sync key"):
		config, err := replica.LoadConfig(command.Key.dir())
		if err == nil && config.Key == "" {
			err = replica.ErrNoKey
		}

		return printResult(command.Key.JSON, output, syncKey{Key: config.Key}, err)
	case strings.HasPrefix(commandName, "sync enable"):
		return withEngine(ctx, command.Enable.syncFlags, output, func(services *syncServices, engine *replica.Engine) (any, error) {
			return enableSync(ctx, services, engine, cwd, command.Enable.Project, command.Enable.New)
		})
	case strings.HasPrefix(commandName, "sync disable"):
		services, app := syncApplication(command.Disable.Database)

		return runProjectApplication(ctx, command.Disable.JSON, output, app, func() (any, error) {
			current, err := services.projects.Current(ctx, cwd)
			if err != nil {
				return nil, err
			}
			removed, err := services.state.Disable(ctx, current.ID)
			if err != nil {
				return nil, err
			}
			if !removed {
				return nil, fmt.Errorf("project %s is not synced on this device", current.Name)
			}
			return fmt.Sprintf("Stopped syncing %s on this device. Its data stays on the hub and here.", current.Name), nil
		})
	case strings.HasPrefix(commandName, "sync projects"):
		return withEngine(ctx, command.Projects.syncFlags, output, func(services *syncServices, engine *replica.Engine) (any, error) {
			remote, err := engine.RemoteProjects(ctx)
			if err != nil {
				return nil, err
			}
			return syncRemoteProjects(remote), nil
		})
	case strings.HasPrefix(commandName, "sync status"):
		return runSyncStatus(ctx, command.Status, output)
	case strings.HasPrefix(commandName, "sync"):
		return withEngine(ctx, command.Run.syncFlags, output, func(services *syncServices, engine *replica.Engine) (any, error) {
			return syncProjects(ctx, services, engine, command.Run.Project)
		})
	default:
		return fmt.Errorf("unsupported command %q", commandName)
	}
}

func runSyncJoin(ctx context.Context, command SyncJoinCommand, output io.Writer) error {
	services, app := syncApplication(command.Database)

	return runProjectApplication(ctx, command.JSON, output, app, func() (any, error) {
		unlock, err := replica.Lock(ctx, command.dir())
		if err != nil {
			return nil, err
		}
		defer unlock()

		result, err := replica.Join(ctx, command.dir(), command.Code, command.Key, services.state.Reset)
		if err != nil {
			return nil, err
		}

		return syncJoined(result), nil
	})
}

func enableSync(ctx context.Context, services *syncServices, engine *replica.Engine, cwd, hubProject string, publishNew bool) (any, error) {
	current, currentErr := services.projects.Current(ctx, cwd)
	if currentErr != nil && project.ErrorCode(currentErr) != project.CodeNotFound {
		return nil, currentErr
	}

	switch {
	case currentErr == nil && (hubProject == "" || hubProject == current.ID):
		return publishProject(ctx, services, engine, current, hubProject == "" && !publishNew)
	case currentErr == nil:
		return nil, fmt.Errorf("this folder already belongs to project %s (%s); run the command in the folder of the hub project", current.Name, current.ID)
	case hubProject == "":
		found, err := hubProjectOf(ctx, engine, cwd)
		if err != nil {
			return nil, err
		}
		hubProject = found
	}

	result, err := engine.Clone(ctx, hubProject)
	if err != nil {
		return nil, err
	}

	bound, err := services.projects.Rebind(ctx, hubProject, cwd, nil)
	if err != nil {
		return nil, fmt.Errorf("fetched the project but could not bind this folder: %w", err)
	}

	return syncResults{{Name: bound.Name, Result: result}}, nil
}

// publishProject starts syncing a local project. Publishing it for the first
// time is refused when checkDuplicate is set and the hub already has another
// project of the same repository: the folder most likely belongs to that one.
func publishProject(ctx context.Context, services *syncServices, engine *replica.Engine, current project.Project, checkDuplicate bool) (any, error) {
	var repository gitremote.Identity
	if current.Root != nil {
		repository = gitremote.Of(ctx, current.Root.CanonicalPath)
	}

	synced, err := services.state.Synced(ctx, current.ID)
	if err != nil {
		return nil, err
	}

	if checkDuplicate && !synced && repository.Known() {
		remote, err := engine.RemoteProjects(ctx)
		if err != nil {
			return nil, err
		}

		for _, value := range remote {
			if value.ID != current.ID && value.Repository.Matches(repository) {
				return nil, fmt.Errorf("the hub already has project %s (%s) of this repository, and this folder belongs to the local project %s. "+
					"To work on the hub project here, run istok project delete %s and istok sync enable again; to publish %s as a separate project, pass --new",
					value.Name, value.ID, current.Name, current.ID, current.Name)
			}
		}
	}

	if err := engine.Register(ctx, current.ID, current.Name, repository); err != nil {
		return nil, err
	}

	result, err := engine.Sync(ctx, current.ID)

	return syncResults{{Name: current.Name, Result: result}}, err
}

// hubProjectOf finds the hub project of a folder that is not an Istok project
// yet by its git repository.
func hubProjectOf(ctx context.Context, engine *replica.Engine, cwd string) (string, error) {
	repository := gitremote.Of(ctx, cwd)
	if !repository.Known() {
		return "", errors.New("this folder is not an Istok project: run istok init to sync a new one, or pass --project with an ID from istok sync projects")
	}

	remote, err := engine.RemoteProjects(ctx)
	if err != nil {
		return "", err
	}

	var matches []replica.RemoteProject
	for _, value := range remote {
		if value.Repository.Matches(repository) {
			matches = append(matches, value)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("the hub has no project of repository %s: run istok init to sync a new one, or pass --project with an ID from istok sync projects", repository.Remotes[0])
	case 1:
		return matches[0].ID, nil
	}

	names := make([]string, 0, len(matches))
	for _, value := range matches {
		names = append(names, fmt.Sprintf("%s (%s)", value.Name, value.ID))
	}

	return "", fmt.Errorf("the hub has several projects of this repository: %s; pass --project with one of the IDs", strings.Join(names, ", "))
}

func syncProjects(ctx context.Context, services *syncServices, engine *replica.Engine, selector string) (any, error) {
	enabled, err := services.state.Projects(ctx)
	if err != nil {
		return nil, err
	}

	if selector != "" {
		chosen, err := services.projects.Resolve(ctx, selector, false)
		if err != nil {
			return nil, err
		}

		filtered := enabled[:0]
		for _, value := range enabled {
			if value.ID == chosen.ID {
				filtered = append(filtered, value)
			}
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("project %s is not synced on this device: run istok sync enable in its folder", chosen.Name)
		}
		enabled = filtered
	}

	if len(enabled) == 0 {
		return nil, errors.New("no project is synced on this device: run istok sync enable in a project folder")
	}

	results := syncResults{}
	var failures []error
	for _, value := range enabled {
		name := value.ID
		if local, err := services.projects.Resolve(ctx, value.ID, false); err == nil {
			name = local.Name
		}

		result, err := engine.Sync(ctx, value.ID)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", name, err))
			continue
		}
		results = append(results, syncResult{Name: name, Result: result})
	}

	return results, errors.Join(failures...)
}

func runSyncStatus(ctx context.Context, command SyncStatusCommand, output io.Writer) error {
	services, app := syncApplication(command.Database)

	return runProjectApplication(ctx, command.JSON, output, app, func() (any, error) {
		config, err := replica.LoadConfig(command.dir())
		if err != nil {
			return nil, err
		}

		enabled, err := services.state.Projects(ctx)
		if err != nil {
			return nil, err
		}

		status := syncStatus{Hub: config.HubAddress, Fingerprint: config.HubFingerprint, Device: config.DeviceName,
			DeviceID: config.DeviceID, HasKey: config.Key != "", Projects: []syncStatusProject{}}
		for _, value := range enabled {
			entry := syncStatusProject{ID: value.ID, Name: value.ID, Cursor: value.Cursor, SyncedAt: value.SyncedAt}
			if local, err := services.projects.Resolve(ctx, value.ID, false); err == nil {
				entry.Name = local.Name
			}
			status.Projects = append(status.Projects, entry)
		}

		return status, nil
	})
}

type syncJoined replica.JoinResult

func (j syncJoined) String() string {
	var output strings.Builder
	output.WriteString(presentation.SectionTitle("Joined hub"))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(presentation.Key("Hub") + " " + j.HubAddress))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(presentation.Key("Device") + " " + j.DeviceName))
	output.WriteString("\n")

	if j.SwitchedHub {
		output.WriteString(presentation.Metadata("This device left its previous hub; enable sync for each project again."))
		output.WriteString("\n")
	}

	if j.CreatedKey {
		output.WriteString("\n")
		output.WriteString(presentation.SectionTitle("Sync key"))
		output.WriteString("\n")
		output.WriteString(j.Recovery)
		output.WriteString("\n\n")
		output.WriteString(presentation.Metadata("Rows are encrypted with this key; the hub cannot read them. Keep it safe: " +
			"other devices join with --key, and without it the hub's data cannot be read. istok sync key prints it again."))
		output.WriteString("\n")
	}

	output.WriteString("\n")
	output.WriteString(presentation.Metadata("Next: run istok sync enable in a project folder."))

	return output.String()
}

type syncKey struct {
	Key string `json:"key"`
}

func (k syncKey) String() string {
	return k.Key + "\n\n" + presentation.Metadata("Join another device with: istok sync join CODE --key "+k.Key)
}

type syncResult struct {
	Name string `json:"name"`
	replica.Result
}

type syncResults []syncResult

func (r syncResults) String() string {
	var output strings.Builder
	output.WriteString(presentation.SectionTitle("Sync"))

	for _, value := range r {
		output.WriteString("\n")

		parts := []string{fmt.Sprintf("pulled %d", value.Pulled), fmt.Sprintf("pushed %d", value.Pushed)}
		if value.Stale > 0 {
			parts = append(parts, fmt.Sprintf("%d newer on the hub", value.Stale))
		}
		if value.Conflicts > 0 {
			parts = append(parts, fmt.Sprintf("%d conflicts kept on the hub", value.Conflicts))
		}
		output.WriteString(presentation.RailLine(value.Name + "  " + presentation.Metadata(strings.Join(parts, " · "))))

		for _, renumbered := range value.Renumbered {
			output.WriteString("\n")
			output.WriteString(presentation.RailLine(presentation.Metadata(fmt.Sprintf("renumbered #%d → #%d %s", renumbered.From, renumbered.To, renumbered.Title))))
		}
		for _, lost := range value.LostLeases {
			output.WriteString("\n")
			output.WriteString(presentation.RailLine(presentation.Metadata(fmt.Sprintf("run %s: device %s works on this task now", lost.RunID, lost.DeviceName))))
		}

		if value.Import == nil {
			continue
		}
		for _, conflict := range value.Import.Conflicts {
			output.WriteString("\n")
			output.WriteString(presentation.RailLine(presentation.Metadata(fmt.Sprintf("%s %s: %s", conflict.Table, conflict.ID, conflict.Reason))))
		}
		for _, renumbered := range value.Import.Renumbered {
			output.WriteString("\n")
			output.WriteString(presentation.RailLine(presentation.Metadata(fmt.Sprintf("renumbered #%d → #%d %s", renumbered.From, renumbered.To, renumbered.Title))))
		}
	}

	return output.String()
}

type syncRemoteProjects []replica.RemoteProject

func (p syncRemoteProjects) String() string {
	if len(p) == 0 {
		return presentation.Metadata("The hub has no projects yet. Run istok sync enable in a project folder.")
	}

	rows := make([][]string, 0, len(p))
	for _, value := range p {
		repository := strings.Join(value.Repository.Remotes, ", ")
		if value.Repository.Subdir != "" {
			repository += " · " + value.Repository.Subdir
		}
		rows = append(rows, []string{value.Name, repository, value.ID})
	}

	return presentation.RenderTable([]string{"NAME", "REPOSITORY", "ID"}, rows) + "\n" +
		presentation.Metadata("Fetch one into a clone of its repository with istok sync enable, or into any folder with --project ID")
}

type syncStatusProject struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Cursor   int64      `json:"cursor"`
	SyncedAt *time.Time `json:"synced_at,omitempty"`
}

type syncStatus struct {
	Hub         string              `json:"hub"`
	Fingerprint string              `json:"hub_fingerprint"`
	Device      string              `json:"device"`
	DeviceID    string              `json:"device_id"`
	HasKey      bool                `json:"has_key"`
	Projects    []syncStatusProject `json:"projects"`
}

func (s syncStatus) String() string {
	var output strings.Builder
	output.WriteString(presentation.SectionTitle("Sync"))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(presentation.Key("Hub") + " " + s.Hub))
	output.WriteString("\n")
	output.WriteString(presentation.RailLine(presentation.Key("Device") + " " + s.Device))
	output.WriteString("\n")
	if !s.HasKey {
		output.WriteString(presentation.RailLine(presentation.Metadata("No sync key yet: repeat istok sync join with --key")))
		output.WriteString("\n")
	}

	if len(s.Projects) == 0 {
		output.WriteString(presentation.Metadata("No project is synced on this device."))
		return output.String()
	}

	rows := make([][]string, 0, len(s.Projects))
	for _, value := range s.Projects {
		synced := "never"
		if value.SyncedAt != nil {
			synced = value.SyncedAt.Local().Format("2006-01-02 15:04")
		}
		rows = append(rows, []string{value.Name, synced})
	}
	output.WriteString("\n")
	output.WriteString(presentation.RenderTable([]string{"PROJECT", "LAST SYNC"}, rows))

	return output.String()
}
