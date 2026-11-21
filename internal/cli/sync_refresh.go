package cli

import (
	"context"
	"time"

	"github.com/vtimame/istok.sh/internal/replica"
)

// refreshBeforeRead syncs the project of cwd before a command shows its
// tasks, so the list includes what other devices did. A device that has not
// joined a hub opens nothing; an unreachable hub delays the command by at
// most a few seconds and is skipped for a while after.
func refreshBeforeRead(ctx context.Context, database, cwd string) {
	syncer := replica.NewSyncer(replica.Dir(), nil, nil, nil)
	if !syncer.Joined() {
		return
	}

	services, app := syncApplication(database)
	if err := app.Start(ctx); err != nil {
		return
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Stop(stopCtx)
	}()

	current, err := services.projects.Current(ctx, cwd)
	if err != nil {
		return
	}

	replica.NewSyncer(replica.Dir(), services.exchange, services.state, nil).Refresh(ctx, current.ID)
}
