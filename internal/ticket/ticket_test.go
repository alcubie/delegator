package ticket

import "testing"

const validTicket = `---
schema: 1
id: 2
title: Fix the login redirect
---

title: Remove staging was the old title.
`

func TestParseReadsTheHeader(t *testing.T) {
	tk, err := Parse([]byte(validTicket))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if tk.Schema != 1 {
		t.Errorf("Schema = %d, want %d", tk.Schema, 1)
	}
	if tk.ID != 2 {
		t.Errorf("Id = %d, want %d", tk.ID, 2)
	}
	if tk.Title != "Fix the login redirect" {
		t.Errorf("Title = %q, want %q", tk.Title, "Fix the login redirect")
	}
}

func TestParseReadsTheBody(t *testing.T) {
	tk, err := Parse([]byte(validTicket))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if tk.Title != "Fix the login redirect" {
		t.Errorf("Title = %q, want %q", tk.Title, "Fix the login redirect")
	}
	if tk.Body != "title: Remove staging was the old title.\n" {
		t.Errorf("Body = %q, want %q", tk.Body, "title: Remove staging was the old title.\n")
	}
}
