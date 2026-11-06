package store

import (
	"slices"
	"testing"
)

func TestReorder(t *testing.T) {
	tests := []struct {
		name  string
		moved []int64
		move  Move
		want  []int64
	}{
		{"up from the middle", []int64{30}, Up, []int64{10, 30, 20, 40}},
		{"up from the end", []int64{40}, Up, []int64{10, 20, 40, 30}},
		{"up from the top is no change", []int64{10}, Up, []int64{10, 20, 30, 40}},

		{"down from the middle", []int64{20}, Down, []int64{10, 30, 20, 40}},
		{"down from the top", []int64{10}, Down, []int64{20, 10, 30, 40}},
		{"down from the end is no change", []int64{40}, Down, []int64{10, 20, 30, 40}},

		{"to the top", []int64{30}, Top, []int64{30, 10, 20, 40}},
		{"to the top from the top is no change", []int64{10}, Top, []int64{10, 20, 30, 40}},

		{"to the bottom", []int64{20}, Bottom, []int64{10, 30, 40, 20}},
		{"to the bottom from the end is no change", []int64{40}, Bottom, []int64{10, 20, 30, 40}},

		{"a block up", []int64{20, 30}, Up, []int64{20, 30, 10, 40}},
		{"a block down", []int64{20, 30}, Down, []int64{10, 40, 20, 30}},
		{"a block down past one ticket", []int64{10, 20}, Down, []int64{30, 10, 20, 40}},
		{"a block to the top", []int64{20, 40}, Top, []int64{20, 40, 10, 30}},
		{"a block to the bottom", []int64{10, 30}, Bottom, []int64{20, 40, 10, 30}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ids := []int64{10, 20, 30, 40}
			got := reorder(ids, test.moved, test.move)
			if !slices.Equal(got, test.want) {
				t.Errorf("reorder(%v, %v, %s) = %v, want %v", ids, test.moved, test.move, got, test.want)
			}
		})
	}
}

func TestReorderLeavesTheGivenSliceAlone(t *testing.T) {
	ids := []int64{10, 20, 30, 40}
	reorder(ids, []int64{40}, Top)
	if want := []int64{10, 20, 30, 40}; !slices.Equal(ids, want) {
		t.Errorf("the slice that reorder got is now %v, want %v", ids, want)
	}
}

func TestReorderWithOneTicket(t *testing.T) {
	for _, move := range []Move{Up, Down, Top, Bottom} {
		got := reorder([]int64{10}, []int64{10}, move)
		if want := []int64{10}; !slices.Equal(got, want) {
			t.Errorf("reorder with %s = %v, want %v", move, got, want)
		}
	}
}

func TestReorderBefore(t *testing.T) {
	tests := []struct {
		name   string
		moved  []int64
		target int64
		want   []int64
	}{
		{"down the queue: 10 goes where 40 is", []int64{10}, 40, []int64{20, 30, 10, 40}},
		{"up the queue: 40 goes where 20 is", []int64{40}, 20, []int64{10, 40, 20, 30}},
		{"to the top", []int64{30}, 10, []int64{30, 10, 20, 40}},
		{"one place down", []int64{10}, 30, []int64{20, 10, 30, 40}},
		{"one place up", []int64{30}, 20, []int64{10, 30, 20, 40}},
		{"the ticket below it", []int64{20}, 30, []int64{10, 20, 30, 40}},
		{"a target among the moved tickets changes nothing", []int64{20}, 20, []int64{10, 20, 30, 40}},

		{"a block up the queue", []int64{20, 40}, 10, []int64{20, 40, 10, 30}},
		{"a block down the queue", []int64{10, 20}, 40, []int64{30, 10, 20, 40}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ids := []int64{10, 20, 30, 40}
			got := reorderBefore(ids, test.moved, test.target)
			if !slices.Equal(got, test.want) {
				t.Errorf("reorderBefore(%v, %v, %d) = %v, want %v",
					ids, test.moved, test.target, got, test.want)
			}
		})
	}
}

// The bottom of the queue has no ticket below it, so a move there needs the
// keyword bottom and not an id.
func TestReorderBeforeLeavesTheGivenSliceAlone(t *testing.T) {
	ids := []int64{10, 20, 30, 40}
	reorderBefore(ids, []int64{40}, 10)
	if want := []int64{10, 20, 30, 40}; !slices.Equal(ids, want) {
		t.Errorf("the slice that reorderBefore got is now %v, want %v", ids, want)
	}
}

// A missing target preserves the order; the store reports the invalid
// selection.
func TestReorderBeforeWithATargetThatIsNotThere(t *testing.T) {
	ids := []int64{10, 20, 30}
	got := reorderBefore(ids, []int64{10}, 99)
	if want := []int64{10, 20, 30}; !slices.Equal(got, want) {
		t.Errorf("reorderBefore = %v, want %v", got, want)
	}
}
