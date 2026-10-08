package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

func runHistory(t *testing.T, dataDir, repo string) (int64, []store.Run) {
	t.Helper()
	id, err := ticketIn(t, dataDir, repo, "Keep run history", "")
	if err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)
	if err := s.SaveAgent(store.Agent{Name: "history-agent", Argv: []string{"history-agent"}}); err != nil {
		t.Fatal(err)
	}
	agentID, err := s.AgentID("history-agent")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Claim(id, "delegator/history", agentID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSession(first, "session", "history-model"); err != nil {
		t.Fatal(err)
	}
	if err := s.EndRun(first, 7); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, store.Failed); err != nil {
		t.Fatal(err)
	}
	firstRun, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Restart(id, agentID, firstRun.ModelID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EndRun(second, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, store.Failed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restart(id, agentID, firstRun.ModelID); err != nil {
		t.Fatal(err)
	}
	runs, err := s.Runs(id)
	if err != nil {
		t.Fatal(err)
	}
	return id, runs
}

func TestTicketRunsShowsAnExplicitEmptyHistory(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	id, err := ticketIn(t, dataDir, repo, "Never run", "")
	if err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo, "ticket", "runs", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("No runs recorded for ticket #%d.\n", id); out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestTicketRunsRendersEveryRecordedValueNewestFirst(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	id, runs := runHistory(t, dataDir, repo)

	out, err := runIn(t, dataDir, repo, "ticket", "runs", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	newer, middle, older := runs[0], runs[1], runs[2]
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 || strings.Fields(lines[1])[0] != fmt.Sprint(newer.ID) ||
		strings.Fields(lines[2])[0] != fmt.Sprint(middle.ID) ||
		strings.Fields(lines[3])[0] != fmt.Sprint(older.ID) {
		t.Fatalf("runs are not newest first:\n%s", out)
	}
	for _, want := range []string{
		fmt.Sprint(newer.ID), "history-agent", "history-model",
		newer.StartedAt.Format("2006-01-02T15:04:05Z07:00"), "-",
		older.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		older.EndedAt.Format("2006-01-02T15:04:05Z07:00"), "0", "7",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
}

func TestTicketRunsStructuredOutputMatchesTerminalValues(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	id, runs := runHistory(t, dataDir, repo)
	request := fmt.Sprintf(`{"jsonrpc":"2.0","method":"ticket.runs","params":{"args":[%d]},"id":1}`, id)
	out, err := rpcIn(t, dataDir, repo, request)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := rpcObject(t, out)["result"].(map[string]any)
	if !ok {
		t.Fatalf("response = %s, want an object result", out)
	}
	if result["ticket_id"] != float64(id) {
		t.Errorf("ticket_id = %#v, want %d", result["ticket_id"], id)
	}
	gotRuns, ok := result["runs"].([]any)
	if !ok || len(gotRuns) != 3 {
		t.Fatalf("runs = %#v, want three runs", result["runs"])
	}
	active := gotRuns[0].(map[string]any)
	if active["id"] != float64(runs[0].ID) || active["agent"] != "history-agent" ||
		active["model"] != "history-model" || active["started"] != runs[0].StartedAt.Format("2006-01-02T15:04:05Z07:00") ||
		active["ended"] != nil || active["exit_code"] != nil {
		t.Errorf("active run = %#v, want recorded values and null end metadata", active)
	}
}

func TestRunsValuePreservesUnavailableRecordedMetadataAsNull(t *testing.T) {
	value := runsValue(42, []store.Run{{ID: 9}})
	if len(value.Runs) != 1 {
		t.Fatalf("runs = %#v, want one", value.Runs)
	}
	run := value.Runs[0]
	if run.Agent != nil || run.Model != nil || run.Started != nil || run.Ended != nil || run.ExitCode != nil {
		t.Errorf("run = %#v, want every unavailable value to be null", run)
	}
}

func TestTicketRunsRejectsUnknownAndInvalidIdentifiers(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	if _, err := ticketIn(t, dataDir, repo, "Existing", ""); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{"ticket", "runs"}, {"ticket", "runs", "0"}, {"ticket", "runs", "-1"}, {"ticket", "runs", "nope"}, {"ticket", "runs", "1", "2"}} {
		if _, err := runIn(t, dataDir, repo, args...); err == nil {
			t.Errorf("dg %v gave no error", args)
		}
	}
	if _, err := runIn(t, dataDir, repo, "ticket", "runs", "9999"); err == nil {
		t.Error("unknown ticket gave no error")
	}
}

func TestTicketRunsResultSchemaValidatesCompleteResults(t *testing.T) {
	document := rpcDiscover(t)
	validator := openRPCResultValidator(t, document, "ticket.runs")
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	id, _ := runHistory(t, dataDir, repo)
	actual := rpcDocument(t, dataDir, repo, "ticket.runs", fmt.Sprint(id))
	if err := validator.Validate(actual); err != nil {
		t.Fatalf("run history does not satisfy its advertised schema: %v", err)
	}

	encoded, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	var copied map[string]any
	if err := json.Unmarshal(encoded, &copied); err != nil {
		t.Fatal(err)
	}
	first := copied["runs"].([]any)[0].(map[string]any)
	delete(first, "ended")
	if err := validator.Validate(copied); err == nil {
		t.Error("result missing a nullable run field satisfies the schema")
	}

	nulls := runsValue(1, []store.Run{{ID: 1}})
	nullData, err := json.Marshal(nulls)
	if err != nil {
		t.Fatal(err)
	}
	var nullResult map[string]any
	if err := json.Unmarshal(nullData, &nullResult); err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(nullResult); err != nil {
		t.Errorf("null recorded values do not satisfy the schema: %v", err)
	}
	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"nonpositive ticket identifier", func(result map[string]any) { result["ticket_id"] = float64(0) }},
		{"nonpositive run identifier", func(result map[string]any) { result["runs"].([]any)[0].(map[string]any)["id"] = float64(0) }},
		{"wrong agent type", func(result map[string]any) { result["runs"].([]any)[0].(map[string]any)["agent"] = float64(1) }},
		{"malformed start", func(result map[string]any) { result["runs"].([]any)[0].(map[string]any)["started"] = "yesterday" }},
		{"wrong exit code type", func(result map[string]any) { result["runs"].([]any)[0].(map[string]any)["exit_code"] = "zero" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var result map[string]any
			if err := json.Unmarshal(encoded, &result); err != nil {
				t.Fatal(err)
			}
			test.change(result)
			if err := validator.Validate(result); err == nil {
				t.Error("invalid result satisfies the schema")
			}
		})
	}
}
