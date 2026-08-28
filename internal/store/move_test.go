package store

import (
	"slices"
	"testing"
)

func TestReorder(t *testing.T) {
	tests := []struct {
		name string
		from int
		move Move
		want []int64
	}{
		{"up from the middle", 2, Up, []int64{10, 30, 20, 40}},
		{"up from the end", 3, Up, []int64{10, 20, 40, 30}},
		{"up from the top is no change", 0, Up, []int64{10, 20, 30, 40}},

		{"down from the middle", 1, Down, []int64{10, 30, 20, 40}},
		{"down from the top", 0, Down, []int64{20, 10, 30, 40}},
		{"down from the end is no change", 3, Down, []int64{10, 20, 30, 40}},

		{"to the top", 2, Top, []int64{30, 10, 20, 40}},
		{"to the top from the top is no change", 0, Top, []int64{10, 20, 30, 40}},

		{"to the bottom", 1, Bottom, []int64{10, 30, 40, 20}},
		{"to the bottom from the end is no change", 3, Bottom, []int64{10, 20, 30, 40}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ids := []int64{10, 20, 30, 40}
			got := reorder(ids, test.from, test.move)
			if !slices.Equal(got, test.want) {
				t.Errorf("reorder(%v, %d, %s) = %v, want %v", ids, test.from, test.move, got, test.want)
			}
		})
	}
}

func TestReorderLeavesTheGivenSliceAlone(t *testing.T) {
	ids := []int64{10, 20, 30, 40}
	reorder(ids, 3, Top)
	if want := []int64{10, 20, 30, 40}; !slices.Equal(ids, want) {
		t.Errorf("the slice that reorder got is now %v, want %v", ids, want)
	}
}

func TestReorderWithOneTicket(t *testing.T) {
	for _, move := range []Move{Up, Down, Top, Bottom} {
		got := reorder([]int64{10}, 0, move)
		if want := []int64{10}; !slices.Equal(got, want) {
			t.Errorf("reorder with %s = %v, want %v", move, got, want)
		}
	}
}

func TestReorderBefore(t *testing.T) {
	tests := []struct {
		name   string
		from   int
		target int64
		want   []int64
	}{
		{"down the queue: 10 goes where 40 is", 0, 40, []int64{20, 30, 10, 40}},
		{"up the queue: 40 goes where 20 is", 3, 20, []int64{10, 40, 20, 30}},
		{"to the top", 2, 10, []int64{30, 10, 20, 40}},
		{"one place down", 0, 30, []int64{20, 10, 30, 40}},
		{"one place up", 2, 20, []int64{10, 30, 20, 40}},
		{"the ticket below it", 1, 30, []int64{10, 20, 30, 40}},
		{"itself changes nothing", 1, 20, []int64{10, 20, 30, 40}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ids := []int64{10, 20, 30, 40}
			got := reorderBefore(ids, test.from, test.target)
			if !slices.Equal(got, test.want) {
				t.Errorf("reorderBefore(%v, %d, %d) = %v, want %v",
					ids, test.from, test.target, got, test.want)
			}
		})
	}
}

// The bottom of the queue has no ticket below it, so a move there needs the
// keyword bottom and not an id.
func TestReorderBeforeLeavesTheGivenSliceAlone(t *testing.T) {
	ids := []int64{10, 20, 30, 40}
	reorderBefore(ids, 3, 10)
	if want := []int64{10, 20, 30, 40}; !slices.Equal(ids, want) {
		t.Errorf("the slice that reorderBefore got is now %v, want %v", ids, want)
	}
}

// A target that the queue does not hold gives back the order that it had, and
// the store tells the person.
func TestReorderBeforeWithATargetThatIsNotThere(t *testing.T) {
	ids := []int64{10, 20, 30}
	got := reorderBefore(ids, 0, 99)
	if want := []int64{10, 20, 30}; !slices.Equal(got, want) {
		t.Errorf("reorderBefore = %v, want %v", got, want)
	}
}
