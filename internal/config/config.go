// Package config describes the instance settings stored by store.
package config

import (
	"time"
)

// Config is a snapshot of the instance settings.
type Config struct {
	// Runs limits the total number of running and ready tickets.
	Runs int
	// TimeoutMinutes is the time a run can take before delegator stops it.
	TimeoutMinutes int
	// DoneHours is how long accepted tickets remain in the inbox.
	// DoneWindow converts it to a duration.
	DoneHours int
	// MaxRunsPerProject limits running and ready tickets per project. The
	// same limit applies to every project. Zero uses the global Runs
	// limit; see ProjectRuns.
	MaxRunsPerProject int
	// DefaultAgent is the registry name used for new runs. It is stored in
	// SQLite as a reference to the agent registry.
	DefaultAgent string
	// DefaultModel selects an ACP model for runs. Empty uses the agent default.
	DefaultModel string
	// Telemetry is nil until answered. Only an explicit true permits reporting.
	Telemetry *bool
}

// Definition describes a setting and its help text. Definitions belong to the
// application version, not the stored instance settings.
type Definition struct {
	Name        string
	Description string
}

// Definitions is the ordered list of settings and their help text.
var Definitions = []Definition{
	{"runs", "the number of tickets that can be Running or Ready at a time."},
	{"timeout_minutes", "the time in minutes that a run can take before delegator stops it."},
	{"done_hours", "the time in hours that a ticket stays in DONE at the top of the inbox after dg accept closes it. A value of 0 leaves DONE empty."},
	{"max_runs_per_project", "the number of tickets of one project that can be Running or Ready at a time. A value of 0 is ignored and runs is used as the limit."},
	{"default_agent", "the registered agent used for new runs."},
	{"default_model", "the ACP model ID used for runs. null uses the agent's default model."},
	{"telemetry", "share usage statistics; null means disabled because unanswered. Set true or false to choose."},
}

// ProjectRuns returns the per-project limit, falling back to Runs when
// MaxRunsPerProject is zero. This limit can prevent concurrent runs from
// competing for shared resources such as databases or ports.
func (c Config) ProjectRuns() int {
	if c.MaxRunsPerProject > 0 {
		return c.MaxRunsPerProject
	}
	return c.Runs
}

// DoneWindow returns how long accepted tickets remain in the inbox.
func (c Config) DoneWindow() time.Duration {
	return time.Duration(c.DoneHours) * time.Hour
}

// Timeout returns the run time limit. Zero means no limit.
func (c Config) Timeout() time.Duration {
	return time.Duration(c.TimeoutMinutes) * time.Minute
}
