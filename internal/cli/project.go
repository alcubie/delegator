// dg project shows the repositories that this instance has registered.

package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// errNoProjectName requires one of the two forms, so a rename that lost its
// argument to a shell does not clear the name it meant to set.
var errNoProjectName = errors.New("dg project rename takes a name, or --clear to use the folder name again")

// errProjectNameLineBreak keeps a name to the one line that a listing row and
// a ticket heading give it.
var errProjectNameLineBreak = errors.New("a project name is one line, and this one holds a line break")

// errProjectNameAndClear rejects the two forms together.
var errProjectNameAndClear = errors.New("dg project rename takes a name or --clear, and got both")

// errProjectArchived refuses new work for an archived project rather than
// restoring the project as a side effect of the work.
var errProjectArchived = errors.New("the project is archived")

// refuseArchived refuses the project registered at path when it is archived.
// A path that no project holds is not archived.
func refuseArchived(s *store.Store, path string) error {
	p, err := s.ProjectByPath(path)
	if errors.Is(err, store.ErrNoProject) {
		return nil
	}
	if err != nil {
		return err
	}
	if p.ArchivedAt.IsZero() {
		return nil
	}
	return fmt.Errorf("%w: %s; restore it with dg project restore", errProjectArchived, path)
}

type projectJSON struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
}

// projectName names a project after the folder of its path when it has no
// given name.
func projectName(name, path string) string {
	if name != "" {
		return name
	}
	return filepath.Base(path)
}

// projectValue resolves the name, so that a client reads one field rather than
// a fallback.
func projectValue(p store.Project) projectJSON {
	return projectJSON{
		ID:            p.ID,
		Name:          projectName(p.Name, p.Path),
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
		Long: "Register a repository as a project, name a project, archive one to hide it from " +
			"the lists, restore an archived one, and show the repositories that this Delegator " +
			"instance has registered.",
		Example: `  dg project add
  dg project list
  dg project rename Backend
  dg project archive
  dg project restore`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(projectAddCommand(dataDir, workDir, cfg))
	cmd.AddCommand(projectArchiveCommand(dataDir, workDir, cfg))
	cmd.AddCommand(projectListCommand(dataDir, cfg))
	cmd.AddCommand(projectRenameCommand(dataDir, workDir, cfg))
	cmd.AddCommand(projectRestoreCommand(dataDir, workDir, cfg))
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
// with no commit, since that leaves no branch to start a worktree from, and
// an archived project, which takes no new work until it is restored. It
// returns the stored row, so a project registered earlier reports the branch
// of that registration rather than the branch git reports today.
func registerProject(s *store.Store, dir string) (store.Project, error) {
	root, err := project.Root(dir)
	if err != nil {
		return store.Project{}, err
	}
	if err := refuseArchived(s, root); err != nil {
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
				p, err := s.ArchiveProject(absoluteDir(workDir, arg))
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

// projectRestoreCommand brings an archived project back to the lists. dir
// defaults to the current working directory.
func projectRestoreCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	return rpcOperationCommand("project.restore", &cobra.Command{
		Use:   "restore [dir]",
		Short: "Restore an archived project to the lists.",
		Long: "Restore the project registered at dir, or at the current working directory, so that it " +
			"returns to the lists of the desktop app. dir has to match the registered path of a project " +
			"exactly: it is not resolved through Git, and it does not have to exist, so a project whose " +
			"repository was deleted can still be restored. The tickets of the project are untouched. " +
			"Restoring a project that is not archived changes nothing and is not an error.",
		Example: `  dg project restore
  dg project restore ~/projects/widget`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var arg string
			if len(args) == 1 {
				arg = args[0]
			}
			var value projectJSON
			if err := withStore(*dataDir, cfg, func(s *store.Store) error {
				p, err := s.RestoreProject(absoluteDir(workDir, arg))
				if err != nil {
					return err
				}
				value = projectValue(p)
				return nil
			}); err != nil {
				return err
			}
			return writeValue(cmd.OutOrStdout(), value, false, func(out io.Writer) {
				fmt.Fprintf(out, "Restored %s\n", value.Path)
			})
		},
	})
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

// projectRenameCommand gives a project a name of its own, for an instance
// where two projects share the folder name of their paths.
func projectRenameCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	var clear bool
	cmd := &cobra.Command{
		Use:   "rename [name]",
		Short: "Give a project a name.",
		Long: "Give the project registered at a directory a name of its own, for an instance " +
			"where two projects share the folder name of their paths. Two projects may hold " +
			"the same name. The directory matches the registered path of a project exactly, " +
			"and need not exist, so a project whose repository moved or was deleted can " +
			"still be renamed.",
		Example: `  dg project rename Backend
  dg project rename "Sleep backend" --project ~/projects/sleep/backend
  dg project rename --clear`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := givenProjectName(args, clear)
			if err != nil {
				return err
			}
			dir := absoluteDir(workDir, projectDir)
			var value projectJSON
			if err := withStore(*dataDir, cfg, func(s *store.Store) error {
				p, err := s.SetProjectName(dir, name)
				if err != nil {
					return err
				}
				value = projectValue(p)
				return nil
			}); err != nil {
				return err
			}
			return writeValue(cmd.OutOrStdout(), value, false, func(out io.Writer) {
				fmt.Fprintln(out, value.Name)
			})
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"rename the project registered at this directory (default: the current directory)")
	cmd.Flags().BoolVar(&clear, "clear", false,
		"remove the given name, so the project uses the folder name of its path again")
	return rpcOperationCommand("project.rename", cmd)
}

// givenProjectName reads the name from the argument of dg project rename. It
// gives the empty string for --clear, which is the stored form that means the
// project takes the folder name of its path.
func givenProjectName(args []string, clear bool) (string, error) {
	if clear {
		if len(args) == 1 {
			return "", errProjectNameAndClear
		}
		return "", nil
	}
	if len(args) == 0 {
		return "", errNoProjectName
	}
	name := strings.TrimSpace(args[0])
	if name == "" {
		return "", errNoProjectName
	}
	if strings.ContainsAny(name, "\r\n") {
		return "", errProjectNameLineBreak
	}
	return name, nil
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
// project, as project.add, project.archive and project.restore return it.
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
