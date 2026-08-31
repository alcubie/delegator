package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/store"
)

// threeInTheQueue makes three tickets and returns the store and their ids, in
// the order that the queue holds them.
func threeInTheQueue(t *testing.T) (string, string, []int64) {
	t.Helper()
	dataDir := t.TempDir()
	repo := gitRepo(t)
	var ids []int64
	for _, title := range []string{"first", "second", "third"} {
		out, err := runIn(t, dataDir, repo, "ticket", title)
		if err != nil {
			t.Fatal(err)
		}
		var id int64
		if _, err := fmt.Sscan(strings.TrimSpace(out), &id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return dataDir, repo, ids
}

// queueTitlesOf reads the order of the queue through the store.
func queueTitlesOf(t *testing.T, dataDir string) []string {
	t.Helper()
	queue, err := openStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	titles := make([]string, 0, len(queue))
	for _, ticket := range queue {
		titles = append(titles, ticket.Title)
	}
	return titles
}

func TestRunMoveInEachDirection(t *testing.T) {
	tests := []struct {
		where string
		want  []string
	}{
		{"up", []string{"second", "first", "third"}},
		{"down", []string{"first", "third", "second"}},
		{"top", []string{"second", "first", "third"}},
		{"bottom", []string{"first", "third", "second"}},
	}
	for _, test := range tests {
		t.Run(test.where, func(t *testing.T) {
			dataDir, repo, ids := threeInTheQueue(t)

			out, err := runIn(t, dataDir, repo, "move", fmt.Sprint(ids[1]), test.where)
			if err != nil {
				t.Fatal(err)
			}
			if out != "" {
				t.Errorf("dg move wrote %q, want nothing", out)
			}
			if got := queueTitlesOf(t, dataDir); !slices.Equal(got, test.want) {
				t.Errorf("the queue is %v, want %v", got, test.want)
			}
		})
	}
}

func TestRunMoveWithAWordThatIsNotADirection(t *testing.T) {
	dataDir, repo, ids := threeInTheQueue(t)
	before := queueTitlesOf(t, dataDir)

	_, err := runIn(t, dataDir, repo, "move", fmt.Sprint(ids[0]), "sideways")
	if err == nil {
		t.Fatal("dg move took a word that is not a direction")
	}
	// the message names each direction, so the person can see the four
	for _, want := range []string{"sideways", "up", "down", "top", "bottom"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, and it does not name %q", err, want)
		}
	}
	if got := queueTitlesOf(t, dataDir); !slices.Equal(got, before) {
		t.Errorf("the queue is %v, want %v", got, before)
	}
}

func TestRunMoveWithATicketThatIsNotInTheQueue(t *testing.T) {
	dataDir, repo, ids := threeInTheQueue(t)
	if err := openStore(t, dataDir).ChangeStatus(ids[1], store.Running); err != nil {
		t.Fatal(err)
	}
	before := queueTitlesOf(t, dataDir)

	err := func() error {
		_, err := runIn(t, dataDir, repo, "move", fmt.Sprint(ids[1]), "top")
		return err
	}()
	if !errors.Is(err, store.ErrNotInTheQueue) {
		t.Fatalf("err = %v, want ErrNotInTheQueue", err)
	}
	if got := queueTitlesOf(t, dataDir); !slices.Equal(got, before) {
		t.Errorf("the queue is %v, want %v", got, before)
	}
}

func TestRunMoveWithNoDirection(t *testing.T) {
	dataDir, repo, ids := threeInTheQueue(t)
	if _, err := runIn(t, dataDir, repo, "move", fmt.Sprint(ids[0])); err == nil {
		t.Fatal("dg move took no direction")
	}
}

// <where> also takes the id of another ticket, so a person reaches a place in
// the middle of the queue with one command.
func TestRunMoveBeforeATicket(t *testing.T) {
	tests := []struct {
		name   string
		moved  int
		target int
		want   []string
	}{
		{"down the queue", 0, 2, []string{"second", "first", "third"}},
		{"up the queue", 2, 0, []string{"third", "first", "second"}},
		{"itself changes nothing", 1, 1, []string{"first", "second", "third"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDir, repo, ids := threeInTheQueue(t)

			out, err := runIn(t, dataDir, repo, "move",
				fmt.Sprint(ids[test.moved]), fmt.Sprint(ids[test.target]))
			if err != nil {
				t.Fatal(err)
			}
			if out != "" {
				t.Errorf("dg move wrote %q, want nothing", out)
			}
			if got := queueTitlesOf(t, dataDir); !slices.Equal(got, test.want) {
				t.Errorf("the queue is %v, want %v", got, test.want)
			}
		})
	}
}

func TestRunMoveBeforeATicketThatIsNotInTheQueue(t *testing.T) {
	dataDir, repo, ids := threeInTheQueue(t)
	if err := openStore(t, dataDir).ChangeStatus(ids[2], store.Running); err != nil {
		t.Fatal(err)
	}
	before := queueTitlesOf(t, dataDir)

	_, err := runIn(t, dataDir, repo, "move", fmt.Sprint(ids[0]), fmt.Sprint(ids[2]))
	if !errors.Is(err, store.ErrNotInTheQueue) {
		t.Fatalf("err = %v, want ErrNotInTheQueue", err)
	}
	if got := queueTitlesOf(t, dataDir); !slices.Equal(got, before) {
		t.Errorf("the queue is %v, want %v", got, before)
	}
}

// A word that is not a direction and not a number names each direction, and
// says that an id is the other thing that dg move takes.
func TestRunMoveErrorNamesBothWays(t *testing.T) {
	dataDir, repo, ids := threeInTheQueue(t)

	_, err := runIn(t, dataDir, repo, "move", fmt.Sprint(ids[0]), "sideways")
	if err == nil {
		t.Fatal("dg move took a word that is neither")
	}
	for _, want := range []string{"sideways", "up", "down", "top", "bottom", "id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, and it does not name %q", err, want)
		}
	}
}
