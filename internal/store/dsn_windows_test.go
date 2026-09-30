package store

import (
	"net/url"
	"path/filepath"
	"testing"
)

func TestDSNWindowsDrivePath(t *testing.T) {
	u, err := url.Parse(dsn(`C:\Users\Example User\AppData\Local\delegator`))
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "file" || u.Host != "" || u.Path != "/C:/Users/Example User/AppData/Local/delegator/delegator.db" {
		t.Fatalf("invalid SQLite file URL: %s", u)
	}
	// Exercise the driver as well as URL parsing, using legal Windows path
	// characters that still need URI escaping.
	s, err := Open(filepath.Join(t.TempDir(), "Example User #1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
