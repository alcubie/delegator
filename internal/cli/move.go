// The command that changes the order of the queue. internal/store holds the
// move itself, and this file takes the word that the person wrote and gives it
// a direction.

package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/store"
)

// directions holds each word that a person writes, and the move that it makes.
// The help and the error both read this, so a direction that arrives is in each
// of them.
var directions = map[string]store.Move{
	"up":     store.Up,
	"down":   store.Down,
	"top":    store.Top,
	"bottom": store.Bottom,
}

// directionNames returns each word that dg move takes, always in one order,
// because a map gives its keys in no order.
func directionNames() []string {
	return []string{"up", "down", "top", "bottom"}
}

// moveCommand returns the command dg move.
func moveCommand(dataDir string) *cobra.Command {
	return &cobra.Command{
		Use: "move <id> <where>",
		Short: "Move one ticket in the queue. <where> is " +
			strings.Join(directionNames(), ", ") + ", or the id of another ticket.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%q is not the id of a ticket", args[0])
			}
			s, err := store.Open(dataDir)
			if err != nil {
				return err
			}
			defer s.Close()

			if move, there := directions[args[1]]; there {
				return s.MoveTicket(id, move)
			}
			// A direction is a word and an id is a number, so the two never
			// take one another.
			target, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("%q is not a direction and not an id: dg move takes %s, or the id of another ticket",
					args[1], strings.Join(directionNames(), ", "))
			}
			return s.MoveTicketBefore(id, target)
		},
	}
}
