// Terminal color selection supports --color overrides for piped consumers
// such as watch -c.

package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// colourMode implements pflag.Value so invalid --color values fail during
// argument parsing.
type colourMode string

const (
	colourAuto   colourMode = "auto"
	colourAlways colourMode = "always"
	colourNever  colourMode = "never"
)

func (m colourMode) String() string { return string(m) }

func (m *colourMode) Type() string { return "mode" }

// colourModes defines the display order for help and errors.
var colourModes = []colourMode{colourAlways, colourNever, colourAuto}

// Set validates --color. Cobra prefixes errors with the flag and supplied
// value.
func (m *colourMode) Set(value string) error {
	for _, allowed := range colourModes {
		if colourMode(value) == allowed {
			*m = allowed
			return nil
		}
	}
	return fmt.Errorf("the value must be %s, %s or %s", colourModes[0], colourModes[1], colourModes[2])
}

// Queue status colors: green for running, yellow for paused.
const (
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	plain  = "\x1b[0m"
)

// colour colors the status word, leaving the label plain, when enabled for
// out.
func colour(tty bool, code, line string) string {
	if !tty {
		return line
	}
	label, word, _ := strings.Cut(line, " ")
	return label + " " + code + word + plain
}

// on applies explicit always/never modes first. In auto mode, NO_COLOR
// disables color, CLICOLOR_FORCE can enable it for non-terminals, and
// otherwise only terminals receive color.
func (m colourMode) on(out io.Writer) bool {
	switch m {
	case colourAlways:
		return true
	case colourNever:
		return false
	}
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	if _, set := os.LookupEnv("CLICOLOR_FORCE"); set {
		return true
	}
	return isTerminal(out)
}

// isTerminal checks terminal capability, not just character-device mode,
// which would incorrectly accept /dev/null.
func isTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	return ok && isatty.IsTerminal(f.Fd())
}
