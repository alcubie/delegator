// dg project shows the repositories that this instance has registered.

package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

type projectJSON struct {
	ID            int64  `json:"id"`
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch"`
}

// projectCommand is the namespace for the projects of the instance.
func projectCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "project",
		Short:   "Inspect the registered projects.",
		Long:    "Show the repositories that this Delegator instance has registered.",
		Example: "  dg project list",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(projectListCommand(dataDir, cfg))
	return cmd
}

// projectListCommand writes every registered project, including those whose
// tickets have all left the inbox.
func projectListCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	return rpcOperationCommand("project.list", &cobra.Command{
		Use:   "list",
		Short: "List every registered project.",
		Long: "List every project that this Delegator instance has registered, in " +
			"alphabetical order of directory, with the branch its worktrees start from.",
		Example: `  dg project list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var projects []store.Project
			if err := withStore(*dataDir, cfg, func(s *store.Store) error {
				var err error
				projects, err = s.Projects()
				return err
			}); err != nil {
				return err
			}
			value := projectValues(projects)
			return writeValue(cmd.OutOrStdout(), value, false, func(out io.Writer) {
				writeProjects(out, value)
			})
		},
	})
}

// projectValues keeps an instance with no projects an empty array rather than
// null, so a caller can read the result without a nil case. The store returns
// the rows in the order of their path.
func projectValues(projects []store.Project) []projectJSON {
	values := make([]projectJSON, 0, len(projects))
	for _, p := range projects {
		values = append(values, projectJSON{ID: p.ID, Path: p.Path, DefaultBranch: p.DefaultBranch})
	}
	return values
}

func writeProjects(out io.Writer, projects []projectJSON) {
	if len(projects) == 0 {
		fmt.Fprintln(out, "No projects are registered.")
		return
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PATH\tDEFAULT BRANCH")
	for _, p := range projects {
		fmt.Fprintf(w, "%s\t%s\n", p.Path, p.DefaultBranch)
	}
	_ = w.Flush()
}

// projectListResultSchema describes the array of projects returned by
// project.list.
func projectListResultSchema() map[string]any {
	project := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":             map[string]any{"type": "integer", "minimum": 1},
			"path":           map[string]any{"type": "string"},
			"default_branch": map[string]any{"type": "string"},
		},
		"required":             []string{"id", "path", "default_branch"},
		"additionalProperties": true,
	}
	return map[string]any{
		"type":  "array",
		"items": project,
	}
}
