// Command dg-fake-agent does what a script says, so a test can start a real
// program where delegator starts an agent. internal/fakeagent holds the body.
package main

import (
	"fmt"
	"os"

	"github.com/alcubie/delegator/internal/fakeagent"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "dg-fake-agent: one argument, the path of the script")
		os.Exit(2)
	}
	status, err := fakeagent.Run(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "dg-fake-agent:", err)
		os.Exit(1)
	}
	os.Exit(status)
}
