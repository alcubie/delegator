package ticket

import "testing"

const validTicket = `---
schema: 1
title: Fix the login redirect
---

title: Remove staging was the old title.
`

func TestParseReadsTheTitle(t *testing.T) {
	tk, err := Parse([]byte(validTicket))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if tk.Title != "Fix the login redirect" {
		t.Errorf("Title = %q, want %q", tk.Title, "Fix the login redirect")
	}
}

func TestParseReadsTheSchema(t *testing.T) {
	tk, err := Parse([]byte(validTicket))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	expected := 1
	if tk.Schema != expected {
		t.Errorf("Schema = %d, want %d", tk.Schema, expected)
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
