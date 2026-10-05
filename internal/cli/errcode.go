package cli

import (
	"errors"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// codeUnknown is the fallback for errors without a recognized sentinel.
const codeUnknown = 1

// Application error codes use a block of 100 per package so additions do not
// renumber other packages. Positive values avoid JSON-RPC's reserved protocol
// range (-32768 through -32000).
const (
	codeNoTitle = 100 + iota
	codeTwoBodies
	codeBodyAndNoBody
	codeNoBody
	codeEditTwoBodies
	codeEditorAndText
	codeEditNoForm
)

const (
	codeGitNotOnPath = 200 + iota
	codeNoCommit
	codeNotARepository
	codeUnknownCommit
	codeCommitNotOnBranch
	codeBranchNotMerged
	codeWorktreeDirty
)

const (
	codeNewerDatabase = 300 + iota
	codeSelfDependency
	codeDependencyRing
	codeNoDependency
	codeNotQueued
	codeInvalidTicketStateChange
	codeNotMovable
	codeNotInTheSameList
	codeNoTicket
	codeNoRoom
	codeNoRun
	codeInvalidAgent
)

// errorCodes maps CLI, project, and store errors to stable machine-readable
// codes.
var errorCodes = map[error]int{
	ErrNoTitle:                        codeNoTitle,
	errTwoBodies:                      codeTwoBodies,
	errBodyAndNoBody:                  codeBodyAndNoBody,
	errNoBody:                         codeNoBody,
	errEditTwoBodies:                  codeEditTwoBodies,
	errEditorAndText:                  codeEditorAndText,
	errEditNoForm:                     codeEditNoForm,
	project.ErrGitNotOnPath:           codeGitNotOnPath,
	project.ErrNoCommit:               codeNoCommit,
	project.ErrUnknownCommit:          codeUnknownCommit,
	project.ErrCommitNotOnBranch:      codeCommitNotOnBranch,
	project.ErrBranchNotMerged:        codeBranchNotMerged,
	project.ErrWorktreeDirty:          codeWorktreeDirty,
	project.ErrNotARepository:         codeNotARepository,
	store.ErrNewerDatabase:            codeNewerDatabase,
	store.ErrSelfDependency:           codeSelfDependency,
	store.ErrDependencyRing:           codeDependencyRing,
	store.ErrNoDependency:             codeNoDependency,
	store.ErrNotQueued:                codeNotQueued,
	store.ErrInvalidTicketStateChange: codeInvalidTicketStateChange,
	store.ErrNotMovable:               codeNotMovable,
	store.ErrNotInTheSameList:         codeNotInTheSameList,
	store.ErrNoTicket:                 codeNoTicket,
	store.ErrNoRoom:                   codeNoRoom,
	store.ErrNoRun:                    codeNoRun,
	store.ErrInvalidAgent:             codeInvalidAgent,
}

// ErrorCode finds a recognized wrapped error, falling back to codeUnknown.
func ErrorCode(err error) int {
	for named, code := range errorCodes {
		if errors.Is(err, named) {
			return code
		}
	}
	return codeUnknown
}
