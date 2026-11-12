package run

import (
	"os/exec"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// correctConflicts starts a supervisor for each ready ticket that the default
// branch of its project broke, so its agent merges the branch and corrects the
// conflicts without a command from the person.
//
// The setting resolve_conflicts turns these runs off for a person who prefers
// to merge the branch themselves. It is read before anything else: the check
// must not start a Git process, and must not spend the one chance that the
// stored commit of a ticket gives it.
//
// A paused queue starts nothing and checks nothing, for the same reason: the
// check stores the commit it checked against, so a check while paused would be
// the only one for that commit and the run would never happen. The queue state
// is read before Conflicting for that reason.
//
// A run that fails leaves the ticket failed, as any other run does, and the
// ticket is no longer ready for a second try.
func correctConflicts(s *store.Store, launch func(int64) *exec.Cmd, cfg config.Config) error {
	if !cfg.ResolveConflicts {
		return nil
	}
	running, err := s.IsQueueRunning()
	if err != nil || !running {
		return err
	}
	conflicting, err := Conflicting(s)
	if err != nil {
		return err
	}
	for _, id := range conflicting {
		if err := Detach(launch(id)); err != nil {
			return err
		}
	}
	return nil
}

// Conflicting returns the ids of the ready tickets whose commit no longer
// merges into the default branch of their project. The branch moves without
// dg, by a commit, a pull or a merge of the person, so a command asks this to
// find the tickets that the move broke.
//
// Each project costs one Git process to read its default branch, and the
// stored commit of a ticket keeps its check to one for each move of that
// branch.
//
// A Git failure skips the rest of that project and stores nothing for it: a
// repository the person moved or removed must not break every dg command.
func Conflicting(s *store.Store) ([]int64, error) {
	ready, err := s.ReadyMerges()
	if err != nil {
		return nil, err
	}

	var conflicting []int64
	for _, p := range ready {
		base, err := project.ResolveCommit(p.Project.Path, p.Project.DefaultBranch)
		if err != nil {
			continue
		}
		for _, t := range p.Tickets {
			if t.CheckedBase == base {
				continue
			}
			clean, err := project.MergesCleanly(p.Project.Path, t.Commit, base)
			if err != nil {
				break
			}
			if err := s.SetCheckedBase(t.ID, base); err != nil {
				return nil, err
			}
			if !clean {
				conflicting = append(conflicting, t.ID)
			}
		}
	}
	return conflicting, nil
}
