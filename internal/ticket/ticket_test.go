package ticket

import "testing"

func TestParseReadsTheTitle(t *testing.T) {
	data := []byte("---\ntitle: Fix the login redirect\n---\n")

	tk, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if tk.Title != "Fix the login redirect" {
		t.Errorf("Title = %q, want %q", tk.Title, "Fix the login redirect")
	}
}
