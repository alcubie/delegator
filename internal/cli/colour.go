// The flag --color, and the decision it makes. The word of the status line is
// coloured at a terminal and plain in a pipe, and the flag lets a person ask
// for either, whatever the output is: `watch -c -n 1 dg --color=always` reads
// dg through a pipe and wants the codes.

package cli

import (
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// colourMode is the value of --color. It is a pflag.Value, so the root holds
// it as the type and not as a string that each reader converts.
type colourMode string

const (
	colourAuto   colourMode = "auto"
	colourAlways colourMode = "always"
	colourNever  colourMode = "never"
)

func (m colourMode) String() string { return string(m) }

func (m *colourMode) Type() string { return "mode" }

// Set takes the value from the command line.
func (m *colourMode) Set(value string) error {
	*m = colourMode(value)
	return nil
}

// on reports whether the output gets the colour codes. always and never say so
// themselves; auto colours a terminal and nothing else.
func (m colourMode) on(out io.Writer) bool {
	switch m {
	case colourAlways:
		return true
	case colourNever:
		return false
	}
	return isTerminal(out)
}

// isTerminal reports whether out is a real terminal. It rejects a pipe, a
// file, the buffer of a test, and /dev/null, which a check of the file's mode
// alone would accept because it is a character device.
func isTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	return ok && isatty.IsTerminal(f.Fd())
}
