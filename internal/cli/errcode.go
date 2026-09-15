package cli

import (
	"errors"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// codeUnknown is the code of an error that wraps no named error. A caller has a
// code for whatever came back, and the one code it cannot act on is this.
const codeUnknown = 1

// The codes of the named errors, one block of a hundred to a package, so that a
// package can gain an error without moving the code of another. A code is a
// positive integer: the error object of JSON-RPC 2.0 keeps -32768 to -32000 for
// the protocol itself, and an application code has to fall outside that.
const (
	codeNoTitle = 100 + iota
	codeTwoBodies
	codeBodyAndNoBody
	codeNoBody
)

const (
	codeGitNotOnPath = 200 + iota
	codeNoCommit
	codeNotARepository
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
)

// errorCodes gives the code of each named error of internal/cli,
// internal/project and internal/store. The GUI starts dg for each write and
// reads what comes back, and a sentence is for a person to read and not for a
// program to decide on, so every condition it has to tell apart has a number
// here.
var errorCodes = map[error]int{
	ErrNoTitle:                        codeNoTitle,
	errTwoBodies:                      codeTwoBodies,
	errBodyAndNoBody:                  codeBodyAndNoBody,
	errNoBody:                         codeNoBody,
	project.ErrGitNotOnPath:           codeGitNotOnPath,
	project.ErrNoCommit:               codeNoCommit,
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
}

// ErrorCode gives the code of an error. A command says what it was doing and
// wraps what went wrong, so the code is the code of the named error anywhere
// under it, and codeUnknown when there is none.
func ErrorCode(err error) int {
	for named, code := range errorCodes {
		if errors.Is(err, named) {
			return code
		}
	}
	return codeUnknown
}
