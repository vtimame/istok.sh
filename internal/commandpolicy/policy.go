// Package commandpolicy classifies commands that require an explicit local
// override and redacts common secret-bearing arguments before persistence.
package commandpolicy

import (
	"path/filepath"
	"regexp"
	"strings"
)

type Decision struct {
	Dangerous bool
	Reason    string
}

var (
	recursiveRemovePattern = regexp.MustCompile(`(?i)(^|[;&|]\s*)rm\s+[^;&|]*(?:-[a-z]*r|--recursive)`)
	deviceWritePattern     = regexp.MustCompile(`(?i)\bdd\s+[^;&|]*\bof=/(?:dev(?:/|\b)|\s|$)`)
	pipeToShellPattern     = regexp.MustCompile(`(?i)\b(?:curl|wget)\b[^|]*\|\s*(?:ba|z|da|k)?sh\b`)
	systemToolPattern      = regexp.MustCompile(`(?i)(^|[;&|]\s*)(?:mkfs(?:\.[a-z0-9]+)?|fdisk|parted|shutdown|reboot)(?:\s|$)`)
)

func Inspect(argv []string) Decision {
	if len(argv) == 0 {
		return Decision{}
	}

	command := strings.ToLower(filepath.Base(argv[0]))
	if command == "env" {
		if nested := environmentCommand(argv[1:]); len(nested) > 0 {
			return Inspect(nested)
		}
	}
	if strings.HasPrefix(command, "mkfs.") {
		return dangerous("system-destructive command")
	}
	switch command {
	case "sudo", "su", "doas":
		return dangerous("privilege escalation command")
	case "mkfs", "fdisk", "parted", "shutdown", "reboot":
		return dangerous("system-destructive command")
	case "rm":
		if hasRecursiveRemoveFlag(argv[1:]) {
			return dangerous("recursive file removal")
		}
	case "dd":
		if writesSystemPath(argv[1:]) {
			return dangerous("raw write to a system path")
		}
	case "git":
		if len(argv) > 1 && argv[1] == "clean" && hasForceFlag(argv[2:]) {
			return dangerous("forced git clean")
		}
	}

	if isShell(command) {
		if script, ok := shellScript(argv[1:]); ok {
			if recursiveRemovePattern.MatchString(script) {
				return dangerous("shell script contains recursive file removal")
			}
			if deviceWritePattern.MatchString(script) {
				return dangerous("shell script writes to a system path")
			}
			if pipeToShellPattern.MatchString(script) {
				return dangerous("shell script pipes a download to a shell")
			}
			if systemToolPattern.MatchString(script) {
				return dangerous("shell script invokes a system-destructive command")
			}
		}
	}

	return Decision{}
}

func Redact(argv []string) []string {
	result := append([]string(nil), argv...)
	redactNext := false

	for index, arg := range result {
		if redactNext {
			result[index] = "<redacted>"
			redactNext = false
			continue
		}

		name, value, found := strings.Cut(arg, "=")
		if found && sensitiveName(strings.TrimLeft(name, "-")) {
			result[index] = name + "=<redacted>"
			continue
		}
		if strings.HasPrefix(arg, "--") && sensitiveName(strings.TrimPrefix(arg, "--")) {
			redactNext = true
			continue
		}

		if found && sensitiveName(name) && value != "" {
			result[index] = name + "=<redacted>"
		}

	}

	return result
}

func dangerous(reason string) Decision {
	return Decision{Dangerous: true, Reason: reason}
}

func hasRecursiveRemoveFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--recursive" {
			return true
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsAny(arg[1:], "rR") {
			return true
		}
	}

	return false
}

func hasForceFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--force" {
			return true
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg[1:], "f") {
			return true
		}
	}

	return false
}

func writesSystemPath(args []string) bool {
	for _, arg := range args {
		normalized := strings.ToLower(arg)
		if !strings.HasPrefix(normalized, "of=") {
			continue
		}

		path := strings.TrimSpace(strings.TrimPrefix(normalized, "of="))
		if path == "/" || strings.HasPrefix(path, "/dev/") {
			return true
		}
	}

	return false
}

func isShell(command string) bool {
	switch command {
	case "sh", "bash", "zsh", "dash", "ksh", "fish":
		return true
	default:
		return false
	}
}

func shellScript(args []string) (string, bool) {
	for index, arg := range args {
		if arg == "-c" && index+1 < len(args) {
			return args[index+1], true
		}
	}

	return "", false
}

func environmentCommand(args []string) []string {
	for index, arg := range args {
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
			continue
		}

		return args[index:]
	}

	return nil
}

func sensitiveName(value string) bool {
	normalized := strings.ToUpper(strings.ReplaceAll(value, "-", "_"))
	for _, part := range []string{"TOKEN", "PASSWORD", "PASSWD", "SECRET", "API_KEY", "AUTHORIZATION", "PRIVATE_KEY"} {
		if normalized == part || strings.HasSuffix(normalized, "_"+part) {
			return true
		}
	}

	return false
}
