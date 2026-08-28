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

// reorder returns the sequence that ids becomes when the ticket at from goes in
// the direction of move. A move that goes past the end of the sequence is no
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

	moved := slices.Clone(ids)
	id := moved[from]
	moved = slices.Delete(moved, from, from+1)
	return slices.Insert(moved, to, id)
}
