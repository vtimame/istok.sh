package presentation

import (
	"io"
	"os"
	"regexp"

	"github.com/charmbracelet/x/term"
)

var ansiControlSequence = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

// ForOutput retains styles only when the destination is an actual terminal.
// Renderers remain independently testable with their styled output.
func ForOutput(output io.Writer, value string) string {
	if OutputIsTerminal(output) {
		return value
	}

	return ansiControlSequence.ReplaceAllString(value, "")
}

func OutputIsTerminal(output io.Writer) bool {
	file, ok := output.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}
