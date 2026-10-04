package uiservice

import (
	"slices"
	"strings"
	"testing"
)

func TestQuoteArgumentEscapesSystemdSpecials(t *testing.T) {
	cases := map[string]string{
		"plain":             `"plain"`,
		"":                  `""`,
		"with space":        `"with space"`,
		`back\slash`:        `"back\\slash"`,
		`say "hi"`:          `"say \"hi\""`,
		"100%":              `"100%%"`,
		"%h/istok.db":       `"%%h/istok.db"`,
		"$HOME":             `"$$HOME"`,
		"${XDG_DATA_HOME}":  `"$${XDG_DATA_HOME}"`,
		`\"%$`:              `"\\\"%%$$"`,
		"/opt/my app/istok": `"/opt/my app/istok"`,
	}

	for input, want := range cases {
		if got := quoteArgument(input); got != want {
			t.Errorf("quoteArgument(%q) = %s, want %s", input, got, want)
		}
	}
}

func TestArgumentsOrderAndOptionalDatabase(t *testing.T) {
	withoutDatabase := Config{Executable: "/usr/bin/istok", Port: 7700}
	want := []string{"/usr/bin/istok", "ui", "--no-open", "--port", "7700"}
	if got := withoutDatabase.Arguments(); !slices.Equal(got, want) {
		t.Fatalf("Arguments() = %q, want %q", got, want)
	}

	withDatabase := Config{Executable: "/usr/bin/istok", Port: 0, Database: "/data/istok.db"}
	want = []string{"/usr/bin/istok", "ui", "--no-open", "--port", "0", "--database", "/data/istok.db"}
	if got := withDatabase.Arguments(); !slices.Equal(got, want) {
		t.Fatalf("Arguments() = %q, want %q", got, want)
	}
}

func TestRenderUnitQuotesEveryExecStartWord(t *testing.T) {
	unit := RenderUnit(Config{
		Executable: "/home/user/my bin/istok",
		Port:       8123,
		Database:   `/data/100%/$name/"q"\db`,
	})

	want := strings.Join([]string{
		"# Managed by `istok ui service install`; manual edits are overwritten.",
		"[Unit]",
		"Description=Istok web UI",
		"",
		"[Service]",
		"Type=simple",
		`ExecStart="/home/user/my bin/istok" "ui" "--no-open" "--port" "8123" "--database" "/data/100%%/$$name/\"q\"\\db"`,
		"Restart=on-failure",
		"RestartSec=2",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n")
	if unit != want {
		t.Fatalf("RenderUnit() =\n%s\nwant\n%s", unit, want)
	}
}

func TestRenderUnitOmitsDatabaseWhenUnset(t *testing.T) {
	unit := RenderUnit(Config{Executable: "/usr/bin/istok", Port: 7700})

	wantLine := `ExecStart="/usr/bin/istok" "ui" "--no-open" "--port" "7700"` + "\n"
	if !strings.Contains(unit, wantLine) {
		t.Fatalf("RenderUnit() = %q, want line %q", unit, wantLine)
	}
	if strings.Contains(unit, "--database") {
		t.Fatalf("RenderUnit() = %q, want no --database", unit)
	}
}
