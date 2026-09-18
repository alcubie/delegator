// The command that changes the order of the queue and the order of READY.
// internal/store holds the move itself, and this file takes the word that the
// person wrote and gives it a direction.

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
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
func moveCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "move <id> <where>",
		Short: "Reorder a ticket in its queue or ready list.",
		Long: "Move a ticket within QUEUED or READY. The destination may be " +
			strings.Join(directionNames(), ", ") + ", or the ID of another ticket in the same list, which places the ticket immediately before it.",
		Example: `  dg move 42 top
  dg move 42 down
  dg move 42 57`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ticketArg(args[0])
			if err != nil {
				return err
			}
			return withStore(*dataDir, cfg, func(s *store.Store) error {

				if move, there := directions[args[1]]; there {
					return s.MoveTicket(id, move)
				}
				// A direction is a word and an id is a number, so the two never
				// take one another.
				target, err := ticketArg(args[1])
				if err != nil {
					return fmt.Errorf("%q is not a direction and not an id: dg move takes %s, or the id of another ticket",
						args[1], strings.Join(directionNames(), ", "))
				}
				return s.MoveTicketBefore(id, target)
			})
		},
	}
}
