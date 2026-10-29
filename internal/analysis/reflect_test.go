package analysis

import (
	"reflect"
	"testing"
)

func TestFormatVerbs(t *testing.T) {
	v, plus, sharp := verb{r: 'v'}, verb{r: 'v', plus: true}, verb{r: 'v', sharp: true}
	for _, c := range []struct {
		format string
		want   map[int][]verb
	}{
		{"%+v", map[int][]verb{0: {plus}}},
		{"%v %+v", map[int][]verb{0: {v}, 1: {plus}}},
		{"%+[2]v %v", map[int][]verb{1: {plus}, 2: {v}}},
		{"%*d %#v", map[int][]verb{1: {{r: 'd'}}, 2: {sharp}}},
		{"%-10.*f %+v", map[int][]verb{1: {{r: 'f'}}, 2: {plus}}},
		{"%% %+v", map[int][]verb{0: {plus}}},
		{"%[3]*.[2]*[1]f %+v", map[int][]verb{0: {{r: 'f'}}, 1: {plus}}},
		{"%+v %#v %#[1]v", map[int][]verb{0: {plus, sharp}, 1: {sharp}}},
		{"%+d %#x %T", map[int][]verb{0: {{r: 'd', plus: true}}, 1: {{r: 'x', sharp: true}}, 2: {{r: 'T'}}}},
		{"größe %+v", map[int][]verb{0: {plus}}},
	} {
		got, ok := formatVerbs(c.format)
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("formatVerbs(%q) = %v, %v; want %v", c.format, got, ok, c.want)
		}
	}
	for _, bad := range []string{"%[x]v", "%[0]v", "%+"} {
		if _, ok := formatVerbs(bad); ok {
			t.Errorf("formatVerbs(%q) is ok; want not", bad)
		}
	}
}
