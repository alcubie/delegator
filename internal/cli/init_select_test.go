package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestAgentSelectorUsesArrowKeysAndWraps(t *testing.T) {
	for _, test := range []struct {
		name string
		keys string
		want int
	}{
		{"down", "\x1b[B\r", 1},
		{"up wraps", "\x1b[A\r", 2},
		{"down wraps", "\x1b[B\x1b[B\x1b[B\r", 0},
		{"j and k", "jjk\r", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			got, chose, err := readAgentSelection(strings.NewReader(test.keys), &out, []string{"one", "two", "three"}, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !chose || got != test.want {
				t.Errorf("selection = %d, %t; want %d, true", got, chose, test.want)
			}
			if !strings.Contains(out.String(), "↑/↓ move") {
				t.Errorf("selector did not explain its keys:\n%s", out.String())
			}
		})
	}
}

func TestAgentSelectorCanCancel(t *testing.T) {
	for _, key := range []string{"q", "Q", "\x03"} {
		if _, chose, err := readAgentSelection(strings.NewReader(key), &bytes.Buffer{}, []string{"one"}, 0); err != nil || chose {
			t.Errorf("key %q gave chose=%t, err=%v; want a clean cancellation", key, chose, err)
		}
	}
}
