package main

import (
	"encoding/json"
	"fmt"

	"github.com/inoio/agents-sandbox/internal/termio"
)

type listOutput int

const (
	listOutputTable listOutput = iota
	listOutputIdentifiers
	listOutputJSON
)

// resolveListOutput selects a list rendering mode from the output format flag
// and the global quiet flag. Table is the default; quiet renders identifiers
// only; an explicit format selects structured output. Quiet and an explicit
// format are mutually exclusive.
func resolveListOutput(format string, quiet bool) (listOutput, error) {
	switch {
	case format == "" && !quiet:
		return listOutputTable, nil
	case format == "":
		return listOutputIdentifiers, nil
	case format != formatJSON:
		return 0, fmt.Errorf("unsupported format %q: only %q is supported", format, formatJSON)
	case quiet:
		return 0, fmt.Errorf("--format %s and --quiet are mutually exclusive", formatJSON)
	default:
		return listOutputJSON, nil
	}
}

// writeJSON writes value as indented JSON on stdout. It is the shared
// structured-output path for both list and single-object inspection renderers.
func writeJSON(ui termio.UI, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	ui.Out(string(data))
	return nil
}

// printList renders items in the selected mode: identifiers (cells[0] per
// item), JSON via jsonValue, or the default aligned table built from cells.
func printList[T any](
	ui termio.UI,
	mode listOutput,
	items []T,
	emptyMsg string,
	headers []string,
	jsonValue func([]T) any,
	cells ...func(T) string,
) error {
	switch mode {
	case listOutputIdentifiers:
		for _, item := range items {
			ui.Print(cells[0](item))
		}
	case listOutputJSON:
		return writeJSON(ui, jsonValue(items))
	case listOutputTable:
		if len(items) == 0 {
			ui.Info(emptyMsg)
			return nil
		}
		table := ui.NewTable(headers...)
		for _, item := range items {
			row := make([]string, len(cells))
			for i, cell := range cells {
				row[i] = cell(item)
			}
			table.AddRow(row...)
		}
		table.Print()
	}
	return nil
}
