// Package presentation renders human-readable CLI output.
package presentation

import "github.com/jedib0t/go-pretty/v6/table"

// RenderTable renders headers and rows using the CLI's stable light table style.
func RenderTable(headers []string, rows [][]string) string {
	writer := table.NewWriter()
	writer.SetStyle(table.StyleLight)
	writer.AppendHeader(stringsToRow(headers))

	tableRows := make([]table.Row, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, stringsToRow(row))
	}
	writer.AppendRows(tableRows)

	return writer.Render()
}

func stringsToRow(values []string) table.Row {
	row := make(table.Row, len(values))
	for i, value := range values {
		row[i] = value
	}

	return row
}
