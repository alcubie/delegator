package cli

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// agentsCommand lists the agent programs that delegator knows and owns the
// command that adds a locally installed or custom one.
func agentsCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "List available ACP agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var agents []store.Agent
			if err := withStore(*dataDir, cfg, func(s *store.Store) error {
				var err error
				agents, err = s.Agents()
				return err
			}); err != nil {
				return err
			}
			writeAgents(cmd.OutOrStdout(), agents, cfg.DefaultAgent, all)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include known agents whose command is not on PATH")
	cmd.AddCommand(addAgentCommand(dataDir, cfg))
	return cmd
}

func writeAgents(out io.Writer, agents []store.Agent, defaultName string, all bool) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCOMMAND\tPATH\tRESUME\tDEFAULT")
	for _, agent := range agents {
		path, err := exec.LookPath(agent.Argv[0])
		if err != nil && !all {
			continue
		}
		pathResult := path
		if err != nil {
			pathResult = "missing"
		}
		resume := "no"
		if len(agent.Resume) != 0 {
			resume = "yes"
		}
		marker := ""
		if agent.Name == defaultName {
			marker = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", agent.Name, commandText(agent.Argv), pathResult, resume, marker)
		if err != nil && agent.InstallHint != "" {
			fmt.Fprintf(w, "\t%s\n", agent.InstallHint)
		}
	}
	_ = w.Flush()
	fmt.Fprintln(out, "PATH lookup confirms only that the command exists; it does not prove the agent works.")
}

func commandText(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		if arg == "" || strings.ContainsAny(arg, " \t\n\"'") {
			parts[i] = strconv.Quote(arg)
		} else {
			parts[i] = arg
		}
	}
	return strings.Join(parts, " ")
}

func addAgentCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	var path, command string
	var arguments []string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add or update an ACP agent command",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if path != "" && command != "" {
				return errors.New("--path and --command cannot be used together")
			}
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				agent, err := s.Agent(args[0])
				known := err == nil
				if err != nil && !errors.Is(err, store.ErrInvalidAgent) {
					return err
				}
				if path != "" {
					if !known {
						return errors.New("--path can only update a known agent; use --command for a new agent")
					}
					agent.Argv[0] = path
				} else if command != "" {
					agent = store.Agent{Name: args[0], Argv: append([]string{command}, arguments...)}
				} else {
					return errors.New("one of --path or --command is required")
				}
				if path != "" && cmd.Flags().Changed("arg") {
					agent.Argv = append([]string{path}, arguments...)
				}
				if _, err := exec.LookPath(agent.Argv[0]); err != nil {
					return fmt.Errorf("agent command %q is not executable: %w", agent.Argv[0], err)
				}
				agent.InstallHint = ""
				return s.SaveAgent(agent)
			})
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "executable path for a known agent")
	cmd.Flags().StringVar(&command, "command", "", "executable for a custom agent command")
	cmd.Flags().StringArrayVar(&arguments, "arg", nil, "command argument (may be repeated)")
	return cmd
}
