package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

type initOptions struct {
	agent string
}

type adapterSpec struct {
	CLI        string
	Executable string
	Package    string
}

var adapterSpecs = map[string]adapterSpec{
	"claude": {CLI: "claude", Executable: "claude-agent-acp", Package: "@agentclientprotocol/claude-agent-acp"},
	"codex":  {CLI: "codex", Executable: "codex-acp", Package: "@agentclientprotocol/codex-acp"},
}

type initAgentChoice struct {
	Agent        store.Agent
	Adapter      adapterSpec
	NeedsAdapter bool
}

type adapterInstaller func(*cobra.Command, adapterSpec) error

// initCommand guides a person through the minimum choice needed before they
// add work: which registered agent executes their tickets.
func initCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	var options initOptions
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up Alcubi Delegator for a first run.",
		Long: "Discover installed agents, select or configure the default agent, and explain " +
			"how to view the inbox and create the first ticket.",
		Example: `  dg init
  dg init --agent codex`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			selector := agentSelectorFor(cmd.InOrStdin(), cmd.OutOrStdout())
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				return runInit(cmd, s, cfg, options, selector, runAdapterInstaller)
			})
		},
	}
	cmd.Flags().StringVar(&options.agent, "agent", "",
		"select this available registered agent as the default without prompting")
	return cmd
}

type initPrompter struct {
	lines *bufio.Scanner
	out   io.Writer
}

func newInitPrompter(in io.Reader, out io.Writer) *initPrompter {
	return &initPrompter{lines: bufio.NewScanner(in), out: out}
}

// line reads one answer. End of input means the person declined the rest of
// onboarding, rather than turning a closed terminal into an unsafe default.
func (p *initPrompter) line(prompt string) (string, bool, error) {
	fmt.Fprint(p.out, prompt)
	if !p.lines.Scan() {
		if err := p.lines.Err(); err != nil {
			return "", false, err
		}
		fmt.Fprintln(p.out)
		return "", false, nil
	}
	return strings.TrimSpace(p.lines.Text()), true, nil
}

func (p *initPrompter) confirm(prompt string) (bool, error) {
	for {
		answer, read, err := p.line(prompt + " [y/N] ")
		if err != nil || !read {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		default:
			fmt.Fprintln(p.out, "Please answer yes or no.")
		}
	}
}

func discoverInitAgents(agents []store.Agent) []initAgentChoice {
	choices := make([]initAgentChoice, 0, len(agents))
	for _, agent := range agents {
		if _, err := agentExecutable(agent); err == nil {
			choices = append(choices, initAgentChoice{Agent: agent})
			continue
		}
		spec, knownAdapter := adapterSpecs[agent.Name]
		if !knownAdapter {
			continue
		}
		if _, err := exec.LookPath(spec.CLI); err == nil {
			choices = append(choices, initAgentChoice{Agent: agent, Adapter: spec, NeedsAdapter: true})
		}
	}
	return choices
}

func displayAgentName(name string) string {
	switch name {
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	case "cursor":
		return "Cursor Agent"
	case "gemini":
		return "Gemini"
	case "github-copilot":
		return "GitHub Copilot"
	case "goose":
		return "Goose"
	case "opencode":
		return "OpenCode"
	case "pi":
		return "Pi"
	default:
		return name
	}
}

func choiceLabels(choices []initAgentChoice, current string) ([]string, int) {
	labels := make([]string, len(choices))
	selected := 0
	for i, choice := range choices {
		label := displayAgentName(choice.Agent.Name)
		if choice.NeedsAdapter {
			label += " — ACP adapter not installed"
		}
		if choice.Agent.Name == current {
			label += " (current)"
			selected = i
		}
		labels[i] = label
	}
	return labels, selected
}

func choiceNamed(choices []initAgentChoice, name string) (initAgentChoice, bool) {
	for _, choice := range choices {
		if choice.Agent.Name == name {
			return choice, true
		}
	}
	return initAgentChoice{}, false
}

func runInit(cmd *cobra.Command, s *store.Store, cfg *config.Config, options initOptions, selector agentSelector, install adapterInstaller) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Welcome to Alcubi Delegator.")
	if cfg.DefaultAgent == "" {
		fmt.Fprintln(out, "Current default agent: none")
	} else {
		fmt.Fprintf(out, "Current default agent: %s\n", displayAgentName(cfg.DefaultAgent))
	}

	agents, err := s.Agents()
	if err != nil {
		return err
	}
	choices := discoverInitAgents(agents)

	if options.agent != "" {
		choice, found := choiceNamed(choices, options.agent)
		if found && choice.NeedsAdapter && selector != nil {
			ready, err := prepareAdapter(cmd, s, &choice, install)
			if err != nil {
				return err
			}
			if !ready {
				return writeIncompleteInit(out)
			}
		}
		if err := setSetting(s, "default_agent", options.agent); err != nil {
			return err
		}
		cfg.DefaultAgent = options.agent
		fmt.Fprintf(out, "Default agent set to %s.\n", displayAgentName(options.agent))
		return writeCompletedInit(out, cfg.DefaultAgent)
	}

	if len(choices) == 0 {
		if selector == nil {
			fmt.Fprintln(out, "\nNo supported ACP agent command was found.")
			fmt.Fprintln(out, "Run dg init in a terminal to configure an agent command, or use dg agents add.")
			return writeIncompleteInit(out)
		}
		name, configured, err := configureAgent(cmd, s)
		if err != nil {
			return err
		}
		if !configured {
			return writeIncompleteInit(out)
		}
		cfg.DefaultAgent = name
		return writeCompletedInit(out, cfg.DefaultAgent)
	}

	if selector == nil {
		if choice, found := choiceNamed(choices, cfg.DefaultAgent); found && !choice.NeedsAdapter {
			return writeCompletedInit(out, cfg.DefaultAgent)
		}
		fmt.Fprintln(out, "\nAvailable agents:")
		for _, choice := range choices {
			fmt.Fprintf(out, "  %s\n", displayAgentName(choice.Agent.Name))
		}
		fmt.Fprintln(out, "Run dg init in a terminal to choose one, or use dg init --agent NAME.")
		return writeIncompleteInit(out)
	}

	labels, current := choiceLabels(choices, cfg.DefaultAgent)
	selected, chose, err := selector(cmd.InOrStdin(), out, labels, current)
	if err != nil {
		return err
	}
	if !chose {
		if choice, found := choiceNamed(choices, cfg.DefaultAgent); found && !choice.NeedsAdapter {
			fmt.Fprintln(out, "Default agent was not changed.")
			return writeCompletedInit(out, cfg.DefaultAgent)
		}
		return writeIncompleteInit(out)
	}

	choice := choices[selected]
	if choice.NeedsAdapter {
		ready, err := prepareAdapter(cmd, s, &choice, install)
		if err != nil {
			return err
		}
		if !ready {
			return writeIncompleteInit(out)
		}
	}
	if err := setSetting(s, "default_agent", choice.Agent.Name); err != nil {
		return err
	}
	cfg.DefaultAgent = choice.Agent.Name
	fmt.Fprintf(out, "Default agent set to %s.\n", displayAgentName(choice.Agent.Name))
	return writeCompletedInit(out, cfg.DefaultAgent)
}

func prepareAdapter(cmd *cobra.Command, s *store.Store, choice *initAgentChoice, install adapterInstaller) (bool, error) {
	out := cmd.OutOrStdout()
	prompt := newInitPrompter(cmd.InOrStdin(), out)
	installArgv := []string{"npm", "install", "-g", choice.Adapter.Package}
	fmt.Fprintf(out, "\n%s is installed, but its ACP adapter %s is not.\n",
		displayAgentName(choice.Agent.Name), choice.Adapter.Executable)
	wantInstall, err := prompt.confirm("Install it now with " + commandText(installArgv) + "?")
	if err != nil {
		return false, err
	}
	if wantInstall {
		if err := install(cmd, choice.Adapter); err != nil {
			return false, err
		}
		if _, err := agentExecutable(choice.Agent); err == nil {
			choice.NeedsAdapter = false
			return true, nil
		}
		fmt.Fprintf(out, "%s was installed but is not on PATH.\n", choice.Adapter.Executable)
	}

	command, read, err := prompt.line("ACP adapter command or absolute path (leave blank to cancel): ")
	if err != nil || !read || command == "" {
		return false, err
	}
	choice.Agent.Argv[0] = command
	if _, err := agentExecutable(choice.Agent); err != nil {
		return false, fmt.Errorf("agent command %q is not executable: %w", command, err)
	}
	choice.Agent.InstallHint = ""
	if err := s.SaveAgent(choice.Agent); err != nil {
		return false, err
	}
	choice.NeedsAdapter = false
	return true, nil
}

func runAdapterInstaller(cmd *cobra.Command, spec adapterSpec) error {
	argv := []string{"npm", "install", "-g", spec.Package}
	fmt.Fprintf(cmd.OutOrStdout(), "Running %s\n", commandText(argv))
	install := exec.Command(argv[0], argv[1:]...)
	install.Stdin = cmd.InOrStdin()
	install.Stdout = cmd.OutOrStdout()
	install.Stderr = cmd.ErrOrStderr()
	if err := install.Run(); err != nil {
		return fmt.Errorf("install %s: %w", spec.Executable, err)
	}
	return nil
}

func configureAgent(cmd *cobra.Command, s *store.Store) (string, bool, error) {
	out := cmd.OutOrStdout()
	prompt := newInitPrompter(cmd.InOrStdin(), out)
	fmt.Fprintln(out, "\nNo supported ACP agent command was found. Configure one now.")
	name, read, err := prompt.line("Agent name [custom]: ")
	if err != nil || !read {
		return "", false, err
	}
	if name == "" {
		name = "custom"
	}

	for {
		command, read, err := prompt.line("ACP executable command or absolute path (leave blank to cancel): ")
		if err != nil || !read || command == "" {
			return "", false, err
		}
		agent, err := s.Agent(name)
		if errors.Is(err, store.ErrInvalidAgent) {
			agent = store.Agent{Name: name, Argv: []string{command}}
		} else if err != nil {
			return "", false, err
		} else {
			agent.Argv[0] = command
		}
		if _, err := agentExecutable(agent); err != nil {
			fmt.Fprintf(out, "%q is not an executable command or path. Try again.\n", command)
			continue
		}
		agent.InstallHint = ""
		if err := s.SaveAgent(agent); err != nil {
			return "", false, err
		}
		if err := setSetting(s, "default_agent", name); err != nil {
			return "", false, err
		}
		fmt.Fprintf(out, "Saved %s and selected it as the default agent.\n", displayAgentName(name))
		return name, true, nil
	}
}

func writeIncompleteInit(out io.Writer) error {
	fmt.Fprintln(out, "\nSetup is incomplete until a default agent is selected.")
	return nil
}

func writeCompletedInit(out io.Writer, agent string) error {
	fmt.Fprintf(out, "\nSetup complete. Default agent: %s\n", displayAgentName(agent))
	fmt.Fprintln(out, "The default agent will be used to execute ticket tasks with your user permissions.")
	fmt.Fprintln(out, "Agent runs can use tokens from your plan.")
	fmt.Fprintln(out, "\nNext steps:")
	fmt.Fprintln(out, "    dg          view the inbox")
	fmt.Fprintln(out, "    dg ticket   create your first ticket")
	return nil
}
