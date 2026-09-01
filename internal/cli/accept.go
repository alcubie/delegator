package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/store"
)

// acceptCommand returns the command dg accept.
func acceptCommand(dataDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "accept <id>",
		Short: "Close a ready ticket.",
		Args:  cobra.ExactArgs(1),
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

			return s.ChangeStatus(id, store.Done)
		},
	}
}
