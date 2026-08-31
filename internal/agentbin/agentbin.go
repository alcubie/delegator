// Package agentbin compiles dg-fake-agent for a test binary that needs to start
// the real command.
//
// Go has no setup step that spans packages: each package's tests are their own
// process, so every package needing the command compiles its own copy from its
// TestMain. This package holds that build in one place, not to save the second
// compile, which no shared state can avoid, but so the two callers cannot drift.
package agentbin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// command is the package that Build compiles.
const command = "github.com/alcubie/delegator/cmd/dg-fake-agent"

// Build compiles dg-fake-agent into a temporary directory of its own and
// returns the path of the binary, together with a function that removes the
// directory. The directory is unique because go test runs packages side by
// side, and a shared path would have two of them writing one file.
func Build() (string, func(), error) {
	dir, err := os.MkdirTemp("", "delegator-agentbin")
	if err != nil {
		return "", nil, err
	}
	remove := func() { os.RemoveAll(dir) }

	binary := filepath.Join(dir, "dg-fake-agent")
	if out, err := exec.Command("go", "build", "-o", binary, command).CombinedOutput(); err != nil {
		remove()
		return "", nil, fmt.Errorf("go build %s: %w: %s", command, err, out)
	}
	return binary, remove, nil
}
