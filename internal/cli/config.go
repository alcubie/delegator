package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
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
				for i, setting := range settings {
					if i > 0 {
						fmt.Fprintln(out)
					}
					fmt.Fprintf(out, "%s = %s  # %s\n", setting.Name, setting.Value, setting.Description)
				}
			})
		})
	}
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or change instance settings.",
		Long: "Show all settings for the selected Delegator instance. Use the get and set " +
			"subcommands to inspect or change one setting.",
		Example: `  dg config
  dg config get runs
  dg config set runs 2`,
		Args: cobra.NoArgs,
		RunE: list,
	}
	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Short:   "Show every supported instance setting.",
		Long:    "Show the name, current value, and description of every supported instance setting.",
		Example: `  dg config list`,
		Args:    cobra.NoArgs,
		RunE:    list,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Show one instance setting.",
		Long:  "Write the current value of one supported instance setting.",
		Example: `  dg config get done_hours
  dg config get default_agent`,
		Args: cobra.ExactArgs(1),
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
		Long: "Validate and store a new value for one instance setting. The change applies " +
			"to subsequent commands and agent runs.",
		Example: `  dg config set runs 2
  dg config set default_agent codex`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				if args[0] == "default_agent" {
					agent, err := s.Agent(args[1])
					if err != nil {
						if errors.Is(err, store.ErrInvalidAgent) {
							add := commandText([]string{"dg", "agents", "add", args[1], "--command", "/path/to/executable"})
							return fmt.Errorf("agent %q is not registered.\nAdd it with:\n    %s", args[1], add)
						}
						return err
					}
					if _, err := agentExecutable(agent); err != nil {
						path := "/path/to/" + filepath.Base(agent.Argv[0])
						configure := commandText([]string{"dg", "agents", "add", agent.Name, "--path", path})
						return fmt.Errorf("agent %q uses executable %q, but it was not found on PATH.\nInstall it or configure its executable with:\n    %s", agent.Name, agent.Argv[0], configure)
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
