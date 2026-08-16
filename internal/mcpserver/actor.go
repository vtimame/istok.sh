package mcpserver

import (
	"strings"

	contextmodel "s26.dev/istok-cli/internal/context"
	"s26.dev/istok-cli/internal/run"
	"s26.dev/istok-cli/internal/task"
)

type actorIdentity struct {
	ID   string
	Name string
}

func newActor(id, name string) (actorIdentity, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		generated, err := run.NewID()
		if err != nil {
			return actorIdentity{}, err
		}

		id = generated
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = "MCP Agent"
	}

	return actorIdentity{ID: id, Name: name}, nil
}

func (actor actorIdentity) task() task.ActorSnapshot {
	return task.ActorSnapshot{ID: actor.ID, Kind: "agent", Name: actor.Name}
}

func (actor actorIdentity) context() contextmodel.ActorSnapshot {
	return contextmodel.ActorSnapshot{ID: actor.ID, Kind: "agent", Name: actor.Name}
}

func (actor actorIdentity) run() run.ActorSnapshot {
	return run.ActorSnapshot{ID: actor.ID, Kind: "agent", Name: actor.Name}
}
