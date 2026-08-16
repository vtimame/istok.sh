package presentation

import "testing"

func TestRenderTableUsesStableLightStyle(t *testing.T) {
	got := RenderTable([]string{"ID", "NAME"}, [][]string{{"1", "Istok"}})
	want := "┌────┬───────┐\n" +
		"│ ID │ NAME  │\n" +
		"├────┼───────┤\n" +
		"│ 1  │ Istok │\n" +
		"└────┴───────┘"
	if got != want {
		t.Errorf("RenderTable() =\n%s\nwant:\n%s", got, want)
	}
}
