// Windows installer acceptance checks using a real ConPTY, including keyboard
// input through the documented iwr | iex command. No live release is downloaded.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type transcript struct {
	sync.Mutex
	bytes.Buffer
}

func (t *transcript) Write(p []byte) (int, error) {
	t.Lock()
	defer t.Unlock()
	return t.Buffer.Write(p)
}

var ansi = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07]*(?:\x07|\x1b\\)`)

func (t *transcript) text() string {
	t.Lock()
	defer t.Unlock()
	return ansi.ReplaceAllString(t.Buffer.String(), "")
}

func terminal(args []string, answer string) (string, error) {
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer inRead.Close()
	defer inWrite.Close()
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer outRead.Close()
	defer outWrite.Close()
	var console windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: 240, Y: 60}, windows.Handle(inRead.Fd()), windows.Handle(outWrite.Fd()), 0, &console); err != nil {
		return "", err
	}
	defer func() {
		if console != 0 {
			windows.ClosePseudoConsole(console)
		}
	}()
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return "", err
	}
	defer attributes.Delete()
	// This attribute takes the opaque handle itself, unlike most attributes,
	// which take a pointer to their value. Load its bits without uintptr casts.
	consolePointer := *(*unsafe.Pointer)(unsafe.Pointer(&console))
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, consolePointer, unsafe.Sizeof(console)); err != nil {
		return "", err
	}
	startup := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	// Null handles with this flag request ConPTY's console handles instead of
	// inheriting the CI runner's redirected standard streams.
	startup.Flags = windows.STARTF_USESTDHANDLES
	command, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return "", err
	}
	var process windows.ProcessInformation
	if err := windows.CreateProcess(nil, command, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT, nil, nil, &startup.StartupInfo, &process); err != nil {
		return "", err
	}
	defer windows.CloseHandle(process.Process)
	defer windows.CloseHandle(process.Thread)
	inRead.Close()
	outWrite.Close()
	var output transcript
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		_, _ = io.Copy(&output, outRead)
	}()
	deadline := time.Now().Add(90 * time.Second)
	sent := false
	var runErr error
	for {
		if !sent && answer != "" && strings.Contains(output.text(), "(q to cancel):") {
			if _, err := io.WriteString(inWrite, answer); err != nil {
				runErr = err
				break
			}
			sent = true
		}
		status, err := windows.WaitForSingleObject(process.Process, 50)
		if err != nil {
			runErr = err
			break
		}
		if status == windows.WAIT_OBJECT_0 {
			var code uint32
			if err := windows.GetExitCodeProcess(process.Process, &code); err != nil {
				runErr = err
			} else if code != 0 {
				runErr = fmt.Errorf("PowerShell exited with %d", code)
			}
			break
		}
		if time.Now().After(deadline) {
			runErr = fmt.Errorf("timed out waiting for installer/setup")
			break
		}
	}
	if runErr != nil {
		_ = windows.TerminateProcess(process.Process, 1)
	}
	// Drain output concurrently with closing ConPTY, which can emit final output.
	windows.ClosePseudoConsole(console)
	console = 0
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
		return output.text(), fmt.Errorf("console output did not close")
	}
	return output.text(), runErr
}

func run() error {
	if len(os.Args) != 5 {
		return fmt.Errorf("usage: console-check POWERSHELL SESSION_SCRIPT INSTALLER FIXTURE_ROOT")
	}
	for _, scenario := range []struct{ name, answer, want string }{
		{"select", "1\r", "Setup complete."},
		{"cancel", "q\r", "Setup is incomplete until a default agent is selected."},
		{"failure", "", "Delegator is installed, but setup is incomplete."},
		{"optout", "", "    dg init"},
		{"noninteractive", "", "    dg init"},
		{"redirected", "", "    dg init"},
		{"checksum", "", "SESSION PASS: checksum"},
		{"download", "", "SESSION PASS: download"},
	} {
		args := []string{os.Args[1], "-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass"}
		if scenario.name == "noninteractive" {
			args = append(args, "-NonInteractive")
		}
		args = append(args, "-File", os.Args[2], "-Installer", os.Args[3], "-Root", os.Args[4], "-Case", scenario.name)
		var output string
		var err error
		if scenario.name == "redirected" {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			cmd := exec.CommandContext(ctx, args[0], args[1:]...)
			var data []byte
			data, err = cmd.CombinedOutput()
			output = string(data)
			cancel()
		} else {
			output, err = terminal(args, scenario.answer)
		}
		if err != nil || !strings.Contains(output, scenario.want) || !strings.Contains(output, "SESSION PASS: "+scenario.name) {
			return fmt.Errorf("%s: %v; expected %q and session pass marker\n%s", scenario.name, err, scenario.want, output)
		}
		if scenario.answer == "" && strings.Contains(output, "Choose your default agent:") {
			return fmt.Errorf("%s unexpectedly prompted:\n%s", scenario.name, output)
		}
		fmt.Printf("Windows installer console check passed: %s\n", scenario.name)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
