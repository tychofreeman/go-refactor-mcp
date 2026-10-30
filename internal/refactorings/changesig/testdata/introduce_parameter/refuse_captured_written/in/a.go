package m

type typer interface{ Type() any }

type target struct{ calls []int }

func (t *target) find(n int) string {
	t.calls = append(t.calls, n)
	return ""
}

func forFn(t *target, fn typer, n int) string {
	if fn.Type() == nil {
		return "no type"
	}
	count := func() int { return len(t.calls) }
	if n > 1 {
		t = &target{}
	}
	find := t.find
	return find(n) + string(rune(count()))
}

func use(t *target, fn typer) string {
	return forFn(t, fn, 1)
}
