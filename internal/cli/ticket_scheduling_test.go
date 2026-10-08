package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

func TestTicketMoveMatchesRootCompatibilityForm(t *testing.T) {
	for _, prefix := range [][]string{{"move"}, {"ticket", "move"}} {
		t.Run(strings.Join(prefix, " "), func(t *testing.T) {
			dataDir, repo, ids := threeInTheQueue(t)
			args := append(append([]string{}, prefix...), fmt.Sprint(ids[1]), "bottom")

			out, err := runIn(t, dataDir, repo, args...)
			if err != nil {
				t.Fatal(err)
			}
			if out != "" {
				t.Errorf("dg %s wrote %q, want nothing", strings.Join(prefix, " "), out)
			}
			if got, want := queueTitlesOf(t, dataDir), []string{"first", "third", "second"}; !slices.Equal(got, want) {
				t.Errorf("the queue is %v, want %v", got, want)
			}
		})
	}
}

func TestTicketMoveInvalidStateMatchesRootWithoutChangingTheQueue(t *testing.T) {
	for _, prefix := range [][]string{{"move"}, {"ticket", "move"}} {
		t.Run(strings.Join(prefix, " "), func(t *testing.T) {
			dataDir, repo, ids := threeInTheQueue(t)
			if err := testfix.OpenStore(t, dataDir).ChangeStatus(ids[1], store.Running); err != nil {
				t.Fatal(err)
			}
			before := queueTitlesOf(t, dataDir)
			args := append(append([]string{}, prefix...), fmt.Sprint(ids[1]), "top")

			if _, err := runIn(t, dataDir, repo, args...); !errors.Is(err, store.ErrNotMovable) {
				t.Fatalf("error = %v, want ErrNotMovable", err)
			}
			if got := queueTitlesOf(t, dataDir); !slices.Equal(got, before) {
				t.Errorf("the queue is %v, want %v", got, before)
			}
		})
	}
}

func TestTicketDependMatchesRootCompatibilityForm(t *testing.T) {
	for _, prefix := range [][]string{{"depend"}, {"ticket", "depend"}} {
		t.Run(strings.Join(prefix, " "), func(t *testing.T) {
			dataDir := t.TempDir()
			repo := testfix.Repo(t, repoBranch)
			first, second := twoTickets(t, dataDir, repo)
			third, err := ticketIn(t, dataDir, repo, "Dependent ticket", "")
			if err != nil {
				t.Fatal(err)
			}
			args := append(append([]string{}, prefix...), fmt.Sprint(third), "--after", fmt.Sprintf("%d,%d", first, second))

			out, err := runIn(t, dataDir, repo, args...)
			if err != nil {
				t.Fatal(err)
			}
			if out != "" {
				t.Errorf("dg %s wrote %q, want nothing", strings.Join(prefix, " "), out)
			}
			if got, want := testfix.Dependencies(t, dataDir, third), []int64{first, second}; !slices.Equal(got, want) {
				t.Errorf("dependencies = %v, want %v", got, want)
			}
		})
	}
}

func TestTicketDependInvalidLinkMatchesRootWithoutChangingDependencies(t *testing.T) {
	for _, prefix := range [][]string{{"depend"}, {"ticket", "depend"}} {
		t.Run(strings.Join(prefix, " "), func(t *testing.T) {
			dataDir := t.TempDir()
			repo := testfix.Repo(t, repoBranch)
			first, _ := twoTickets(t, dataDir, repo)
			args := append(append([]string{}, prefix...), fmt.Sprint(first), "--after", fmt.Sprint(first))

			if _, err := runIn(t, dataDir, repo, args...); !errors.Is(err, store.ErrSelfDependency) {
				t.Fatalf("error = %v, want ErrSelfDependency", err)
			}
			if got := testfix.Dependencies(t, dataDir, first); len(got) != 0 {
				t.Errorf("dependencies = %v, want none", got)
			}
		})
	}
}

func TestTicketSchedulingRPCMethodsReturnTheSameResults(t *testing.T) {
	for _, name := range []string{"move", "depend"} {
		t.Run(name, func(t *testing.T) {
			result := func(method string) any {
				t.Helper()
				dataDir, repo, ids := threeInTheQueue(t)
				params := map[string]any{"args": []any{ids[1], "top"}}
				if name == "depend" {
					params = map[string]any{"args": []any{ids[1]}, "after": []any{ids[0]}}
				}
				request, err := json.Marshal(map[string]any{
					"jsonrpc": "2.0", "method": method, "params": params, "id": method,
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
			if legacy, canonical := result(name), result("ticket."+name); !reflect.DeepEqual(legacy, canonical) {
				t.Errorf("%s result = %#v, ticket.%s result = %#v", name, legacy, name, canonical)
			}
		})
	}
}

func TestTicketSchedulingCommandFlagsHaveIndependentBindings(t *testing.T) {
	root := Root(t.TempDir())
	rootDepend, _, err := root.Find([]string{"depend"})
	if err != nil {
		t.Fatal(err)
	}
	ticketDepend, _, err := root.Find([]string{"ticket", "depend"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rootDepend.Flags().Set("remove", "true"); err != nil {
		t.Fatal(err)
	}
	if got := ticketDepend.Flags().Lookup("remove").Value.String(); got != "false" {
		t.Errorf("setting root depend --remove changed ticket depend --remove to %q", got)
	}
}
