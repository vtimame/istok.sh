package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpAgent is the MCP client side of the session, started the way a coding
// agent starts Istok: from the repository directory with a stable identity.
type mcpAgent struct {
	session *mcp.ClientSession
	stderr  strings.Builder
}

type actor struct {
	id   string
	name string
}

var (
	claude = actor{id: "claude", name: "Claude Code"}
	codex  = actor{id: "codex", name: "Codex"}
)

func connect(ctx context.Context, binary string, demo paths, identity actor) (*mcpAgent, error) {
	agent := &mcpAgent{}

	command := exec.Command(binary, "mcp", "--actor-id", identity.id, "--actor-name", identity.name)
	command.Dir = demo.repository
	command.Stderr = &agent.stderr

	// A private Go build cache keeps `go test` from printing "(cached)".
	command.Env = append(os.Environ(),
		"HOME="+demo.home,
		"ISTOK_DATABASE="+demo.database,
		"ISTOK_INDEX_ROOT="+filepath.Join(demo.root, "indexes"),
		"GOCACHE="+filepath.Join(demo.root, "gocache"),
	)

	client := mcp.NewClient(&mcp.Implementation{Name: "demosession", Version: "1"}, nil)

	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		return nil, fmt.Errorf("start istok mcp: %w", err)
	}
	agent.session = session

	return agent, nil
}

// call invokes a tool and decodes its structured result into out.
func (a *mcpAgent) call(ctx context.Context, name string, arguments map[string]any, out any) error {
	result, err := a.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return fmt.Errorf("%s: encode result: %w", name, err)
	}
	if result.IsError {
		return fmt.Errorf("%s failed: %s %s", name, encoded, a.stderr.String())
	}

	if err := json.Unmarshal(encoded, out); err != nil {
		return fmt.Errorf("%s: decode result: %w", name, err)
	}

	return nil
}

func (a *mcpAgent) close() {
	_ = a.session.Close()
}
