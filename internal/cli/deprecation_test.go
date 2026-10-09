package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompatibilityCommandsWarnWithoutChangingOutput(t *testing.T) {
	for _, name := range []string{"list", "search", "show", "map", "edit", "move", "depend", "finish", "accept", "cancel", "restart", "chat", "pause", "start"} {
		t.Run(name, func(t *testing.T) {
			root := Root(t.TempDir())
			root.PersistentPreRunE = nil
			root.PersistentPostRun = nil
			command, _, err := root.Find([]string{name})
			if err != nil {
				t.Fatal(err)
			}
			group := "ticket"
			if name == "pause" || name == "start" {
				group = "queue"
			}
			var out, warnings bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&warnings)
			command.RunE = func(cmd *cobra.Command, _ []string) error { cmd.Print("result\n"); return nil }
			// An argument error must also guide users to the replacement.
			root.SetArgs([]string{name})
			_ = root.Execute()
			if !strings.Contains(warnings.String(), "deprecated") || !strings.Contains(warnings.String(), "dg "+group+" "+name) {
				t.Fatalf("warning = %q", warnings.String())
			}
			if strings.Contains(out.String(), "deprecated") {
				t.Fatalf("warning polluted stdout: %q", out.String())
			}
			if !command.Hidden {
				t.Fatal("compatibility command remains listed in help and docs")
			}
			if name != "chat" {
				method := rpcOpenRPCMethod(name, command)
				encoded, _ := json.Marshal(method)
				if !strings.Contains(string(encoded), `"deprecated":true`) {
					t.Fatal("remote method is not marked deprecated")
				}
			}
		})
	}
}

func TestDeprecatedPauseStillWorks(t *testing.T) {
	data := t.TempDir()
	out, warning, err := runInOutputs(t, data, t.TempDir(), "", "pause")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "queue is paused") || !strings.Contains(warning, "dg queue pause") {
		t.Fatalf("output=%q warning=%q", out, warning)
	}
	_, warning, err = runInOutputs(t, data, t.TempDir(), "", "queue", "pause")
	if err != nil || warning != "" {
		t.Fatalf("canonical: warning=%q error=%v", warning, err)
	}
}
