package m

import "sort"

func sortCounting(xs []int) int {
	calls := 0
	sort.Slice(xs, func(i, j int) bool {
		less := xs[i] < xs[j]
		count(&calls)
		return less
	})
	return calls
}

func count(calls *int) {
	*calls++
}
