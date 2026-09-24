package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestAgentSelectorUsesNumbersAndDefaultsToTheCurrentChoice(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  int
	}{
		{"first", "1\n", 0},
		{"last", "3\n", 2},
		{"blank keeps current", "\n", 1},
		{"invalid answers retry", "0\nfour\n2\n", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			got, chose, err := readAgentSelection(strings.NewReader(test.input), &out, []string{"one", "two", "three"}, 1)
			if err != nil {
				t.Fatal(err)
			}
			if !chose || got != test.want {
				t.Errorf("selection = %d, %t; want %d, true", got, chose, test.want)
			}
			for _, want := range []string{"1. one", "2. two", "3. three", "Selection [2]"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("selector output does not contain %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestAgentSelectorCanCancel(t *testing.T) {
	for _, input := range []string{"q\n", "Q\n", ""} {
		if _, chose, err := readAgentSelection(strings.NewReader(input), &bytes.Buffer{}, []string{"one"}, 0); err != nil || chose {
			t.Errorf("input %q gave chose=%t, err=%v; want a clean cancellation", input, chose, err)
		}
	}
}
