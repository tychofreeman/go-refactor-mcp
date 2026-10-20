package m

import (
	"slices"
	"testing"
)

func TestKeys(t *testing.T) {
	if got := Keys(map[string]int{"a": 1}); !slices.Equal(got, []string{"a"}) {
		t.Fatal(got)
	}
}
