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
		Use:   "move <id> <where>",
		Short: "Reorder a ticket in its queue or ready list.",
		Long: "Move a ticket within QUEUED or READY. The destination may be " +
			strings.Join(directionNames(), ", ") + ", or the ID of another ticket in the same list, which places the ticket immediately before it.",
		Example: fmt.Sprintf(`  %s 42 top
  %s 42 down
  %s 42 57`, commandPath, commandPath, commandPath),
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
				target, err := ticketArg(args[1])
				if err != nil {
					return fmt.Errorf("%q is not a direction and not an id: %s takes %s, or the id of another ticket",
						args[1], commandPath, strings.Join(directionNames(), ", "))
				}
				return s.MoveTicketBefore(id, target)
			})
		},
	})
}
