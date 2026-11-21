// Package presentation renders human-readable CLI output.
package presentation

import (
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

const allowedRowLength = 100

// RenderTable renders headers and rows using the CLI's stable borderless table style.
func RenderTable(headers []string, rows [][]string) string {
	return renderTable(headers, rows, allowedRowLength)
}

// RenderTableFitting renders a table like RenderTable, but when a row would
// be too long it shortens the flex column (with an ellipsis) instead of
// cutting off the columns after it.
func RenderTableFitting(headers []string, rows [][]string, flex int) string {
	return renderTable(headers, fitColumn(headers, rows, flex, allowedRowLength), allowedRowLength)
}

// columnPadding is the space the borderless style puts around each cell.
const columnPadding = 2

// minimumFlexWidth keeps a shortened column readable.
const minimumFlexWidth = 12

func fitColumn(headers []string, rows [][]string, flex, rowLength int) [][]string {
	widths := make([]int, len(headers))
	for index, header := range headers {
		widths[index] = text.RuneWidthWithoutEscSequences(header)
	}
	for _, row := range rows {
		for index, value := range row {
			if index < len(widths) {
				widths[index] = max(widths[index], text.RuneWidthWithoutEscSequences(value))
			}
		}
	}

	budget := rowLength - columnPadding
	for index, width := range widths {
		if index != flex {
			budget -= width + columnPadding
		}
	}
	if flex < 0 || flex >= len(widths) || widths[flex] <= budget || budget < minimumFlexWidth {
		return rows
	}

	fitted := make([][]string, len(rows))
	for index, row := range rows {
		fitted[index] = append([]string(nil), row...)
		if flex < len(row) && text.RuneWidthWithoutEscSequences(row[flex]) > budget {
			fitted[index][flex] = text.Trim(row[flex], budget-1) + "…"
		}
	}

	return fitted
}

// RenderUnboundedTable renders a table without row truncation.
func RenderUnboundedTable(headers []string, rows [][]string) string {
	return renderTable(headers, rows, 0)
}

func renderTable(headers []string, rows [][]string, rowLength int) string {
	writer := newWriter(rowLength)
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
	writer := newWriter(allowedRowLength)
	writer.AppendRows(stringsToRows(rows))

	return writer.Render()
}

func newWriter(rowLength int) table.Writer {
	writer := table.NewWriter()
	style := table.StyleLight
	style.Options = table.OptionsNoBordersAndSeparators
	writer.SetStyle(style)
	writer.SetAllowedRowLength(rowLength)

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
