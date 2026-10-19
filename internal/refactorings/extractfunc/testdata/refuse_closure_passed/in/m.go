package m

var saved []func(string)

func keep(f func(string)) { saved = append(saved, f) }

func collect(keys []string) []string {
	var out []string
	var add func(k string)
	add = func(k string) { out = append(out, k) }
	keep(add)
	for _, k := range keys {
		add(k)
	}

	return out
}
