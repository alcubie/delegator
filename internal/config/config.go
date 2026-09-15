// Package config reads the config file of the person. The file is
// config.toml below the config directory of delegator, and a person edits it
// with an editor. State that a command changes is not config, and it stays in
// the database.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/BurntSushi/toml"
)

// Config holds the values of the config file.
type Config struct {
	// Runs is the number of tickets that can be open at one time: a ticket
	// that runs, and a ticket in ready that waits for the person, each hold
	// one of the places it gives.
	Runs int `toml:"runs"`
	// TimeoutMinutes is the time a run can take before delegator stops it.
	TimeoutMinutes int `toml:"timeout_minutes"`
	// DoneHours is how long a ticket the person accepted stays in DONE at the
	// top of the inbox. DoneWindow gives it as a duration.
	DoneHours int `toml:"done_hours"`
	// MaxRunsPerProject is the number of tickets of one project that can be
	// open at one time. It holds for every project, so a person sets it once
	// and names no project. A value of 0 is no limit of its own, and each
	// project then takes Runs. ProjectRuns gives the limit that holds.
	MaxRunsPerProject int `toml:"max_runs_per_project"`
	// Runner is how a run drives its agent: RunnerCLI, the command line of
	// the agent through internal/adapters, or RunnerACP, the Agent Client
	// Protocol through internal/handler.
	Runner string `toml:"runner"`
	// Agent is the agent that the ACP runner starts, by the name delegator
	// knows it under: one of the agents delegator holds, or one that a section
	// of Agents adds. A name that no agent has is an error at the run that
	// would have started it, and the error names the agents there are.
	Agent string `toml:"agent"`
	// Agents holds one section for each agent the person says something
	// about, as [agents.claude]. A name delegator already knows takes the
	// keys the section gives and keeps the rest of what delegator holds for
	// it; a name delegator does not know is an agent of its own.
	Agents map[string]Agent `toml:"agents"`
}

// The runners a run can drive its agent with. A person selects one with the
// key runner, and the two paths are the two packages that start an agent.
const (
	RunnerCLI = "cli"
	RunnerACP = "acp"
)

// An Agent is the section [agents.<name>] of the config file: the command
// that starts the agent's ACP server on stdio, and the command that opens one
// of its sessions in a terminal, in which {session} stands for the id of the
// session. It is for a person whose agent is not on the path under the name
// delegator expects, or whose agent delegator does not know at all.
type Agent struct {
	Argv   []string `toml:"argv"`
	Resume []string `toml:"resume"`
}

// ProjectRuns is how many tickets of one project can be open at one time. Two
// runs of one project can use the same resource outside the worktree, such as
// a database or a port, and the limit of the whole queue says nothing about
// that.
//
// A value of 0 for the key is no limit for each project, and this method is
// the one place that turns it into Runs, the limit of the whole queue, so no
// caller writes a fallback of its own.
func (c Config) ProjectRuns() int {
	if c.MaxRunsPerProject > 0 {
		return c.MaxRunsPerProject
	}
	return c.Runs
}

// DoneWindow is how far back DONE reaches. The name of the key holds the unit,
// and this method is the one place that turns it into a duration, so no caller
// multiplies by an hour of its own.
func (c Config) DoneWindow() time.Duration {
	return time.Duration(c.DoneHours) * time.Hour
}

// Timeout is how long a run can take before delegator stops it. The name of
// the key holds the unit, and this method is the one place that turns it into
// a duration, so no caller multiplies by a minute of its own. A value of 0 is
// no limit.
func (c Config) Timeout() time.Duration {
	return time.Duration(c.TimeoutMinutes) * time.Minute
}

// Dir returns the directory that holds config.toml, for the system the build
// is for.
func Dir() (string, error) {
	return dir(runtime.GOOS)
}

// dir is Dir with the system as a parameter, so that the tests can ask for a
// system that the build is not for. XDG_CONFIG_HOME names the directory on
// every system. A person who has not set that variable gets what their system
// asks for: APPDATA on Windows, which has no XDG rule, and the directory of
// the XDG specification elsewhere.
func dir(goos string) (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "delegator"), nil
	}
	if goos == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", errors.New("%APPDATA% is not set")
		}
		return filepath.Join(appData, "delegator"), nil
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

// defaultFile is the text of the config file that Init writes. Each key has
// one comment that says what it does, because the file is where the person
// changes a value.
//
//go:embed default.toml
var defaultFile []byte

// Default is the config of a person who has written no file. A fresh
// install has no config, and delegator must work before a person writes one.
// It is the embedded file decoded, so the file that a person opens and the
// config of a person who has no file cannot come apart.
var Default = decodeDefault()

// decodeDefault decodes the embedded file. Text that does not decode is a
// fault in the build and not in anything a person did, so it panics.
func decodeDefault() Config {
	var cfg Config
	if _, err := toml.Decode(string(defaultFile), &cfg); err != nil {
		panic("config: default.toml does not decode: " + err.Error())
	}
	return cfg
}

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
	if _, err := f.Write(defaultFile); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Load reads config.toml from Dir. A file that is not there gives Default and
// no error. A value of the wrong type, or a key that delegator does not know,
// gives an error that names the key: a misspelt key that quietly became a
// default would be the hardest fault to find. A runner that is neither of the
// two is refused for the same reason, because a key that reads is not yet a
// value that delegator can act on.
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
	if cfg.Runner != RunnerCLI && cfg.Runner != RunnerACP {
		return Config{}, fmt.Errorf("%s: runner is %q, and it is %q or %q",
			path, cfg.Runner, RunnerCLI, RunnerACP)
	}
	return cfg, nil
}
