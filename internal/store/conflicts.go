package store

import "database/sql"

// ReadyMerge is a ready ticket that reported a commit, with the commit of the
// default branch at its last conflict check. An empty CheckedBase means that
// the ticket has not been checked. MergesCleanly is the verdict of that check,
// and is nil for a ticket without one.
type ReadyMerge struct {
	ID            int64
	Commit        string
	CheckedBase   string
	MergesCleanly *bool

	// ConflictRunBase is the commit of the default branch for which a conflict
	// run started. It is empty for a ticket without one.
	ConflictRunBase string
}

type ReadyProject struct {
	Project Project
	Tickets []ReadyMerge
}

// ReadyMerges returns the ready tickets that reported a commit, grouped by
// their project, for the projects that are not archived. A project without
// such a ticket is absent, so a caller starts no Git process for it, and a
// ticket that reported no commit holds nothing to merge.
//
// The groups come in the order of the project path and the tickets of a group
// in the order of their id.
func (s *Store) ReadyMerges() ([]ReadyProject, error) {
	rows, err := s.db.Query(`
		SELECT projects.id, projects.path, projects.default_branch,
		       COALESCE(projects.name, ''), projects.archived_at,
		       tickets.id, tickets.commit_id,
		       COALESCE(tickets.checked_base_commit, ''),
		       tickets.checked_merges_cleanly,
		       COALESCE(tickets.conflict_run_base, '')
		FROM tickets
		JOIN projects ON projects.id = tickets.project_id
		WHERE tickets.status = ? AND projects.archived_at IS NULL
		      AND COALESCE(tickets.commit_id, '') <> ''
		ORDER BY projects.path, tickets.id`, Ready)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ready []ReadyProject
	for rows.Next() {
		var p Project
		var m ReadyMerge
		var verdict sql.NullBool
		if err := rows.Scan(
			&p.ID, &p.Path, &p.DefaultBranch, &p.Name, timeColumn{&p.ArchivedAt},
			&m.ID, &m.Commit, &m.CheckedBase, &verdict, &m.ConflictRunBase); err != nil {
			return nil, err
		}
		if verdict.Valid {
			m.MergesCleanly = &verdict.Bool
		}
		if len(ready) == 0 || ready[len(ready)-1].Project.ID != p.ID {
			ready = append(ready, ReadyProject{Project: p})
		}
		last := &ready[len(ready)-1]
		last.Tickets = append(last.Tickets, m)
	}
	return ready, rows.Err()
}

// SetConflictCheck records the commit of the default branch that the ticket was
// checked against and whether the ticket merged cleanly into it, so the next
// check of that ticket waits for the branch to move.
func (s *Store) SetConflictCheck(id int64, base string, mergesCleanly bool) error {
	_, err := s.db.Exec(
		"UPDATE tickets SET checked_base_commit = ?, checked_merges_cleanly = ? WHERE id = ?",
		base, mergesCleanly, id)
	return err
}

// SetConflictRunBase records the commit of the default branch for which a
// conflict run started, so the next command doesn't start a second run for it.
func (s *Store) SetConflictRunBase(id int64, base string) error {
	_, err := s.db.Exec("UPDATE tickets SET conflict_run_base = ? WHERE id = ?", base, id)
	return err
}
