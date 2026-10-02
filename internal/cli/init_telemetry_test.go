package cli

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

func TestInitTelemetryPromptSubmissionAndExits(t *testing.T) {
	for _, tt := range []struct {
		name, input, want string
	}{
		{"enter", "\n", "true"},
		{"yes", "YES\n", "true"},
		{"no", "n\n", "false"},
		{"eof", "", "null"},
		{"unsubmitted yes", "yes", "null"},
		{"unsubmitted no", "n", "null"},
		{"interrupt", "\x03", "null"},
		{"escape", "\x1b", "null"},
		{"control d", "\x04", "null"},
		{"skip is invalid", "s\nn\n", "false"},
		{"cancel is invalid", "q\nyes\n", "true"},
		{"invalid then eof", "maybe\n", "null"},
		{"invalid then no", "maybe\nno\n", "false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := t.TempDir()
			executable(t, bin, "goose")
			t.Setenv("PATH", bin)
			out, err := runInteractiveInit(t, dir, "\n"+tt.input, initOptions{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			s := testfix.OpenStore(t, dir)
			cfg, err := s.Settings()
			if err != nil {
				t.Fatal(err)
			}
			if got := settingText(settingValue(cfg, "telemetry")); got != tt.want {
				t.Fatalf("telemetry = %s, want %s", got, tt.want)
			}
			if cfg.DefaultAgent != "goose" {
				t.Fatalf("agent setup changed: agent = %q", cfg.DefaultAgent)
			}
			changeTo := "true"
			if tt.want == "true" {
				changeTo = "false"
			}
			prompt := "Would you like to share your usage data to help improve Delegator?\n" +
				"This will only share number of actions taken and configuration information. Your specific tickets and files will never be shared.\n" +
				"See https://alcubi.ai/delegator/privacy/ for details."
			for _, want := range []string{prompt, "Share data? [Y/n]", "Usage data sharing: " + tt.want, "dg config set telemetry " + changeTo + "."} {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q: %s", want, out)
				}
			}
			promptAt := strings.Index(out, "Share data? [Y/n]")
			completeAt := strings.Index(out, "Setup complete.")
			statusAt := strings.Index(out, "Usage data sharing:")
			if promptAt > completeAt || completeAt > statusAt {
				t.Errorf("usage-data question was not immediately before setup completion: %s", out)
			}
			if !strings.Contains(out, "Welcome to Alcubi Delegator.\n\nCurrent default agent:") || !strings.Contains(out, "Selection [1] (q to cancel): \nWould you like") {
				t.Errorf("setup sections are not separated by blank lines: %s", out)
			}
			state, err := s.TelemetryState()
			if err != nil || (state.ConsentStart != nil) != (tt.want == "true") {
				t.Fatalf("consent bookkeeping = %+v, %v", state, err)
			}
		})
	}
}

type telemetryErrorReader struct{}

func (telemetryErrorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestTelemetryPromptReadFailureDoesNotSubmit(t *testing.T) {
	var out bytes.Buffer
	choice, err := promptTelemetry(telemetryErrorReader{}, &out)
	if choice != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("prompt returned %v, %v", choice, err)
	}
}

func TestInitTelemetryPersistsBeforeAgentSetupFails(t *testing.T) {
	dir := t.TempDir()
	if _, err := runIn(t, dir, t.TempDir(), "init", "--agent", "missing", "--telemetry=true"); err == nil {
		t.Fatal("expected setup error")
	}
	cfg, err := testfix.OpenStore(t, dir).Settings()
	if err != nil || cfg.Telemetry == nil || !*cfg.Telemetry || cfg.DefaultAgent != "" {
		t.Fatalf("consent lost on incomplete setup: %+v, %v", cfg, err)
	}
}

func TestIncompleteInitDoesNotAskToShareUsageData(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	dir := t.TempDir()
	out, err := runInteractiveInit(t, dir, "", initOptions{}, nil)
	if err != nil || !strings.Contains(out, "No supported Agent") || strings.Contains(out, "share your usage data") {
		t.Fatalf("setup = %s, %v", out, err)
	}
	cfg, err := testfix.OpenStore(t, dir).Settings()
	if err != nil || cfg.Telemetry != nil || cfg.DefaultAgent != "" {
		t.Fatalf("incomplete setup changed settings: %+v, %v", cfg, err)
	}
}

func TestScriptedInitTelemetryPreservesOrExplicitlyChangesConsent(t *testing.T) {
	bin := t.TempDir()
	executable(t, bin, "goose")
	t.Setenv("PATH", bin)
	for _, initial := range []string{"null", "false", "true"} {
		for _, flag := range []string{"", "false", "true"} {
			t.Run(initial+"/flag="+flag, func(t *testing.T) {
				dir := t.TempDir()
				s := testfix.OpenStore(t, dir)
				if initial != "null" {
					if err := s.SetSetting("telemetry", initial); err != nil {
						t.Fatal(err)
					}
				}
				before, err := s.TelemetryState()
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"init", "--agent", "goose"}
				want := initial
				if flag != "" {
					args = append(args, "--telemetry="+flag)
					want = flag
				}
				out, err := runIn(t, dir, t.TempDir(), args...)
				if err != nil || strings.Contains(out, "share your usage data") {
					t.Fatalf("scripted setup = %s, %v", out, err)
				}
				cfg, err := s.Settings()
				if err != nil || settingText(settingValue(cfg, "telemetry")) != want || cfg.DefaultAgent != "goose" {
					t.Fatalf("scripted settings = %+v, %v", cfg, err)
				}
				after, err := s.TelemetryState()
				if err != nil {
					t.Fatal(err)
				}
				if (flag == "" || initial == flag) && !reflect.DeepEqual(before, after) {
					t.Fatal("scripted rerun changed reporting state")
				}
			})
		}
	}
}

func TestInitExistingTelemetryConsentDoesNotPromptOrRestart(t *testing.T) {
	bin := t.TempDir()
	executable(t, bin, "goose")
	t.Setenv("PATH", bin)
	for _, choice := range []string{"true", "false"} {
		t.Run(choice, func(t *testing.T) {
			dir := t.TempDir()
			s := testfix.OpenStore(t, dir)
			if err := s.SetSetting("telemetry", choice); err != nil {
				t.Fatal(err)
			}
			before, err := s.TelemetryState()
			if err != nil {
				t.Fatal(err)
			}
			out, err := runInteractiveInit(t, dir, "\n", initOptions{}, nil)
			if err != nil || strings.Contains(out, "share your usage data") || !strings.Contains(out, "Setup complete.") {
				t.Fatalf("repeated init = %s, %v", out, err)
			}
			after, err := s.TelemetryState()
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("repeated init changed state: %+v, %v", after, err)
			}
		})
	}
}

func TestInitTelemetryScriptErrors(t *testing.T) {
	for _, args := range [][]string{
		{"init", "--telemetry=invalid"},
		{"init", "--telemetry"},
		{"init"},
		{"init", "--agent", "missing"},
		{"init", "--agent", "missing", "--telemetry=true"},
		{"init", "--telemetry=true"},
		{"init", "--telemetry=false"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := t.TempDir()
			if _, err := runIn(t, dir, t.TempDir(), args...); err == nil {
				t.Fatal("expected setup error")
			}
			cfg, err := testfix.OpenStore(t, dir).Settings()
			if err != nil {
				t.Fatal(err)
			}
			want := "null"
			for _, arg := range args {
				if arg == "--telemetry=true" || arg == "--telemetry=false" {
					want = strings.TrimPrefix(arg, "--telemetry=")
				}
			}
			if got := settingText(settingValue(cfg, "telemetry")); got != want {
				t.Fatalf("after failed setup telemetry = %s, want %s", got, want)
			}
		})
	}
}
