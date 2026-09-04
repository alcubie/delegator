// Package config reads the config file of the person. The file is
// config.toml below the config directory of delegator, and a person edits it
// with an editor. State that a command changes is not config, and it stays in
// the database.
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds the values of the config file.
type Config struct {
	// Runs is the number of tickets that can run at one time.
	Runs int `toml:"runs"`
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

// Load reads config.toml from Dir.
func Load() (Config, error) {
	dir, err := Dir()
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if _, err := toml.DecodeFile(filepath.Join(dir, "config.toml"), &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
