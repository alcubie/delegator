package cli

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

func TestConfigTelemetryPreservesNullableValuesThroughCLIAndRPC(t *testing.T) {
	dir, work := t.TempDir(), t.TempDir()
	for _, choice := range []string{"null", "false", "true", "true", "false", "true"} {
		if choice != "null" {
			out, err := rpcIn(t, dir, work, fmt.Sprintf(`{"jsonrpc":"2.0","method":"config.set","params":{"args":["telemetry",%s]},"id":1}`, choice))
			if err != nil {
				t.Fatal(err)
			}
			response := rpcObject(t, out)
			if _, ok := response["result"]; !ok || response["error"] != nil {
				t.Fatalf("config.set failed: %s", out)
			}
		}
		out, err := runIn(t, dir, work, "config", "get", "telemetry")
		if err != nil || out != choice+"\n" {
			t.Fatalf("CLI get = %q, %v, want %s", out, err, choice)
		}
		out, err = runIn(t, dir, work, "config", "list")
		if err != nil || !strings.Contains(out, "telemetry = "+choice+"  #") {
			t.Fatalf("CLI list = %q, %v", out, err)
		}
		var want any
		if choice != "null" {
			want = choice == "true"
		}
		out, err = rpcIn(t, dir, work, `{"jsonrpc":"2.0","method":"config.get","params":{"args":["telemetry"]},"id":1}`)
		if err != nil {
			t.Fatal(err)
		}
		response := rpcObject(t, out)
		if result, ok := response["result"]; !ok || result != want {
			t.Fatalf("RPC get = %s, want %v", out, want)
		}
		out, err = rpcIn(t, dir, work, `{"jsonrpc":"2.0","method":"config.list","id":1}`)
		if err != nil {
			t.Fatal(err)
		}
		response = rpcObject(t, out)
		settings, ok := response["result"].([]any)
		if !ok {
			t.Fatalf("RPC list = %s", out)
		}
		found := false
		for _, item := range settings {
			setting := item.(map[string]any)
			if setting["name"] == "telemetry" {
				found = true
				if value, ok := setting["value"]; !ok || value != want {
					t.Fatalf("RPC list telemetry = %v, want %v", setting, want)
				}
			}
		}
		if !found {
			t.Fatal("RPC list omitted telemetry")
		}
		for _, hidden := range []string{"instance_id", "consent_start", "last_attempt", "reported_through", "first_consent_date", "installation_acknowledged"} {
			if strings.Contains(out, hidden) {
				t.Errorf("exposed bookkeeping %s", hidden)
			}
		}
	}
}

func TestConfigTelemetryUsesSharedTransitionsAndRejectsNull(t *testing.T) {
	dir, work := t.TempDir(), t.TempDir()
	s := testfix.OpenStore(t, dir)
	for _, choice := range []string{"true", "true", "false", "true"} {
		before, err := s.TelemetryState()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runIn(t, dir, work, "config", "set", "telemetry", choice); err != nil {
			t.Fatal(err)
		}
		after, err := s.TelemetryState()
		if err != nil || after.Consent == nil || *after.Consent != (choice == "true") {
			t.Fatalf("config choice not persisted: %+v, %v", after, err)
		}
		if before.Consent != nil && *before.Consent && choice == "true" && !reflect.DeepEqual(before, after) {
			t.Fatal("repeated config true reset state")
		}
	}
	before, err := s.TelemetryState()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runIn(t, dir, work, "config", "set", "telemetry", "null"); err == nil {
		t.Fatal("config accepted null submission")
	}
	after, err := s.TelemetryState()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("invalid config submission changed state")
	}
}
