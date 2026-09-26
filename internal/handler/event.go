package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// Event types use stable strings for JSON consumers. Consumers should ignore
// unknown types for forward compatibility.
const (
	TypeText       string = "text"        // assistant text
	TypeThought    string = "thought"     // reported reasoning text
	TypeTool       string = "tool"        // tool call started
	TypeToolUpdate string = "tool_update" // tool call status changed
	TypePermission string = "permission"  // permission decision
	TypeReplay     string = "replay"      // replayed turn
	TypeResult     string = "result"      // turn stopped
	TypeError      string = "error"       // turn error
)

// Permission decision statuses. Tool events instead carry the agent's status:
// pending, in_progress, completed, or failed.
const (
	StatusAllowed  string = "allowed"
	StatusRejected string = "rejected"
)

// StopEndTurn identifies a normally completed turn. Other stop reasons
// indicate an interrupted or limited turn.
const StopEndTurn = string(acp.StopReasonEndTurn)

// Event represents agent activity in a shared format. Fields apply to the
// event types named below; empty fields are omitted from JSON.
type Event struct {
	Type    string `json:"type,omitempty"`    // one of the types above
	Text    string `json:"text,omitempty"`    // text, thought and result
	Tool    string `json:"tool,omitempty"`    // tool, tool_update and permission: the tool's name
	Summary string `json:"summary,omitempty"` // tool and tool_update: command or file summary
	Kind    string `json:"kind,omitempty"`    // tool, tool_update and permission: tool kind
	Status  string `json:"status,omitempty"`  // tool status, permission decision, or result stop reason
	Session string `json:"session,omitempty"` // result: session ID for resuming
	Code    int    `json:"code,omitempty"`    // result and error: the exit code
	Err     string `json:"err,omitempty"`     // error message
	Replay  bool   `json:"replay,omitempty"`  // event belongs to loaded history
}

// WriteEvent encodes an event as compact JSON followed by a newline in one
// Write call. It leaves <, >, and & unescaped so code remains readable in
// logs.
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

// ReadEvents reads newline-delimited events, skipping blank lines. Invalid
// JSON produces a line-numbered error and reading continues. A read error is
// reported last. Lines have no length limit because event text can be
// arbitrarily large.
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
