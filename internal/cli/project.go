// dg project shows the repositories that this instance has registered.

package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

type projectJSON struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
}

// projectValue names a project after the folder of its path when it has no
// given name, so that a client reads one field rather than a fallback.
func projectValue(p store.Project) projectJSON {
	name := p.Name
	if name == "" {
		name = filepath.Base(p.Path)
	}
	return projectJSON{
		ID:            p.ID,
		Name:          name,
		Path:          p.Path,
		DefaultBranch: p.DefaultBranch,
		Archived:      !p.ArchivedAt.IsZero(),
	}
}

// projectCommand is the namespace for the projects of the instance.
func projectCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Work with the registered projects.",
		Long: "Register a repository as a project, archive one to hide it from the lists, and show the " +
			"repositories that this Delegator instance has registered.",
		Example: `  dg project add
  dg project list
  dg project archive`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(projectAddCommand(dataDir, workDir, cfg))
	cmd.AddCommand(projectArchiveCommand(dataDir, workDir, cfg))
	cmd.AddCommand(projectListCommand(dataDir, cfg))
	return cmd
}

// projectAddCommand registers a repository as a project without adding a
// ticket, so a project with no work yet can still be registered. dir
// defaults to the current working directory.
func projectAddCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	return rpcOperationCommand("project.add", &cobra.Command{
		Use:   "add [dir]",
		Short: "Register a repository as a project.",
		Long: "Register the Git repository at dir, or the current working directory, as a Delegator project, " +
			"without adding a ticket to its queue. A repository that is already registered, including from a " +
			"subdirectory or a linked worktree, returns the project already on record.",
		Example: `  dg project add
  dg project add ~/projects/widget`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var arg string
			if len(args) == 1 {
				arg = args[0]
			}
			dir, err := ticketProject(workDir, arg)
			if err != nil {
				return err
			}
			var value projectJSON
			if err := withStore(*dataDir, cfg, func(s *store.Store) error {
				p, err := registerProject(s, dir)
				if err != nil {
					return err
				}
				value = projectValue(p)
				return nil
			}); err != nil {
				return err
			}
			return writeValue(cmd.OutOrStdout(), value, false, func(out io.Writer) {
				fmt.Fprintln(out, value.Path)
			})
		},
	})
}

// registerProject resolves dir to its repository root and registers it with
// the store, creating the project row if needed. It refuses a repository
// with no commit, since that leaves no branch to start a worktree from. It
// returns the stored row, so a project registered earlier reports the branch
// of that registration rather than the branch git reports today.
func registerProject(s *store.Store, dir string) (store.Project, error) {
	root, err := project.Root(dir)
	if err != nil {
		return store.Project{}, err
	}
	if _, err := project.FirstCommit(root); err != nil {
		return store.Project{}, err
	}
	branch, err := project.DefaultBranch(root)
	if err != nil {
		return store.Project{}, err
	}
	if _, err := s.ProjectID(root, branch); err != nil {
		return store.Project{}, err
	}
	return s.ProjectByPath(root)
}

// projectArchiveCommand hides a project from the lists without deleting it.
// dir defaults to the current working directory.
func projectArchiveCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	return rpcOperationCommand("project.archive", &cobra.Command{
		Use:   "archive [dir]",
		Short: "Archive a project so it leaves the lists.",
		Long: "Archive the project registered at dir, or at the current working directory, so that it " +
			"leaves the lists of the desktop app without being deleted from Delegator. dir has to match " +
			"the registered path of a project exactly: it is not resolved through Git, and it does not " +
			"have to exist, so a project whose repository was deleted can still be archived. The tickets " +
			"of the project are untouched, and an open ticket keeps running and stays in the inbox. " +
			"Archiving an archived project keeps the time of its first archive.",
		Example: `  dg project archive
  dg project archive ~/projects/widget`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var arg string
			if len(args) == 1 {
				arg = args[0]
			}
			var value projectJSON
			if err := withStore(*dataDir, cfg, func(s *store.Store) error {
				p, err := s.ArchiveProject(namedProjectPath(workDir, arg))
				if err != nil {
					return err
				}
				value = projectValue(p)
				return nil
			}); err != nil {
				return err
			}
			return writeValue(cmd.OutOrStdout(), value, false, func(out io.Writer) {
				fmt.Fprintf(out, "Archived %s\n", value.Path)
			})
		},
	})
}

// namedProjectPath makes arg absolute against workDir and cleans it, so that
// it can be matched against the registered path of a project. It neither
// resolves the path through Git nor requires it to exist, because a command
// that names a registered project has to reach one whose repository was
// deleted.
func namedProjectPath(workDir, arg string) string {
	if filepath.IsAbs(arg) {
		return filepath.Clean(arg)
	}
	return filepath.Join(workDir, arg)
}

// projectListCommand writes every registered project, including those whose
// tickets have all left the inbox.
func projectListCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	return rpcOperationCommand("project.list", &cobra.Command{
		Use:   "list",
		Short: "List every registered project.",
		Long: "List every project that this Delegator instance has registered, in " +
			"alphabetical order of directory, with its name, the branch its worktrees start " +
			"from, and a mark on an archived project. A project without a given name takes " +
			"the name of its folder.",
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
		values = append(values, projectValue(p))
	}
	return values
}

func writeProjects(out io.Writer, projects []projectJSON) {
	if len(projects) == 0 {
		fmt.Fprintln(out, "No projects are registered.")
		return
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tPATH\tDEFAULT BRANCH\tSTATUS")
	for _, p := range projects {
		status := ""
		if p.Archived {
			status = "archived"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name, p.Path, p.DefaultBranch, status)
	}
	_ = w.Flush()
}

// projectResultSchema describes the object that identifies a registered
// project, as project.add and project.archive return it.
func projectResultSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":             map[string]any{"type": "integer", "minimum": 1},
			"name":           map[string]any{"type": "string"},
			"path":           map[string]any{"type": "string"},
			"default_branch": map[string]any{"type": "string"},
			"archived":       map[string]any{"type": "boolean"},
		},
		"required":             []string{"id", "name", "path", "default_branch", "archived"},
		"additionalProperties": true,
	}
}

// projectListResultSchema describes the array of projects returned by
// project.list.
func projectListResultSchema() map[string]any {
	return map[string]any{
		"type":  "array",
		"items": projectResultSchema(),
	}
}
