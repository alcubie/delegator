package cli

import (
	"fmt"
	"io"
	"strings"
)

// writeMermaid writes the same dependency component as writeMap, with a class
// on each node for a stylesheet to distinguish ticket states. An edge points
// from the work that must finish to the work that waits for it.
func writeMermaid(out io.Writer, tickets []mappedTicket) {
	fmt.Fprintln(out, "flowchart TD")
	for _, ticket := range tickets {
		fmt.Fprintf(out, "    ticket%d[\"#%d %s\"]:::%s\n",
			ticket.ID, ticket.ID, mermaidLabel(ticket.Title), ticket.Status)
	}
	for _, ticket := range tickets {
		for _, dependency := range ticket.DependsOn {
			fmt.Fprintf(out, "    ticket%d --> ticket%d\n", dependency, ticket.ID)
		}
	}
}

// mermaidLabel keeps a quoted flowchart label from becoming Mermaid syntax.
// Hashes are entities in Mermaid, and the HTML-sensitive characters are
// entities in the label that Mermaid writes into the SVG.
func mermaidLabel(label string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"\"", "&quot;",
		"#", "#35;",
		"<", "&lt;",
		">", "&gt;",
		"\r", "#13;",
		"\n", "#10;",
	).Replace(label)
}
