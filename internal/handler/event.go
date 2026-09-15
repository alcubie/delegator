package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"strings"
)

// The types an Event can have. A consumer switches on Type and ignores what
// it does not know, so an agent that grows a new kind of output does not
// break it. The constants are strings because the wire holds strings, and
// a reader in another language sees the same words.
const (
	TypeText       string = "text"        // what the agent says
	TypeThought    string = "thought"     // what the agent thinks on the way
	TypeTool       string = "tool"        // a tool the agent runs
	TypeToolUpdate string = "tool_update" // how a tool it started is going
	TypePermission string = "permission"  // a permission the policy answered
	TypeReplay     string = "replay"      // a turn of a loaded session, before this one
	TypeResult     string = "result"      // the run finished
	TypeError      string = "error"       // the run failed
)

// The two statuses a permission event has. A tool event carries the agent's
// own status instead, which is one of pending, in_progress, completed and
// failed.
const (
	StatusAllowed  string = "allowed"
	StatusRejected string = "rejected"
)

// An Event is one thing an agent did, in the form every agent is turned into.
// A field belongs to the types its comment names and is empty in the others,
// and an empty field is left out of the JSON, so a reader in another language
// sees only what the event carries.
type Event struct {
	Type    string `json:"type,omitempty"`    // one of the types above
	Text    string `json:"text,omitempty"`    // text, thought and result
	Tool    string `json:"tool,omitempty"`    // tool, tool_update and permission: the tool's name
	Summary string `json:"summary,omitempty"` // tool and tool_update: one line, the file or the command
	Kind    string `json:"kind,omitempty"`    // tool, tool_update and permission: read, edit, execute and the rest
	Status  string `json:"status,omitempty"`  // tool and tool_update: how it is going; permission: allowed or rejected
	Session string `json:"session,omitempty"` // result: the session id for the next call
	Code    int    `json:"code,omitempty"`    // result and error: the exit code
	Err     string `json:"err,omitempty"`     // error: what went wrong
	Replay  bool   `json:"replay,omitempty"`  // the event is of a turn before the one running now
}

// WriteEvent writes one event as a compact JSON object and a newline. It
// builds the line first and writes it once, so two events never interleave,
// and it leaves <, > and & as they are: the stream is read by people as well
// as by programs, and the escapes would hide the code an agent quotes.
func WriteEvent(w io.Writer, e Event) error {
	var line bytes.Buffer
	enc := json.NewEncoder(&line)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		return err
	}
	_, err := w.Write(line.Bytes())
	return err
}

// ReadEvents reads a stream of the lines WriteEvent writes, one event per
// line. A blank line is skipped. A line that is not an event gives an error
// that names the line, and the next line is read all the same, because one bad
// line from an agent must not end the run. The stream ends at the end of the
// reader, or at the first read error, which it gives last.
//
// It reads a line at a time with no limit on its length, because the text of
// an event is whatever the agent wrote.
func ReadEvents(r io.Reader) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		br := bufio.NewReader(r)
		for n := 1; ; n++ {
			line, err := br.ReadString('\n')
			if line = strings.TrimSpace(line); line != "" {
				var e Event
				if bad := json.Unmarshal([]byte(line), &e); bad != nil {
					if !yield(Event{}, fmt.Errorf("line %d: %w", n, bad)) {
						return
					}
				} else if !yield(e, nil) {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					yield(Event{}, err)
				}
				return
			}
		}
	}
}
