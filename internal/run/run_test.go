package run

import (
	"fmt"
	"testing"
)

func TestBranchSuffix(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{"UPPERCASE", "uppercase"},
		{"MixedCase", "mixedcase"},
		{"12numbered", "12numbered"},
		{"Very Very Long Title With Many Words and Characters in this Sentence", "very-very-long-title-with-many-words-and"},
		{"Supercalifragilisticexpialidocious antidisestablishmentarianism xyz", "supercalifragilisticexpialidocious"},
		{"antidisestablishmentarianismfloccinaucinihilipilification", "antidisestablishmentarianismfloccinaucin"},
		{"Handle café orders for Straße", "handle-cafe-orders-for-strasse"},
		{"Здравствуйте мир", "zdravstvuite-mir"},
	}

	for _, test := range tests {
		got := branch(1, test.title)
		want := fmt.Sprintf("delegator/1-%s", test.want)
		if got != want {
			t.Errorf("got = %s, want = %s", got, want)
		}
	}
}

func TestBranchNoAsciiTitle(t *testing.T) {
	got := branch(1, "🎉🎉🎉")
	want := "delegator/1"
	if got != want {
		t.Errorf("got = %s, want = %s", got, want)
	}
}
