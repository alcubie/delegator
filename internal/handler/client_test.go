package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// serve gives the client that the tests call the way an agent calls it.
func serve() *client { return &client{} }

func TestReadTextFileServesAnAbsolutePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(path, []byte("what the agent asked for"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := serve().ReadTextFile(t.Context(), acp.ReadTextFileRequest{Path: path})
	if err != nil {
		t.Fatalf("ReadTextFile: %v", err)
	}
	if r.Content != "what the agent asked for" {
		t.Errorf("the content is %q", r.Content)
	}
}

func TestReadTextFileRefusesARelativePath(t *testing.T) {
	_, err := serve().ReadTextFile(t.Context(), acp.ReadTextFileRequest{Path: "hello.txt"})
	if err == nil {
		t.Fatal("a relative path was read")
	}
	if !strings.Contains(err.Error(), "hello.txt") || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("the error is %v, and it should name the path and say what is wrong with it", err)
	}
}

func TestReadTextFileReportsAFileThatIsNotThere(t *testing.T) {
	_, err := serve().ReadTextFile(t.Context(), acp.ReadTextFileRequest{Path: filepath.Join(t.TempDir(), "gone.txt")})
	if err == nil {
		t.Fatal("a file that is not there was read")
	}
}

func TestWriteTextFileWritesThePathAndItsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "hello.txt")
	if _, err := serve().WriteTextFile(t.Context(), acp.WriteTextFileRequest{Path: path, Content: "written"}); err != nil {
		t.Fatalf("WriteTextFile: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	if string(b) != "written" {
		t.Errorf("the file holds %q", b)
	}
}

func TestWriteTextFileRefusesARelativePath(t *testing.T) {
	_, err := serve().WriteTextFile(t.Context(), acp.WriteTextFileRequest{Path: "hello.txt", Content: "written"})
	if err == nil {
		t.Fatal("a relative path was written")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("the error is %v, and it should say what is wrong with the path", err)
	}
}

func TestTheTerminalIsNotServed(t *testing.T) {
	c := serve()
	_, create := c.CreateTerminal(t.Context(), acp.CreateTerminalRequest{})
	_, output := c.TerminalOutput(t.Context(), acp.TerminalOutputRequest{})
	_, wait := c.WaitForTerminalExit(t.Context(), acp.WaitForTerminalExitRequest{})
	_, kill := c.KillTerminal(t.Context(), acp.KillTerminalRequest{})
	_, release := c.ReleaseTerminal(t.Context(), acp.ReleaseTerminalRequest{})
	for name, err := range map[string]error{"create": create, "output": output, "wait": wait, "kill": kill, "release": release} {
		if err == nil || !strings.Contains(err.Error(), "terminal not supported") {
			t.Errorf("terminal %s gave %v, and the client serves no terminal", name, err)
		}
	}
}

func TestRequestPermissionCancels(t *testing.T) {
	r, err := serve().RequestPermission(t.Context(), acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: acp.PermissionOptionId("allow")}},
	})
	if err != nil {
		t.Fatalf("RequestPermission: %v", err)
	}
	if r.Outcome.Cancelled == nil || r.Outcome.Selected != nil {
		t.Errorf("the outcome is %+v, and no option is answered yet", r.Outcome)
	}
}

func TestSessionUpdateIsIgnored(t *testing.T) {
	if err := serve().SessionUpdate(t.Context(), acp.SessionNotification{Update: acp.UpdateAgentMessageText("hello")}); err != nil {
		t.Errorf("SessionUpdate: %v", err)
	}
}
