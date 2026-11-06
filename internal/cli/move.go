// dg move parses a direction or target ID and delegates queue or ready-list
// reordering to store.

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// directions maps command words to moves and supplies both help and
// validation.
var directions = map[string]store.Move{
	"up":     store.Up,
	"down":   store.Down,
	"top":    store.Top,
	"bottom": store.Bottom,
}

// directionNames returns direction names in a fixed order for help and
// errors.
func directionNames() []string {
	return []string{"up", "down", "top", "bottom"}
}

// moveCommand returns a fresh move command whose examples use commandPath.
func moveCommand(dataDir *string, cfg *config.Config, commandPath string) *cobra.Command {
	return rpcOperationCommand("move", &cobra.Command{
		Use:   "move <id>... <where>",
		Short: "Reorder tickets in their queue or ready list.",
		Long: "Move one or more tickets within QUEUED or READY. Every ticket must be in the same list, and they land together as one block in the order that the list already holds them. The destination may be " +
			strings.Join(directionNames(), ", ") + ", or the ID of another ticket in the same list, which places the tickets immediately before it. Up and down take one ticket.",
		Example: fmt.Sprintf(`  %s 42 top
  %s 42 down
  %s 42 57
  %s 42 57 93 bottom
  %s 42 57 93 18`, commandPath, commandPath, commandPath, commandPath, commandPath),
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			where := args[len(args)-1]
			ids := make([]int64, 0, len(args)-1)
			for _, arg := range args[:len(args)-1] {
				id, err := ticketArg(arg)
				if err != nil {
					return err
				}
				ids = append(ids, id)
			}
			return withStore(*dataDir, cfg, func(s *store.Store) error {

				if move, there := directions[where]; there {
					return s.MoveTickets(ids, move)
				}
				target, err := ticketArg(where)
				if err != nil {
					return fmt.Errorf("%q is not a direction and not an id: %s takes %s, or the id of another ticket",
						where, commandPath, strings.Join(directionNames(), ", "))
				}
				return s.MoveTicketsBefore(ids, target)
			})
		},
	})
}
