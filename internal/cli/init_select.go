package cli

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// agentSelector returns the chosen index and whether the person made a
// choice. A nil selector means that init is not attached to a terminal and
// must not prompt.
type agentSelector func(io.Reader, io.Writer, []string, int) (int, bool, error)

func agentSelectorFor(in io.Reader, out io.Writer) agentSelector {
	input, inputOK := in.(*os.File)
	output, outputOK := out.(*os.File)
	if inputOK && outputOK && term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd())) {
		return terminalAgentSelector
	}
	return nil
}

// terminalAgentSelector puts a terminal into raw mode while the chooser reads
// arrow keys. Both streams must be terminals: redirecting the output must not
// write cursor-control sequences into a file.
func terminalAgentSelector(in io.Reader, out io.Writer, choices []string, current int) (int, bool, error) {
	input, inputOK := in.(*os.File)
	output, outputOK := out.(*os.File)
	if !inputOK || !outputOK || !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		return 0, false, fmt.Errorf("agent selection requires a terminal")
	}
	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return 0, false, err
	}
	defer term.Restore(int(input.Fd()), state)
	return readAgentSelection(in, out, choices, current)
}

// readAgentSelection renders the chooser and reads one key at a time. It is
// separate from raw-mode setup so tests can drive every key without taking
// control of the terminal that runs the test.
func readAgentSelection(in io.Reader, out io.Writer, choices []string, current int) (int, bool, error) {
	if len(choices) == 0 {
		return 0, false, nil
	}
	if current < 0 || current >= len(choices) {
		current = 0
	}

	const footerLines = 1
	rendered := false
	render := func() {
		if rendered {
			fmt.Fprintf(out, "\x1b[%dA", len(choices)+footerLines)
		}
		for i, choice := range choices {
			marker := "  "
			if i == current {
				marker = "> "
			}
			fmt.Fprintf(out, "\r\x1b[2K%s%s\n", marker, choice)
		}
		fmt.Fprint(out, "\r\x1b[2K↑/↓ move • Enter select • q cancel\n")
		rendered = true
	}

	fmt.Fprintln(out, "Choose your default agent:")
	fmt.Fprint(out, "\x1b[?25l")
	defer fmt.Fprint(out, "\x1b[?25h")
	render()

	var key [1]byte
	for {
		if _, err := io.ReadFull(in, key[:]); err != nil {
			return 0, false, err
		}
		switch key[0] {
		case '\r', '\n':
			return current, true, nil
		case 'q', 'Q', 3: // q or Ctrl-C
			return 0, false, nil
		case 'j':
			current = (current + 1) % len(choices)
			render()
		case 'k':
			current = (current - 1 + len(choices)) % len(choices)
			render()
		case 0x1b:
			var sequence [2]byte
			if _, err := io.ReadFull(in, sequence[:]); err != nil {
				return 0, false, err
			}
			if sequence[0] != '[' {
				continue
			}
			switch sequence[1] {
			case 'A':
				current = (current - 1 + len(choices)) % len(choices)
				render()
			case 'B':
				current = (current + 1) % len(choices)
				render()
			}
		}
	}
}
