package ticket

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const validYAML = `schema: 1
id: 4
title: Remove staging infrastructure
state: ready
project: /home/person/projects/web-api
branch: delegator/4-remove-staging-infrastructure
worktree: /home/person/.local/share/delegator/projects/web-api-4f2a91/worktrees/0004
session: e55e382e-2c88-4de7-a31d-ab8763a0fb5a
result: Staging infra removed. Gate green, 433 tests.
flags: terraform apply is blocked, the token in .env is invalid.
created: 2026-08-17T09:30:00Z
`

// write puts one file in the directory of a ticket.
func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestLoadReadsTheFields(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ticket.yaml", validYAML)

	tk, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}

	if tk.Schema != 1 {
		t.Errorf("Schema = %d, want 1", tk.Schema)
	}
	if tk.ID != 4 {
		t.Errorf("ID = %d, want 4", tk.ID)
	}
	for _, c := range []struct{ name, got, want string }{
		{"Title", tk.Title, "Remove staging infrastructure"},
		{"State", tk.State, "ready"},
		{"Project", tk.Project, "/home/person/projects/web-api"},
		{"Branch", tk.Branch, "delegator/4-remove-staging-infrastructure"},
		{"Session", tk.Session, "e55e382e-2c88-4de7-a31d-ab8763a0fb5a"},
		{"Result", tk.Result, "Staging infra removed. Gate green, 433 tests."},
		{"Flags", tk.Flags, "terraform apply is blocked, the token in .env is invalid."},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if want := time.Date(2026, 8, 17, 9, 30, 0, 0, time.UTC); !tk.Created.Equal(want) {
		t.Errorf("Created = %v, want %v", tk.Created, want)
	}

	if tk.Body != "" {
		t.Errorf("Body = %q, want <empty string>", tk.Body)
	}
}

func TestLoadReadsTheBody(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ticket.yaml", validYAML)

	const body = "Line one\n\nLine two\n"
	write(t, dir, "ticket.md", body)

	tk, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}

	if tk.Body != body {
		t.Errorf("Body = %q, want %q", tk.Body, body)
	}
}

func TestLoadNoFieldsFile(t *testing.T) {
	dir := t.TempDir()

	tk, err := Load(dir)
	if err == nil {
		t.Fatal("Load gave no error, but ticket.yaml is not present")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error = %v, want an error that matches fs.ErrNotExist", err)
	}
	if tk != nil {
		t.Errorf("Ticket = %+v, want nil", tk)
	}
}

// The schema below is a literal, and not CurrentSchema+1. When the schema of
// delegator becomes 2, this test must fail, because that is the moment to give
// schema 1 a read path.
func TestLoadUnknownSchema(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ticket.yaml", "schema: 2\nid: 4\ntitle: From a later version\n")

	tk, err := Load(dir)
	if err == nil {
		t.Fatal("Load gave no error for a schema that this version does not know")
	}
	if !errors.Is(err, ErrUnknownSchema) {
		t.Errorf("error = %v, want an error that matches ErrUnknownSchema", err)
	}
	if tk != nil {
		t.Errorf("Ticket = %+v, want nil", tk)
	}
}

func TestLoadNoSchema(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ticket.yaml", "id: 4\ntitle: A ticket with no schema\n")

	tk, err := Load(dir)
	if err == nil {
		t.Fatal("Load gave no error for a ticket with no schema")
	}
	if !errors.Is(err, ErrUnknownSchema) {
		t.Errorf("error = %v, want an error that matches ErrUnknownSchema", err)
	}
	if tk != nil {
		t.Errorf("Ticket = %+v, want nil", tk)
	}
}

func TestLoadNoResultOrFlags(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ticket.yaml",
		"schema: 1\nid: 4\ntitle: A ticket in the queue\nstate: queued\n")

	tk, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	if tk.Result != "" {
		t.Errorf("Result = %q, want an empty string", tk.Result)
	}
	if tk.Flags != "" {
		t.Errorf("Flags = %q, want an empty string", tk.Flags)
	}
}

func TestSaveFieldsAndLoadGiveTheSameTicket(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ticket.md", "Line one\n\nLine two\n")

	want := &Ticket{
		Schema:   1,
		ID:       4,
		Title:    "Remove staging infrastructure",
		State:    "ready",
		Project:  "/home/person/projects/web-api",
		Branch:   "delegator/4-remove-staging-infrastructure",
		Worktree: "/home/person/.local/share/delegator/projects/web-api-4f2a91/worktrees/0004",
		Session:  "e55e382e-2c88-4de7-a31d-ab8763a0fb5a",
		Result:   "Staging infra removed. Gate green, 433 tests.",
		Flags: "terraform apply is blocked, the token in .env is invalid. " +
			"Do not destroy the app first, because DNS points at it.",
		Created: time.Date(2026, 8, 17, 9, 30, 0, 0, time.UTC),
		Body:    "Line one\n\nLine two\n",
	}

	if err := want.SaveFields(dir); err != nil {
		t.Fatalf("SaveFields returned an error: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}

	// Two times can be equal and not identical, so Created is compared on its
	// own and then removed from the comparison of the two structs.
	if !got.Created.Equal(want.Created) {
		t.Errorf("Created = %v, want %v", got.Created, want.Created)
	}
	got.Created, want.Created = time.Time{}, time.Time{}
	if *got != *want {
		t.Errorf("Load after SaveFields =\n%+v\nwant\n%+v", got, want)
	}
}

func TestSaveFieldsDoesNotWriteTheProse(t *testing.T) {
	dir := t.TempDir()
	const prose = "The prose of the person.\n"
	write(t, dir, "ticket.md", prose)

	tk := &Ticket{Schema: 1, ID: 4, Title: "A ticket", State: "queued",
		Body: "Text that SaveFields must not write.\n"}
	if err := tk.SaveFields(dir); err != nil {
		t.Fatalf("SaveFields returned an error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "ticket.md"))
	if err != nil {
		t.Fatalf("read ticket.md: %v", err)
	}
	if string(got) != prose {
		t.Errorf("ticket.md = %q, want %q", got, prose)
	}
}
