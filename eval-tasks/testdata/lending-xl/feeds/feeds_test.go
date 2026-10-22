package feeds

import (
	"reflect"
	"testing"

	"example.com/lending/lending"
)

func TestFeeds(t *testing.T) {
	c := &lending.Catalog{}
	for _, f := range []func(*lending.Catalog) error{Novels, Films, Maps, History} {
		if err := f(c); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.Items) != 7 {
		t.Errorf("loaded %d items", len(c.Items))
	}
	if err := Novels(c); err == nil {
		t.Errorf("loading novels twice did not fail")
	}
	if got, want := Preview("a|Emma|book|1\nbad\n# x\nb|Heat|dvd|2"), []string{"Emma", "Heat"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Preview = %q", got)
	}
}
