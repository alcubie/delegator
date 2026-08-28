package store

import "slices"

// Move is a direction that one ticket goes in the queue.
type Move string

const (
	Up     Move = "up"
	Down   Move = "down"
	Top    Move = "top"
	Bottom Move = "bottom"
)

// reorder returns the order that ids becomes when the ticket at from goes in
// the direction of move. A move that goes past the end of the order is no
// move, so the ticket at the top stays there when it goes up. The slice that
// reorder gets does not change.
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

// reorderBefore returns the order that ids becomes when the ticket at from goes
// to the place that target holds. target and each ticket below it go down one
// place. A target that the queue does not hold, and a target that is the ticket
// itself, give the order back as it was.
//
// The ticket comes out of the order before the place of target is read. The
// place of a target below the ticket moves up by one when the ticket comes out,
// and a read that came first would put the ticket one place short.
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

// moveTo returns the order that ids becomes when the ticket at from goes to to.
// The slice that moveTo gets does not change.
func moveTo(ids []int64, from, to int) []int64 {
	moved := slices.Clone(ids)
	id := moved[from]
	moved = slices.Delete(moved, from, from+1)
	return slices.Insert(moved, to, id)
}
