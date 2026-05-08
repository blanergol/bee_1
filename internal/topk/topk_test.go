package topk_test

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/redmadrobot/topwords/internal/topk"
)

func TestSelect_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		counts map[string]int
		n      int
		want   []topk.Entry
	}{
		{
			name:   "empty map",
			counts: map[string]int{},
			n:      10,
			want:   nil,
		},
		{
			name:   "n is zero",
			counts: map[string]int{"a": 1},
			n:      0,
			want:   nil,
		},
		{
			name:   "n is negative",
			counts: map[string]int{"a": 1},
			n:      -1,
			want:   nil,
		},
		{
			name:   "fewer unique than n",
			counts: map[string]int{"a": 5, "b": 2, "c": 1},
			n:      10,
			want: []topk.Entry{
				{Word: "a", Count: 5},
				{Word: "b", Count: 2},
				{Word: "c", Count: 1},
			},
		},
		{
			name:   "exact n",
			counts: map[string]int{"a": 5, "b": 2, "c": 1},
			n:      3,
			want: []topk.Entry{
				{Word: "a", Count: 5},
				{Word: "b", Count: 2},
				{Word: "c", Count: 1},
			},
		},
		{
			name:   "more unique than n",
			counts: map[string]int{"a": 5, "b": 4, "c": 3, "d": 2, "e": 1},
			n:      3,
			want: []topk.Entry{
				{Word: "a", Count: 5},
				{Word: "b", Count: 4},
				{Word: "c", Count: 3},
			},
		},
		{
			name:   "equal counts deterministic order",
			counts: map[string]int{"banana": 2, "apple": 2, "cherry": 2},
			n:      2,
			want: []topk.Entry{
				{Word: "apple", Count: 2},
				{Word: "banana", Count: 2},
			},
		},
		{
			name:   "all equal less than n",
			counts: map[string]int{"x": 1, "y": 1, "z": 1},
			n:      10,
			want: []topk.Entry{
				{Word: "x", Count: 1},
				{Word: "y", Count: 1},
				{Word: "z", Count: 1},
			},
		},
		{
			name: "tie at boundary chooses lex-smaller",
			counts: map[string]int{
				"alpha": 5, "beta": 4, "gamma": 4, "delta": 4,
			},
			n: 2,
			want: []topk.Entry{
				{Word: "alpha", Count: 5},
				{Word: "beta", Count: 4},
			},
		},
		{
			name:   "single entry n=1",
			counts: map[string]int{"only": 7},
			n:      1,
			want:   []topk.Entry{{Word: "only", Count: 7}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := topk.Select(tc.counts, tc.n)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("entries diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSelect_Stability_AcrossRuns(t *testing.T) {
	t.Parallel()

	counts := map[string]int{
		"foo": 10, "bar": 10, "baz": 10, "qux": 5, "quux": 5, "corge": 1,
	}
	first := topk.Select(counts, 4)
	for i := 0; i < 50; i++ {
		got := topk.Select(counts, 4)
		require.Equal(t, first, got, "non-deterministic at iteration %d", i)
	}
}

func BenchmarkSelect_100k(b *testing.B) {
	counts := make(map[string]int, 100_000)
	for i := 0; i < 100_000; i++ {
		counts[fmt.Sprintf("word_%06d", i)] = i % 1000
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		out := topk.Select(counts, 10)
		if len(out) != 10 {
			b.Fatalf("got %d entries, want 10", len(out))
		}
	}
}
