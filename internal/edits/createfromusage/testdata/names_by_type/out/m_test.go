package m

import "testing"

func TestStore(t *testing.T) {
	var ids []int
	if Count([]int{1, 2}, func(i int) bool { return i > 0 }) != 2 {
		t.Fatal("wrong")
	}
	Store(map[string]int{}, &Config{}, []Config{}, make(chan int), func() {}, error(nil), len(ids), ids[0], 3)
}
