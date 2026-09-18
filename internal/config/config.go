// Package config describes the instance settings stored by store.
package config

import (
	"time"
)

// Config is one snapshot of the instance settings.
type Config struct {
	// Runs is the number of tickets that can be open at one time: a ticket
	// that runs, and a ticket in ready that waits for the person, each hold
	// one of the places it gives.
	Runs int
	// TimeoutMinutes is the time a run can take before delegator stops it.
	TimeoutMinutes int
	// DoneHours is how long a ticket the person accepted stays in DONE at the
	// top of the inbox. DoneWindow gives it as a duration.
	DoneHours int
	// MaxRunsPerProject is the number of tickets of one project that can be
	// open at one time. It holds for every project, so a person sets it once
	// and names no project. A value of 0 is no limit of its own, and each
	// project then takes Runs. ProjectRuns gives the limit that holds.
	MaxRunsPerProject int
	// DefaultAgent is the registry name used for new runs. It is stored in
	// SQLite as a reference to the agent registry.
	DefaultAgent string
}

// Definition describes one setting shown by configuration commands. Settings
// are fixed by this version of delegator, so their explanatory text belongs
// with the code that understands them rather than in mutable instance data.
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
