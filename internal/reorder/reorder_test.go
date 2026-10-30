package reorder

import "testing"

func TestRepeat(t *testing.T) {
	for _, c := range []struct {
		name    string
		t       Times
		f       Facts
		between bool
		want    Kind
	}{
		{"pure, several", Several, Facts{}, false, None},
		{"a write, maybe", Maybe, Facts{Writes: true, Throws: OtherThrow}, false, Writing},
		{"a throw, maybe", Maybe, Facts{Throws: OnNull}, false, Throwing},
		{"a throw, shared", Shared, Facts{Throws: OtherThrow}, false, None},
		{"a throw, several", Several, Facts{Throws: OtherThrow}, false, Throwing},
		{"a read, several", Several, Facts{Reads: true}, false, Reading},
		{"a read, several, judged between", Several, Facts{Reads: true}, true, None},
		{"a new value, shared", Shared, Facts{Makes: true}, false, Making},
		{"a new value, maybe", Maybe, Facts{Makes: true}, false, None},
		{"leaving, maybe", Maybe, Facts{Leaves: true}, false, Leaving},
		{"once", Once, Facts{Writes: true}, false, None},
	} {
		if got := Repeat(c.t, c.f, c.between); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestConflict(t *testing.T) {
	w, r := Facts{Writes: true}, Facts{Reads: true}
	null, other := Facts{Throws: OnNull}, Facts{Throws: OtherThrow}
	leave, stop := Facts{Leaves: true}, Facts{Stops: true}
	for _, c := range []struct {
		name   string
		e, p   Facts
		ek, pk Kind
	}{
		{"write past write", w, w, Writing, Writing},
		{"write past read", w, r, Writing, Reading},
		{"read past write", r, w, Reading, Writing},
		{"read past read", r, r, None, None},
		{"write past a throw on null", w, null, None, None},
		{"write past another throw", w, other, Writing, Throwing},
		{"a throw on null past a write", null, w, None, None},
		{"another throw past a write", other, w, Throwing, Writing},
		{"two throws on null", null, null, None, None},
		{"a throw on null past another", null, other, Throwing, Throwing},
		{"write past leaving", w, leave, Writing, Leaving},
		{"leaving past a write", leave, w, Leaving, Writing},
		{"a throw past leaving", null, leave, Throwing, Leaving},
		{"write past code that may not finish", w, stop, Writing, Stopping},
		{"a throw past code that may not finish", null, stop, Throwing, Stopping},
	} {
		if ek, pk := Conflict(c.e, c.p); ek != c.ek || pk != c.pk {
			t.Errorf("%s: got %v, %v, want %v, %v", c.name, ek, pk, c.ek, c.pk)
		}
	}
}
