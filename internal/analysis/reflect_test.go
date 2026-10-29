package analysis

import (
	"reflect"
	"testing"
)

func TestNamedVerbs(t *testing.T) {
	for _, c := range []struct {
		format string
		want   map[int]verbFlags
	}{
		{"%+v", map[int]verbFlags{0: {plus: true}}},
		{"%v %+v", map[int]verbFlags{1: {plus: true}}},
		{"%+[2]v %v", map[int]verbFlags{1: {plus: true}}},
		{"%*d %#v", map[int]verbFlags{2: {sharp: true}}},
		{"%-10.*f %+v", map[int]verbFlags{2: {plus: true}}},
		{"%% %+v", map[int]verbFlags{0: {plus: true}}},
		{"%[3]*.[2]*[1]f %+v", map[int]verbFlags{1: {plus: true}}},
		{"%+v %#v %#[1]v", map[int]verbFlags{0: {plus: true, sharp: true}, 1: {sharp: true}}},
		{"%+d %#x %+q", map[int]verbFlags{}},
		{"größe %+v", map[int]verbFlags{0: {plus: true}}},
	} {
		got, ok := namedVerbs(c.format)
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("namedVerbs(%q) = %v, %v; want %v", c.format, got, ok, c.want)
		}
	}
	for _, bad := range []string{"%[x]v", "%[0]v", "%+"} {
		if _, ok := namedVerbs(bad); ok {
			t.Errorf("namedVerbs(%q) is ok; want not", bad)
		}
	}
}
