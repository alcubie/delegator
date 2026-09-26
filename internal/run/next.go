package run

import (
	"os/exec"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// Next starts detached supervisors for the currently claimable work and
// returns without waiting for runs to finish. Each supervisor claims its own
// ticket atomically, so concurrent triggers cannot duplicate a claim.
//
// ClaimableCount accounts for dependencies and project limits as well as
// global capacity. Counting empty slots alone could repeatedly launch
// supervisors that find no eligible work.
//
// launch builds the supervisor command: dg uses its own executable with run;
// tests can substitute an observable command.
func Next(s *store.Store, cfg config.Config, launch func() *exec.Cmd) error {
	claimable, err := s.ClaimableCount(cfg)
	if err != nil {
		return err
	}
	for range claimable {
		if err := Detach(launch()); err != nil {
			return err
		}
	}
	return nil
}
