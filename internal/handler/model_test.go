package handler

import (
	"io"
	"strings"
	"testing"
)

func TestSessionModelSelection(t *testing.T) {
	for _, loaded := range []bool{false, true} {
		for _, tc := range []struct{ options, model, wantError string }{
			{"model-flat", "model-v2", ""},
			{"model-grouped", "model-v2", ""},
			{"model-no-category", "model-v2", ""},
			{"model-flat", "missing", "available model IDs: model-v1, model-v2"},
			{stubFullOptions, "model-v2", "does not expose an ACP model config option"},
			{"model-rejected", "model-v2", "model rejected"},
			{"model-unconfirmed", "model-v2", "did not confirm"},
			{stubFullOptions, "", ""},
			{stubFullOptions, "null", ""},
			{"model-flat", "default", "model \"default\" is unavailable"},
		} {
			t.Run(tc.options+"/"+tc.model+map[bool]string{true: "/load", false: "/start"}[loaded], func(t *testing.T) {
				name, argv, record := stubLaunch(t, tc.options, stubTurnUpdates, stubLoadsLong)
				options := SessionOptions{Model: tc.model}
				var session *Session
				var err error
				if loaded {
					session, err = Load(t.Context(), name, argv, Deny(), t.TempDir(), stubSessionID, options, io.Discard)
				} else {
					session, err = Start(t.Context(), name, argv, Deny(), t.TempDir(), options, io.Discard)
				}
				if session != nil {
					defer session.Close()
				}
				if tc.wantError != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantError) {
						t.Fatalf("error = %v, want %q", err, tc.wantError)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				requests := readRecord(t, record).ModelRequests
				if tc.model == "model-v2" {
					if len(requests) != 1 || requests[0].ValueId == nil {
						t.Fatalf("model requests = %+v", requests)
					}
					request := requests[0].ValueId
					id := "agent-model"
					if tc.options == "model-no-category" {
						id = "model"
					}
					if string(request.ConfigId) != id || request.Value != "model-v2" || request.SessionId != stubSessionID {
						t.Fatalf("model request = %+v", request)
					}
				} else if len(requests) != 0 {
					t.Fatalf("default sent model requests: %+v", requests)
				}
			})
		}
	}
}
