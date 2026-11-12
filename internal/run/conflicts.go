package run

import (
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

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
