package agenthooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const foreignSettings = `{
  "env": {"A": "1"},
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "orca-hook && echo ok > /dev/null"}]}
    ],
    "PostToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "lint"}]}
    ]
  },
  "statusLine": {"type": "command", "command": "cs"}
}
`

func TestInstallKeepsOtherSettingsAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(foreignSettings), 0o600); err != nil {
		t.Fatal(err)
	}

	change, err := Install(path, Claude, Hooks(Claude, true), "/opt/my tools/istok")
	if err != nil {
		t.Fatal(err)
	}
	if !change.Changed || change.Backup == "" || !slices.Equal(change.Events, []Event{EventSessionStart, EventPreCompact, EventPostToolUse, EventStop}) {
		t.Fatalf("install = %+v", change)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	if !strings.Contains(text, `"orca-hook && echo ok > /dev/null"`) {
		t.Errorf("foreign hook command changed:\n%s", text)
	}
	if !(strings.Index(text, `"env"`) < strings.Index(text, `"hooks"`) && strings.Index(text, `"hooks"`) < strings.Index(text, `"statusLine"`)) {
		t.Errorf("top-level key order changed:\n%s", text)
	}
	if !strings.Contains(text, `"matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "'/opt/my tools/istok' hook claude post-tool-use"`) {
		t.Errorf("commit hook missing or without the Bash matcher:\n%s", text)
	}
	if !strings.Contains(text, `"command": "'/opt/my tools/istok' hook claude session-start"`) {
		t.Errorf("istok hook missing:\n%s", text)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v", info.Mode().Perm(), err)
	}

	var parsed struct {
		Hooks map[string][]struct {
			Hooks []handler `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Hooks["SessionStart"]) != 2 || len(parsed.Hooks["PostToolUse"]) != 2 {
		t.Errorf("hook groups = %+v", parsed.Hooks)
	}

	again, err := Install(path, Claude, Hooks(Claude, true), "/opt/my tools/istok")
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed {
		t.Errorf("second install changed the file: %+v", again)
	}

	removed, err := Uninstall(path, Claude)
	if err != nil {
		t.Fatal(err)
	}
	if !removed.Changed || len(removed.Events) != 0 {
		t.Fatalf("uninstall = %+v", removed)
	}

	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !same([]byte(foreignSettings), restored) {
		t.Errorf("uninstall did not restore the settings:\n%s", restored)
	}
}

func TestInstallCreatesCodexHooksWithoutCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex", "hooks.json")

	change, err := Install(path, Codex, Hooks(Codex, false), "istok")
	if err != nil {
		t.Fatal(err)
	}
	if !change.Changed || change.Backup != "" || !slices.Equal(change.Events, []Event{EventSessionStart}) {
		t.Fatalf("install = %+v", change)
	}

	installed, err := Installed(path, Claude)
	if err != nil || len(installed) != 0 {
		t.Fatalf("claude hooks in the codex file = %v, %v", installed, err)
	}

	removed, err := Uninstall(path, Codex)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !removed.Changed || !bytes.Equal(bytes.TrimSpace(data), []byte("{}")) {
		t.Fatalf("uninstall = %+v, file %q", removed, data)
	}

	missing := filepath.Join(t.TempDir(), "none.json")
	untouched, err := Uninstall(missing, Codex)
	if err != nil || untouched.Changed {
		t.Fatalf("uninstall of a missing file = %+v, %v", untouched, err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("uninstall created %s", missing)
	}
}

func TestInstallRejectsInvalidSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"hooks": `), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(path, Claude, Hooks(Claude, false), "istok"); err == nil {
		t.Fatal("install into invalid JSON succeeded")
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{"hooks": ` {
		t.Fatalf("invalid settings were rewritten: %q", data)
	}
}

func TestIstokCommandsAreRecognizedByAgent(t *testing.T) {
	cases := map[string]bool{
		"istok hook claude stop":                        true,
		"'/opt/my tools/istok' hook claude stop":        true,
		"/usr/local/bin/istok hook codex session-start": false,
		"other hook claude stop":                        false,
		"istok resume":                                  false,
	}
	for command, want := range cases {
		if got := isIstokCommand(command, Claude); got != want {
			t.Errorf("isIstokCommand(%q, claude) = %v, want %v", command, got, want)
		}
	}
}
