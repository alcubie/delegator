package store

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestRunsReturnsNoRunsForAnExistingTicket(t *testing.T) {
	s, id := oneTicket(t)

	runs, err := s.Runs(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Errorf("runs = %+v, want none", runs)
	}
}

func TestRunsRejectsATicketThatDoesNotExist(t *testing.T) {
	s, _ := oneTicket(t)

	_, err := s.Runs(9999)
	if !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
}

func TestRunsReturnsTheCompleteHistoryNewestFirst(t *testing.T) {
	s, id := oneTicket(t)
	if err := s.SaveAgent(Agent{Name: "history-agent", Argv: []string{"history-agent"}}); err != nil {
		t.Fatal(err)
	}
	agentID, err := s.AgentID("history-agent")
	if err != nil {
		t.Fatal(err)
	}

	firstID, err := s.Claim(id, "delegator/1-my-ticket", agentID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSession(firstID, "session", "history-model"); err != nil {
		t.Fatal(err)
	}
	first, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EndRun(firstID, 7); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Failed); err != nil {
		t.Fatal(err)
	}

	secondID, err := s.Restart(id, agentID, first.ModelID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EndRun(secondID, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Failed); err != nil {
		t.Fatal(err)
	}

	thirdID, err := s.Restart(id, agentID, first.ModelID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		"UPDATE runs SET agent_id = NULL, model_id = NULL WHERE id = ?", thirdID,
	); err != nil {
		t.Fatal(err)
	}

	wantStarted := map[int64]string{
		firstID:  "2026-01-01T01:00:00Z",
		secondID: "2026-01-01T02:00:00Z",
		thirdID:  "2026-01-01T03:00:00Z",
	}
	for runID, started := range wantStarted {
		if _, err := s.db.Exec("UPDATE runs SET started_at = ? WHERE id = ?", started, runID); err != nil {
			t.Fatal(err)
		}
	}

	runs, err := s.Runs(id)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := runIDs(runs), []int64{thirdID, secondID, firstID}; !slices.Equal(got, want) {
		t.Fatalf("run IDs = %v, want %v", got, want)
	}

	ongoing := runs[0]
	if ongoing.TicketID != id || ongoing.AgentID != 0 || ongoing.Agent != "" ||
		ongoing.ModelID.Valid || ongoing.Model != "" || !ongoing.EndedAt.IsZero() || ongoing.ExitCode.Valid {
		t.Errorf("ongoing run = %+v, want missing agent/model and no end or exit", ongoing)
	}
	if got := ongoing.StartedAt.Format(time.RFC3339); got != wantStarted[thirdID] {
		t.Errorf("ongoing start = %q, want %q", got, wantStarted[thirdID])
	}

	finished := runs[1]
	if finished.AgentID != agentID || finished.Agent != "history-agent" ||
		finished.ModelID != first.ModelID || finished.Model != "history-model" {
		t.Errorf("finished run identity = %+v, want recorded agent and model", finished)
	}
	if finished.EndedAt.IsZero() || !finished.ExitCode.Valid || finished.ExitCode.V != 0 {
		t.Errorf("finished run = %+v, want an end and exit code 0", finished)
	}

	failed := runs[2]
	if failed.EndedAt.IsZero() || !failed.ExitCode.Valid || failed.ExitCode.V != 7 {
		t.Errorf("failed run = %+v, want an end and exit code 7", failed)
	}
}

func TestRunsKeepsTicketHistoriesIsolated(t *testing.T) {
	s, ids := threeTickets(t)
	firstRun, err := s.Claim(ids[0], "delegator/1-first", testAgentID)
	if err != nil {
		t.Fatal(err)
	}
	secondRun, err := s.Claim(ids[1], "delegator/2-second", testAgentID)
	if err != nil {
		t.Fatal(err)
	}

	first, err := s.Runs(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Runs(ids[1])
	if err != nil {
		t.Fatal(err)
	}
	third, err := s.Runs(ids[2])
	if err != nil {
		t.Fatal(err)
	}
	if got := runIDs(first); !slices.Equal(got, []int64{firstRun}) {
		t.Errorf("first ticket runs = %v, want [%d]", got, firstRun)
	}
	if got := runIDs(second); !slices.Equal(got, []int64{secondRun}) {
		t.Errorf("second ticket runs = %v, want [%d]", got, secondRun)
	}
	if len(third) != 0 {
		t.Errorf("third ticket runs = %+v, want none", third)
	}
}

func runIDs(runs []Run) []int64 {
	ids := make([]int64, len(runs))
	for i, run := range runs {
		ids[i] = run.ID
	}
	return ids
}
