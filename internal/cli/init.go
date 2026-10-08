package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

type initOptions struct {
	agent     string
	telemetry *bool
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
	var telemetry bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up Alcubi Delegator for a first run.",
		Long: "Discover installed agents, select or configure the default agent, and explain " +
			"how to view the inbox and create the first ticket.",
		Example: `  dg init
  dg init --agent codex`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("telemetry") {
				options.telemetry = &telemetry
			}
			selector := agentSelectorFor(cmd.InOrStdin(), cmd.OutOrStdout())
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				return runInit(cmd, s, cfg, options, selector, runAdapterInstaller)
			})
		},
	}
	cmd.Flags().StringVar(&options.agent, "agent", "",
		"select this available registered agent as the default without prompting")
	cmd.Flags().BoolVar(&telemetry, "telemetry", false, "submit a usage-data sharing choice (`true|false`)")
	cmd.Flags().Lookup("telemetry").NoOptDefVal = ""
	return rpcOperationCommand("init", cmd)
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
	fmt.Fprintln(out)
	if err := setInitTelemetry(s, cfg, options.telemetry); err != nil {
		return err
	}
	if options.agent == "" && selector == nil {
		return errors.New("dg init requires an interactive terminal; use dg init --agent NAME for scripted setup")
	}

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
		return completeInit(cmd, s, cfg, false)
	}

	if len(choices) == 0 {
		fmt.Fprintln(out, "\nNo supported Agent Client Protocol (ACP) command was found.")
		fmt.Fprintln(out, "Register one with:")
		fmt.Fprintln(out, "    dg agents add NAME --command /path/to/executable")
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
			return completeInit(cmd, s, cfg, true)
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
	return completeInit(cmd, s, cfg, true)
}

func prepareAdapter(cmd *cobra.Command, s *store.Store, choice *initAgentChoice, install adapterInstaller) (bool, error) {
	out := cmd.OutOrStdout()
	prompt := newInitPrompter(cmd.InOrStdin(), out)
	installArgv := []string{"npm", "install", "-g", choice.Adapter.Package}
	fmt.Fprintf(out, "\nDelegator uses Agent Client Protocol (ACP) to communicate with %s while it runs ticket tasks.\n",
		displayAgentName(choice.Agent.Name))
	fmt.Fprintf(out, "The required ACP command %s is not available.\n", choice.Adapter.Executable)
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
		path, err := installedAdapterPath(cmd, choice.Adapter)
		if err == nil {
			choice.Agent.Argv[0] = path
			choice.Agent.InstallHint = ""
			if err := s.SaveAgent(choice.Agent); err != nil {
				return false, err
			}
			fmt.Fprintf(out, "Using installed ACP command: %s\n", path)
			choice.NeedsAdapter = false
			return true, nil
		}
		fmt.Fprintf(out, "npm completed, but Delegator could not locate %s: %v\n", choice.Adapter.Executable, err)
		fmt.Fprintln(out, "Check npm prefix -g for the global install directory. Add that directory (bin on Unix) to PATH and restart your terminal, or enter the full ACP command path below.")
	}

	command, read, err := prompt.line("ACP command or absolute path (leave blank to cancel): ")
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

// npm can install successfully outside the current process's PATH. Persist the
// resolved command so future runs do not depend on a terminal PATH refresh.
func installedAdapterPath(cmd *cobra.Command, spec adapterSpec) (string, error) {
	query := exec.CommandContext(cmd.Context(), "npm", "prefix", "-g")
	query.Stderr = cmd.ErrOrStderr()
	output, err := query.Output()
	if err != nil {
		return "", fmt.Errorf("query npm global prefix: %w", err)
	}
	return adapterPathAtPrefix(strings.TrimSpace(string(output)), spec.Executable, runtime.GOOS)
}

func adapterPathAtPrefix(prefix, executable, goos string) (string, error) {
	if !filepath.IsAbs(prefix) {
		return "", fmt.Errorf("npm returned an invalid global prefix %q", prefix)
	}
	bin := prefix
	if goos != "windows" {
		bin = filepath.Join(prefix, "bin")
	}
	return exec.LookPath(filepath.Join(bin, executable))
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

func writeIncompleteInit(out io.Writer) error {
	fmt.Fprintln(out, "\nSetup is incomplete until a default agent is selected.")
	return nil
}

func completeInit(cmd *cobra.Command, s *store.Store, cfg *config.Config, interactive bool) error {
	if err := initTelemetry(cmd, s, cfg, interactive); err != nil {
		return err
	}
	return writeCompletedInit(cmd.OutOrStdout(), cfg.DefaultAgent)
}

func writeCompletedInit(out io.Writer, agent string) error {
	fmt.Fprintln(out, "\nSetup complete.")
	fmt.Fprintf(out, "\nDefault agent: %s\n", displayAgentName(agent))
	fmt.Fprintln(out, "\nThe default agent will be used to execute ticket tasks with your user permissions.")
	fmt.Fprintln(out, "Agent runs can use tokens from your plan.")
	fmt.Fprintln(out, "\nNext steps:")
	fmt.Fprintln(out, "    dg          view the inbox")
	fmt.Fprintln(out, "    dg ticket   create your first ticket")
	return nil
}
