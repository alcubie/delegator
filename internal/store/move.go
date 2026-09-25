package store

import "slices"

// Move is a direction for reordering a ticket within its list.
type Move string

const (
	Up     Move = "up"
	Down   Move = "down"
	Top    Move = "top"
	Bottom Move = "bottom"
)

// reorder returns a copy of ids with the ticket at from moved in the
// requested direction. Moving beyond either end leaves the order unchanged.
func reorder(ids []int64, from int, move Move) []int64 {
	to := from
	switch move {
	case Up:
		to = from - 1
	case Down:
		to = from + 1
	case Top:
		to = 0
	case Bottom:
		to = len(ids) - 1
	}
	if to < 0 {
		to = 0
	}
	if to > len(ids)-1 {
		to = len(ids) - 1
	}

	return moveTo(ids, from, to)
}

// reorderBefore moves the ticket at from immediately before target. A missing
// target or the ticket itself leaves the order unchanged.
//
// Remove the ticket before finding the target position, since removal shifts
// later positions.
func reorderBefore(ids []int64, from int, target int64) []int64 {
	moved := slices.Clone(ids)
	id := moved[from]
	moved = slices.Delete(moved, from, from+1)

	to := slices.Index(moved, target)
	if to < 0 {
		return slices.Clone(ids)
	}
	return slices.Insert(moved, to, id)
}

// moveTo returns a copy of ids with the ticket at from moved to to.
func moveTo(ids []int64, from, to int) []int64 {
	moved := slices.Clone(ids)
	id := moved[from]
	moved = slices.Delete(moved, from, from+1)
	return slices.Insert(moved, to, id)
}
