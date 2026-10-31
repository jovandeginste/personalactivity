package activity

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"strings"
	"text/tabwriter"
)

func Write(w io.Writer, events []Event, format string) error {
	switch format {
	case "json":
		for i := range events {
			maps.DeleteFunc(events[i].Attributes, func(_, v string) bool { return strings.TrimSpace(v) == "" })
		}
		return json.NewEncoder(w).Encode(events)
	case "table":
		table := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(table, "DATE\tSOURCE\tTOPIC\tSUMMARY")
		for _, event := range events {
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", event.Date.Format("2006-01-02 15:04:05 MST"), event.Source, event.Topic, event.Summary)
		}
		return table.Flush()
	default:
		return fmt.Errorf("unsupported format %q (use table or json)", format)
	}
}
