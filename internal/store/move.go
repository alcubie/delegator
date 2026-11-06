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

// reorder returns a copy of ids with the tickets of moved shifted together in
// the requested direction. Moving beyond either end leaves the order
// unchanged. moved holds its tickets in the order that ids holds them, and
// holds at least one.
func reorder(ids, moved []int64, move Move) []int64 {
	rest := without(ids, moved)
	to := 0
	switch move {
	case Up:
		to = above(ids, moved, moved[0]) - 1
	case Down:
		to = above(ids, moved, moved[len(moved)-1]) + 1
	case Top:
		to = 0
	case Bottom:
		to = len(rest)
	}
	return slices.Insert(rest, min(max(to, 0), len(rest)), moved...)
}

// reorderBefore moves the tickets of moved together immediately before
// target. A missing target leaves the order unchanged.
//
// Remove the moved tickets before finding the target position, since removal
// shifts later positions.
func reorderBefore(ids, moved []int64, target int64) []int64 {
	rest := without(ids, moved)
	to := slices.Index(rest, target)
	if to < 0 {
		return slices.Clone(ids)
	}
	return slices.Insert(rest, to, moved...)
}

// without returns the tickets of ids that moved leaves in place, in the order
// that ids holds them.
func without(ids, moved []int64) []int64 {
	rest := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(moved, id) {
			rest = append(rest, id)
		}
	}
	return rest
}

// above returns how many of the tickets that moved leaves in place come
// before id.
func above(ids, moved []int64, id int64) int {
	n := 0
	for _, other := range ids[:slices.Index(ids, id)] {
		if !slices.Contains(moved, other) {
			n++
		}
	}
	return n
}
