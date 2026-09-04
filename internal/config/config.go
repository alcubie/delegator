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

	"github.com/BurntSushi/toml"
)

// Config holds the values of the config file.
type Config struct {
	// Runs is the number of tickets that can run at one time.
	Runs int `toml:"runs"`
	// TimeoutMinutes is the time a run can take before delegator stops it.
	TimeoutMinutes int `toml:"timeout_minutes"`
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
var Default = Config{Runs: 1, TimeoutMinutes: 60}

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
