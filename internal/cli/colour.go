// The flag --color, and the decision it makes. The word of the status line is
// coloured at a terminal and plain in a pipe, and the flag lets a person ask
// for either, whatever the output is: `watch -c -n 1 dg --color=always` reads
// dg through a pipe and wants the codes.

package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// colourMode is the value of --color. It is a pflag.Value, so cobra refuses a
// value that is not one of the three at the parse, before a command runs.
type colourMode string

const (
	colourAuto   colourMode = "auto"
	colourAlways colourMode = "always"
	colourNever  colourMode = "never"
)

func (m colourMode) String() string { return string(m) }

func (m *colourMode) Type() string { return "mode" }

// colourModes is each value in the order that the help and the error name them.
var colourModes = []colourMode{colourAlways, colourNever, colourAuto}

// Set takes the value from the command line. cobra puts the value and the
// name of the flag in front of the error, so the error itself names the three
// that are allowed, and a person who wrote --color=yes sees what to write.
func (m *colourMode) Set(value string) error {
	for _, allowed := range colourModes {
		if colourMode(value) == allowed {
			*m = allowed
			return nil
		}
	}
	return fmt.Errorf("the value must be %s, %s or %s", colourModes[0], colourModes[1], colourModes[2])
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
