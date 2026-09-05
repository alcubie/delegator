// Package config reads the config file of the person. The file is
// config.toml below the config directory of delegator, and a person edits it
// with an editor. State that a command changes is not config, and it stays in
// the database.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Config holds the values of the config file.
type Config struct {
	// Runs is the number of tickets that can run at one time.
	Runs int `toml:"runs"`
	// TimeoutMinutes is the time a run can take before delegator stops it.
	TimeoutMinutes int `toml:"timeout_minutes"`
	// DoneHours is how long a ticket the person accepted stays in DONE at the
	// top of the inbox. DoneWindow gives it as a duration.
	DoneHours int `toml:"done_hours"`
}

// DoneWindow is how far back DONE reaches. The name of the key holds the unit,
// and this method is the one place that turns it into a duration, so no caller
// multiplies by an hour of its own.
func (c Config) DoneWindow() time.Duration {
	return time.Duration(c.DoneHours) * time.Hour
}

// Dir returns the directory that holds config.toml. XDG_CONFIG_HOME names
// it, and a person who has not set that variable gets the directory that the
// XDG specification asks for.
func Dir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "delegator"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "delegator"), nil
}

// file returns the path of config.toml below Dir.
func file() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Default is the config of a person who has written no file. A fresh
// install has no config, and delegator must work before a person writes one.
var Default = Config{Runs: 1, TimeoutMinutes: 60, DoneHours: 24}

// defaultFile is the text of the config file that Init writes. Each key has
// one comment that says what it does, because the file is where the person
// changes a value. The values are the fields of Default, so the file that a
// person opens and the config of a person who has no file cannot come apart.
const defaultFile = `# runs is the number of tickets that can run at one time.
runs = %d

# timeout_minutes is the time in minutes that a run can take before delegator
# stops it.
timeout_minutes = %d

# done_hours is the time in hours that a ticket stays in DONE at the top of the
# inbox after dg accept closes it. A value of 0 leaves DONE empty.
done_hours = %d
`

// Init writes config.toml with each key at its default when the file is not
// there, and makes Dir when that is not there. A file that is there is left
// as it is, whatever it holds: the person edits it, and delegator never
// writes it again. O_EXCL makes that one step, so two commands that start at
// the same time cannot each write it.
func Init() error {
	path, err := file()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	text := fmt.Sprintf(defaultFile,
		Default.Runs, Default.TimeoutMinutes, Default.DoneHours)
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Load reads config.toml from Dir. A file that is not there gives Default and
// no error. A value of the wrong type, or a key that delegator does not know,
// gives an error that names the key: a misspelt key that quietly became a
// default would be the hardest fault to find.
func Load() (Config, error) {
	path, err := file()
	if err != nil {
		return Config{}, err
	}
	cfg := Default
	md, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return Default, nil
	}
	if err != nil {
		return Config{}, err
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		return Config{}, fmt.Errorf("%s: unknown key %q", path, unknown[0].String())
	}
	return cfg, nil
}
