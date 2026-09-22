// Command dg-docs generates the command reference for dg.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alcubie/delegator/internal/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dg-docs:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := docsCommand()
	command.SetArgs(args)
	return command.Execute()
}

func docsCommand() *cobra.Command {
	var outputDir string
	command := &cobra.Command{
		Use:           "dg-docs",
		Short:         "Generate the dg command reference",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return generate(outputDir)
		},
	}
	command.Flags().StringVar(&outputDir, "output-dir", "", "directory in which to write generated files")
	return command
}

func generate(outputDir string) error {
	if outputDir == "" {
		return errors.New("--output-dir is required")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("make output directory %q: %w", outputDir, err)
	}

	root := cli.Root("")
	disableAutoGenTags(root)
	if err := doc.GenMarkdownTreeCustom(root, outputDir, markdownTitle, func(link string) string {
		return link
	}); err != nil {
		return fmt.Errorf("generate Markdown in %q: %w", outputDir, err)
	}
	return nil
}

func disableAutoGenTags(command *cobra.Command) {
	command.DisableAutoGenTag = true
	for _, child := range command.Commands() {
		disableAutoGenTags(child)
	}
}

func markdownTitle(filename string) string {
	command := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	command = strings.ReplaceAll(command, "_", " ")
	title := "Alcubi Delegator CLI Reference"
	if command != "dg" {
		title += ": " + command
	}
	return "# " + title + "\n\n"
}
