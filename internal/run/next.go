package run

import (
	"os/exec"

	"github.com/alcubie/delegator/internal/store"
)

// Next starts a run for the first ticket of the queue, if the queue holds one
// and no run is active. It returns once the program has started, and does not
// wait for it: the caller is a command a person typed, or a supervisor that is
// about to exit, and neither should stay alive for the length of a run.
//
// launch returns the command for one ticket. dg passes its own executable with
// "run <id>", and a test passes something it can observe.
func Next(dataDir string, launch func(id int64) *exec.Cmd) error {
	return store.With(dataDir, func(s *store.Store) error {

		queue, err := s.ListQueue()
		if err != nil {
			return err
		}
		if len(queue) == 0 {
			return nil
		}
		return launch(queue[0].ID).Start()
	})
}
