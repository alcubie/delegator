package store

// ReadyMerge is a ready ticket that reported a commit, with the commit of the
// default branch at its last conflict check. An empty CheckedBase means that
// the ticket has not been checked.
type ReadyMerge struct {
	ID          int64
	Commit      string
	CheckedBase string
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
		       COALESCE(tickets.checked_base_commit, '')
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
		if err := rows.Scan(
			&p.ID, &p.Path, &p.DefaultBranch, &p.Name, timeColumn{&p.ArchivedAt},
			&m.ID, &m.Commit, &m.CheckedBase); err != nil {
			return nil, err
		}
		if len(ready) == 0 || ready[len(ready)-1].Project.ID != p.ID {
			ready = append(ready, ReadyProject{Project: p})
		}
		last := &ready[len(ready)-1]
		last.Tickets = append(last.Tickets, m)
	}
	return ready, rows.Err()
}

// SetCheckedBase records the commit of the default branch that the ticket was
// checked against, so the next check of that ticket waits for the branch to
// move.
func (s *Store) SetCheckedBase(id int64, commit string) error {
	_, err := s.db.Exec("UPDATE tickets SET checked_base_commit = ? WHERE id = ?", commit, id)
	return err
}
