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
