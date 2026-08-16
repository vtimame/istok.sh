package discovery

import "testing"

func TestLanguageForPath(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"main.go":       "go",
		"component.tsx": "tsx",
		"script.py":     "python",
		"README.md":     "markdown",
		"Dockerfile":    "dockerfile",
		"unknown.data":  "plain_text",
	}
	for path, want := range tests {
		path, want := path, want
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			if got := LanguageForPath(path); got != want {
				t.Fatalf("LanguageForPath(%q) = %q, want %q", path, got, want)
			}
		})
	}
}
