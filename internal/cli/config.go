package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

type setting struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

// configCommand returns the commands that inspect and change the settings in
// the instance database. Reads use the snapshot loaded by the root hook.
func configCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	list := func(cmd *cobra.Command, _ []string) error {
		return withStore(*dataDir, cfg, func(*store.Store) error {
			settings := make([]setting, 0, len(config.Definitions))
			for _, definition := range config.Definitions {
				settings = append(settings, setting{
					Name: definition.Name, Value: settingValue(*cfg, definition.Name), Description: definition.Description,
				})
			}
			return writeValue(cmd.OutOrStdout(), settings, false, func(out io.Writer) {
				for _, setting := range settings {
					fmt.Fprintf(out, "# %s\n%s = %s\n", setting.Description, setting.Name, setting.Value)
				}
			})
		})
	}
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or change instance settings.",
		Args:  cobra.NoArgs,
		RunE:  list,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Show every supported instance setting.",
		Args:  cobra.NoArgs,
		RunE:  list,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Show one instance setting.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !knownSetting(args[0]) {
				return fmt.Errorf("unknown setting %q", args[0])
			}
			return withStore(*dataDir, cfg, func(*store.Store) error {
				return writeValue(cmd.OutOrStdout(), settingValue(*cfg, args[0]), false, func(out io.Writer) {
					fmt.Fprintln(out, settingValue(*cfg, args[0]))
				})
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set <name> <value>",
		Short: "Change one instance setting.",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				if args[0] == "default_agent" {
					agent, err := s.Agent(args[1])
					if err != nil {
						return err
					}
					if _, err := agentExecutable(agent); err != nil {
						return fmt.Errorf("agent %q command %q is not executable: %w", agent.Name, agent.Argv[0], err)
					}
				}
				return s.SetSetting(args[0], args[1])
			})
		},
	})
	return cmd
}

func knownSetting(name string) bool {
	for _, definition := range config.Definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}

func settingValue(cfg config.Config, name string) string {
	switch name {
	case "runs":
		return strconv.Itoa(cfg.Runs)
	case "timeout_minutes":
		return strconv.Itoa(cfg.TimeoutMinutes)
	case "done_hours":
		return strconv.Itoa(cfg.DoneHours)
	case "max_runs_per_project":
		return strconv.Itoa(cfg.MaxRunsPerProject)
	case "default_agent":
		return cfg.DefaultAgent
	default:
		panic("settingValue called with unknown setting " + name)
	}
}
