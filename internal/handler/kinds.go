package handler

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/alcubie/delegator/internal/config"
)

// sessionPlaceholder is what a resume command holds where the id of the
// session goes.
const sessionPlaceholder = "{session}"

// table is the agent of each name that delegator knows without being told.
//
// Each entry was run through initialize and session/new with the client of
// the ACP spike on 2026-09-14:
//
//   - claude: Verified. claude-agent-acp 0.77.0 reported protocol 1 and
//     loadSession, and opened a session. claude 2.1.271 takes
//     --resume <session-id>.
//   - codex: Verified. codex-acp 1.11.0, the bin of the npm package
//     @agentclientprotocol/codex-acp, reported protocol 1 and loadSession,
//     and opened a session. The command is not installed on this machine, so
//     npx fetched the package to run it. The package @zed-industries/codex-acp
//     that the spike ran is deprecated in favour of it. codex-cli 0.154.0
//     takes resume <SESSION_ID>.
//   - gemini: Not verified. gemini is not installed here, so neither the argv
//     nor a command that resumes could be run. Resume is empty until one is,
//     which is an error from ResumeArgv rather than a command that opens
//     something other than the session.
//
// Antigravity's agy is not in the table: its help offers no ACP mode, only
// the stream-json of claude's command line.
var table = []Kind{
	{Name: "claude", Argv: []string{"claude-agent-acp"}, Resume: []string{"claude", "--resume", sessionPlaceholder}},
	{Name: "codex", Argv: []string{"codex-acp"}, Resume: []string{"codex", "resume", sessionPlaceholder}},
	{Name: "gemini", Argv: []string{"gemini", "--experimental-acp"}},
}

// Kinds is the table with the agents section of the config file applied. A
// config file that does not read gives the table alone: every command loads
// the file before it runs and stops with what is wrong with it, so a second
// report of the same fault from here would say it twice.
func Kinds() []Kind {
	cfg, err := config.Load()
	if err != nil {
		return slices.Clone(table)
	}
	return withAgents(table, cfg.Agents)
}

// withAgents applies the sections of the config file to the table. A section
// of the name of an entry changes the keys it gives and leaves the rest, so a
// person who names another command for an agent keeps the way to resume it. A
// section of a name the table does not hold is an agent of its own, added
// after the table by name.
func withAgents(kinds []Kind, agents map[string]config.Agent) []Kind {
	out := slices.Clone(kinds)
	for _, name := range slices.Sorted(maps.Keys(agents)) {
		i := slices.IndexFunc(out, func(k Kind) bool { return k.Name == name })
		if i < 0 {
			out = append(out, Kind{Name: name})
			i = len(out) - 1
		}
		if argv := agents[name].Argv; argv != nil {
			out[i].Argv = argv
		}
		if resume := agents[name].Resume; resume != nil {
			out[i].Resume = resume
		}
	}
	return out
}

// ResumeArgv is the command that opens a session of an agent in a terminal,
// with the id of the session in the place the command holds for it. An agent
// with no such command gives an error, because an argv without the id opens
// something other than the session the person asked for.
func ResumeArgv(kind, session string) ([]string, error) {
	kinds := Kinds()
	i := slices.IndexFunc(kinds, func(k Kind) bool { return k.Name == kind })
	if i < 0 {
		return nil, fmt.Errorf("no agent %q; delegator knows %s", kind, nameList(kinds))
	}
	if len(kinds[i].Resume) == 0 {
		return nil, fmt.Errorf("agent %q has no command that opens a session", kind)
	}
	argv := make([]string, 0, len(kinds[i].Resume))
	for _, arg := range kinds[i].Resume {
		argv = append(argv, strings.ReplaceAll(arg, sessionPlaceholder, session))
	}
	return argv, nil
}

// nameList is the name of each kind, for an error that says what there is.
func nameList(kinds []Kind) string {
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, k.Name)
	}
	return strings.Join(names, ", ")
}
