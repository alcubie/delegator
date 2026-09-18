// Command dg-fake-agent is an agent of the Agent Client Protocol that does
// what a script says, so that a test of an ACP client runs a real protocol
// exchange in milliseconds and for nothing. It serves the protocol on stdin
// and stdout, where an ACP agent serves it, and takes one argument: the path
// of the script.
//
// A line of the script is one action, and the actions of a script are the turn
// the agent takes for every prompt:
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
// A wait that the client cancels ends the turn with the cancelled reason and
// runs no more of the script, which is how a test drives a client that stops a
// turn part way.
//
// A line of "history:" starts the section that runs to the end of the file.
// Its actions are the turn the session already took, which the agent replays
// when a client loads the session.
//
// The agent records each permission answer in a file beside the script, one
// option id to the line, because the client owns the agent's pipes and a file
// is what a test can read.
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
