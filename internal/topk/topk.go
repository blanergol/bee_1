// Package topk выбирает N самых частых слов из карты подсчётов.
package topk

import (
	"container/heap"
	"sort"
)

// Entry описывает одно слово и его частоту в выборке топ-N.
type Entry struct {
	Word  string
	Count int
}

// Select возвращает не более n самых частых записей из counts, отсортированных
// по убыванию Count. При равной частоте записи упорядочены лексикографически
// по возрастанию Word. Если n <= 0 или counts пуст, возвращается nil.
func Select(counts map[string]int, n int) []Entry {
	if n <= 0 || len(counts) == 0 {
		return nil
	}

	if len(counts) <= n {
		out := make([]Entry, 0, len(counts))
		for w, c := range counts {
			out = append(out, Entry{Word: w, Count: c})
		}
		sortDescending(out)
		return out
	}

	h := &minHeap{}
	heap.Init(h)

	for w, c := range counts {
		if h.Len() < n {
			heap.Push(h, Entry{Word: w, Count: c})
			continue
		}
		root := (*h)[0]
		if betterThan(Entry{Word: w, Count: c}, root) {
			(*h)[0] = Entry{Word: w, Count: c}
			heap.Fix(h, 0)
		}
	}

	out := make([]Entry, h.Len())
	copy(out, *h)
	sortDescending(out)
	return out
}

// betterThan сообщает, должен ли cand вытеснить root из выборки. Кандидат
// лучше, если у него выше Count; при равенстве — если его Word
// лексикографически меньше (т.к. при равной частоте мы предпочитаем меньшие
// слова в финальной выдаче).
func betterThan(cand, root Entry) bool {
	if cand.Count != root.Count {
		return cand.Count > root.Count
	}
	return cand.Word < root.Word
}

func sortDescending(out []Entry) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Word < out[j].Word
	})
}

// minHeap — мин-куча по Count, с тай-брейком: в корне держим элемент, который
// мы готовы вытеснить первым. То есть наименее предпочтительный элемент
// (меньшая Count или, при равной частоте, лексикографически бо́льшее слово).
type minHeap []Entry

func (h minHeap) Len() int      { return len(h) }
func (h minHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h minHeap) Less(i, j int) bool {
	if h[i].Count != h[j].Count {
		return h[i].Count < h[j].Count
	}
	return h[i].Word > h[j].Word
}

func (h *minHeap) Push(x any) { *h = append(*h, x.(Entry)) }

func (h *minHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
