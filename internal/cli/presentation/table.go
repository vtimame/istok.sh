// Package presentation renders human-readable CLI output.
package presentation

import "github.com/jedib0t/go-pretty/v6/table"

const allowedRowLength = 100

// RenderTable renders headers and rows using the CLI's stable borderless table style.
func RenderTable(headers []string, rows [][]string) string {
	writer := newWriter()
	styledHeaders := make([]string, len(headers))
	for i, header := range headers {
		styledHeaders[i] = Header(header)
	}

	writer.AppendHeader(stringsToRow(styledHeaders))
	writer.AppendRows(stringsToRows(rows))

	return writer.Render()
}

// RenderRows renders rows without a header using the CLI's stable borderless table style.
func RenderRows(rows [][]string) string {
	writer := newWriter()
	writer.AppendRows(stringsToRows(rows))

	return writer.Render()
}

func newWriter() table.Writer {
	writer := table.NewWriter()
	style := table.StyleLight
	style.Options = table.OptionsNoBordersAndSeparators
	writer.SetStyle(style)
	writer.SetAllowedRowLength(allowedRowLength)

	return writer
}

func stringsToRows(rows [][]string) []table.Row {
	tableRows := make([]table.Row, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, stringsToRow(row))
	}

	return tableRows
}

func stringsToRow(values []string) table.Row {
	row := make(table.Row, len(values))
	for i, value := range values {
		row[i] = value
	}

	return row
}
