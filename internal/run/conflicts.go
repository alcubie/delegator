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
// A conflict starts a run once for each commit of the default branch: the
// ticket records that commit before the launch, so two commands in a row give
// it one run and not two. Whoever stored the verdict, this call or an inbox
// render, the run still starts.
//
// The setting resolve_conflicts turns these runs off for a person who prefers
// to merge the branch themselves. It is read before anything else: the check
// must not start a Git process.
//
// A paused queue starts nothing and checks nothing, for the same reason.
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
	answers, err := answerMerges(s)
	if err != nil {
		return err
	}
	for _, a := range answers {
		if a.clean != nil && !*a.clean && a.conflictRunBase != a.base {
			if err := s.SetConflictRunBase(a.id, a.base); err != nil {
				return err
			}
			if err := Detach(launch(a.id)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Conflicting returns the ids of the ready tickets whose commit no longer
// merges into the default branch of their project, from the stored verdict or
// a check, as MergeAnswers describes.
func Conflicting(s *store.Store) ([]int64, error) {
	answers, err := answerMerges(s)
	if err != nil {
		return nil, err
	}
	var conflicting []int64
	for _, a := range answers {
		if a.clean != nil && !*a.clean {
			conflicting = append(conflicting, a.id)
		}
	}
	return conflicting, nil
}

// MergeAnswers returns whether the commit of each ready ticket merges into the
// default branch of its project: true or false, or nil where Git failed. The
// branch moves without dg, by a commit, a pull or a merge of the person, so a
// ticket whose checked base is not the base now is checked and the verdict is
// stored; any other ticket answers from the store.
//
// Each project costs one Git process to read its default branch, and the
// stored verdict keeps the check of a ticket to one for each move of that
// branch.
//
// A Git failure answers nil for the tickets it affects and stores nothing for
// them: a repository the person moved or removed must not break every dg
// command.
func MergeAnswers(s *store.Store) (map[int64]*bool, error) {
	list, err := answerMerges(s)
	if err != nil {
		return nil, err
	}
	answers := make(map[int64]*bool, len(list))
	for _, a := range list {
		answers[a.id] = a.clean
	}
	return answers, nil
}

type mergeAnswer struct {
	id              int64
	clean           *bool
	base            string
	conflictRunBase string
}

func answerMerges(s *store.Store) ([]mergeAnswer, error) {
	ready, err := s.ReadyMerges()
	if err != nil {
		return nil, err
	}

	var answers []mergeAnswer
	for _, p := range ready {
		base, err := project.ResolveCommit(p.Project.Path, p.Project.DefaultBranch)
		for _, t := range p.Tickets {
			if err != nil {
				answers = append(answers, mergeAnswer{id: t.ID})
				continue
			}
			if t.CheckedBase == base && t.MergesCleanly != nil {
				answers = append(answers, mergeAnswer{id: t.ID, clean: t.MergesCleanly, base: base, conflictRunBase: t.ConflictRunBase})
				continue
			}
			clean, err := project.MergesCleanly(p.Project.Path, t.Commit, base)
			if err != nil {
				answers = append(answers, mergeAnswer{id: t.ID})
				continue
			}
			if err := s.SetConflictCheck(t.ID, base, clean); err != nil {
				return nil, err
			}
			answers = append(answers, mergeAnswer{id: t.ID, clean: &clean, base: base, conflictRunBase: t.ConflictRunBase})
		}
	}
	return answers, nil
}
