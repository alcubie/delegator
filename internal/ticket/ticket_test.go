package ticket

import "testing"

// The body starts with "title: " on purpose. Parse must stop at the "---", so a
// line of the body cannot overwrite a field of the header.
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
		t.Errorf("Schema = %d, want 1", tk.Schema)
	}
	if tk.ID != 2 {
		t.Errorf("ID = %d, want 2", tk.ID)
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
	want := "title: Remove staging was the old title.\n"
	if tk.Body != want {
		t.Errorf("Body = %q, want %q", tk.Body, want)
	}
}

// A long value can continue on the next line, with an indent and no key of its
// own. Section 7 of the technical document has this example.
const foldedTicket = `---
id: 4
flags: terraform apply is blocked, the token in .env is invalid. Do not destroy
  the app first, because DNS points at it.
created: 2026-08-17T09:30:00Z
---
`

func TestParseReadsAFoldedValue(t *testing.T) {
	tk, err := Parse([]byte(foldedTicket))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	want := "terraform apply is blocked, the token in .env is invalid. " +
		"Do not destroy the app first, because DNS points at it."
	if tk.Flags != want {
		t.Errorf("Flags = %q, want %q", tk.Flags, want)
	}
}
