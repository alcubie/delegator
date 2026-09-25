// Command dg-fake-agent serves scripted ACP responses over stdin and stdout
// for integration tests. Its only argument is the script path. Each prompt
// executes the scripted actions:
//
//	text <t>                  say t
//	thought <t>               think t
//	tool <kind> <title> [path]  start a tool call, on a file when path is there
//	update <status>           report that status of the tool it started last
//	permission <title> <kind>   ask to run a tool and record the answer
//	write <path> <content>    write the file through the client
//	prompt <path>             write the prompt to a file
//	wait <duration>           wait that long, or until the client cancels
//	usage <json>              return that standard ACP Usage object
//	stop <reason>             end the turn with that reason
//
// Cancelling a wait ends the turn with the cancelled reason and skips the
// remaining actions. A "history:" line starts replay actions, which run when
// a client loads a session instead of sending a prompt.
//
// Permission answers are recorded beside the script, one option ID per line,
// so tests can inspect them without accessing the protocol pipes.
package main

import (
	"fmt"
	"os"

	acp "github.com/coder/acp-go-sdk"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "dg-fake-agent: one argument, the path of the script")
		os.Exit(2)
	}
	if err := serve(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "dg-fake-agent:", err)
		os.Exit(1)
	}
}

// serve reads the script and answers the client until the connection ends.
func serve(path string) error {
	s, err := readScript(path)
	if err != nil {
		return err
	}
	agent := &fakeAgent{script: s, record: path + recordSuffix}
	conn := acp.NewAgentSideConnection(agent, os.Stdout, os.Stdin)
	agent.conn = conn
	<-conn.Done()
	return nil
}
