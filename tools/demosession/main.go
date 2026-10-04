// demosession records one agent session against a demo database for the
// website's terminal animation.
//
// It plays the agent's side by hand but talks to the real `istok mcp` server:
// it creates and claims a task, edits code in the demo repository, validates
// the change with a real `go test` through Istok and completes the task. Every
// number and line of output in the transcript comes from those calls; only the
// user's prompt and the agent's closing message are written here.
//
// Run it on a fresh root made by tools/demoseed:
//
//	go run ./tools/demoseed -root DIR
//	go run ./tools/demosession -root DIR -out session.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", "", "demo root created by tools/demoseed")
	out := flag.String("out", "", "file to write the session transcript to")
	binary := flag.String("istok", "istok", "istok binary that serves MCP")
	flag.Parse()

	if *root == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "demosession: -root and -out are required")
		os.Exit(2)
	}

	absolute, err := filepath.Abs(*root)
	check(err)

	demo := demoPaths(absolute)
	if _, err := os.Stat(demo.database); err != nil {
		check(fmt.Errorf("no demo database at %s; run tools/demoseed first: %w", demo.database, err))
	}

	ctx := context.Background()

	agent, err := connect(ctx, *binary, demo)
	check(err)
	defer agent.close()

	transcript, err := playSession(ctx, agent, demo)
	check(err)

	encoded, err := json.MarshalIndent(transcript, "", "  ")
	check(err)
	check(os.WriteFile(*out, append(encoded, '\n'), 0o644))

	fmt.Printf("Session written to %s (task #%d)\n", *out, transcript.TaskNumber)
}

type paths struct {
	root       string
	database   string
	artifacts  string
	home       string
	repository string
}

// demoPaths mirrors the layout tools/demoseed creates under its root.
func demoPaths(root string) paths {
	home := filepath.Join(root, "home")

	return paths{
		root:       root,
		database:   filepath.Join(root, "istok.db"),
		artifacts:  filepath.Join(root, "artifacts"),
		home:       home,
		repository: filepath.Join(home, "work", "acme-api"),
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "demosession: %v\n", err)
		os.Exit(1)
	}
}
