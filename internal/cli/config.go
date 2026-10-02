package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

type setting struct {
	Name        string `json:"name"`
	Value       any    `json:"value"`
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
				writeSettings(out, settings, outputWidth(out))
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
					fmt.Fprintln(out, settingText(settingValue(*cfg, args[0])))
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
				return setSetting(s, args[0], args[1])
			})
		},
	})
	return cmd
}

// writeSettings renders descriptions within the available output width and
// keeps wrapped lines aligned with the description column.
func writeSettings(out io.Writer, settings []setting, width int) {
	settingWidth, valueWidth := len("SETTING"), len("VALUE")
	for _, setting := range settings {
		settingWidth = max(settingWidth, utf8.RuneCountInString(setting.Name))
		valueWidth = max(valueWidth, utf8.RuneCountInString(settingText(setting.Value)))
	}
	// Ten columns account for the four borders and six padding spaces.
	descriptionWidth := max(len("DESCRIPTION"), width-settingWidth-valueWidth-10)

	writeRule := func(left, middle, right string) {
		fmt.Fprintf(out, "%s%s%s%s%s%s%s\n",
			left, strings.Repeat("─", settingWidth+2),
			middle, strings.Repeat("─", valueWidth+2),
			middle, strings.Repeat("─", descriptionWidth+2), right)
	}
	writeRow := func(name, value, description string) {
		fmt.Fprintf(out, "│ %-*s │ %-*s │ %-*s │\n",
			settingWidth, name, valueWidth, value, descriptionWidth, description)
	}

	writeRule("┌", "┬", "┐")
	writeRow("SETTING", "VALUE", "DESCRIPTION")
	writeRule("├", "┼", "┤")
	for i, setting := range settings {
		lines := wrap(setting.Description, descriptionWidth)
		if len(lines) == 0 {
			lines = []string{""}
		}
		writeRow(setting.Name, settingText(setting.Value), lines[0])
		for _, line := range lines[1:] {
			writeRow("", "", line)
		}
		if i < len(settings)-1 {
			writeRule("├", "┼", "┤")
		}
	}
	writeRule("└", "┴", "┘")
}

// setSetting is the validation path shared by dg config set and guided
// onboarding. The store validates every setting; the command layer also makes
// sure a selected agent is runnable in this environment and gives the person
// a command that fixes a missing registry entry or executable.
func setSetting(s *store.Store, name, value string) error {
	if name == "default_agent" {
		agent, err := s.Agent(value)
		if err != nil {
			if errors.Is(err, store.ErrInvalidAgent) {
				add := commandText([]string{"dg", "agents", "add", value, "--command", "/path/to/executable"})
				return fmt.Errorf("agent %q is not registered.\nAdd it with:\n    %s", value, add)
			}
			return err
		}
		if _, err := agentExecutable(agent); err != nil {
			path := "/path/to/" + filepath.Base(agent.Argv[0])
			configure := commandText([]string{"dg", "agents", "add", agent.Name, "--path", path})
			return fmt.Errorf("agent %q uses executable %q, but it was not found on PATH.\nInstall it or configure its executable with:\n    %s", agent.Name, agent.Argv[0], configure)
		}
	}
	return s.SetSetting(name, value)
}

func knownSetting(name string) bool {
	for _, definition := range config.Definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}

func settingText(value any) string {
	if value == nil {
		return "null"
	}
	return fmt.Sprint(value)
}

func settingValue(cfg config.Config, name string) any {
	switch name {
	case "telemetry":
		if cfg.Telemetry == nil {
			return nil
		}
		return *cfg.Telemetry
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
	case "default_model":
		if cfg.DefaultModel == "" {
			return nil
		}
		return cfg.DefaultModel
	default:
		panic("settingValue called with unknown setting " + name)
	}
}
