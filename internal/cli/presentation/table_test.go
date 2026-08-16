package presentation

import (
	"regexp"
	"strings"
	"testing"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRenderTableUsesStableLightStyle(t *testing.T) {
	got := RenderTable([]string{"ID", "NAME"}, [][]string{{"1", "Istok"}})

	plain := stripANSI(got)
	if plain == "" {
		t.Fatal("rendered table is empty")
	}
	if !strings.Contains(got, "\x1b[") {
		t.Fatal("rendered table does not use ANSI styling")
	}
	if strings.ContainsAny(got, "┌┐└┘") {
		t.Fatalf("rendered table uses boxed borders:\n%s", got)
	}
	if !strings.Contains(plain, "ID") || !strings.Contains(plain, "NAME") {
		t.Fatalf("rendered table is missing headers: %q", plain)
	}
	if !strings.Contains(plain, "1") || !strings.Contains(plain, "Istok") {
		t.Fatalf("rendered table is missing row data: %q", plain)
	}
}

func stripANSI(value string) string {
	return ansiEscape.ReplaceAllString(value, "")
}
