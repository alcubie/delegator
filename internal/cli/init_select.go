package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
)

// agentSelector returns the chosen index and whether the person made a
// choice. A nil selector means that init is not attached to a terminal and
// must not prompt.
type agentSelector func(io.Reader, io.Writer, []string, int) (int, bool, error)

func agentSelectorFor(in io.Reader, out io.Writer) agentSelector {
	input, inputOK := in.(*os.File)
	if inputOK && isatty.IsTerminal(input.Fd()) && isTerminal(out) {
		return readAgentSelection
	}
	return nil
}

// readAgentSelection prints numbered choices and reads exactly one line without
// buffering later answers needed by adapter or custom-agent prompts.
func readAgentSelection(in io.Reader, out io.Writer, choices []string, current int) (int, bool, error) {
	if len(choices) == 0 {
		return 0, false, nil
	}
	if current < 0 || current >= len(choices) {
		current = 0
	}

	fmt.Fprintln(out, "Choose your default agent:")
	for i, choice := range choices {
		fmt.Fprintf(out, "  %d. %s\n", i+1, choice)
	}
	for {
		fmt.Fprintf(out, "Selection [%d] (q to cancel): ", current+1)
		answer, read, err := readAgentSelectionLine(in)
		if err != nil {
			return 0, false, err
		}
		if !read {
			fmt.Fprintln(out)
			return 0, false, nil
		}
		switch strings.ToLower(answer) {
		case "":
			return current, true, nil
		case "q":
			return 0, false, nil
		default:
			selected, err := strconv.Atoi(answer)
			if err == nil && selected >= 1 && selected <= len(choices) {
				return selected - 1, true, nil
			}
			fmt.Fprintf(out, "Enter a number from 1 to %d, or q to cancel.\n", len(choices))
		}
	}
}

func readAgentSelectionLine(in io.Reader) (string, bool, error) {
	var answer strings.Builder
	var next [1]byte
	for {
		_, err := io.ReadFull(in, next[:])
		if err == io.EOF {
			if answer.Len() == 0 {
				return "", false, nil
			}
			return strings.TrimSpace(answer.String()), true, nil
		}
		if err != nil {
			return "", false, err
		}
		if next[0] == '\n' {
			return strings.TrimSpace(answer.String()), true, nil
		}
		answer.WriteByte(next[0])
	}
}
