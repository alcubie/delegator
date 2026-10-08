package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

func TestTicketLifecycleCommandsMatchRootCompatibilityForms(t *testing.T) {
	tests := []struct {
		name   string
		setUp  func(*testing.T, string) (*store.Store, int64, string)
		args   func(*testing.T, *store.Store, int64, string) []string
		status store.TicketStatus
	}{
		{
			name: "finish",
			setUp: func(t *testing.T, dataDir string) (*store.Store, int64, string) {
				s, id, repo, _ := runningTicket(t, dataDir)
				return s, id, repo
			},
			args: func(t *testing.T, s *store.Store, id int64, repo string) []string {
				ticket, err := s.Ticket(id)
				if err != nil {
					t.Fatal(err)
				}
				return []string{fmt.Sprint(id), testfix.GitOut(t, repo, "rev-parse", ticket.Branch)}
			},
			status: store.Ready,
		},
		{name: "accept", setUp: readyTicket, args: ticketIDArgs, status: store.Done},
		{name: "cancel", setUp: queuedTicket, args: ticketIDArgs, status: store.Cancelled},
	}
	for _, test := range tests {
		for _, prefix := range [][]string{{test.name}, {"ticket", test.name}} {
			t.Run(strings.Join(prefix, " "), func(t *testing.T) {
				dataDir := t.TempDir()
				s, id, repo := test.setUp(t, dataDir)
				args := append(append([]string{}, prefix...), test.args(t, s, id, repo)...)

				out, err := runIn(t, dataDir, repo, args...)
				if err != nil {
					t.Fatal(err)
				}
				if out != "" {
					t.Errorf("dg %s wrote %q, want nothing", strings.Join(prefix, " "), out)
				}
				if got := testfix.ReadTicket(t, dataDir, id).Status; got != test.status {
					t.Errorf("status = %q, want %q", got, test.status)
				}
			})
		}
	}
}

func ticketIDArgs(_ *testing.T, _ *store.Store, id int64, _ string) []string {
	return []string{fmt.Sprint(id)}
}

func TestTicketAcceptWithoutIDUsesTheCurrentProject(t *testing.T) {
	for _, prefix := range [][]string{{"accept"}, {"ticket", "accept"}} {
		t.Run(strings.Join(prefix, " "), func(t *testing.T) {
			dataDir := t.TempDir()
			_, id, repo := readyTicket(t, dataDir)

			out, err := runIn(t, dataDir, repo, prefix...)
			if err != nil {
				t.Fatal(err)
			}
			if want := fmt.Sprintf("%d\n", id); out != want {
				t.Errorf("output = %q, want %q", out, want)
			}
			if got := testfix.ReadTicket(t, dataDir, id).Status; got != store.Done {
				t.Errorf("status = %q, want %q", got, store.Done)
			}
		})
	}
}

func TestTicketRestartMatchesRootCompatibilityForm(t *testing.T) {
	for _, prefix := range [][]string{{"restart"}, {"ticket", "restart"}} {
		t.Run(strings.Join(prefix, " "), func(t *testing.T) {
			dataDir := t.TempDir()
			_, id, repo := failedTicket(t, dataDir)
			launcher, record := testfix.RecordingLaunch(t)
			useLaunch(t, launcher)
			var launched int64
			saved := restartLaunch
			restartLaunch = func(id int64, _ string) *exec.Cmd {
				launched = id
				return launcher()
			}
			t.Cleanup(func() { restartLaunch = saved })
			args := append(append([]string{}, prefix...), fmt.Sprint(id))

			if out, err := runIn(t, dataDir, repo, args...); err != nil || out != "" {
				t.Fatalf("output = %q, error = %v", out, err)
			}
			if launched != id {
				t.Errorf("restart launched ticket %d, want %d", launched, id)
			}
			testfix.WaitForStarts(t, record, 1)
		})
	}
}

func TestTicketLifecycleArgumentErrorsMatchRootForms(t *testing.T) {
	for _, name := range []string{"finish", "cancel", "restart"} {
		var rootErr error
		for _, prefix := range [][]string{{name}, {"ticket", name}} {
			_, err := runIn(t, t.TempDir(), t.TempDir(), prefix...)
			if err == nil {
				t.Fatalf("dg %s accepted missing arguments", strings.Join(prefix, " "))
			}
			if rootErr == nil {
				rootErr = err
			} else if err.Error() != rootErr.Error() {
				t.Errorf("dg %s error = %q, root error = %q", strings.Join(prefix, " "), err, rootErr)
			}
		}
	}
}

func TestTicketAcceptRPCMatchesRootResult(t *testing.T) {
	result := func(method string) any {
		t.Helper()
		dataDir := t.TempDir()
		_, id, repo := readyTicket(t, dataDir)
		request, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "method": method,
			"params": map[string]any{"args": []any{id}}, "id": method,
		})
		if err != nil {
			t.Fatal(err)
		}
		out, err := rpcIn(t, dataDir, repo, string(request))
		if err != nil {
			t.Fatal(err)
		}
		response := rpcObject(t, out)
		if response["error"] != nil {
			t.Fatalf("%s response = %#v, want a result", method, response)
		}
		return response["result"]
	}
	legacy, canonical := result("accept"), result("ticket.accept")
	if !reflect.DeepEqual(legacy, canonical) || !reflect.DeepEqual(canonical, map[string]any{"id": float64(1)}) {
		t.Errorf("accept result = %#v, ticket.accept result = %#v", legacy, canonical)
	}
}

func TestTicketLifecycleCommandFlagsHaveIndependentBindings(t *testing.T) {
	root := Root(t.TempDir())
	for _, test := range []struct{ command, flag, value string }{{"accept", "force", "true"}, {"restart", "model", "changed"}} {
		rootCommand, _, err := root.Find([]string{test.command})
		if err != nil {
			t.Fatal(err)
		}
		groupedCommand, _, err := root.Find([]string{"ticket", test.command})
		if err != nil {
			t.Fatal(err)
		}
		before := groupedCommand.Flags().Lookup(test.flag).Value.String()
		if err := rootCommand.Flags().Set(test.flag, test.value); err != nil {
			t.Fatal(err)
		}
		if got := groupedCommand.Flags().Lookup(test.flag).Value.String(); got != before {
			t.Errorf("setting root %s --%s changed grouped value to %q", test.command, test.flag, got)
		}
	}
}

func TestTicketLifecycleInvalidStatePreservesTicket(t *testing.T) {
	for _, prefix := range [][]string{{"restart"}, {"ticket", "restart"}} {
		dataDir := t.TempDir()
		_, id, repo := queuedTicket(t, dataDir)
		_, err := runIn(t, dataDir, repo, append(prefix, fmt.Sprint(id))...)
		if !errors.Is(err, store.ErrInvalidTicketStateChange) {
			t.Fatalf("dg %s error = %v, want invalid state", strings.Join(prefix, " "), err)
		}
		if got := testfix.ReadTicket(t, dataDir, id).Status; got != store.Queued {
			t.Errorf("status = %q, want %q", got, store.Queued)
		}
	}
}
